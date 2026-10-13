//go:build unit

package handler

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/model"
	"github.com/Wei-Shaw/sub2api/internal/pkg/tlsfingerprint"
	"github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

const (
	retryFilterAntigravityPoolID  int64 = 811
	retryFilterAntigravityOAuthID int64 = 812
	retryFilterAntigravityRuleID  int64 = 88
	retryFilterAntigravityModel         = "claude-sonnet-4-5"
)

type retryFilterAntigravityLoopUpstream struct {
	hits               []int64
	oauthAuthorization string
	oauthPath          string
}

func (u *retryFilterAntigravityLoopUpstream) Do(req *http.Request, _ string, accountID int64, _ int) (*http.Response, error) {
	u.hits = append(u.hits, accountID)
	if accountID == retryFilterAntigravityPoolID {
		return &http.Response{StatusCode: http.StatusUnprocessableEntity, Header: http.Header{"Content-Type": {"application/json"}},
			Body: io.NopCloser(strings.NewReader(`{"error":{"code":"pool_only","message":"pool failure"}}`))}, nil
	}
	if accountID != retryFilterAntigravityOAuthID {
		return nil, fmt.Errorf("unexpected account %d", accountID)
	}
	u.oauthAuthorization, u.oauthPath = req.Header.Get("Authorization"), req.URL.Path
	return &http.Response{StatusCode: http.StatusOK,
		Header: http.Header{"Content-Type": {"text/event-stream"}, "X-Request-Id": {"antigravity-isolation-B"}},
		Body:   io.NopCloser(strings.NewReader(`data: {"response":{"candidates":[{"content":{"parts":[{"thought":true,"text":"isolated thought"}]},"finishReason":"STOP"}],"usageMetadata":{"promptTokenCount":11,"candidatesTokenCount":7}}}` + "\n\n"))}, nil
}

func (u *retryFilterAntigravityLoopUpstream) DoWithTLS(req *http.Request, proxy string, accountID int64, concurrency int, _ *tlsfingerprint.Profile) (*http.Response, error) {
	return u.Do(req, proxy, accountID, concurrency)
}

type retryFilterAntigravityLoopFixture struct {
	router   *gin.Engine
	upstream *retryFilterAntigravityLoopUpstream
	slots    *retryFilterLoopSlots
	usage    *openAIWSUsageHandlerUsageLogRepoStub
	events   []*service.OpsUpstreamErrorEvent
}

