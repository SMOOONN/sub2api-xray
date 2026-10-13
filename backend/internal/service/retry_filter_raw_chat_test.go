//go:build unit

package service

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/model"
	"github.com/stretchr/testify/require"
)

const retryFilterRawFinish = "data: {\"choices\":[{\"index\":0,\"delta\":{},\"finish_reason\":\"stop\"}]}\n\ndata: [DONE]\n\n"

func TestRetryFilterRawChatSemanticBoundary(t *testing.T) {
	for _, test := range []struct {
		name  string
		body  string
		event RetryFilterEventType
		large bool
	}{
		{name: "empty EOF", event: RetryFilterEventEmptyInterruptedStream},
		{name: "comment EOF", body: ": upstream keepalive\n\n", event: RetryFilterEventEmptyInterruptedStream},
		{name: "role completed", body: retryFilterCCRole + retryFilterRawFinish, event: RetryFilterEventEmptyCompletedResponse},
		{name: "thinking completed", body: retryFilterCCThinking + retryFilterRawFinish, event: RetryFilterEventEmptyCompletedResponse},
		{name: "usage completed", body: "data: {\"choices\":[],\"usage\":{\"prompt_tokens\":11,\"completion_tokens\":7}}\n\n" + retryFilterRawFinish, event: RetryFilterEventEmptyCompletedResponse},
		{name: "text", body: retryFilterCCThinking + retryFilterCCText + retryFilterRawFinish},
		{name: "tool", body: "data: {\"choices\":[{\"delta\":{\"tool_calls\":[{\"id\":\"call_1\",\"type\":\"function\",\"function\":{\"name\":\"lookup\",\"arguments\":\"{}\"}}]}}]}\n\n" + retryFilterRawFinish},
		{name: "large image without usage", body: "data: {\"choices\":[{\"delta\":{\"images\":[{\"image_url\":{\"url\":\"https://example.com/image.png\"}}]}}]}\n\n" + retryFilterRawFinish, large: true},
		{name: "large audio without usage", body: "data: {\"choices\":[{\"delta\":{\"audio\":{\"data\":\"YXVkaW8=\"}}}]}\n\n" + retryFilterRawFinish, large: true},
		{name: "raw JSON retains native", body: `{"choices":[],"usage":{"prompt_tokens":11}}` + "\n"},
	} {
		t.Run(test.name, func(t *testing.T) {
			runtime := retryFilterAttemptRuntimeForTest(&model.RetryFilterRule{EmptyResponseMode: model.RetryFilterEmptyResponseBoth, Action: model.RetryFilterActionSameAccountRetry})
			c, rec, account := retryFilterAttemptContextForTest(runtime)
			c.Request.URL.Path = "/v1/chat/completions"
			resp := &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": {"text/event-stream"}}, Body: io.NopCloser(strings.NewReader(test.body))}
			requestLen := 64
			if test.large {
				requestLen = openAISilentRefusalMinRequestBodyBytes
			}
			svc := &OpenAIGatewayService{cfg: rawChatCompletionsTestConfig()}
			_, err := svc.streamRawChatCompletions(c, resp, account, "gpt-5.4", "gpt-5.4", "gpt-5.4", nil, nil, time.Now(), requestLen)
			if test.event != "" {
				var candidate *UpstreamFailoverError
				require.ErrorAs(t, err, &candidate)
				require.Equal(t, test.event, candidate.RetryFilterEventType)
				require.True(t, candidate.RetryableOnSameAccount)
				require.False(t, c.Writer.Written())
				runtime.Apply(c.Request.Context(), c, RetryFilterInput{Account: account, FailoverErr: candidate})
				require.Len(t, retryFilterAttemptEvents(t, c), 1)
			} else {
				require.NoError(t, err)
				require.Equal(t, test.body, rec.Body.String())
				require.Empty(t, retryFilterAttemptEvents(t, c))
			}
		})
	}
}

