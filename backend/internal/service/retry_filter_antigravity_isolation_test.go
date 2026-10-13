//go:build unit

package service

import (
	"bufio"
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/model"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

const retryFilterAntigravityModel = "gemini-3.1-pro"

func retryFilterAntigravityContext(t *testing.T, mode string) (*gin.Context, *httptest.ResponseRecorder) {
	t.Helper()
	c, rec := newAntigravityCompatContext(http.MethodPost, "/isolation", nil)
	runtime := retryFilterAttemptRuntimeForTest(&model.RetryFilterRule{
		EmptyResponseMode: model.RetryFilterEmptyResponseBoth, Action: model.RetryFilterActionTerminate,
	})
	pool := &Account{ID: 1, Platform: PlatformAnthropic, Type: AccountTypeAPIKey,
		Credentials: map[string]any{"pool_mode": true, "retry_filter_preset_id": int64(1)}}
	// A live preset must match the same empty-response event on a pool account;
	// an empty repository would make the non-pool isolation control vacuous.
	failure := &UpstreamFailoverError{StatusCode: http.StatusBadGateway, RetryFilterEventType: RetryFilterEventEmptyCompletedResponse}
	require.True(t, runtime.Evaluate(c.Request.Context(), pool, nil, retryFilterAntigravityModel, failure).Actionable())
	account := newAntigravityCompatAccount(AccountTypeOAuth)
	account.Credentials["retry_filter_preset_id"] = int64(1)
	require.Equal(t, int64(1), runtime.ResolvePresetID(c.Request.Context(), account, nil))
	require.Nil(t, runtime.Evaluate(c.Request.Context(), account, nil, retryFilterAntigravityModel, failure))
	if mode == "disabled" {
		runtime.enabled = func(context.Context) bool { return false }
		require.Nil(t, runtime.Evaluate(c.Request.Context(), pool, nil, retryFilterAntigravityModel, failure))
	}
	if mode != "no_attempt" {
		c.Request = c.Request.WithContext(WithRetryFilterAttempt(c.Request.Context(), runtime, nil, 0, retryFilterAntigravityModel))
	}
	return c, rec
}

func retryFilterAntigravityFrame(candidate string) string {
	return `data: {"response":{"candidates":[` + candidate + `],"usageMetadata":{"promptTokenCount":11,"candidatesTokenCount":7}}}` + "\n\n"
}

type retryFilterAntigravityWire struct {
	name, textPath, thoughtPath, terminalPath, terminal, inputPath, outputPath string
	textEvent, thoughtEvent                                                    string
}

var retryFilterAntigravityWires = []retryFilterAntigravityWire{
	{name: "chat_stream", textPath: "choices.0.delta.content", thoughtPath: "choices.0.delta.reasoning_content", terminalPath: "choices.0.finish_reason", terminal: "stop", inputPath: "usage.prompt_tokens", outputPath: "usage.completion_tokens"},
	{name: "responses_stream", textPath: "delta", thoughtPath: "delta", textEvent: "response.output_text.delta", thoughtEvent: "response.reasoning_summary_text.delta", terminalPath: "type", terminal: "response.completed", inputPath: "response.usage.input_tokens", outputPath: "response.usage.output_tokens"},
	{name: "messages_stream", textPath: "delta.text", thoughtPath: "delta.thinking", terminalPath: "type", terminal: "message_stop", inputPath: "usage.input_tokens", outputPath: "usage.output_tokens"},
	{name: "gemini_stream", textPath: "candidates.0.content.parts.0.text", thoughtPath: "candidates.0.content.parts.0.text", terminalPath: "candidates.0.finishReason", terminal: "STOP", inputPath: "usageMetadata.promptTokenCount", outputPath: "usageMetadata.candidatesTokenCount"},
	{name: "chat_json", textPath: "choices.0.message.content", thoughtPath: "choices.0.message.reasoning_content", terminalPath: "choices.0.finish_reason", terminal: "stop", inputPath: "usage.prompt_tokens", outputPath: "usage.completion_tokens"},
	{name: "responses_json", textPath: `output.#(type=="message").content.0.text`, thoughtPath: `output.#(type=="reasoning").summary.0.text`, terminalPath: "status", terminal: "completed", inputPath: "usage.input_tokens", outputPath: "usage.output_tokens"},
	{name: "messages_json", textPath: "content.0.text", thoughtPath: "content.0.thinking", terminalPath: "stop_reason", terminal: "end_turn", inputPath: "usage.input_tokens", outputPath: "usage.output_tokens"},
	{name: "gemini_json", textPath: "candidates.0.content.parts.0.text", thoughtPath: "candidates.0.content.parts.0.text", terminalPath: "candidates.0.finishReason", terminal: "STOP", inputPath: "usageMetadata.promptTokenCount", outputPath: "usageMetadata.candidatesTokenCount"},
}

func retryFilterAntigravityPayloads(t *testing.T, body string, stream bool) []gjson.Result {
	t.Helper()
	if !stream {
		require.True(t, gjson.Valid(body), body)
		return []gjson.Result{gjson.Parse(body)}
	}
	var payloads []gjson.Result
	for _, line := range strings.Split(body, "\n") {
		if !strings.HasPrefix(line, "data:") {
			continue
		}
		data := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		if data == "[DONE]" {
			continue
		}
		require.True(t, gjson.Valid(data), line)
		payloads = append(payloads, gjson.Parse(data))
	}
	return payloads
}

func retryFilterAntigravityAssertWire(t *testing.T, wire retryFilterAntigravityWire, body, wantText, wantThought string) {
	t.Helper()
	stream := strings.HasSuffix(wire.name, "_stream")
	payloads := retryFilterAntigravityPayloads(t, body, stream)
	var text, thought string
	var terminalCount int
	var input, output int64 = -1, -1
	for _, payload := range payloads {
		if strings.HasPrefix(wire.name, "gemini_") {
			if payload.Get("candidates.0.content.parts.0.thought").Bool() {
				thought += payload.Get(wire.thoughtPath).String()
			} else {
				text += payload.Get(wire.textPath).String()
			}
		} else {
			if wire.textEvent == "" || payload.Get("type").String() == wire.textEvent {
				text += payload.Get(wire.textPath).String()
			}
			if wire.thoughtEvent == "" || payload.Get("type").String() == wire.thoughtEvent {
				thought += payload.Get(wire.thoughtPath).String()
			}
		}
		if payload.Get(wire.terminalPath).String() == wire.terminal {
			terminalCount++
		}
		if payload.Get(wire.inputPath).Exists() {
			input, output = payload.Get(wire.inputPath).Int(), payload.Get(wire.outputPath).Int()
		}
		require.False(t, payload.Get("error").Exists(), body)
		if payload.Get("type").String() == "message_delta" {
			require.Equal(t, "end_turn", payload.Get("delta.stop_reason").String())
		}
		if payload.Get("type").String() == "response.completed" {
			require.Equal(t, "completed", payload.Get("response.status").String())
		}
	}
	require.Equal(t, wantText, text, body)
	require.Equal(t, wantThought, thought, body)
	require.Equal(t, 1, terminalCount, body)
	require.Equal(t, int64(11), input, body)
	require.Equal(t, int64(7), output, body)
	if wire.name == "chat_stream" {
		require.Equal(t, 1, strings.Count(body, "data: [DONE]"))
	}
}

func TestRetryFilterAntigravityOfficialSemanticsOutsidePoolConsumers(t *testing.T) {
	gin.SetMode(gin.TestMode)
	fixtures := []struct{ name, candidate, text, thought string }{
		{"thinking_only", `{"content":{"parts":[{"thought":true,"text":"isolated thought"}]},"finishReason":"STOP"}`, "", "isolated thought"},
		{"text", `{"content":{"parts":[{"text":"visible answer"}]},"finishReason":"STOP"}`, "visible answer", ""},
		{"whitespace", `{"content":{"parts":[{"text":"   "}]},"finishReason":"STOP"}`, "   ", ""},
		{"finish_only", `{"finishReason":"STOP"}`, "", ""},
	}
	for _, mode := range []string{"no_attempt", "disabled", "enabled_nonpool"} {
		for _, wire := range retryFilterAntigravityWires {
			for _, fixture := range fixtures {
				t.Run(mode+"/"+wire.name+"/"+fixture.name, func(t *testing.T) {
					svc := newAntigravityCompatService(config.GatewayConfig{MaxLineSize: defaultMaxLineSize}, nil)
					c, rec := retryFilterAntigravityContext(t, mode)
					resp := &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": {"text/event-stream"}},
						Body: io.NopCloser(strings.NewReader(retryFilterAntigravityFrame(fixture.candidate)))}
					defer resp.Body.Close()
					var result *antigravityStreamResult
					var err error
					start := time.Now()
					switch wire.name {
					case "chat_stream":
						result, err = svc.handleChatCompletionsStreamingFromAntigravity(c, resp, start, retryFilterAntigravityModel, true)
					case "responses_stream":
						result, err = svc.handleResponsesStreamingFromAntigravity(c, resp, start, retryFilterAntigravityModel)
					case "messages_stream":
						result, err = svc.handleClaudeStreamingResponse(c, resp, start, retryFilterAntigravityModel)
					case "gemini_stream":
						result, err = svc.handleGeminiStreamingResponse(c, resp, start)
					case "chat_json":
						result, err = svc.handleChatCompletionsNonStreamingFromAntigravity(c, resp, start, retryFilterAntigravityModel)
					case "responses_json":
						result, err = svc.handleResponsesNonStreamingFromAntigravity(c, resp, start, retryFilterAntigravityModel)
					case "messages_json":
						result, err = svc.handleClaudeStreamToNonStreaming(c, resp, start, retryFilterAntigravityModel)
					case "gemini_json":
						result, err = svc.handleGeminiStreamToNonStreaming(c, resp, start)
					}
					require.Empty(t, retryFilterAttemptEvents(t, c))
					var failure *UpstreamFailoverError
					if fixture.name == "finish_only" && (wire.name == "chat_stream" || wire.name == "responses_stream") {
						require.ErrorAs(t, err, &failure)
						require.Equal(t, http.StatusBadGateway, failure.StatusCode)
						require.True(t, failure.RetryableOnSameAccount)
						require.Empty(t, failure.RetryFilterEventType, "this is the official native empty-stream error")
						require.Contains(t, string(failure.ResponseBody), "empty stream response")
						require.Nil(t, result)
						require.Empty(t, rec.Body.String())
						require.Empty(t, rec.Header().Get("Content-Type"))
						return
					}
					require.NoError(t, err)
					require.False(t, errors.As(err, &failure))
					require.NotNil(t, result)
					require.NotNil(t, result.usage)
					require.Equal(t, 11, result.usage.InputTokens)
					require.Equal(t, 7, result.usage.OutputTokens)
					require.NotNil(t, result.firstTokenMs)
					require.False(t, result.clientDisconnect)
					require.Equal(t, http.StatusOK, rec.Code)
					retryFilterAntigravityAssertWire(t, wire, rec.Body.String(), fixture.text, fixture.thought)
				})
			}
		}
	}
}

