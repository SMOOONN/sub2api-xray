//go:build unit

package service

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/model"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func retryFilterAttemptRuntimeForTest(rule *model.RetryFilterRule) *RetryFilterRuntime {
	rule.ID, rule.PresetID, rule.Enabled = 71, 1, true
	filters := newRetryFilterTestService(
		map[int64]*model.RetryFilterPreset{1: enabledRetryFilterPreset(1)},
		map[int64][]*model.RetryFilterRule{1: {rule}},
	)
	return NewRetryFilterRuntime(filters, func(context.Context) bool { return true }, nil)
}

func retryFilterAttemptContextForTest(runtime *RetryFilterRuntime) (*gin.Context, *httptest.ResponseRecorder, *Account) {
	gin.SetMode(gin.TestMode)
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/messages", nil)
	c.Request = c.Request.WithContext(WithRetryFilterAttempt(c.Request.Context(), runtime, &Group{ID: 9}, 9, "gpt-5.4"))
	account := forceChatMessagesFallbackAccount()
	account.Credentials["pool_mode"] = true
	account.Credentials["retry_filter_preset_id"] = int64(1)
	return c, rec, account
}

func retryFilterAttemptEvents(t *testing.T, c *gin.Context) []*OpsUpstreamErrorEvent {
	t.Helper()
	value, exists := c.Get(OpsUpstreamErrorsKey)
	if !exists {
		return nil
	}
	events, ok := value.([]*OpsUpstreamErrorEvent)
	require.True(t, ok)
	return events
}

func TestRetryFilterAttemptPrepareReusesPositiveAndNegativeVerdict(t *testing.T) {
	for _, matches := range []bool{false, true} {
		t.Run(map[bool]string{false: "unmatched", true: "matched"}[matches], func(t *testing.T) {
			rule := &model.RetryFilterRule{StatusCodes: []int{422}, Action: model.RetryFilterActionSameAccountRetry}
			runtime := retryFilterAttemptRuntimeForTest(rule)
			c, _, account := retryFilterAttemptContextForTest(runtime)
			calls := 0
			runtime.enabled = func(context.Context) bool { calls++; return true }
			status := 400
			if matches {
				status = 422
			}
			deadline := time.Now().Add(time.Minute)
			candidate := &UpstreamFailoverError{
				StatusCode: status, ResponseBody: []byte(`{"error":{"code":"raw_code"}}`),
				ResponseHeaders: http.Header{"Retry-After": {"3"}}, NextAccountAction: NextAccountStop,
				SameAccountRetryDeadline: deadline, SameAccountRetryMax: 2, SameAccountRetryDelay: 3 * time.Second,
			}
			first := prepareRetryFilterFailure(c.Request.Context(), c, account, candidate)
			if first != nil {
				recordRetryFilterFailure(c, account, candidate)
			}
			// A later handler sees the failure object, including a negative verdict.
			second := runtime.Apply(c.Request.Context(), c, RetryFilterInput{Account: account, Model: "different-model", FailoverErr: candidate})
			if first == nil {
				require.Nil(t, second)
			} else {
				require.Same(t, first, second)
			}
			require.Equal(t, 1, calls)
			require.Equal(t, matches, candidate.RetryableOnSameAccount)
			require.Equal(t, deadline, candidate.SameAccountRetryDeadline)
			require.Equal(t, 2, candidate.SameAccountRetryMax)
			require.Equal(t, 3*time.Second, candidate.SameAccountRetryDelay)
			require.Equal(t, "3", candidate.ResponseHeaders.Get("Retry-After"))
			if matches {
				require.Len(t, retryFilterAttemptEvents(t, c), 1)
			} else {
				require.Empty(t, retryFilterAttemptEvents(t, c))
			}
		})
	}
}

