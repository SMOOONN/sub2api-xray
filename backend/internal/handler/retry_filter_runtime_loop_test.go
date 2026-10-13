//go:build unit

package handler

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/model"
	"github.com/Wei-Shaw/sub2api/internal/pkg/openai_compat"
	"github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type retryFilterLoopSlots struct {
	fakeConcurrencyCache
	mu          sync.Mutex
	held        bool
	forceWait   bool
	waitEntered bool
	waitForBeat <-chan struct{}
	releases    int
}

func (s *retryFilterLoopSlots) AcquireAccountSlot(context.Context, int64, int, string) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.held || (s.forceWait && !s.waitEntered) {
		return false, nil
	}
	if s.waitForBeat != nil {
		select {
		case <-s.waitForBeat:
			s.waitForBeat = nil
		default:
			return false, nil
		}
	}
	s.held = true
	return true, nil
}

func (s *retryFilterLoopSlots) ReleaseAccountSlot(context.Context, int64, string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.held = false
	s.releases++
	return nil
}

func (s *retryFilterLoopSlots) IncrementAccountWaitCount(context.Context, int64, int) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.waitEntered = true
	return true, nil
}

type retryFilterLoopUpstream struct {
	service.HTTPUpstream
	hits     []int64
	failures int
	beat     <-chan struct{}
	status   int
}

func (u *retryFilterLoopUpstream) Do(req *http.Request, _ string, accountID int64, _ int) (*http.Response, error) {
	u.hits = append(u.hits, accountID)
	if len(u.hits) <= u.failures {
		if u.status > 0 {
			return &http.Response{StatusCode: u.status, Header: http.Header{"Content-Type": {"application/json"}},
				Body: io.NopCloser(strings.NewReader(`{"error":{"code":"try_again","message":"transient"}}`))}, nil
		}
		return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": {"text/event-stream"}},
			Body: &retryFilterWaitForBeatBody{reader: strings.NewReader("data: {\"error\":{\"code\":\"try_again\",\"message\":\"transient\"}}\n\n"), beat: u.beat}}, nil
	}
	return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": {"text/event-stream"}},
		Body: io.NopCloser(strings.NewReader("data: {\"id\":\"recovered\",\"choices\":[{\"index\":0,\"delta\":{\"role\":\"assistant\",\"content\":\"recovered\"},\"finish_reason\":\"stop\"}]}\n\ndata: [DONE]\n\n"))}, nil
}

// The fake upstream releases its error only after the actual gateway has sent
// a heartbeat. This proves the writer/handler seam without relying on sleeps.
type retryFilterWaitForBeatBody struct {
	reader *strings.Reader
	beat   <-chan struct{}
	waited bool
}

func (r *retryFilterWaitForBeatBody) Read(p []byte) (int, error) {
	if !r.waited {
		r.waited = true
		timer := time.NewTimer(5 * time.Second)
		defer timer.Stop()
		select {
		case <-r.beat:
		case <-timer.C:
			return 0, errors.New("gateway did not send the expected heartbeat")
		}
	}
	return r.reader.Read(p)
}

func (r *retryFilterWaitForBeatBody) Close() error { return nil }

type retryFilterBeatRecorder struct {
	*httptest.ResponseRecorder
	beat chan struct{}
}

func (r *retryFilterBeatRecorder) Write(p []byte) (int, error) {
	n, err := r.ResponseRecorder.Write(p)
	if bytes.HasPrefix(p, []byte(":")) || bytes.HasPrefix(p, []byte(SSEPingFormatClaude)) {
		select {
		case r.beat <- struct{}{}:
		default:
		}
	}
	return n, err
}