func TestRetryFilterAntigravityThoughtReleasesPreContentDeadline(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, wire := range retryFilterAntigravityWires[:2] {
		t.Run(wire.name, func(t *testing.T) {
			svc := newAntigravityCompatService(config.GatewayConfig{MaxLineSize: defaultMaxLineSize, StreamDataIntervalTimeout: 5}, nil)
			c, rec := retryFilterAntigravityContext(t, "enabled_nonpool")
			notifier := &antigravityCompatNotifyingWriter{ResponseWriter: c.Writer, wrote: make(chan struct{}, 1)}
			c.Writer = notifier
			reader, writer := io.Pipe()
			releaseText, cancelWriter := make(chan struct{}), make(chan struct{})
			t.Cleanup(func() { close(cancelWriter); _ = reader.Close(); _ = writer.Close() })
			written := make(chan error, 1)
			go func() {
				defer writer.Close()
				_, err := io.WriteString(writer, `data: {"response":{"candidates":[{"content":{"parts":[{"thought":true,"text":"isolated thought"}]}}]}}`+"\n\n")
				if err == nil {
					select {
					case <-releaseText:
						_, err = io.WriteString(writer, retryFilterAntigravityFrame(`{"content":{"parts":[{"text":"late answer"}]},"finishReason":"STOP"}`))
					case <-cancelWriter:
						err = io.ErrClosedPipe
					}
				}
				written <- err
			}()
			var adapter antigravityCompatStreamAdapter = newAntigravityResponsesStreamAdapter(retryFilterAntigravityModel)
			if wire.name == "chat_stream" {
				adapter = newAntigravityChatStreamAdapter(retryFilterAntigravityModel, true)
			}
			type outcome struct {
				result *antigravityStreamResult
				err    error
			}
			done := make(chan outcome, 1)
			start, maxWait := time.Now(), 500*time.Millisecond
			go func() {
				result, err := svc.handleAntigravityCompatStreamWithKeepaliveInterval(c,
					&http.Response{StatusCode: http.StatusOK, Body: reader}, start, retryFilterAntigravityModel,
					adapter, "retry-filter isolation", 10*time.Second, maxWait)
				done <- outcome{result, err}
			}()
			select {
			case <-notifier.wrote:
			case result := <-done:
				t.Fatalf("thought did not release buffered events: %v", result.err)
			case <-time.After(2 * time.Second):
				t.Fatal("thought was not forwarded")
			}
			deadline := time.NewTimer(time.Until(start.Add(maxWait + 50*time.Millisecond)))
			defer deadline.Stop()
			select {
			case <-deadline.C:
			case result := <-done:
				t.Fatalf("stream ended before the gated text: %v", result.err)
			}
			require.Greater(t, time.Since(start), maxWait)
			close(releaseText)
			select {
			case result := <-done:
				require.NoError(t, result.err)
				require.NotNil(t, result.result)
				require.NotNil(t, result.result.firstTokenMs)
				require.Less(t, *result.result.firstTokenMs, int(maxWait.Milliseconds()))
				require.Equal(t, 11, result.result.usage.InputTokens)
				require.Equal(t, 7, result.result.usage.OutputTokens)
			case <-time.After(3 * time.Second):
				t.Fatal("stream did not finish before its read timeout")
			}
			require.NoError(t, <-written)
			retryFilterAntigravityAssertWire(t, wire, rec.Body.String(), "late answer", "isolated thought")
			require.Empty(t, retryFilterAttemptEvents(t, c))
		})
	}
}

