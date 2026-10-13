//go:build unit

package service

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"errors"
	"hash/crc32"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/model"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

const (
	retryBedrockStart = `{"type":"message_start","message":{"id":"m1","role":"assistant","content":[],"usage":{"input_tokens":11,"output_tokens":0}}}`
	retryBedrockThink = `{"type":"content_block_delta","index":0,"delta":{"type":"thinking_delta","thinking":"private reasoning"}}`
	retryBedrockText  = `{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"visible"}}`
	retryBedrockUsage = `{"type":"message_delta","delta":{"stop_reason":"end_turn"},"amazon-bedrock-invocationMetrics":{"inputTokenCount":11,"outputTokenCount":7}}`
	retryBedrockStop  = `{"type":"message_stop"}`
)

func retryBedrockRawFrame(payload []byte) []byte {
	headers := []byte("\x0b:event-type\x07\x00\x05chunk\x0d:message-type\x07\x00\x05event")
	frame := make([]byte, 16+len(headers)+len(payload))
	binary.BigEndian.PutUint32(frame[0:4], uint32(len(frame)))
	binary.BigEndian.PutUint32(frame[4:8], uint32(len(headers)))
	binary.BigEndian.PutUint32(frame[8:12], crc32.ChecksumIEEE(frame[:8]))
	copy(frame[12:], headers)
	copy(frame[12+len(headers):], payload)
	binary.BigEndian.PutUint32(frame[len(frame)-4:], crc32.ChecksumIEEE(frame[:len(frame)-4]))
	return frame
}

func retryBedrockFrames(t *testing.T, events ...string) []byte {
	t.Helper()
	var wire []byte
	for _, event := range events {
		payload, err := json.Marshal(map[string]string{"bytes": base64.StdEncoding.EncodeToString([]byte(event))})
		require.NoError(t, err)
		wire = append(wire, retryBedrockRawFrame(payload)...)
	}
	return wire
}

func retryBedrockContext(t *testing.T, rule *model.RetryFilterRule) (*gin.Context, *httptest.ResponseRecorder, *Account, *RetryFilterRuntime) {
	t.Helper()
	runtime := retryFilterAttemptRuntimeForTest(rule)
	c, rec, account := retryFilterAttemptContextForTest(runtime)
	account.Type, account.Platform = AccountTypeBedrock, PlatformAnthropic
	return c, rec, account, runtime
}

func TestRetryFilterBedrockStreamEmptyAndNativeBoundaries(t *testing.T) {
	for _, mode := range []string{"completed", "thinking", "eof", "meta-eof", "truncated-frame", "switch", "terminate", "observe", "off", "no-runtime", "nonpool", "unmatched", "completed-only-eof", "interrupted-only-completed", "text", "tool"} {
		t.Run(mode, func(t *testing.T) {
			rule := &model.RetryFilterRule{EmptyResponseMode: model.RetryFilterEmptyResponseBoth, Action: model.RetryFilterActionSameAccountRetry}
			if mode == "switch" {
				rule.Action = model.RetryFilterActionSwitchAccount
			}
			if mode == "terminate" {
				rule.Action = model.RetryFilterActionTerminate
			}
			if mode == "observe" {
				rule.Action = model.RetryFilterActionObserve
			}
			if mode == "unmatched" {
				rule.EmptyResponseMode = model.RetryFilterEmptyResponseNone
				rule.StatusCodes = []int{422}
			}
			if mode == "completed-only-eof" {
				rule.EmptyResponseMode = model.RetryFilterEmptyResponseCompleted
			}
			if mode == "interrupted-only-completed" {
				rule.EmptyResponseMode = model.RetryFilterEmptyResponseInterrupted
			}
			c, rec, account, runtime := retryBedrockContext(t, rule)
			if mode == "off" {
				runtime.enabled = func(context.Context) bool { return false }
			}
			if mode == "no-runtime" {
				c.Request = c.Request.WithContext(context.Background())
			}
			if mode == "nonpool" {
				account.Credentials["pool_mode"] = false
			}
			events := []string{retryBedrockStart, retryBedrockThink, retryBedrockStop}
			eventType := RetryFilterEventEmptyCompletedResponse
			switch mode {
			case "eof":
				events = nil
				eventType = RetryFilterEventEmptyInterruptedStream
			case "meta-eof", "truncated-frame", "completed-only-eof":
				events = []string{retryBedrockStart}
				eventType = RetryFilterEventEmptyInterruptedStream
			case "text":
				events = []string{retryBedrockStart, retryBedrockText, retryBedrockUsage, retryBedrockStop}
			case "tool":
				events = []string{retryBedrockStart, `{"type":"content_block_start","index":0,"content_block":{"type":"tool_use","id":"call_1","name":"lookup","input":{}}}`, retryBedrockUsage, retryBedrockStop}
			}
			wire := retryBedrockFrames(t, events...)
			if mode == "truncated-frame" {
				wire = append(wire, 0, 0)
			}
			resp := &http.Response{StatusCode: 200, Header: http.Header{}, Body: io.NopCloser(bytes.NewReader(wire))}
			result, err := (&GatewayService{cfg: rawChatCompletionsTestConfig()}).handleBedrockStreamingResponse(c.Request.Context(), resp, c, account, time.Now(), "gpt-5.4")
			intercept := mode == "completed" || mode == "thinking" || mode == "eof" || mode == "meta-eof" || mode == "truncated-frame" || mode == "switch" || mode == "terminate"
			var candidate *UpstreamFailoverError
			if intercept {
				require.ErrorAs(t, err, &candidate)
				require.Equal(t, eventType, candidate.RetryFilterEventType)
				require.Empty(t, rec.Body.String())
				require.False(t, c.Writer.Written())
			} else {
				require.NoError(t, err)
				require.False(t, errors.As(err, &candidate))
				require.NotNil(t, result)
				if mode == "text" || mode == "tool" {
					require.Equal(t, 11, result.usage.InputTokens)
					require.Equal(t, 7, result.usage.OutputTokens)
				}
				if mode == "observe" || mode == "off" || mode == "nonpool" || mode == "no-runtime" || mode == "unmatched" {
					require.Contains(t, rec.Body.String(), "private reasoning")
				}
			}
			observed := retryFilterAttemptEvents(t, c)
			if intercept || mode == "observe" {
				require.Len(t, observed, 1)
				require.Equal(t, intercept, observed[0].RetryFilterBehaviorChanged)
			} else {
				require.Empty(t, observed)
			}
		})
	}
}