func newRetryFilterLoopHandler(t *testing.T, rule *model.RetryFilterRule, upstream *retryFilterLoopUpstream, slots *retryFilterLoopSlots) (*gin.Engine, *[][]*service.OpsUpstreamErrorEvent) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	groupID := int64(901)
	repo := &grokCredentialHandlerRepo{accounts: []service.Account{{
		ID: 801, Name: "pool", Platform: service.PlatformOpenAI, Type: service.AccountTypeAPIKey,
		Status: service.StatusActive, Schedulable: true, Concurrency: 1, Priority: 1,
		Credentials: map[string]any{"api_key": "test-key", "base_url": "https://upstream.example", "pool_mode": true, "retry_filter_preset_id": int64(1)},
		Extra:       map[string]any{openai_compat.ExtraKeyResponsesMode: string(openai_compat.ResponsesSupportModeForceChatCompletions)},
	}}}
	cfg := &config.Config{RunMode: config.RunModeSimple}
	cfg.Gateway.MaxAccountSwitches = 2
	cfg.Gateway.StreamKeepaliveInterval = 1
	cfg.Gateway.Scheduling.FallbackWaitTimeout = 5 * time.Second
	cfg.Concurrency.PingInterval = 1
	concurrency := service.NewConcurrencyService(slots)
	billingCache := service.NewBillingCacheService(nil, nil, nil, nil, nil, nil, cfg, nil)
	t.Cleanup(billingCache.Stop)
	gateway := service.NewOpenAIGatewayService(
		repo, nil, nil, nil, nil, nil, nil, cfg, nil, concurrency,
		service.NewBillingService(cfg, nil), nil, billingCache, upstream,
		&service.DeferredService{}, nil, nil, nil, nil, nil, nil, nil,
	)
	h := NewOpenAIGatewayHandler(gateway, concurrency, billingCache, &service.APIKeyService{}, nil, nil, nil, nil, cfg)
	h.retryFilterRuntime = newHandlerRetryFilterRuntime(rule)
	apiKey := &service.APIKey{ID: 902, GroupID: &groupID, User: &service.User{ID: 903, Status: service.StatusActive},
		Group: &service.Group{ID: groupID, Platform: service.PlatformOpenAI, Status: service.StatusActive, AllowMessagesDispatch: true}}
	var requests [][]*service.OpsUpstreamErrorEvent
	router := gin.New()
	router.Use(func(c *gin.Context) {
		c.Set(string(middleware.ContextKeyAPIKey), apiKey)
		c.Set(string(middleware.ContextKeyUser), middleware.AuthSubject{UserID: apiKey.User.ID, Concurrency: 1})
		c.Next()
		value, _ := c.Get(service.OpsUpstreamErrorsKey)
		events, _ := value.([]*service.OpsUpstreamErrorEvent)
		requests = append(requests, events)
	})
	// Production middleware also applies request-local route defaults. The
	// handler and service still execute their real selection/forwarding loops.
	router.POST("/v1/messages", h.Messages)
	router.POST("/v1/responses", h.Responses)
	return router, &requests
}

const retryFilterLoopBody = `{"model":"gpt-5.4","max_tokens":16,"messages":[{"role":"user","content":"hello"}],"stream":true}`

func TestRetryFilterHandlerTwoHeartbeatFailuresThenSuccess(t *testing.T) {
	delay, count := 0, 2
	beat := make(chan struct{}, 4)
	upstream := &retryFilterLoopUpstream{failures: 2, beat: beat}
	slots := &retryFilterLoopSlots{}
	router, requests := newRetryFilterLoopHandler(t, &model.RetryFilterRule{ID: 81, PresetID: 1, Enabled: true,
		UpstreamCodes: []string{"try_again"}, UpstreamCodePaths: []string{"error.code"}, Action: model.RetryFilterActionSameAccountRetry,
		RetryCountOverride: &count, RetryDelayMs: &delay}, upstream, slots)
	rec := &retryFilterBeatRecorder{ResponseRecorder: httptest.NewRecorder(), beat: beat}
	req := httptest.NewRequest(http.MethodPost, "/v1/messages", strings.NewReader(retryFilterLoopBody))
	router.ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	require.Equal(t, []int64{801, 801, 801}, upstream.hits)
	require.Equal(t, http.StatusOK, rec.Code)
	require.GreaterOrEqual(t, strings.Count(rec.Body.String(), ": keepalive"), 2)
	require.Contains(t, rec.Body.String(), "recovered")
	require.NotContains(t, rec.Body.String(), "try_again")
	require.NotContains(t, rec.Body.String(), "event: error")
	require.Equal(t, 1, strings.Count(rec.Body.String(), "event: message_start"))
	require.Len(t, *requests, 1)
	require.Len(t, (*requests)[0], 2)
	for _, event := range (*requests)[0] {
		require.Equal(t, int64(801), event.AccountID)
		require.Equal(t, int64(81), event.RetryFilterRuleID)
	}
	require.False(t, slots.held)
	require.Equal(t, 3, slots.releases)
}