func newRetryFilterAntigravityLoopFixture(t *testing.T, mode string) *retryFilterAntigravityLoopFixture {
	t.Helper()
	gin.SetMode(gin.TestMode)
	groupID := int64(911)
	group := &service.Group{ID: groupID, Hydrated: true, Platform: service.PlatformAnthropic, Status: service.StatusActive, RateMultiplier: 1}
	pool := &service.Account{ID: retryFilterAntigravityPoolID, Name: "pool-A", Platform: service.PlatformAnthropic,
		Type: service.AccountTypeAPIKey, Status: service.StatusActive, Schedulable: true, Concurrency: 1, Priority: 1,
		Credentials:   map[string]any{"api_key": "synthetic-pool-key", "pool_mode": true, "retry_filter_preset_id": int64(1)},
		AccountGroups: []service.AccountGroup{{AccountID: retryFilterAntigravityPoolID, GroupID: groupID}}}
	oauth := &service.Account{ID: retryFilterAntigravityOAuthID, Name: "antigravity-B", Platform: service.PlatformAntigravity,
		Type: service.AccountTypeOAuth, Status: service.StatusActive, Schedulable: true, Concurrency: 1, Priority: 2,
		Credentials: map[string]any{
			"access_token": "synthetic-antigravity-token", "project_id": "synthetic-project",
			"expires_at": time.Now().Add(time.Hour).Format(time.RFC3339), "retry_filter_preset_id": int64(1),
			"model_mapping": map[string]any{retryFilterAntigravityModel: retryFilterAntigravityModel},
		},
		Extra:         map[string]any{"mixed_scheduling": true},
		AccountGroups: []service.AccountGroup{{AccountID: retryFilterAntigravityOAuthID, GroupID: groupID}}}
	rule := &model.RetryFilterRule{ID: retryFilterAntigravityRuleID, PresetID: 1, Enabled: true,
		StatusCodes: []int{http.StatusUnprocessableEntity}, MatchMode: model.MatchModeAny,
		EmptyResponseMode: model.RetryFilterEmptyResponseBoth, Action: model.RetryFilterActionSwitchAccount}
	runtime := newHandlerRetryFilterRuntime(rule)
	ctx := context.Background()
	// Both the HTTP failure and an empty response really match this preset.
	for _, failure := range []*service.UpstreamFailoverError{
		{StatusCode: http.StatusUnprocessableEntity},
		{StatusCode: http.StatusBadGateway, RetryFilterEventType: service.RetryFilterEventEmptyCompletedResponse},
	} {
		require.True(t, runtime.Evaluate(ctx, pool, group, retryFilterAntigravityModel, failure).Actionable())
		require.Nil(t, runtime.Evaluate(ctx, oauth, group, retryFilterAntigravityModel, failure))
	}
	require.Equal(t, int64(1), runtime.ResolvePresetID(ctx, oauth, group))
	accounts := []*service.Account{oauth}
	if mode == "pool_switch" {
		accounts = []*service.Account{pool, oauth}
	} else if mode == "disabled" {
		filters := service.NewRetryFilterService(&handlerRetryFilterRepository{
			presets: map[int64]*model.RetryFilterPreset{1: {ID: 1, Name: "disabled-control", Enabled: true}},
			rules:   map[int64][]*model.RetryFilterRule{1: {rule}},
		}, nil)
		runtime = service.NewRetryFilterRuntime(filters, func(context.Context) bool { return false }, nil)
		require.Nil(t, runtime.Evaluate(ctx, pool, group, retryFilterAntigravityModel, &service.UpstreamFailoverError{StatusCode: 422}))
	} else {
		missingPreset := int64(999)
		oauth.Credentials["retry_filter_preset_id"] = missingPreset
		group.RetryFilterConfig.PresetID = &missingPreset
		require.Zero(t, runtime.ResolvePresetID(ctx, oauth, group), "account, group and global resolution must end without an effective preset")
	}
	repo := &grokCredentialHandlerRepo{}
	for _, account := range accounts {
		repo.accounts = append(repo.accounts, *account)
	}
	f := &retryFilterAntigravityLoopFixture{upstream: &retryFilterAntigravityLoopUpstream{}, slots: &retryFilterLoopSlots{},
		usage: &openAIWSUsageHandlerUsageLogRepoStub{created: make(chan *service.UsageLog, 4)}}
	cfg := &config.Config{RunMode: config.RunModeSimple}
	cfg.Default.RateMultiplier = 1
	cfg.Gateway.MaxAccountSwitches = 2
	cfg.Gateway.StreamDataIntervalTimeout = 2
	cfg.Gateway.Scheduling.FallbackWaitTimeout = time.Second
	concurrency := service.NewConcurrencyService(f.slots)
	billingCache := service.NewBillingCacheService(nil, nil, nil, nil, nil, nil, cfg, nil)
	t.Cleanup(billingCache.Stop)
	snapshot := service.NewSchedulerSnapshotService(&fakeSchedulerCache{accounts: accounts}, nil, nil, nil, nil)
	gateway := service.NewGatewayService(
		repo, &fakeGroupRepo{group: group}, f.usage, nil, nil, nil, nil, nil, cfg,
		snapshot, concurrency, service.NewBillingService(cfg, nil), nil, billingCache, nil, f.upstream,
		&service.DeferredService{}, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil,
	)
	antigravity := service.NewAntigravityGatewayService(repo, nil, snapshot,
		service.NewAntigravityTokenProvider(repo, nil, nil), nil, f.upstream,
		service.NewSettingService(&oauthCaptchaSettingRepo{}, cfg), nil)
	h := &GatewayHandler{gatewayService: gateway, antigravityGatewayService: antigravity,
		billingCacheService: billingCache, apiKeyService: &service.APIKeyService{}, cfg: cfg, maxAccountSwitches: 2,
		concurrencyHelper: NewConcurrencyHelper(concurrency, SSEPingFormatClaude, time.Second), retryFilterRuntime: runtime}
	apiKey := &service.APIKey{ID: 912, GroupID: &groupID, Group: group, User: &service.User{ID: 913, Status: service.StatusActive}}
	f.router = gin.New()
	f.router.Use(func(c *gin.Context) {
		c.Set(string(middleware.ContextKeyAPIKey), apiKey)
		c.Set(string(middleware.ContextKeyUser), middleware.AuthSubject{UserID: apiKey.User.ID, Concurrency: 1})
		c.Next()
		value, _ := c.Get(service.OpsUpstreamErrorsKey)
		f.events, _ = value.([]*service.OpsUpstreamErrorEvent)
	})
	f.router.POST("/v1/chat/completions", h.ChatCompletions)
	f.router.POST("/v1/responses", h.Responses)
	return f
}

