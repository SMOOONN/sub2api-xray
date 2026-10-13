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

func TestRetryFilterHTTPConsumerPreservesLegacyAndInterceptsRaw422(t *testing.T) {
	for _, route := range []string{"openai", "openai_passthrough", "anthropic", "anthropic_to_responses", "anthropic_to_chat"} {
		t.Run(route, func(t *testing.T) {
			var legacyBody string
			var legacyStatus int
			for _, mode := range []string{"off", "unmatched", "observe", "nonpool", "retry"} {
				t.Run(mode, func(t *testing.T) {
					rule := &model.RetryFilterRule{StatusCodes: []int{422}, UpstreamCodes: []string{"try_again"}, UpstreamCodePaths: []string{"error.code"}, MatchMode: model.MatchModeAll, Action: model.RetryFilterActionSameAccountRetry}
					if mode == "unmatched" {
						rule.StatusCodes = []int{503}
					}
					if mode == "observe" {
						rule.Action = model.RetryFilterActionObserve
					}
					runtime := retryFilterAttemptRuntimeForTest(rule)
					if mode == "off" {
						runtime.enabled = func(context.Context) bool { return false }
					}
					c, rec, _ := retryFilterAttemptContextForTest(runtime)
					raw := `{"error":{"code":"try_again","message":"original upstream message"}}`
					resp := &http.Response{StatusCode: 422, Header: http.Header{"Content-Type": {"application/json"}, "Retry-After": {"3"}}, Body: io.NopCloser(strings.NewReader(raw))}
					var err error
					var account *Account
					if strings.HasPrefix(route, "openai") {
						c.Request.URL.Path = "/v1/responses"
						account = newOpenAIImageGenerationControlTestAccount()
						account.Credentials["pool_mode"] = mode != "nonpool"
						account.Credentials["retry_filter_preset_id"] = int64(1)
						account.Extra = map[string]any{"openai_passthrough": route == "openai_passthrough"}
						svc := newOpenAIImageGenerationControlTestService(&httpUpstreamRecorder{resp: resp})
						_, err = svc.Forward(c.Request.Context(), c, account, []byte(`{"model":"gpt-5.4","input":"hello","stream":false}`))
					} else {
						account = newAnthropicAPIKeyAccountForTest()
						account.Credentials["pool_mode"] = mode != "nonpool"
						account.Credentials["retry_filter_preset_id"] = int64(1)
						svc := &GatewayService{cfg: rawChatCompletionsTestConfig(), httpUpstream: &anthropicHTTPUpstreamRecorder{resp: resp}, rateLimitService: &RateLimitService{}, deferredService: &DeferredService{}}
						body := []byte(`{"model":"claude-sonnet-4-5","max_tokens":16,"messages":[{"role":"user","content":"hello"}],"stream":false}`)
						parsed := &ParsedRequest{Body: NewRequestBodyRef(body), Model: "claude-sonnet-4-5"}
						switch route {
						case "anthropic":
							_, err = svc.Forward(c.Request.Context(), c, account, parsed)
						case "anthropic_to_responses":
							c.Request.URL.Path = "/v1/responses"
							_, err = svc.ForwardAsResponses(c.Request.Context(), c, account, []byte(`{"model":"claude-sonnet-4-5","input":"hello","stream":false}`), parsed)
						case "anthropic_to_chat":
							c.Request.URL.Path = "/v1/chat/completions"
							_, err = svc.ForwardAsChatCompletions(c.Request.Context(), c, account, body, parsed)
						}
					}
					require.Error(t, err)
					var failure *UpstreamFailoverError
					if mode == "retry" {
						require.ErrorAs(t, err, &failure)
						require.Equal(t, 422, failure.StatusCode)
						require.Equal(t, raw, string(failure.ResponseBody))
						require.Equal(t, "3", failure.ResponseHeaders.Get("Retry-After"))
						require.True(t, failure.RetryableOnSameAccount)
						require.Empty(t, rec.Body.String())
						runtime.Apply(c.Request.Context(), c, RetryFilterInput{Account: account, FailoverErr: failure})
					} else {
						require.False(t, errors.As(err, &failure), "an unhandled 422 must retain the legacy writer")
						if mode == "off" {
							legacyBody, legacyStatus = rec.Body.String(), rec.Code
							require.NotEmpty(t, legacyBody)
						} else {
							require.Equal(t, legacyStatus, rec.Code)
							require.Equal(t, legacyBody, rec.Body.String())
						}
					}
					matches := 0
					for _, event := range retryFilterAttemptEvents(t, c) {
						if event.RetryFilterRuleID == rule.ID {
							matches++
						}
					}
					if mode == "retry" || mode == "observe" {
						require.Equal(t, 1, matches)
					} else {
						require.Zero(t, matches)
					}
				})
			}
		})
	}
}