const retryFilterCCRole = "data: {\"id\":\"x\",\"choices\":[{\"index\":0,\"delta\":{\"role\":\"assistant\"}}]}\n\n"
const retryFilterCCThinking = "data: {\"id\":\"x\",\"choices\":[{\"index\":0,\"delta\":{\"reasoning_content\":\"thinking\"}}]}\n\n"
const retryFilterCCText = "data: {\"id\":\"x\",\"choices\":[{\"index\":0,\"delta\":{\"content\":\"visible\"}}]}\n\n"
const retryFilterCCError = "data: {\"error\":{\"code\":\"try_again\",\"message\":\"temporary upstream failure\"}}\n\n"

func TestRetryFilterCCAttemptInterceptsBeforeConverterCompletion(t *testing.T) {
	tests := []struct {
		name      string
		body      string
		readErr   error
		wantEvent RetryFilterEventType
	}{
		{name: "raw error", body: retryFilterCCError},
		{name: "role then error", body: retryFilterCCRole + retryFilterCCError},
		{name: "empty EOF", wantEvent: RetryFilterEventEmptyInterruptedStream},
		{name: "role then EOF", body: retryFilterCCRole, wantEvent: RetryFilterEventEmptyInterruptedStream},
		{name: "thinking then EOF", body: retryFilterCCThinking, wantEvent: RetryFilterEventEmptyInterruptedStream},
		{name: "DONE only", body: "data: [DONE]\n\n", wantEvent: RetryFilterEventEmptyCompletedResponse},
		{name: "finish only", body: "data: {\"choices\":[{\"index\":0,\"delta\":{},\"finish_reason\":\"stop\"}]}\n\n", wantEvent: RetryFilterEventEmptyCompletedResponse},
		{name: "read failure", body: retryFilterCCRole, readErr: io.ErrUnexpectedEOF, wantEvent: RetryFilterEventEmptyInterruptedStream},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			runtime := retryFilterAttemptRuntimeForTest(&model.RetryFilterRule{
				UpstreamCodes: []string{"try_again"}, UpstreamCodePaths: []string{"error.code"},
				EmptyResponseMode: model.RetryFilterEmptyResponseBoth, Action: model.RetryFilterActionSameAccountRetry,
			})
			c, rec, account := retryFilterAttemptContextForTest(runtime)
			var reader io.ReadCloser = io.NopCloser(strings.NewReader(tt.body))
			if tt.readErr != nil {
				reader = &errTailReader{data: []byte(tt.body), err: tt.readErr}
			}
			resp := &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": {"text/event-stream"}, "X-Request-Id": {"attempt-1"}}, Body: reader}
			svc := &OpenAIGatewayService{cfg: rawChatCompletionsTestConfig()}
			_, err := svc.streamChatCompletionsAsAnthropic(c, account, resp, "gpt-5.4", "gpt-5.4", "gpt-5.4", nil, nil, time.Now())
			var candidate *UpstreamFailoverError
			require.ErrorAs(t, err, &candidate)
			require.True(t, candidate.RetryableOnSameAccount)
			require.Equal(t, tt.wantEvent, candidate.RetryFilterEventType)
			require.Equal(t, "attempt-1", candidate.ResponseHeaders.Get("x-request-id"))
			require.False(t, candidate.SafeToFailoverAfterWrite, "rule retries must not inherit first-output's single-switch budget")
			require.Empty(t, rec.Body.String(), "neither the intermediate error nor a fabricated completion may escape")
			runtime.Apply(c.Request.Context(), c, RetryFilterInput{Account: account, FailoverErr: candidate})
			require.Len(t, retryFilterAttemptEvents(t, c), 1)
		})
	}
}