func TestRetryFilterBedrockCodecCancellationAndResourceFailures(t *testing.T) {
	for _, mode := range []string{"crc", "bad-base64", "bad-json", "bad-terminal-json", "bad-metrics-json", "bad-outer-json", "error-event", "cancel", "disconnect", "stage-limit", "valid-output-truncated"} {
		t.Run(mode, func(t *testing.T) {
			c, rec, account, _ := retryBedrockContext(t, &model.RetryFilterRule{EmptyResponseMode: model.RetryFilterEmptyResponseBoth, Action: model.RetryFilterActionSameAccountRetry})
			wire := retryBedrockFrames(t, retryBedrockStart, retryBedrockText, retryBedrockUsage, retryBedrockStop)
			var reader io.ReadCloser
			var stageTempDir string
			switch mode {
			case "crc":
				wire = retryBedrockFrames(t, retryBedrockStart)
				wire[len(wire)-1] ^= 0xff
			case "bad-base64":
				wire = retryBedrockRawFrame([]byte(`{"bytes":"!"}`))
			case "bad-json":
				wire = retryBedrockFrames(t, "not-json")
			case "bad-terminal-json":
				wire = retryBedrockFrames(t, `{"type":"message_stop"`)
			case "bad-metrics-json":
				wire = retryBedrockFrames(t, `{"type":"message_stop","amazon-bedrock-invocationMetrics":{"bad":}}`)
			case "bad-outer-json":
				wire = retryBedrockRawFrame([]byte(`{"bytes":"` + base64.StdEncoding.EncodeToString([]byte(retryBedrockStop)) + `"`))
			case "error-event":
				wire = retryBedrockFrames(t, `{"type":"error","error":{"message":"upstream failure"}}`)
			case "cancel":
				reader = &errTailReader{data: retryBedrockFrames(t, retryBedrockStart), err: context.Canceled}
			case "disconnect":
				c.Writer = &openAIRawStreamDisconnectedWriter{ResponseWriter: c.Writer}
			case "stage-limit":
				stageTempDir = t.TempDir()
				t.Setenv("TMPDIR", stageTempDir)
				chunk := `{"type":"content_block_delta","delta":{"type":"thinking_delta","thinking":"` + strings.Repeat("x", 512*1024) + `"}}`
				wire = nil
				for i := 0; i < 17; i++ {
					wire = append(wire, retryBedrockFrames(t, chunk)...)
				}
			case "valid-output-truncated":
				wire = append(retryBedrockFrames(t, retryBedrockStart, retryBedrockText), 0, 0)
			}
			if reader == nil {
				reader = io.NopCloser(bytes.NewReader(wire))
			}
			resp := &http.Response{StatusCode: 200, Header: http.Header{}, Body: reader}
			result, err := (&GatewayService{cfg: rawChatCompletionsTestConfig()}).handleBedrockStreamingResponse(c.Request.Context(), resp, c, account, time.Now(), "gpt-5.4")
			var candidate *UpstreamFailoverError
			require.False(t, errors.As(err, &candidate))
			require.Empty(t, retryFilterAttemptEvents(t, c))
			switch mode {
			case "crc":
				require.ErrorContains(t, err, "CRC mismatch")
			case "stage-limit":
				require.ErrorIs(t, err, errOpenAIFirstOutputStageLimit)
				files, readErr := os.ReadDir(stageTempDir)
				require.NoError(t, readErr)
				require.Empty(t, files)
			case "valid-output-truncated":
				require.ErrorIs(t, err, io.ErrUnexpectedEOF)
				require.Contains(t, rec.Body.String(), "visible")
			case "bad-metrics-json":
				require.NoError(t, err)
				require.Contains(t, rec.Body.String(), `data: {"type":"message_stop"}`)
			case "cancel", "disconnect":
				require.NoError(t, err)
				require.True(t, result.clientDisconnect)
				if mode == "disconnect" {
					require.Equal(t, 11, result.usage.InputTokens)
					require.Equal(t, 7, result.usage.OutputTokens)
				}
			default:
				require.NoError(t, err)
			}
		})
	}
}