func TestRetryFilterAntigravityInlineDataBoundary(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, wire := range retryFilterAntigravityWires[:2] {
		for _, fixture := range []struct{ name, data string }{
			{"image", "R0lGODlhAQABAIAAAAAAAP///yH5BAEAAAAALAAAAAABAAEAAAIBRAA7"},
			{"empty_data", ""},
		} {
			t.Run(wire.name+"/"+fixture.name, func(t *testing.T) {
				svc := newAntigravityCompatService(config.GatewayConfig{MaxLineSize: defaultMaxLineSize}, nil)
				c, rec := retryFilterAntigravityContext(t, "enabled_nonpool")
				body := retryFilterAntigravityFrame(`{"content":{"parts":[{"inlineData":{"mimeType":"image/gif","data":"` + fixture.data + `"}}]},"finishReason":"STOP"}`)
				resp := &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(body))}
				defer resp.Body.Close()
				var adapter antigravityCompatStreamAdapter = newAntigravityResponsesStreamAdapter(retryFilterAntigravityModel)
				if wire.name == "chat_stream" {
					adapter = newAntigravityChatStreamAdapter(retryFilterAntigravityModel, true)
				}
				result, err := svc.handleAntigravityCompatStreamWithKeepaliveInterval(c, resp, time.Now(),
					retryFilterAntigravityModel, adapter, "inline-data isolation", time.Minute, time.Minute)
				require.Empty(t, retryFilterAttemptEvents(t, c))
				if fixture.data == "" {
					var failure *UpstreamFailoverError
					require.ErrorAs(t, err, &failure)
					require.Equal(t, http.StatusBadGateway, failure.StatusCode)
					require.True(t, failure.RetryableOnSameAccount)
					require.Empty(t, failure.RetryFilterEventType)
					require.Nil(t, result)
					require.Empty(t, rec.Body.String())
					require.Empty(t, rec.Header().Get("Content-Type"))
					return
				}
				require.NoError(t, err)
				require.NotNil(t, result)
				require.NotNil(t, result.firstTokenMs)
				require.Equal(t, 11, result.usage.InputTokens)
				require.Equal(t, 7, result.usage.OutputTokens)
				retryFilterAntigravityAssertWire(t, wire, rec.Body.String(), "![image](data:image/gif;base64,"+fixture.data+")", "")
			})
		}
	}
	t.Run("empty_part_collector", func(t *testing.T) {
		svc := newAntigravityCompatService(config.GatewayConfig{MaxLineSize: defaultMaxLineSize}, nil)
		c, rec := retryFilterAntigravityContext(t, "enabled_nonpool")
		// No finish reason: the official collector accepts the part itself even
		// though the image processor emits no text for its empty data.
		body := retryFilterAntigravityFrame(`{"content":{"parts":[{"inlineData":{"mimeType":"image/gif","data":""}}]}}`)
		resp := &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(body))}
		defer resp.Body.Close()
		collected, result, err := svc.collectClaudeStreamResponse(c, resp, time.Now(), retryFilterAntigravityModel)
		require.NoError(t, err)
		require.NotNil(t, result)
		require.NotNil(t, result.firstTokenMs)
		require.Equal(t, 11, result.usage.InputTokens)
		require.Equal(t, 7, result.usage.OutputTokens)
		require.Equal(t, "[]", gjson.GetBytes(collected, "content").Raw)
		require.Equal(t, "end_turn", gjson.GetBytes(collected, "stop_reason").String())
		require.Empty(t, rec.Body.String())
		require.Empty(t, retryFilterAttemptEvents(t, c))
	})
}

