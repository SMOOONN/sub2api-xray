//go:build unit

package service

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/model"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func retryFilterJSONFixture(protocol, kind string) string {
	if strings.HasPrefix(protocol, "messages") {
		content := map[string]string{
			"empty":    "[]",
			"thinking": `[{"type":"thinking","thinking":"private reasoning"}]`,
			"text":     `[{"type":"text","text":"visible"}]`,
			"tool":     `[{"type":"text","text":""},{"type":"tool_use","id":"call_1","name":"lookup","input":{}}]`,
			"image":    `[{"type":"image","source":{"type":"base64","data":"aW1hZ2U="}}]`,
			"audio":    `[{"type":"audio","data":"YXVkaW8="}]`,
		}[kind]
		return `{"id":"msg_1","type":"message","role":"assistant","model":"gpt-5.4","content":` + content + `,"stop_reason":"end_turn","usage":{"input_tokens":11,"output_tokens":7}}`
	}
	if protocol == "chat" {
		message := map[string]string{
			"empty":    `{"role":"assistant","content":null}`,
			"thinking": `{"role":"assistant","content":"","reasoning_content":"private reasoning"}`,
			"text":     `{"role":"assistant","content":"visible"}`,
			"tool":     `{"role":"assistant","content":"","tool_calls":[{"id":"call_1","type":"function","function":{"name":"lookup","arguments":"{}"}}]}`,
			"image":    `{"role":"assistant","images":[{"image_url":{"url":"https://example.com/image.png"}}]}`,
			"audio":    `{"role":"assistant","audio":{"data":"YXVkaW8="}}`,
		}[kind]
		return `{"id":"chat_1","object":"chat.completion","model":"gpt-5.4","choices":[{"index":0,"message":` + message + `,"finish_reason":"stop"}],"usage":{"prompt_tokens":11,"completion_tokens":7}}`
	}
	output := map[string]string{
		"empty":    "[]",
		"thinking": `[{"type":"reasoning","summary":[{"type":"summary_text","text":"private reasoning"}]}]`,
		"text":     `[{"type":"message","content":[{"type":"output_text","text":"visible"}]}]`,
		"tool":     `[{"type":"function_call","call_id":"call_1","name":"lookup","arguments":"{}"}]`,
		"image":    `[{"type":"image_generation_call","result":"aW1hZ2U="}]`,
		"audio":    `[{"type":"output_audio","data":"YXVkaW8="}]`,
	}[kind]
	return `{"id":"resp_1","object":"response","status":"completed","model":"gpt-5.4","output":` + output + `,"usage":{"input_tokens":11,"output_tokens":7,"output_tokens_details":{"reasoning_tokens":7}}}`
}

// Exercise the actual JSON consumers, including their native output/usage path.
func callRetryFilterJSONConsumer(protocol string, c *gin.Context, account *Account, resp *http.Response) (int, int, error) {
	ctx := c.Request.Context()
	if strings.HasPrefix(protocol, "messages") {
		svc := &GatewayService{cfg: rawChatCompletionsTestConfig(), rateLimitService: &RateLimitService{}}
		var usage *ClaudeUsage
		var err error
		if protocol == "messages-bedrock" {
			account.Type, account.Platform = AccountTypeBedrock, PlatformAnthropic
			usage, err = svc.handleBedrockNonStreamingResponse(ctx, resp, c, account)
		} else if protocol == "messages-passthrough" {
			usage, err = svc.handleNonStreamingResponseAnthropicAPIKeyPassthrough(ctx, resp, c, account)
		} else {
			usage, err = svc.handleNonStreamingResponse(ctx, resp, c, account, "gpt-5.4", "gpt-5.4")
		}
		if usage == nil {
			return 0, 0, err
		}
		return usage.InputTokens, usage.OutputTokens, err
	}
	svc := &OpenAIGatewayService{cfg: rawChatCompletionsTestConfig()}
	if protocol == "chat" {
		result, err := svc.bufferRawChatCompletions(c, resp, account, "gpt-5.4", "gpt-5.4", "gpt-5.4", nil, nil, time.Now())
		if result == nil {
			return 0, 0, err
		}
		return result.Usage.InputTokens, result.Usage.OutputTokens, err
	}
	if protocol == "responses-passthrough" {
		result, err := svc.handleNonStreamingResponsePassthrough(ctx, resp, c, account, "gpt-5.4", "gpt-5.4")
		if result == nil {
			return 0, 0, err
		}
		return result.usage.InputTokens, result.usage.OutputTokens, err
	}
	result, err := svc.handleNonStreamingResponse(ctx, resp, c, account, "gpt-5.4", "gpt-5.4")
	if result == nil {
		return 0, 0, err
	}
	return result.usage.InputTokens, result.usage.OutputTokens, err
}