func TestRetryFilterResponsesConsumerThinkingPreservesLegacyWhenUnmatched(t *testing.T) {
	for _, passthrough := range []bool{false, true} {
		for _, ending := range []string{"failed", "EOF", "success"} {
			name := map[bool]string{false: "converted", true: "passthrough"}[passthrough] + "/" + ending
			t.Run(name, func(t *testing.T) {
				var legacyBody string
				for _, mode := range []string{"off", "unmatched", "observe", "retry"} {
					t.Run(mode, func(t *testing.T) {
						rule := &model.RetryFilterRule{UpstreamCodes: []string{"try_again"}, UpstreamCodePaths: []string{"response.error.code"}, EmptyResponseMode: model.RetryFilterEmptyResponseBoth, Action: model.RetryFilterActionSameAccountRetry}
						if mode == "unmatched" {
							rule.UpstreamCodes = []string{"not-this-code"}
							rule.EmptyResponseMode = model.RetryFilterEmptyResponseNone
						}
						if mode == "observe" {
							rule.Action = model.RetryFilterActionObserve
						}
						runtime := retryFilterAttemptRuntimeForTest(rule)
						if mode == "off" {
							runtime.enabled = func(context.Context) bool { return false }
						}
						c, rec, account := retryFilterAttemptContextForTest(runtime)
						body := "data: {\"type\":\"response.created\",\"response\":{\"id\":\"r1\",\"status\":\"in_progress\"}}\n\n" +
							"data: {\"type\":\"response.reasoning_summary_text.delta\",\"delta\":\"thinking\"}\n\n"
						if ending == "failed" {
							body += "data: {\"type\":\"response.failed\",\"response\":{\"id\":\"r1\",\"status\":\"failed\",\"error\":{\"code\":\"try_again\",\"message\":\"temporary failure\"}}}\n\n"
						}
						if ending == "success" {
							body += "data: {\"type\":\"response.output_text.delta\",\"delta\":\"visible\"}\n\n" +
								"data: {\"type\":\"response.completed\",\"response\":{\"id\":\"r1\",\"status\":\"completed\",\"usage\":{\"input_tokens\":2,\"output_tokens\":1}}}\n\n"
						}
						resp := &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": {"text/event-stream"}}, Body: io.NopCloser(strings.NewReader(body))}
						svc := &OpenAIGatewayService{cfg: rawChatCompletionsTestConfig(), toolCorrector: NewCodexToolCorrector()}
						var err error
						if passthrough {
							_, err = svc.handleStreamingResponsePassthrough(c.Request.Context(), resp, c, account, time.Now(), "gpt-5.4", "gpt-5.4")
						} else {
							_, err = svc.handleStreamingResponse(c.Request.Context(), resp, c, account, time.Now(), "gpt-5.4", "gpt-5.4")
						}
						var failure *UpstreamFailoverError
						if mode == "retry" && ending != "success" {
							require.ErrorAs(t, err, &failure)
							require.Empty(t, rec.Body.String())
							require.Len(t, retryFilterAttemptEvents(t, c), 1)
							return
						}
						require.False(t, errors.As(err, &failure))
						if ending == "success" {
							require.NoError(t, err)
							require.Contains(t, rec.Body.String(), "visible")
						} else {
							require.Error(t, err)
						}
						if mode == "off" {
							legacyBody = rec.Body.String()
						} else {
							require.Equal(t, legacyBody, rec.Body.String())
						}
					})
				}
			})
		}
	}
}