func TestRetryFilterCCAttemptLegacyModesAndSemanticBoundary(t *testing.T) {
	for _, mode := range []string{"off", "unmatched", "observe", "nonpool", "text", "tool"} {
		t.Run(mode, func(t *testing.T) {
			runtime := retryFilterAttemptRuntimeForTest(&model.RetryFilterRule{
				UpstreamCodes: []string{"try_again"}, UpstreamCodePaths: []string{"error.code"},
				EmptyResponseMode: model.RetryFilterEmptyResponseBoth, Action: model.RetryFilterActionSameAccountRetry,
			})
			if mode == "off" {
				runtime.enabled = func(context.Context) bool { return false }
			}
			if mode == "unmatched" {
				runtime.filters.local[1][0].UpstreamCodes = []string{"not_this_code"}
				runtime.filters.local[1][0].lowerUpstreamCodes = []string{"not_this_code"}
				runtime.filters.local[1][0].EmptyResponseMode = model.RetryFilterEmptyResponseNone
			}
			if mode == "observe" {
				runtime.filters.local[1][0].Action = model.RetryFilterActionObserve
			}
			c, rec, account := retryFilterAttemptContextForTest(runtime)
			if mode == "nonpool" {
				account.Credentials["pool_mode"] = false
			}
			body := retryFilterCCRole
			if mode == "text" {
				body += retryFilterCCText
			}
			if mode == "tool" {
				body += "data: {\"id\":\"x\",\"choices\":[{\"index\":0,\"delta\":{\"tool_calls\":[{\"index\":0,\"id\":\"call_1\",\"type\":\"function\",\"function\":{\"name\":\"lookup\",\"arguments\":\"{}\"}}]}}]}\n\n"
			}
			body += retryFilterCCError + "data: [DONE]\n\n"
			resp := &http.Response{StatusCode: 200, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(body))}
			svc := &OpenAIGatewayService{cfg: rawChatCompletionsTestConfig()}
			_, err := svc.streamChatCompletionsAsAnthropic(c, account, resp, "gpt-5.4", "gpt-5.4", "gpt-5.4", nil, nil, time.Now())
			var failover *UpstreamFailoverError
			require.False(t, errors.As(err, &failover))
			require.NoError(t, err, "the CC converter retains its legacy completion when interception cannot apply")
			require.Contains(t, rec.Body.String(), "message_stop")
			if mode == "observe" {
				events := retryFilterAttemptEvents(t, c)
				require.Len(t, events, 1)
				require.False(t, events[0].RetryFilterBehaviorChanged)
			} else {
				require.Empty(t, retryFilterAttemptEvents(t, c))
			}
		})
	}
}

func TestRetryFilterHTTP422RawCodeThenSuccess(t *testing.T) {
	runtime := retryFilterAttemptRuntimeForTest(&model.RetryFilterRule{
		StatusCodes: []int{422}, UpstreamCodes: []string{"try_again"}, UpstreamCodePaths: []string{"error.code"},
		MatchMode: model.MatchModeAll, Action: model.RetryFilterActionSameAccountRetry,
	})
	c, rec, account := retryFilterAttemptContextForTest(runtime)
	body := []byte(`{"model":"gpt-5.4","max_tokens":32,"messages":[{"role":"user","content":"hello"}],"stream":false}`)
	rawError := `{"error":{"code":"try_again","message":"temporary failure"}}`
	upstream := &httpUpstreamRecorder{resp: &http.Response{
		StatusCode: 422, Header: http.Header{"Content-Type": {"application/json"}, "Retry-After": {"2"}},
		Body: io.NopCloser(strings.NewReader(rawError)),
	}}
	svc := &OpenAIGatewayService{cfg: rawChatCompletionsTestConfig(), httpUpstream: upstream}
	_, err := svc.ForwardAsAnthropic(c.Request.Context(), c, account, body, "", "")
	var candidate *UpstreamFailoverError
	require.ErrorAs(t, err, &candidate)
	require.True(t, candidate.RetryableOnSameAccount)
	require.Equal(t, rawError, string(candidate.ResponseBody))
	require.Equal(t, "2", candidate.ResponseHeaders.Get("Retry-After"))
	require.Empty(t, rec.Body.String())
	runtime.Apply(c.Request.Context(), c, RetryFilterInput{Account: account, FailoverErr: candidate})

	upstream.resp = &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": {"application/json"}}, Body: io.NopCloser(strings.NewReader(
		`{"id":"ok","object":"chat.completion","model":"gpt-5.4","choices":[{"index":0,"message":{"role":"assistant","content":"recovered"},"finish_reason":"stop"}],"usage":{"prompt_tokens":2,"completion_tokens":1}}`,
	))}
	c.Request = c.Request.WithContext(WithRetryFilterAttempt(c.Request.Context(), runtime, &Group{ID: 9}, 9, "gpt-5.4"))
	_, err = svc.ForwardAsAnthropic(c.Request.Context(), c, account, body, "", "")
	require.NoError(t, err)
	require.Contains(t, rec.Body.String(), "recovered")
	require.NotContains(t, rec.Body.String(), "try_again")
	require.Len(t, retryFilterAttemptEvents(t, c), 1)
}