func TestRetryFilterAntigravityMixedSchedulingIsolation(t *testing.T) {
	for _, mode := range []string{"pool_switch", "disabled", "no_effective_preset"} {
		for _, protocol := range []string{"chat", "responses"} {
			for _, stream := range []bool{false, true} {
				t.Run(fmt.Sprintf("%s/%s/stream=%t", mode, protocol, stream), func(t *testing.T) {
					f := newRetryFilterAntigravityLoopFixture(t, mode)
					path, input := "/v1/responses", `"input":"hello"`
					if protocol == "chat" {
						path, input = "/v1/chat/completions", `"messages":[{"role":"user","content":"hello"}]`
					}
					body := fmt.Sprintf(`{"model":%q,%s,"stream":%t,"stream_options":{"include_usage":true}}`, retryFilterAntigravityModel, input, stream)
					rec := httptest.NewRecorder()
					f.router.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, path, strings.NewReader(body)))
					require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
					require.Contains(t, rec.Body.String(), "isolated thought")
					require.NotContains(t, rec.Body.String(), "pool failure", "A's pending error must not become B's response")
					require.NotContains(t, rec.Body.String(), `"error"`)
					require.Equal(t, "antigravity-isolation-B", rec.Header().Get("X-Request-Id"))
					require.Equal(t, "Bearer synthetic-antigravity-token", f.upstream.oauthAuthorization)
					require.Equal(t, "/v1internal:streamGenerateContent", f.upstream.oauthPath)
					wantHits := []int64{retryFilterAntigravityOAuthID}
					if mode == "pool_switch" {
						wantHits = []int64{retryFilterAntigravityPoolID, retryFilterAntigravityOAuthID}
						require.Len(t, f.events, 1, "one matched A attempt; no B rule hit or inherited A attribution")
						event := f.events[0]
						require.Equal(t, retryFilterAntigravityPoolID, event.AccountID)
						require.Equal(t, http.StatusUnprocessableEntity, event.UpstreamStatusCode)
						require.Equal(t, int64(1), event.RetryFilterPresetID)
						require.Equal(t, retryFilterAntigravityRuleID, event.RetryFilterRuleID)
						require.Equal(t, model.RetryFilterActionSwitchAccount, event.RetryFilterAction)
						require.True(t, event.RetryFilterBehaviorChanged)
					} else {
						require.Empty(t, f.events)
					}
					// B's successful native response needs one call. This does not disable
					// or constrain Antigravity's native retries for upstream failures.
					require.Equal(t, wantHits, f.upstream.hits)
					f.slots.mu.Lock()
					held, releases := f.slots.held, f.slots.releases
					f.slots.mu.Unlock()
					require.False(t, held)
					require.Equal(t, len(wantHits), releases)
					if stream {
						require.Contains(t, rec.Header().Get("Content-Type"), "text/event-stream")
						if protocol == "chat" {
							require.Equal(t, 1, strings.Count(rec.Body.String(), "data: [DONE]"))
							require.Contains(t, rec.Body.String(), `"finish_reason":"stop"`)
						} else {
							require.Equal(t, 1, strings.Count(rec.Body.String(), "event: response.completed\n"))
						}
					} else if protocol == "chat" {
						require.Equal(t, "isolated thought", gjson.Get(rec.Body.String(), "choices.0.message.reasoning_content").String())
						require.Equal(t, "stop", gjson.Get(rec.Body.String(), "choices.0.finish_reason").String())
					} else {
						require.Equal(t, "completed", gjson.Get(rec.Body.String(), "status").String())
						require.Equal(t, "reasoning", gjson.Get(rec.Body.String(), "output.0.type").String())
					}
					// No worker pool is injected, so RecordUsage finishes synchronously.
					// Requiring the row catches the handler's recovered billing panic.
					// Simple mode proves attribution and tokens, not a balance deduction.
					require.Len(t, f.usage.created, 1)
					usage := <-f.usage.created
					require.Equal(t, retryFilterAntigravityOAuthID, usage.AccountID)
					require.Equal(t, "antigravity-isolation-B", usage.RequestID)
					require.Equal(t, 11, usage.InputTokens)
					require.Equal(t, 7, usage.OutputTokens)
					require.Equal(t, stream, usage.Stream)
				})
			}
		}
	}
}