func TestRetryFilterBedrockMalformedMetricsRetainsNativeJSONResponse(t *testing.T) {
	for _, action := range []string{model.RetryFilterActionSameAccountRetry, model.RetryFilterActionObserve, model.RetryFilterActionTerminate} {
		t.Run(action, func(t *testing.T) {
			c, rec, account, _ := retryBedrockContext(t, &model.RetryFilterRule{EmptyResponseMode: model.RetryFilterEmptyResponseBoth, Action: action})
			body := `{"type":"message","role":"assistant","content":[],"stop_reason":"end_turn","amazon-bedrock-invocationMetrics":{"bad":}}`
			require.False(t, json.Valid([]byte(body)))
			resp := &http.Response{StatusCode: 200, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(body))}
			_, err := (&GatewayService{cfg: rawChatCompletionsTestConfig()}).handleBedrockNonStreamingResponse(c.Request.Context(), resp, c, account)
			require.NoError(t, err)
			require.JSONEq(t, `{"type":"message","role":"assistant","content":[],"stop_reason":"end_turn"}`, rec.Body.String())
			require.Empty(t, retryFilterAttemptEvents(t, c))
		})
	}
}

func TestRetryFilterBedrockTerminalBeforeTransportEnd(t *testing.T) {
	for _, ending := range []string{"truncated", "timeout"} {
		for _, emptyMode := range []string{model.RetryFilterEmptyResponseCompleted, model.RetryFilterEmptyResponseInterrupted} {
			t.Run(ending+"/"+emptyMode, func(t *testing.T) {
				c, rec, account, _ := retryBedrockContext(t, &model.RetryFilterRule{EmptyResponseMode: emptyMode, Action: model.RetryFilterActionSameAccountRetry})
				cfg := rawChatCompletionsTestConfig()
				cfg.Gateway.StreamKeepaliveInterval = 30
				wire := retryBedrockFrames(t, retryBedrockStart, retryBedrockStop)
				var reader io.ReadCloser
				if ending == "truncated" {
					reader = io.NopCloser(bytes.NewReader(append(wire, 0, 0)))
				} else {
					pipeReader, pipeWriter := io.Pipe()
					defer func() { _ = pipeReader.Close(); _ = pipeWriter.Close() }()
					go func() { _, _ = pipeWriter.Write(wire) }()
					reader = pipeReader
					cfg.Gateway.StreamDataIntervalTimeout = 1
				}
				resp := &http.Response{StatusCode: 200, Header: http.Header{}, Body: reader}
				_, err := (&GatewayService{cfg: cfg}).handleBedrockStreamingResponse(c.Request.Context(), resp, c, account, time.Now(), "gpt-5.4")
				var candidate *UpstreamFailoverError
				if emptyMode == model.RetryFilterEmptyResponseCompleted {
					require.ErrorAs(t, err, &candidate)
					require.Equal(t, RetryFilterEventEmptyCompletedResponse, candidate.RetryFilterEventType)
					require.Empty(t, rec.Body.String())
				} else {
					require.Error(t, err, "unmatched rule must preserve the native transport error")
					require.False(t, errors.As(err, &candidate))
					require.Contains(t, rec.Body.String(), "message_stop")
					require.Empty(t, retryFilterAttemptEvents(t, c))
				}
			})
		}
	}
}

type retryFilterStageFailWriter struct{ short bool }

func (w retryFilterStageFailWriter) Write(p []byte) (int, error) {
	if w.short {
		return len(p) - 1, nil
	}
	return 0, io.ErrClosedPipe
}