func TestRetryFilterMessagesResponsesCreatedThenFailed(t *testing.T) {
	runtime := retryFilterAttemptRuntimeForTest(&model.RetryFilterRule{
		UpstreamCodes: []string{"try_again"}, UpstreamCodePaths: []string{"response.error.code"}, Action: model.RetryFilterActionSameAccountRetry,
	})
	c, rec, account := retryFilterAttemptContextForTest(runtime)
	body := "data: {\"type\":\"response.created\",\"response\":{\"id\":\"r1\",\"object\":\"response\",\"status\":\"in_progress\",\"model\":\"gpt-5.4\"}}\n\n" +
		"data: {\"type\":\"response.failed\",\"response\":{\"id\":\"r1\",\"status\":\"failed\",\"error\":{\"code\":\"try_again\",\"message\":\"transient\"}}}\n\n"
	resp := &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": {"text/event-stream"}}, Body: io.NopCloser(strings.NewReader(body))}
	svc := &OpenAIGatewayService{cfg: rawChatCompletionsTestConfig()}
	_, err := svc.handleAnthropicStreamingResponse(resp, c, account, "gpt-5.4", "gpt-5.4", "gpt-5.4", time.Now())
	var candidate *UpstreamFailoverError
	require.ErrorAs(t, err, &candidate)
	require.True(t, candidate.RetryableOnSameAccount)
	require.Contains(t, string(candidate.ResponseBody), `"try_again"`)
	require.Empty(t, rec.Body.String())
	require.Len(t, retryFilterAttemptEvents(t, c), 1)
}

func TestRetryFilterNativeAnthropicMetaThenEnd(t *testing.T) {
	meta := "event: message_start\ndata: {\"type\":\"message_start\",\"message\":{\"id\":\"m1\",\"role\":\"assistant\",\"content\":[]}}\n\n"
	for _, ending := range []string{"EOF", "completed", "read_error", "stream_error"} {
		t.Run(ending, func(t *testing.T) {
			runtime := retryFilterAttemptRuntimeForTest(&model.RetryFilterRule{EmptyResponseMode: model.RetryFilterEmptyResponseBoth,
				UpstreamCodes: []string{"try_again"}, UpstreamCodePaths: []string{"error.code"}, Action: model.RetryFilterActionSameAccountRetry})
			c, rec, account := retryFilterAttemptContextForTest(runtime)
			body := meta
			if ending == "completed" {
				body += "event: message_stop\ndata: {\"type\":\"message_stop\"}\n\n"
			}
			if ending == "stream_error" {
				body += "event: error\ndata: {\"type\":\"error\",\"error\":{\"code\":\"try_again\",\"message\":\"temporary failure\"}}\n\n"
			}
			var reader io.ReadCloser = io.NopCloser(strings.NewReader(body))
			if ending == "read_error" {
				reader = &errTailReader{data: []byte(body), err: io.ErrUnexpectedEOF}
			}
			resp := &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": {"text/event-stream"}}, Body: reader}
			svc := &OpenAIGatewayService{cfg: rawChatCompletionsTestConfig()}
			_, err := svc.handleNativeAnthropicStreamingResponse(c.Request.Context(), resp, c, account, "gpt-5.4", "gpt-5.4", "gpt-5.4", nil, time.Now())
			var candidate *UpstreamFailoverError
			require.ErrorAs(t, err, &candidate)
			require.True(t, candidate.RetryableOnSameAccount)
			require.Empty(t, rec.Body.String())
			require.Len(t, retryFilterAttemptEvents(t, c), 1)
		})
	}
}

