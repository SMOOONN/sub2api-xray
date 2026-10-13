package handler

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/model"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

// handlerRetryFilterRepository keeps these request-path tests independent of
// Ent while still exercising the production runtime and decision seam.
type handlerRetryFilterRepository struct {
	presets map[int64]*model.RetryFilterPreset
	rules   map[int64][]*model.RetryFilterRule
}

func (r *handlerRetryFilterRepository) ListPresets(context.Context) ([]*model.RetryFilterPreset, error) {
	out := make([]*model.RetryFilterPreset, 0, len(r.presets))
	for _, preset := range r.presets {
		out = append(out, preset)
	}
	return out, nil
}
func (r *handlerRetryFilterRepository) GetPresetByID(_ context.Context, id int64) (*model.RetryFilterPreset, error) {
	if preset := r.presets[id]; preset != nil {
		return preset, nil
	}
	return nil, errors.New("preset not found")
}
func (r *handlerRetryFilterRepository) CreatePreset(_ context.Context, preset *model.RetryFilterPreset) (*model.RetryFilterPreset, error) {
	r.presets[preset.ID] = preset
	return preset, nil
}
func (r *handlerRetryFilterRepository) UpdatePreset(_ context.Context, preset *model.RetryFilterPreset) (*model.RetryFilterPreset, error) {
	r.presets[preset.ID] = preset
	return preset, nil
}
func (r *handlerRetryFilterRepository) DeletePreset(_ context.Context, id int64) error {
	delete(r.presets, id)
	return nil
}
func (r *handlerRetryFilterRepository) HasPresetReferences(context.Context, int64) (bool, error) {
	return false, nil
}
func (r *handlerRetryFilterRepository) ListRules(_ context.Context, id int64) ([]*model.RetryFilterRule, error) {
	return r.rules[id], nil
}
func (r *handlerRetryFilterRepository) ListEnabledRules(ctx context.Context, id int64) ([]*model.RetryFilterRule, error) {
	return r.ListRules(ctx, id)
}
func (r *handlerRetryFilterRepository) GetRuleByID(_ context.Context, id int64) (*model.RetryFilterRule, error) {
	for _, rules := range r.rules {
		for _, rule := range rules {
			if rule.ID == id {
				return rule, nil
			}
		}
	}
	return nil, errors.New("rule not found")
}
func (r *handlerRetryFilterRepository) CreateRule(_ context.Context, rule *model.RetryFilterRule) (*model.RetryFilterRule, error) {
	r.rules[rule.PresetID] = append(r.rules[rule.PresetID], rule)
	return rule, nil
}
func (r *handlerRetryFilterRepository) UpdateRule(_ context.Context, rule *model.RetryFilterRule) (*model.RetryFilterRule, error) {
	for i, existing := range r.rules[rule.PresetID] {
		if existing.ID == rule.ID {
			r.rules[rule.PresetID][i] = rule
			return rule, nil
		}
	}
	return nil, errors.New("rule not found")
}
func (r *handlerRetryFilterRepository) DeleteRule(_ context.Context, id int64) error {
	for presetID, rules := range r.rules {
		for i, rule := range rules {
			if rule.ID == id {
				r.rules[presetID] = append(rules[:i], rules[i+1:]...)
				return nil
			}
		}
	}
	return nil
}
func (r *handlerRetryFilterRepository) ReorderRules(context.Context, int64, []int64) ([]*model.RetryFilterRule, error) {
	return nil, nil
}

func newHandlerRetryFilterRuntime(rule *model.RetryFilterRule) *service.RetryFilterRuntime {
	repo := &handlerRetryFilterRepository{
		presets: map[int64]*model.RetryFilterPreset{1: {ID: 1, Name: "test", Enabled: true}},
		rules:   map[int64][]*model.RetryFilterRule{1: {rule}},
	}
	filters := service.NewRetryFilterService(repo, nil)
	return service.NewRetryFilterRuntime(filters, func(context.Context) bool { return true }, func(context.Context) (int64, error) {
		return 0, nil
	})
}

func newRetryFilterDecisionContext() *gin.Context {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/test", nil)
	return c
}

func poolModeRetryFilterAccount() *service.Account {
	return &service.Account{
		ID:   7,
		Type: service.AccountTypeAPIKey,
		Credentials: map[string]any{
			"pool_mode":              true,
			"retry_filter_preset_id": int64(1),
		},
	}
}