func TestRetryFilterHandlerHTTP422ThenSuccess(t *testing.T) {
	delay, count := 0, 1
	upstream := &retryFilterLoopUpstream{failures: 1, status: 422}
	router, requests := newRetryFilterLoopHandler(t, &model.RetryFilterRule{ID: 82, PresetID: 1, Enabled: true,
		StatusCodes: []int{422}, Action: model.RetryFilterActionSameAccountRetry, RetryCountOverride: &count, RetryDelayMs: &delay}, upstream, &retryFilterLoopSlots{})
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/v1/messages", strings.NewReader(retryFilterLoopBody)))
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	require.Equal(t, []int64{801, 801}, upstream.hits)
	require.Contains(t, rec.Body.String(), "recovered")
	require.NotContains(t, rec.Body.String(), "try_again")
	require.Len(t, (*requests)[0], 1)
	require.Equal(t, int64(82), (*requests)[0][0].RetryFilterRuleID)
}

func TestRetryFilterHandlerTerminateAfterHeartbeatUsesSSE(t *testing.T) {
	status := 418
	message := "filtered terminal failure"
	beat := make(chan struct{}, 2)
	upstream := &retryFilterLoopUpstream{failures: 1, beat: beat}
	router, requests := newRetryFilterLoopHandler(t, &model.RetryFilterRule{ID: 83, PresetID: 1, Enabled: true,
		UpstreamCodes: []string{"try_again"}, UpstreamCodePaths: []string{"error.code"}, Action: model.RetryFilterActionTerminate,
		TerminateStatusCode: &status, TerminateMessage: &message}, upstream, &retryFilterLoopSlots{})
	rec := &retryFilterBeatRecorder{ResponseRecorder: httptest.NewRecorder(), beat: beat}
	router.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/v1/messages", strings.NewReader(retryFilterLoopBody)))
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	require.Equal(t, []int64{801}, upstream.hits)
	require.Equal(t, http.StatusOK, rec.Code, "a heartbeat already committed HTTP 200")
	require.Equal(t, "text/event-stream", rec.Header().Get("Content-Type"))
	require.Equal(t, 1, strings.Count(rec.Body.String(), "event: error"))
	require.Contains(t, rec.Body.String(), message)
	require.NotContains(t, rec.Body.String(), "message_stop")
	require.Len(t, (*requests)[0], 1)
}

func TestRetryFilterHandlerEmptyTerminateReleasesAcquiredAndWaitedSlot(t *testing.T) {
	for _, wait := range []bool{false, true} {
		t.Run(map[bool]string{false: "scheduler acquired", true: "wait then acquired"}[wait], func(t *testing.T) {
			slots := &retryFilterLoopSlots{forceWait: wait}
			upstream := &retryFilterLoopUpstream{}
			router, _ := newRetryFilterLoopHandler(t, &model.RetryFilterRule{ID: 84, PresetID: 1, Enabled: true,
				EmptyRequestMode: model.RetryFilterEmptyRequestStructured, Action: model.RetryFilterActionTerminate}, upstream, slots)
			rec := httptest.NewRecorder()
			router.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/v1/messages", strings.NewReader(`{"model":"gpt-5","messages":[],"stream":false}`)))
			require.Equal(t, http.StatusBadRequest, rec.Code)
			require.Empty(t, upstream.hits)
			require.False(t, slots.held, "the empty-request early return must release its account slot")
			require.Equal(t, 1, slots.releases)
			require.Equal(t, wait, slots.waitEntered)
			next := httptest.NewRecorder()
			router.ServeHTTP(next, httptest.NewRequest(http.MethodPost, "/v1/messages", strings.NewReader(retryFilterLoopBody)))
			require.Equal(t, http.StatusOK, next.Code, next.Body.String())
			require.Contains(t, next.Body.String(), "recovered")
			require.False(t, slots.held)
			require.Equal(t, 2, slots.releases)
		})
	}
}