func TestRetryFilterRawChatActionsAndNativeModes(t *testing.T) {
	for _, mode := range []string{"same_account_retry", "switch_account", "terminate", "observe", "off", "unmatched", "nonpool"} {
		t.Run(mode, func(t *testing.T) {
			rule := &model.RetryFilterRule{EmptyResponseMode: model.RetryFilterEmptyResponseBoth, Action: model.RetryFilterActionSameAccountRetry}
			switch mode {
			case "switch_account", "terminate", "observe":
				rule.Action = mode
			}
			if mode == "unmatched" {
				rule.EmptyResponseMode = model.RetryFilterEmptyResponseNone
				rule.StatusCodes = []int{422}
			}
			runtime := retryFilterAttemptRuntimeForTest(rule)
			if mode == "off" {
				runtime.enabled = func(context.Context) bool { return false }
			}
			c, rec, account := retryFilterAttemptContextForTest(runtime)
			if mode == "nonpool" {
				account.Credentials["pool_mode"] = false
			}
			body := retryFilterCCThinking + retryFilterRawFinish
			resp := &http.Response{StatusCode: 200, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(body))}
			_, err := (&OpenAIGatewayService{cfg: rawChatCompletionsTestConfig()}).streamRawChatCompletions(c, resp, account, "gpt-5.4", "gpt-5.4", "gpt-5.4", nil, nil, time.Now(), 64)
			if mode == "same_account_retry" || mode == "switch_account" || mode == "terminate" {
				var candidate *UpstreamFailoverError
				require.ErrorAs(t, err, &candidate)
				require.Equal(t, mode == "same_account_retry", candidate.RetryableOnSameAccount)
				require.False(t, c.Writer.Written())
			} else {
				require.NoError(t, err)
				require.Equal(t, body, rec.Body.String())
			}
			events := retryFilterAttemptEvents(t, c)
			if mode == "off" || mode == "unmatched" || mode == "nonpool" {
				require.Empty(t, events)
			} else {
				require.Len(t, events, 1)
				require.Equal(t, rule.Action, events[0].RetryFilterAction)
				require.Equal(t, mode != "observe", events[0].RetryFilterBehaviorChanged)
			}
		})
	}
}

func TestRetryFilterRawChatCommittedPingNeverReplays(t *testing.T) {
	for _, mode := range []string{"retry", "observe", "unmatched"} {
		for _, body := range []string{"", retryFilterRawFinish, retryFilterCCThinking + retryFilterRawFinish} {
			t.Run(mode+"/"+body, func(t *testing.T) {
				rule := &model.RetryFilterRule{EmptyResponseMode: model.RetryFilterEmptyResponseBoth, Action: model.RetryFilterActionSameAccountRetry}
				if mode == "observe" {
					rule.Action = model.RetryFilterActionObserve
				}
				if mode == "unmatched" {
					rule.EmptyResponseMode = model.RetryFilterEmptyResponseNone
					rule.StatusCodes = []int{422}
				}
				c, rec, account := retryFilterAttemptContextForTest(retryFilterAttemptRuntimeForTest(rule))
				c.Writer.Header().Set("Content-Type", "text/event-stream")
				_, err := c.Writer.WriteString(": pre-content ping\n\n")
				require.NoError(t, err)
				c.Writer.Flush()
				resp := &http.Response{StatusCode: 200, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(body))}
				_, err = (&OpenAIGatewayService{cfg: rawChatCompletionsTestConfig()}).streamRawChatCompletions(c, resp, account, "gpt-5.4", "gpt-5.4", "gpt-5.4", nil, nil, time.Now(), openAISilentRefusalMinRequestBodyBytes)
				var candidate *UpstreamFailoverError
				require.False(t, errors.As(err, &candidate), "no candidate may escape through native truncation or silent refusal after ping")
				require.Contains(t, rec.Body.String(), ": pre-content ping")
			})
		}
	}
}