func TestRetryFilterAntigravityReadFailuresBeforeAndAfterThought(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, wire := range retryFilterAntigravityWires[:2] {
		for _, afterThought := range []bool{false, true} {
			for _, ending := range []string{"canceled", "oversized_line"} {
				phase := map[bool]string{false: "before_thought", true: "after_thought"}[afterThought]
				t.Run(wire.name+"/"+phase+"/"+ending, func(t *testing.T) {
					// Scanner starts with the pooled 64 KiB buffer. Exceed that actual
					// capacity, not merely a configured limit below its initial buffer.
					maxLineSize := sseScannerBuf64KSize
					svc := newAntigravityCompatService(config.GatewayConfig{MaxLineSize: maxLineSize}, nil)
					c, rec := retryFilterAntigravityContext(t, "enabled_nonpool")
					prefix := ""
					if afterThought {
						prefix = retryFilterAntigravityFrame(`{"content":{"parts":[{"thought":true,"text":"isolated thought"}]}}`)
					}
					var reader io.ReadCloser = &antigravityCompatErrorReader{data: []byte(prefix), err: context.Canceled}
					if ending == "oversized_line" {
						reader = io.NopCloser(strings.NewReader(prefix + "data: " + strings.Repeat("x", maxLineSize+1) + "\n\n"))
					}
					defer reader.Close()
					var adapter antigravityCompatStreamAdapter = newAntigravityResponsesStreamAdapter(retryFilterAntigravityModel)
					if wire.name == "chat_stream" {
						adapter = newAntigravityChatStreamAdapter(retryFilterAntigravityModel, true)
					}
					result, err := svc.handleAntigravityCompatStreamWithKeepaliveInterval(c,
						&http.Response{StatusCode: http.StatusOK, Body: reader}, time.Now(), retryFilterAntigravityModel,
						adapter, "read-failure isolation", time.Minute, time.Minute)
					require.Empty(t, retryFilterAttemptEvents(t, c))
					var failure *UpstreamFailoverError
					if !afterThought {
						require.ErrorAs(t, err, &failure)
						require.Equal(t, http.StatusBadGateway, failure.StatusCode)
						require.True(t, failure.RetryableOnSameAccount, "retain the native pre-content retry classification")
						require.Empty(t, failure.RetryFilterEventType)
						require.Nil(t, result)
						require.Empty(t, rec.Body.String())
						require.Empty(t, rec.Header().Get("Content-Type"))
						return
					}
					require.False(t, errors.As(err, &failure))
					require.NotNil(t, result)
					require.NotNil(t, result.firstTokenMs)
					require.Equal(t, 11, result.usage.InputTokens)
					require.Equal(t, 7, result.usage.OutputTokens)
					require.Contains(t, rec.Body.String(), "isolated thought")
					require.NotContains(t, rec.Body.String(), "data: [DONE]")
					require.NotContains(t, rec.Body.String(), "event: response.completed")
					if ending == "canceled" {
						require.NoError(t, err)
						require.True(t, result.clientDisconnect)
						require.NotContains(t, rec.Body.String(), `"error"`)
					} else {
						require.ErrorIs(t, err, bufio.ErrTooLong)
						require.False(t, result.clientDisconnect)
						require.True(t, IsResponseCommitted(c))
						require.Equal(t, 1, strings.Count(rec.Body.String(), `"message":"response_too_large"`))
					}
				})
			}
		}
	}
}