func TestRetryFilterStageCommitDistinguishesWriteAndSpoolFailures(t *testing.T) {
	for _, mode := range []string{"memory-success", "spool-success", "memory-write", "spool-write", "short-write", "spool-seek", "spool-read"} {
		t.Run(mode, func(t *testing.T) {
			stage := newDefaultOpenAIFirstOutputStage()
			defer func() { _ = stage.Close() }()
			text := "staged output"
			if strings.HasPrefix(mode, "spool-") {
				text = strings.Repeat("x", openAIFirstOutputStageMemoryLimit+32)
				// Configure a real file without relying on platform-specific unlink behavior.
				file, err := os.CreateTemp(t.TempDir(), "retry-filter-spool-*")
				require.NoError(t, err)
				stage.tempFile, stage.tempPath = file, file.Name()
				n, err := file.WriteString(text)
				require.NoError(t, err)
				require.Equal(t, len(text), n)
				stage.size = int64(n)
			} else {
				_, err := stage.WriteString(text)
				require.NoError(t, err)
			}
			if mode == "spool-seek" {
				require.NoError(t, stage.tempFile.Close())
			}
			if mode == "spool-read" {
				require.NoError(t, stage.tempFile.Truncate(0))
			}
			var output bytes.Buffer
			var dst io.Writer = &output
			writeFailure := mode == "memory-write" || mode == "spool-write" || mode == "short-write"
			if writeFailure {
				dst = retryFilterStageFailWriter{short: mode == "short-write"}
			}
			disconnected, err := commitRetryFilterStage(stage, dst)
			require.Equal(t, writeFailure, disconnected)
			if strings.HasSuffix(mode, "success") {
				require.NoError(t, err)
				require.Equal(t, text, output.String())
			} else {
				require.Error(t, err)
			}
			if mode == "spool-seek" || mode == "spool-read" {
				require.Empty(t, output.String(), "a resource read failure is not a client write failure")
			}
		})
	}
}

type retryBedrockHeartbeatRecorder struct {
	*httptest.ResponseRecorder
	beat chan struct{}
	once sync.Once
}

func (r *retryBedrockHeartbeatRecorder) Write(p []byte) (int, error) {
	n, err := r.ResponseRecorder.Write(p)
	if bytes.Contains(p, []byte(": keepalive")) {
		r.once.Do(func() { close(r.beat) })
	}
	return n, err
}

func TestRetryFilterBedrockActualHeartbeatPreventsReplay(t *testing.T) {
	for _, action := range []string{model.RetryFilterActionSameAccountRetry, model.RetryFilterActionTerminate} {
		t.Run(action, func(t *testing.T) {
			message := "bedrock stopped"
			rule := &model.RetryFilterRule{EmptyResponseMode: model.RetryFilterEmptyResponseBoth, Action: action}
			if action == model.RetryFilterActionTerminate {
				rule.TerminateMessage = &message
			}
			base, _, account, _ := retryBedrockContext(t, rule)
			rec := &retryBedrockHeartbeatRecorder{ResponseRecorder: httptest.NewRecorder(), beat: make(chan struct{})}
			c, _ := gin.CreateTestContext(rec)
			c.Request = base.Request
			ctx, cancel := context.WithTimeout(c.Request.Context(), 4*time.Second)
			defer cancel()
			c.Request = c.Request.WithContext(ctx)
			reader, writer := io.Pipe()
			defer func() { _ = reader.Close() }()
			wire := retryBedrockFrames(t, retryBedrockStart, retryBedrockThink, retryBedrockStop)
			written := make(chan error, 1)
			go func() {
				defer func() { _ = writer.Close() }()
				select {
				case <-rec.beat:
				case <-ctx.Done():
					written <- ctx.Err()
					return
				}
				_, err := writer.Write(wire)
				written <- err
			}()
			cfg := rawChatCompletionsTestConfig()
			cfg.Gateway.StreamKeepaliveInterval = 1
			resp := &http.Response{StatusCode: 200, Header: http.Header{}, Body: reader}
			_, err := (&GatewayService{cfg: cfg}).handleBedrockStreamingResponse(ctx, resp, c, account, time.Now(), "gpt-5.4")
			require.NoError(t, <-written)
			require.Error(t, err)
			var candidate *UpstreamFailoverError
			require.False(t, errors.As(err, &candidate), "committed response must not expose a replayable error")
			require.Equal(t, 200, rec.Code)
			require.Contains(t, rec.Body.String(), ": keepalive")
			require.Equal(t, 1, strings.Count(rec.Body.String(), "event:error\n"))
			require.NotContains(t, rec.Body.String(), "private reasoning")
			require.True(t, IsResponseCommitted(c), "the handler must not append a second fallback error")
			if action == model.RetryFilterActionTerminate {
				require.Contains(t, rec.Body.String(), message)
			}
		})
	}
}