func TestRetryFilterRawChatErrorCancellationAndResourceBoundaries(t *testing.T) {
	for _, mode := range []string{"inline-error", "after-output-error", "cancel", "disconnect", "scanner-limit"} {
		t.Run(mode, func(t *testing.T) {
			rule := &model.RetryFilterRule{EmptyResponseMode: model.RetryFilterEmptyResponseBoth, Keywords: []string{"temporary upstream failure"}, Action: model.RetryFilterActionSameAccountRetry}
			c, rec, account := retryFilterAttemptContextForTest(retryFilterAttemptRuntimeForTest(rule))
			body := retryFilterCCError
			if mode == "after-output-error" {
				body = retryFilterCCText + retryFilterCCError
			}
			var reader io.ReadCloser = io.NopCloser(strings.NewReader(body))
			if mode == "cancel" {
				reader = &errTailReader{data: []byte("data: {\"choices\":[],\"usage\":{\"prompt_tokens\":11,\"completion_tokens\":7}}\n\n"), err: context.Canceled}
			}
			if mode == "disconnect" {
				c.Writer = &openAIRawStreamDisconnectedWriter{ResponseWriter: c.Writer}
				reader = io.NopCloser(strings.NewReader(retryFilterCCText + "data: {\"choices\":[],\"usage\":{\"prompt_tokens\":11,\"completion_tokens\":7}}\n\n"))
			}
			if mode == "scanner-limit" {
				reader = io.NopCloser(strings.NewReader(strings.Repeat("x", openAIFirstOutputStageMaxBytes+1024)))
			}
			resp := &http.Response{StatusCode: 200, Header: http.Header{}, Body: reader}
			result, err := (&OpenAIGatewayService{cfg: rawChatCompletionsTestConfig()}).streamRawChatCompletions(c, resp, account, "gpt-5.4", "gpt-5.4", "gpt-5.4", nil, nil, time.Now(), 64)
			var candidate *UpstreamFailoverError
			if mode == "inline-error" {
				require.ErrorAs(t, err, &candidate)
				require.False(t, c.Writer.Written())
			} else {
				require.False(t, errors.As(err, &candidate))
				for _, event := range retryFilterAttemptEvents(t, c) {
					require.Zero(t, event.RetryFilterRuleID)
				}
				if mode == "cancel" || mode == "disconnect" {
					require.NoError(t, err)
					require.NotNil(t, result)
					require.Equal(t, 11, result.Usage.InputTokens)
					require.Equal(t, 7, result.Usage.OutputTokens)
				}
				if mode == "after-output-error" {
					require.Contains(t, rec.Body.String(), "visible")
				}
				if mode == "scanner-limit" {
					require.Error(t, err)
				}
			}
		})
	}
}

func TestRetryFilterRawChatObservationReusesNativeEvent(t *testing.T) {
	for _, mode := range []string{"observe", "unmatched"} {
		for _, nativeCase := range []string{"empty EOF", "role EOF", "silent refusal"} {
			t.Run(mode+"/"+nativeCase, func(t *testing.T) {
				rule := &model.RetryFilterRule{EmptyResponseMode: model.RetryFilterEmptyResponseBoth, Action: model.RetryFilterActionObserve}
				if mode == "unmatched" {
					rule.EmptyResponseMode = model.RetryFilterEmptyResponseNone
					rule.StatusCodes = []int{422}
				}
				runtime := retryFilterAttemptRuntimeForTest(rule)
				c, rec, account := retryFilterAttemptContextForTest(runtime)
				body, requestLen := "", 64
				if nativeCase == "role EOF" {
					body = retryFilterCCRole
				}
				if nativeCase == "silent refusal" {
					body = retryFilterRawFinish
					requestLen = openAISilentRefusalMinRequestBodyBytes
				}
				resp := &http.Response{StatusCode: 200, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(body))}
				_, err := (&OpenAIGatewayService{cfg: rawChatCompletionsTestConfig()}).streamRawChatCompletions(c, resp, account, "gpt-5.4", "gpt-5.4", "gpt-5.4", nil, nil, time.Now(), requestLen)
				require.Error(t, err)
				var candidate *UpstreamFailoverError
				if nativeCase == "role EOF" {
					require.False(t, errors.As(err, &candidate))
					require.Equal(t, body, rec.Body.String())
				} else {
					require.ErrorAs(t, err, &candidate)
					require.False(t, c.Writer.Written())
					if nativeCase == "silent refusal" {
						require.True(t, IsOpenAISilentRefusalErrorBody(candidate.ResponseBody))
					} else {
						require.Equal(t, openAIRawStreamTruncatedErrorBody(ErrOpenAIUpstreamStreamTruncated), candidate.ResponseBody)
					}
					runtime.Apply(c.Request.Context(), c, RetryFilterInput{Account: account, FailoverErr: candidate})
				}
				events := retryFilterAttemptEvents(t, c)
				require.Len(t, events, 1, "the rule annotation belongs to the native failure event")
				if mode == "observe" {
					require.Equal(t, model.RetryFilterActionObserve, events[0].RetryFilterAction)
					require.False(t, events[0].RetryFilterBehaviorChanged)
				} else {
					require.Zero(t, events[0].RetryFilterRuleID)
				}
			})
		}
	}
}