func TestRetryFilterHeartbeatBytesAccumulateAcrossAttempts(t *testing.T) {
	c, rec, _ := retryFilterAttemptContextForTest(nil)
	for attempt := 0; attempt < 3; attempt++ {
		before := OpenAICompactKeepaliveAdjustedWrittenSize(c)
		stop := startOpenAISSEKeepalive(c, time.Hour)
		value, ok := c.Get(openAICompactSSEKeepaliveKey)
		require.True(t, ok)
		keepalive := value.(*openAICompactSSEKeepalive)
		require.True(t, keepalive.beat())
		stop()
		require.Equal(t, -1, before)
		require.Equal(t, before, OpenAICompactKeepaliveAdjustedWrittenSize(c))
		require.True(t, StopOpenAICompactSSEKeepaliveCommitted(c))
	}
	require.Equal(t, 3, strings.Count(rec.Body.String(), ": keepalive"))
	_, err := c.Writer.WriteString("event: message_start\ndata: {}\n\n")
	require.NoError(t, err)
	require.Equal(t, len("event: message_start\ndata: {}\n\n"), OpenAICompactKeepaliveAdjustedWrittenSize(c))
}

func TestRetryFilterCCResourceLimitIsNotAnEmptyUpstreamFailure(t *testing.T) {
	runtime := retryFilterAttemptRuntimeForTest(&model.RetryFilterRule{EmptyResponseMode: model.RetryFilterEmptyResponseBoth, Action: model.RetryFilterActionSameAccountRetry})
	c, _, account := retryFilterAttemptContextForTest(runtime)
	body := "data: {\"choices\":[{\"index\":0,\"delta\":{\"reasoning_content\":\"" + strings.Repeat("x", openAIFirstOutputStageMaxBytes) + "\"}}]}\n\n"
	resp := &http.Response{StatusCode: 200, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(body))}
	cfg := rawChatCompletionsTestConfig()
	cfg.Gateway.MaxLineSize = 2 * openAIFirstOutputStageMaxBytes
	svc := &OpenAIGatewayService{cfg: cfg}
	_, err := svc.streamChatCompletionsAsAnthropic(c, account, resp, "gpt-5.4", "gpt-5.4", "gpt-5.4", nil, nil, time.Now())
	require.ErrorIs(t, err, errOpenAIFirstOutputStageLimit)
	var failure *UpstreamFailoverError
	require.False(t, errors.As(err, &failure))
	require.Empty(t, retryFilterAttemptEvents(t, c))
}

func TestRetryFilterCCResponsesAttemptInterceptsBeforeCompletion(t *testing.T) {
	for _, body := range []string{retryFilterCCRole + retryFilterCCError, retryFilterCCThinking, "data: [DONE]\n\n"} {
		runtime := retryFilterAttemptRuntimeForTest(&model.RetryFilterRule{EmptyResponseMode: model.RetryFilterEmptyResponseBoth,
			UpstreamCodes: []string{"try_again"}, UpstreamCodePaths: []string{"error.code"}, Action: model.RetryFilterActionSameAccountRetry})
		c, rec, account := retryFilterAttemptContextForTest(runtime)
		resp := &http.Response{StatusCode: 200, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(body))}
		svc := &OpenAIGatewayService{cfg: rawChatCompletionsTestConfig()}
		_, err := svc.streamChatCompletionsAsResponses(c, account, resp, "gpt-5.4", nil, nil, false, nil, "gpt-5.4", "gpt-5.4", nil, nil, time.Now())
		var failure *UpstreamFailoverError
		require.ErrorAs(t, err, &failure)
		require.True(t, failure.RetryableOnSameAccount)
		require.Empty(t, rec.Body.String())
		require.Len(t, retryFilterAttemptEvents(t, c), 1)
	}
}