func TestRetryFilterFailoverDecisionSameAccountOverrideControlsBudgetAndDelay(t *testing.T) {
	delay := 0
	count := 2
	runtime := newHandlerRetryFilterRuntime(&model.RetryFilterRule{
		ID:                 11,
		PresetID:           1,
		Enabled:            true,
		StatusCodes:        []int{429},
		Action:             model.RetryFilterActionSameAccountRetry,
		RetryCountOverride: &count,
		RetryDelayMs:       &delay,
	})
	account := poolModeRetryFilterAccount()
	err := &service.UpstreamFailoverError{StatusCode: 429, NextAccountAction: service.NextAccountRetry}
	state := NewFailoverState(3, false)
	unscheduler := &mockTempUnscheduler{}

	for retry := 1; retry <= 2; retry++ {
		require.Equal(t, FailoverContinue, state.HandleFailoverDecision(newRetryFilterDecisionContext(), unscheduler, runtime, FailoverDecisionInput{
			Account: account, Model: "gpt-4o", RetryLimit: 0, FailoverErr: err,
		}))
		require.Equal(t, retry, state.SameAccountRetryCount[account.ID])
		require.Equal(t, 0, state.SwitchCount)
		require.True(t, err.RetryableOnSameAccount)
	}
	require.Empty(t, unscheduler.calls)

	// The rule budget is exhausted on the next failure, so normal failover
	// resumes instead of silently retrying forever.
	require.Equal(t, FailoverContinue, state.HandleFailoverDecision(newRetryFilterDecisionContext(), unscheduler, runtime, FailoverDecisionInput{
		Account: account, Model: "gpt-4o", RetryLimit: 0, FailoverErr: err,
	}))
	require.Equal(t, 1, state.SwitchCount)
	require.Contains(t, state.FailedAccountIDs, account.ID)
	require.Len(t, unscheduler.calls, 1)
	require.Equal(t, account.ID, unscheduler.calls[0].accountID)
	require.Same(t, err, unscheduler.calls[0].failoverErr)
}

func TestRetryFilterFailoverDecisionSwitchAccountSuppressesPenalty(t *testing.T) {
	runtime := newHandlerRetryFilterRuntime(&model.RetryFilterRule{
		ID:          12,
		PresetID:    1,
		Enabled:     true,
		StatusCodes: []int{429},
		Action:      model.RetryFilterActionSwitchAccount,
	})
	err := &service.UpstreamFailoverError{
		StatusCode:             429,
		RetryableOnSameAccount: true,
		NextAccountAction:      service.NextAccountRetry,
	}
	state := NewFailoverState(2, false)

	require.Equal(t, FailoverContinue, state.HandleFailoverDecision(newRetryFilterDecisionContext(), nil, runtime, FailoverDecisionInput{
		Account: poolModeRetryFilterAccount(), Model: "gpt-4o", RetryLimit: 3, FailoverErr: err,
	}))
	require.False(t, err.RetryableOnSameAccount)
	require.True(t, err.SuppressAccountPenalty)
	require.False(t, err.ShouldReportAccountScheduleFailure())
	require.Equal(t, service.NextAccountRetry, err.NextAccountAction)
	require.Equal(t, 1, state.SwitchCount)
	require.Contains(t, state.FailedAccountIDs, int64(7))
}

func TestRetryFilterFailoverDecisionTerminateReturnsOverride(t *testing.T) {
	status := 418
	message := "retry filter stopped this request"
	runtime := newHandlerRetryFilterRuntime(&model.RetryFilterRule{
		ID:                  13,
		PresetID:            1,
		Enabled:             true,
		StatusCodes:         []int{429},
		Action:              model.RetryFilterActionTerminate,
		TerminateStatusCode: &status,
		TerminateMessage:    &message,
	})
	err := &service.UpstreamFailoverError{
		StatusCode:        429,
		ResponseBody:      []byte(`{"error":{"message":"upstream detail"}}`),
		NextAccountAction: service.NextAccountRetry,
	}
	state := NewFailoverState(3, false)

	require.Equal(t, FailoverExhausted, state.HandleFailoverDecision(newRetryFilterDecisionContext(), nil, runtime, FailoverDecisionInput{
		Account: poolModeRetryFilterAccount(), Model: "gpt-4o", RetryLimit: 3, FailoverErr: err,
	}))
	require.Equal(t, retryFilterTerminateReason, err.Reason)
	require.Equal(t, service.NextAccountStop, err.NextAccountAction)
	require.Equal(t, 0, state.SwitchCount)
	gotStatus, gotMessage, ok := retryFilterClientResponse(err)
	require.True(t, ok)
	require.Equal(t, status, gotStatus)
	require.Equal(t, message, gotMessage)
}

func TestRetryFilterFailoverDecisionObservePreservesLegacyBehavior(t *testing.T) {
	runtime := newHandlerRetryFilterRuntime(&model.RetryFilterRule{
		ID:          14,
		PresetID:    1,
		Enabled:     true,
		StatusCodes: []int{429},
		Action:      model.RetryFilterActionObserve,
	})
	err := &service.UpstreamFailoverError{StatusCode: 429, NextAccountAction: service.NextAccountRetry}
	state := NewFailoverState(2, false)

	require.Equal(t, FailoverContinue, state.HandleFailoverDecision(newRetryFilterDecisionContext(), nil, runtime, FailoverDecisionInput{
		Account: poolModeRetryFilterAccount(), Model: "gpt-4o", RetryLimit: 0, FailoverErr: err,
	}))
	require.False(t, err.RetryableOnSameAccount)
	require.False(t, err.SuppressAccountPenalty)
	require.Equal(t, service.NextAccountRetry, err.NextAccountAction)
	require.Equal(t, 1, state.SwitchCount)
	require.Contains(t, state.FailedAccountIDs, int64(7))
}