func TestRetryFilterCompletedJSONConsumers(t *testing.T) {
	for _, protocol := range []string{"messages", "messages-passthrough", "messages-bedrock", "responses", "responses-passthrough", "chat"} {
		for _, mode := range []string{"empty", "thinking", "switch", "terminate", "off", "no-runtime", "nonpool", "unmatched", "observe", "text", "tool", "image", "audio"} {
			t.Run(protocol+"/"+mode, func(t *testing.T) {
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
				runtime := retryFilterAttemptRuntimeForTest(rule)
				if mode == "off" {
					runtime.enabled = func(context.Context) bool { return false }
				}
				c, rec, account := retryFilterAttemptContextForTest(runtime)
				if strings.HasPrefix(protocol, "messages") {
					account.Platform = PlatformAnthropic
				} else if protocol == "chat" {
					c.Request.URL.Path = "/v1/chat/completions"
				} else {
					c.Request.URL.Path = "/v1/responses"
				}
				if mode == "no-runtime" {
					c.Request = c.Request.WithContext(context.Background())
				}
				if mode == "nonpool" {
					account.Credentials["pool_mode"] = false
				}
				kind := "empty"
				switch mode {
				case "thinking", "text", "tool", "image", "audio":
					kind = mode
				}
				body := retryFilterJSONFixture(protocol, kind)
				resp := &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": {"application/json"}, "X-Request-Id": {"json-attempt"}}, Body: io.NopCloser(strings.NewReader(body))}
				input, output, err := callRetryFilterJSONConsumer(protocol, c, account, resp)
				intercept := mode == "empty" || mode == "thinking" || mode == "switch" || mode == "terminate"
				if intercept {
					var candidate *UpstreamFailoverError
					require.ErrorAs(t, err, &candidate)
					require.Equal(t, RetryFilterEventEmptyCompletedResponse, candidate.RetryFilterEventType)
					require.Equal(t, http.StatusBadGateway, candidate.StatusCode)
					require.Equal(t, body, string(candidate.ResponseBody))
					require.Equal(t, "json-attempt", candidate.ResponseHeaders.Get("x-request-id"))
					require.False(t, c.Writer.Written())
					require.Empty(t, rec.Body.String())
					require.Zero(t, input)
					require.Zero(t, output)
					runtime.Apply(c.Request.Context(), c, RetryFilterInput{Account: account, FailoverErr: candidate})
				} else {
					require.NoError(t, err)
					require.Equal(t, http.StatusOK, rec.Code)
					require.Equal(t, body, rec.Body.String())
					require.Equal(t, 11, input)
					require.Equal(t, 7, output)
				}
				events := retryFilterAttemptEvents(t, c)
				if intercept || mode == "observe" {
					require.Len(t, events, 1)
					require.Equal(t, rule.Action, events[0].RetryFilterAction)
					require.Equal(t, intercept, events[0].RetryFilterBehaviorChanged)
				} else {
					require.Empty(t, events)
				}
			})
		}
	}
}