func TestRetryFilterStagesOnlyPotentialExecutableResponseRules(t *testing.T) {
	for _, rule := range []*model.RetryFilterRule{
		{StatusCodes: []int{500}, Action: model.RetryFilterActionObserve},
		{EmptyRequestMode: model.RetryFilterEmptyRequestBoth, Action: model.RetryFilterActionTerminate},
		{EmptyRequestMode: model.RetryFilterEmptyRequestBoth, StatusCodes: []int{500}, MatchMode: model.MatchModeAll, Action: model.RetryFilterActionTerminate},
		{Models: []string{"not-this-model"}, StatusCodes: []int{500}, MatchMode: model.MatchModeAll, Action: model.RetryFilterActionSameAccountRetry},
	} {
		runtime := retryFilterAttemptRuntimeForTest(rule)
		c, _, account := retryFilterAttemptContextForTest(runtime)
		require.False(t, retryFilterShouldStage(c.Request.Context(), account))
	}
}

func TestRetryFilterResponsesThinkingThenRawFailure(t *testing.T) {
	runtime := retryFilterAttemptRuntimeForTest(&model.RetryFilterRule{
		UpstreamCodes: []string{"try_again"}, UpstreamCodePaths: []string{"response.error.code"}, Action: model.RetryFilterActionSameAccountRetry,
	})
	c, rec, account := retryFilterAttemptContextForTest(runtime)
	body := "data: {\"type\":\"response.created\",\"response\":{\"id\":\"r1\",\"status\":\"in_progress\"}}\n\n" +
		"data: {\"type\":\"response.reasoning_summary_text.delta\",\"item_id\":\"thinking-1\",\"delta\":\"still thinking\"}\n\n" +
		"data: {\"type\":\"response.failed\",\"response\":{\"id\":\"r1\",\"status\":\"failed\",\"error\":{\"code\":\"try_again\",\"message\":\"transient\"}}}\n\n"
	resp := &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": {"text/event-stream"}}, Body: io.NopCloser(strings.NewReader(body))}
	svc := &OpenAIGatewayService{cfg: rawChatCompletionsTestConfig(), toolCorrector: NewCodexToolCorrector()}
	_, err := svc.handleStreamingResponse(c.Request.Context(), resp, c, account, time.Now(), "gpt-5.4", "gpt-5.4")
	var candidate *UpstreamFailoverError
	require.ErrorAs(t, err, &candidate)
	require.True(t, candidate.RetryableOnSameAccount)
	require.Contains(t, string(candidate.ResponseBody), `"try_again"`)
	require.Empty(t, rec.Body.String())
	require.Len(t, retryFilterAttemptEvents(t, c), 1)
}