func TestRetryFilterResponsesEmptyTerminateReleasesAcquiredAndWaitedSlot(t *testing.T) {
	for _, wait := range []bool{false, true} {
		t.Run(map[bool]string{false: "scheduler acquired", true: "wait then acquired"}[wait], func(t *testing.T) {
			slots := &retryFilterLoopSlots{forceWait: wait}
			upstream := &retryFilterLoopUpstream{}
			status, message := http.StatusConflict, "empty input stopped"
			router, requests := newRetryFilterLoopHandler(t, &model.RetryFilterRule{ID: 85, PresetID: 1, Enabled: true,
				EmptyRequestMode: model.RetryFilterEmptyRequestStructured, Action: model.RetryFilterActionTerminate,
				TerminateStatusCode: &status, TerminateMessage: &message}, upstream, slots)
			for attempt := 1; attempt <= 2; attempt++ {
				rec := httptest.NewRecorder()
				router.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(`{"model":"gpt-5","input":[],"stream":false}`)))
				require.Equal(t, status, rec.Code, rec.Body.String())
				require.Contains(t, rec.Body.String(), message)
				require.Empty(t, upstream.hits)
				require.False(t, slots.held, "the empty-request early return must release its account slot")
				require.Equal(t, attempt, slots.releases)
				require.Equal(t, wait, slots.waitEntered)
				require.Len(t, (*requests)[attempt-1], 1)
				require.Equal(t, int64(85), (*requests)[attempt-1][0].RetryFilterRuleID)
			}
		})
	}
}

func TestRetryFilterResponsesEmptyTerminateAfterWaitHeartbeat(t *testing.T) {
	beat := make(chan struct{}, 1)
	slots := &retryFilterLoopSlots{waitForBeat: beat}
	upstream := &retryFilterLoopUpstream{}
	status, message := http.StatusConflict, "empty input stopped after heartbeat"
	router, requests := newRetryFilterLoopHandler(t, &model.RetryFilterRule{ID: 86, PresetID: 1, Enabled: true,
		EmptyRequestMode: model.RetryFilterEmptyRequestStructured, Action: model.RetryFilterActionTerminate,
		TerminateStatusCode: &status, TerminateMessage: &message}, upstream, slots)
	rec := &retryFilterBeatRecorder{ResponseRecorder: httptest.NewRecorder(), beat: beat}
	router.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(`{"model":"gpt-5","input":[],"stream":true}`)))
	require.Equal(t, http.StatusOK, rec.Code, "the actual slot-wait heartbeat committed HTTP 200")
	require.Contains(t, rec.Body.String(), ":\n\n")
	require.Equal(t, 1, strings.Count(rec.Body.String(), "event: response.failed\n"))
	require.Contains(t, rec.Body.String(), message)
	require.Empty(t, upstream.hits)
	require.False(t, slots.held)
	require.Equal(t, 1, slots.releases)
	require.Len(t, (*requests)[0], 1)
}