func TestRetryFilterCompletedJSONWithoutUsage(t *testing.T) {
	for _, protocol := range []string{"messages", "messages-passthrough", "messages-bedrock", "responses", "responses-passthrough", "chat"} {
		for _, mode := range []string{"retry", "off", "observe"} {
			t.Run(protocol+"/"+mode, func(t *testing.T) {
				rule := &model.RetryFilterRule{EmptyResponseMode: model.RetryFilterEmptyResponseBoth, Action: model.RetryFilterActionSameAccountRetry}
				if mode == "observe" {
					rule.Action = model.RetryFilterActionObserve
				}
				runtime := retryFilterAttemptRuntimeForTest(rule)
				if mode == "off" {
					runtime.enabled = func(context.Context) bool { return false }
				}
				c, rec, account := retryFilterAttemptContextForTest(runtime)
				var payload map[string]json.RawMessage
				require.NoError(t, json.Unmarshal([]byte(retryFilterJSONFixture(protocol, "empty")), &payload))
				delete(payload, "usage")
				body, marshalErr := json.Marshal(payload)
				require.NoError(t, marshalErr)
				resp := &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": {"application/json"}}, Body: io.NopCloser(strings.NewReader(string(body)))}
				input, output, err := callRetryFilterJSONConsumer(protocol, c, account, resp)
				if mode == "retry" {
					var candidate *UpstreamFailoverError
					require.ErrorAs(t, err, &candidate)
					require.Equal(t, RetryFilterEventEmptyCompletedResponse, candidate.RetryFilterEventType)
					require.True(t, candidate.RetryableOnSameAccount)
					require.False(t, c.Writer.Written())
				} else if protocol == "responses" {
					// The standard Responses consumer historically requires usage.
					require.EqualError(t, err, "parse response: invalid json response")
					require.False(t, c.Writer.Written())
				} else {
					require.NoError(t, err)
					require.Equal(t, http.StatusOK, rec.Code)
					require.Equal(t, string(body), rec.Body.String())
				}
				require.Zero(t, input)
				require.Zero(t, output)
				events := retryFilterAttemptEvents(t, c)
				if mode == "off" {
					require.Empty(t, events)
				} else {
					require.Len(t, events, 1)
					require.Equal(t, mode == "retry", events[0].RetryFilterBehaviorChanged)
				}
			})
		}
	}
}