func TestRetryFilterOpsObserveDoesNotRelabelPreviousUnmatchedAttempt(t *testing.T) {
	runtime := retryFilterAttemptRuntimeForTest(&model.RetryFilterRule{EmptyResponseMode: model.RetryFilterEmptyResponseBoth, Action: model.RetryFilterActionObserve})
	c, _, account := retryFilterAttemptContextForTest(runtime)
	svc := &OpenAIGatewayService{cfg: rawChatCompletionsTestConfig(), toolCorrector: NewCodexToolCorrector()}
	resp := &http.Response{StatusCode: 502, Header: http.Header{"X-Request-Id": {"first-http"}}}
	first, _ := svc.failoverOpenAIUpstreamHTTPError(c.Request.Context(), c, account, resp, []byte(`{"error":{"message":"first failure"}}`), "first failure", "gpt-5.4")
	require.NotNil(t, first)
	events := retryFilterAttemptEvents(t, c)
	require.Len(t, events, 1)
	require.Zero(t, events[0].RetryFilterRuleID)
	before, err := json.Marshal(events[0])
	require.NoError(t, err)

	c.Request = c.Request.WithContext(WithRetryFilterAttempt(c.Request.Context(), runtime, &Group{ID: 9}, 9, "gpt-5.4"))
	resp = &http.Response{StatusCode: 200, Header: http.Header{"X-Request-Id": {"second-stream"}}, Body: io.NopCloser(strings.NewReader(
		"data: {\"type\":\"response.reasoning_summary_text.delta\",\"delta\":\"thinking\"}\n\n",
	))}
	_, err = svc.handleStreamingResponse(c.Request.Context(), resp, c, account, time.Now(), "gpt-5.4", "gpt-5.4")
	require.Error(t, err)
	events = retryFilterAttemptEvents(t, c)
	require.Len(t, events, 2)
	after, err := json.Marshal(events[0])
	require.NoError(t, err)
	require.JSONEq(t, string(before), string(after))
	require.Equal(t, int64(71), events[1].RetryFilterRuleID)
	require.Equal(t, string(RetryFilterEventEmptyInterruptedStream), events[1].RetryFilterEventType)
	require.Equal(t, "second-stream", events[1].UpstreamRequestID)
}

func TestRetryFilterOpsFailureSnapshotsRemainSeparate(t *testing.T) {
	runtime := retryFilterAttemptRuntimeForTest(&model.RetryFilterRule{StatusCodes: []int{502}, Action: model.RetryFilterActionSameAccountRetry})
	c, _, account := retryFilterAttemptContextForTest(runtime)
	svc := &OpenAIGatewayService{cfg: rawChatCompletionsTestConfig()}
	for i := 0; i < 2; i++ {
		c.Request = c.Request.WithContext(WithRetryFilterAttempt(c.Request.Context(), runtime, &Group{ID: 9}, 9, "gpt-5.4"))
		resp := &http.Response{StatusCode: 502, Header: http.Header{}}
		failure, _ := svc.failoverOpenAIUpstreamHTTPError(c.Request.Context(), c, account, resp, []byte(`{"error":{"message":"failure"}}`), "failure", "gpt-5.4")
		runtime.Apply(c.Request.Context(), c, RetryFilterInput{Account: account, FailoverErr: failure})
		runtime.Apply(c.Request.Context(), c, RetryFilterInput{Account: account, FailoverErr: failure})
		require.Len(t, retryFilterAttemptEvents(t, c), i+1)
	}
	events := retryFilterAttemptEvents(t, c)
	require.NotSame(t, events[0], events[1])
	require.NotSame(t, events[0].retryFilterAttempt, events[1].retryFilterAttempt)
}

func TestRetryFilterHTTPNegativeVerdictSurvivesCompatFallback(t *testing.T) {
	runtime := retryFilterAttemptRuntimeForTest(&model.RetryFilterRule{StatusCodes: []int{422}, Action: model.RetryFilterActionSameAccountRetry})
	checks := 0
	runtime.enabled = func(context.Context) bool {
		checks++
		return checks > 1
	}
	c, rec, account := retryFilterAttemptContextForTest(runtime)
	upstream := &httpUpstreamRecorder{resp: &http.Response{StatusCode: 422, Header: http.Header{"Content-Type": {"application/json"}},
		Body: io.NopCloser(strings.NewReader(`{"error":{"message":"unprocessable input"}}`))}}
	svc := &OpenAIGatewayService{cfg: rawChatCompletionsTestConfig(), httpUpstream: upstream}
	_, err := svc.forwardResponsesViaRawChatCompletions(c.Request.Context(), c, account, []byte(`{"model":"gpt-5.4","input":"hello","stream":false}`), time.Now())
	require.Error(t, err)
	var failure *UpstreamFailoverError
	require.False(t, errors.As(err, &failure))
	require.Equal(t, 1, checks, "the helper and fallback must share the already evaluated negative verdict")
	require.NotEmpty(t, rec.Body.String())
}