func TestRetryFilterGenericChatEmptyTerminateAfterWaitHeartbeat(t *testing.T) {
	gin.SetMode(gin.TestMode)
	beat := make(chan struct{}, 1)
	slots := &retryFilterLoopSlots{waitForBeat: beat}
	upstream := &retryFilterLoopUpstream{}
	groupID := int64(905)
	group := &service.Group{ID: groupID, Hydrated: true, Platform: service.PlatformAnthropic, Status: service.StatusActive}
	account := &service.Account{ID: 804, Name: "cc-pool", Platform: service.PlatformAnthropic, Type: service.AccountTypeAPIKey,
		Status: service.StatusActive, Schedulable: true, Concurrency: 1, Priority: 1,
		Credentials:   map[string]any{"api_key": "test-key", "pool_mode": true, "retry_filter_preset_id": int64(1)},
		AccountGroups: []service.AccountGroup{{AccountID: 804, GroupID: groupID}}}
	cfg := &config.Config{RunMode: config.RunModeSimple}
	concurrency := service.NewConcurrencyService(slots)
	cfg.Gateway.Scheduling.FallbackWaitTimeout = 5 * time.Second
	billingCache := service.NewBillingCacheService(nil, nil, nil, nil, nil, nil, cfg, nil)
	t.Cleanup(billingCache.Stop)
	snapshot := service.NewSchedulerSnapshotService(&fakeSchedulerCache{accounts: []*service.Account{account}}, nil, nil, nil, nil)
	gateway := service.NewGatewayService(
		&grokCredentialHandlerRepo{accounts: []service.Account{*account}}, &fakeGroupRepo{group: group}, nil, nil, nil, nil, nil, nil, cfg,
		snapshot, concurrency, nil, nil, billingCache, nil, upstream, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil,
	)
	status, message := http.StatusConflict, "empty messages stopped after heartbeat"
	h := &GatewayHandler{gatewayService: gateway, billingCacheService: billingCache, cfg: cfg, maxAccountSwitches: 1,
		concurrencyHelper: NewConcurrencyHelper(concurrency, SSEPingFormatClaude, 5*time.Millisecond),
		retryFilterRuntime: newHandlerRetryFilterRuntime(&model.RetryFilterRule{ID: 87, PresetID: 1, Enabled: true,
			EmptyRequestMode: model.RetryFilterEmptyRequestStructured, Action: model.RetryFilterActionTerminate,
			TerminateStatusCode: &status, TerminateMessage: &message})}
	apiKey := &service.APIKey{ID: 906, GroupID: &groupID, Group: group, User: &service.User{ID: 907, Status: service.StatusActive}}
	router := gin.New()
	router.POST("/v1/chat/completions", func(c *gin.Context) {
		c.Set(string(middleware.ContextKeyAPIKey), apiKey)
		c.Set(string(middleware.ContextKeyUser), middleware.AuthSubject{UserID: apiKey.User.ID, Concurrency: 1})
		h.ChatCompletions(c)
	})
	rec := &retryFilterBeatRecorder{ResponseRecorder: httptest.NewRecorder(), beat: beat}
	router.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(`{"model":"claude-test","messages":[],"stream":true}`)))
	require.Equal(t, http.StatusOK, rec.Code, "the actual slot-wait heartbeat committed HTTP 200")
	require.Contains(t, rec.Body.String(), string(SSEPingFormatClaude))
	require.Equal(t, 1, strings.Count(strings.ReplaceAll(rec.Body.String(), "event: ", "event:"), "event:error\n"))
	require.Contains(t, rec.Body.String(), message)
	require.NotContains(t, rec.Body.String(), "[DONE]")
	require.Empty(t, upstream.hits)
	require.False(t, slots.held)
	require.Equal(t, 1, slots.releases)
}

func TestRetryFilterGenericChatTerminatePreservesExistingStreamOutput(t *testing.T) {
	for _, output := range []string{
		"data: {\"choices\":[{\"delta\":{\"content\":\"answer\"}}]}\n\n",
		"event: error\ndata: {\"error\":{\"message\":\"already reported\"}}\n\n",
	} {
		rec := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(rec)
		c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
		_, err := c.Writer.WriteString(output)
		require.NoError(t, err)
		(&GatewayHandler{}).handleCCFailoverExhausted(c, &service.UpstreamFailoverError{
			Reason: retryFilterTerminateReason, NextAccountAction: service.NextAccountStop,
			ClientStatusCode: http.StatusConflict, ClientMessage: "must not be appended",
		}, true)
		require.Equal(t, output, rec.Body.String())
	}
}