func TestRetryFilterCompletedJSONFromSSE(t *testing.T) {
	for _, protocol := range []string{"responses", "responses-passthrough"} {
		for _, mode := range []string{"empty", "thinking", "off", "observe", "delta", "tool", "image", "compact", "no-terminal"} {
			t.Run(protocol+"/"+mode, func(t *testing.T) {
				rule := &model.RetryFilterRule{EmptyResponseMode: model.RetryFilterEmptyResponseBoth, Action: model.RetryFilterActionSameAccountRetry}
				if mode == "observe" {
					rule.Action = model.RetryFilterActionObserve
				}
				runtime := retryFilterAttemptRuntimeForTest(rule)
				if mode == "off" {
					runtime.enabled = func(context.Context) bool { return false }
				}
				c, rec, account := retryFilterAttemptContextForTest(runtime)
				c.Request.URL.Path = "/v1/responses"
				if mode == "compact" {
					c.Request.URL.Path = "/v1/responses/compact"
				}
				kind := "empty"
				if mode == "thinking" || mode == "tool" || mode == "image" {
					kind = mode
				}
				finalJSON := retryFilterJSONFixture("responses", kind)
				body := ""
				if mode == "delta" {
					body = "event: response.output_text.delta\ndata: {\"type\":\"response.output_text.delta\",\"item_id\":\"msg_1\",\"output_index\":0,\"content_index\":0,\"delta\":\"visible from earlier delta\"}\n\n"
				}
				body += "event: response.completed\ndata: {\"type\":\"response.completed\",\"response\":" + finalJSON + "}\n\n"
				if mode == "no-terminal" {
					body = "event: response.created\ndata: {\"type\":\"response.created\",\"response\":{\"id\":\"resp_1\",\"object\":\"response\",\"status\":\"in_progress\",\"output\":[]}}\n\n"
				}
				resp := &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": {"text/event-stream"}}, Body: io.NopCloser(strings.NewReader(body))}
				input, output, err := callRetryFilterJSONConsumer(protocol, c, account, resp)
				intercept := mode == "empty" || mode == "thinking"
				if intercept {
					var candidate *UpstreamFailoverError
					require.ErrorAs(t, err, &candidate)
					require.Equal(t, RetryFilterEventEmptyCompletedResponse, candidate.RetryFilterEventType)
					require.JSONEq(t, finalJSON, string(candidate.ResponseBody), "match the reconstructed JSON, never raw SSE")
					require.False(t, c.Writer.Written())
					require.Zero(t, input)
					require.Zero(t, output)
					runtime.Apply(c.Request.Context(), c, RetryFilterInput{Account: account, FailoverErr: candidate})
				} else {
					require.NoError(t, err)
					require.Equal(t, http.StatusOK, rec.Code)
					switch mode {
					case "delta":
						require.Contains(t, rec.Body.String(), "visible from earlier delta")
						require.True(t, RetryFilterResponseHasValidOutput("", rec.Body.Bytes()))
					case "no-terminal":
						require.Equal(t, body, rec.Body.String())
					default:
						require.JSONEq(t, finalJSON, rec.Body.String())
					}
					if mode != "no-terminal" {
						require.Equal(t, 11, input)
						require.Equal(t, 7, output)
					}
				}
				events := retryFilterAttemptEvents(t, c)
				if intercept || mode == "observe" {
					require.Len(t, events, 1)
					require.Equal(t, intercept, events[0].RetryFilterBehaviorChanged)
				} else {
					require.Empty(t, events)
				}
			})
		}
	}
}

func TestRetryFilterCompletedJSONGuard(t *testing.T) {
	for _, body := range []string{
		`not json`, `null`, `{}`, `{"error":{"message":"failure"}}`,
		`{"object":"response","status":"queued","output":[]}`,
		`{"object":"response","status":"in_progress","output":[]}`,
		`{"object":"response","status":"failed","output":[]}`,
		`{"object":"response","status":"incomplete","output":[]}`,
		`{"object":"response.compaction","output":[{"type":"compaction","encrypted_content":"opaque"}]}`,
		`{"object":"chat.completion","choices":[{"message":{"role":"assistant"},"finish_reason":null}]}`,
		`{"type":"message","content":[],"stop_reason":null}`,
	} {
		t.Run(body, func(t *testing.T) {
			runtime := retryFilterAttemptRuntimeForTest(&model.RetryFilterRule{EmptyResponseMode: model.RetryFilterEmptyResponseBoth, Action: model.RetryFilterActionTerminate})
			c, _, account := retryFilterAttemptContextForTest(runtime)
			require.Nil(t, prepareRetryFilterEmptyJSON(c.Request.Context(), c, account, &http.Response{StatusCode: 200}, []byte(body)))
			require.Empty(t, retryFilterAttemptEvents(t, c))
		})
	}
	for _, mode := range []string{"compact-path", "http-error"} {
		t.Run(mode, func(t *testing.T) {
			runtime := retryFilterAttemptRuntimeForTest(&model.RetryFilterRule{EmptyResponseMode: model.RetryFilterEmptyResponseBoth, Action: model.RetryFilterActionTerminate})
			c, _, account := retryFilterAttemptContextForTest(runtime)
			resp := &http.Response{StatusCode: 200}
			if mode == "compact-path" {
				c.Request.URL.Path = "/v1/responses/compact"
			} else {
				resp.StatusCode = 422
			}
			require.Nil(t, prepareRetryFilterEmptyJSON(c.Request.Context(), c, account, resp, []byte(retryFilterJSONFixture("responses", "empty"))))
			require.Empty(t, retryFilterAttemptEvents(t, c))
		})
	}
}
