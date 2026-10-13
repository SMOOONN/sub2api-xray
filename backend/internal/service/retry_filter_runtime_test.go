//go:build unit

package service

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/model"
	"github.com/stretchr/testify/require"
)

type retryFilterTestRepository struct {
	presets map[int64]*model.RetryFilterPreset
	rules   map[int64][]*model.RetryFilterRule
}

func (r *retryFilterTestRepository) ListPresets(context.Context) ([]*model.RetryFilterPreset, error) {
	ids := make([]int64, 0, len(r.presets))
	for id := range r.presets {
		ids = append(ids, id)
	}
	out := make([]*model.RetryFilterPreset, 0, len(ids))
	for _, id := range ids {
		out = append(out, r.presets[id])
	}
	return out, nil
}

func (r *retryFilterTestRepository) GetPresetByID(_ context.Context, id int64) (*model.RetryFilterPreset, error) {
	if p := r.presets[id]; p != nil {
		return p, nil
	}
	return nil, fmt.Errorf("preset %d not found", id)
}

func (r *retryFilterTestRepository) CreatePreset(_ context.Context, p *model.RetryFilterPreset) (*model.RetryFilterPreset, error) {
	r.presets[p.ID] = p
	return p, nil
}
func (r *retryFilterTestRepository) UpdatePreset(_ context.Context, p *model.RetryFilterPreset) (*model.RetryFilterPreset, error) {
	r.presets[p.ID] = p
	return p, nil
}
func (r *retryFilterTestRepository) DeletePreset(_ context.Context, id int64) error {
	delete(r.presets, id)
	return nil
}
func (r *retryFilterTestRepository) HasPresetReferences(context.Context, int64) (bool, error) {
	return false, nil
}
func (r *retryFilterTestRepository) ListRules(_ context.Context, id int64) ([]*model.RetryFilterRule, error) {
	return append([]*model.RetryFilterRule(nil), r.rules[id]...), nil
}
func (r *retryFilterTestRepository) ListEnabledRules(ctx context.Context, id int64) ([]*model.RetryFilterRule, error) {
	return r.ListRules(ctx, id)
}
func (r *retryFilterTestRepository) GetRuleByID(_ context.Context, id int64) (*model.RetryFilterRule, error) {
	for _, rules := range r.rules {
		for _, rule := range rules {
			if rule.ID == id {
				return rule, nil
			}
		}
	}
	return nil, fmt.Errorf("rule %d not found", id)
}
func (r *retryFilterTestRepository) CreateRule(_ context.Context, rule *model.RetryFilterRule) (*model.RetryFilterRule, error) {
	r.rules[rule.PresetID] = append(r.rules[rule.PresetID], rule)
	return rule, nil
}
func (r *retryFilterTestRepository) UpdateRule(_ context.Context, rule *model.RetryFilterRule) (*model.RetryFilterRule, error) {
	for i, existing := range r.rules[rule.PresetID] {
		if existing.ID == rule.ID {
			r.rules[rule.PresetID][i] = rule
			return rule, nil
		}
	}
	return nil, fmt.Errorf("rule %d not found", rule.ID)
}
func (r *retryFilterTestRepository) DeleteRule(_ context.Context, id int64) error {
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
func (r *retryFilterTestRepository) ReorderRules(_ context.Context, presetID int64, ruleIDs []int64) ([]*model.RetryFilterRule, error) {
	byID := make(map[int64]*model.RetryFilterRule, len(r.rules[presetID]))
	for _, rule := range r.rules[presetID] {
		byID[rule.ID] = rule
	}
	ordered := make([]*model.RetryFilterRule, 0, len(ruleIDs))
	for priority, id := range ruleIDs {
		rule := byID[id]
		rule.Priority = priority
		ordered = append(ordered, rule)
	}
	r.rules[presetID] = ordered
	return ordered, nil
}

func newRetryFilterTestService(presets map[int64]*model.RetryFilterPreset, rules map[int64][]*model.RetryFilterRule) *RetryFilterService {
	return NewRetryFilterService(&retryFilterTestRepository{presets: presets, rules: rules}, nil)
}

func enabledRetryFilterPreset(id int64) *model.RetryFilterPreset {
	return &model.RetryFilterPreset{ID: id, Name: fmt.Sprintf("preset-%d", id), Enabled: true}
}

func TestRetryFilterRequestEmptyEvent(t *testing.T) {
	tests := []struct {
		name string
		body string
		want RetryFilterEventType
	}{
		{name: "blank body", body: "  \n\t", want: RetryFilterEventRequestEmptyBody},
		{name: "null body", body: `null`, want: RetryFilterEventRequestEmptyStructured},
		{name: "empty object", body: `{}`, want: RetryFilterEventRequestEmptyStructured},
		{name: "empty messages", body: `{"model":"gpt-test","messages":[]}`, want: RetryFilterEventRequestEmptyStructured},
		{name: "all supported inputs empty", body: `{"messages":[],"input":[{"content":[]}],"prompt":"  "}`, want: RetryFilterEventRequestEmptyStructured},
		{name: "empty messages do not hide valid input", body: `{"messages":[],"input":[{"text":"hello"}]}`},
		{name: "empty input does not hide valid prompt", body: `{"input":[],"prompt":"draw a cat"}`},
		{name: "nested tool output is content", body: `{"input":[{"type":"function_call_output","output":0}]}`},
		{name: "metadata only stays conservative", body: `{"model":"gpt-test","previous_response_id":"resp_1"}`},
		{name: "invalid json", body: `{`},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			require.Equal(t, test.want, RetryFilterRequestEmptyEvent([]byte(test.body)))
		})
	}
}

func TestRetryFilterResponseHasValidOutput(t *testing.T) {
	require.False(t, RetryFilterResponseHasValidOutput("response.completed", []byte(`{"type":"response.completed","response":{"output":[{"type":"message","content":[]}]}}`)))
	require.False(t, RetryFilterResponseHasValidOutput("response.reasoning.delta", []byte(`{"delta":"internal reasoning"}`)))
	require.True(t, RetryFilterResponseHasValidOutput("response.output_text.delta", []byte(`{"delta":"hello"}`)))
	require.True(t, RetryFilterResponseHasValidOutput("content_block_delta", []byte(`{"delta":{"type":"input_json_delta","partial_json":"{\"city\":\"Paris\"}"}}`)))
}

func TestRetryFilterResponseMediaSource(t *testing.T) {
	for _, test := range []struct {
		name string
		item string
		want bool
	}{
		{name: "base64 image", item: `{"type":"image","source":{"type":"base64","media_type":"image/png","data":"aW1hZ2U="}}`, want: true},
		{name: "URL image", item: `{"type":"image","source":{"type":"url","url":"https://example.com/image.png"}}`, want: true},
		{name: "base64 audio", item: `{"type":"audio","source":{"type":"base64","data":"YXVkaW8="}}`, want: true},
		{name: "empty source", item: `{"type":"image","source":{}}`},
		{name: "source type only", item: `{"type":"image","source":{"type":"base64","media_type":"image/png"}}`},
		{name: "empty data", item: `{"type":"image","source":{"type":"base64","data":"  "}}`},
		{name: "empty URL", item: `{"type":"image","source":{"type":"url","url":""}}`},
		{name: "image type only", item: `{"type":"image"}`},
		{name: "untyped metadata data", item: `{"data":"metadata only"}`},
		{name: "untyped metadata source", item: `{"source":{"type":"base64","data":"metadata only"}}`},
	} {
		t.Run(test.name, func(t *testing.T) {
			// The shared semantic owner is consumed by JSON and stream paths.
			jsonBody := `{"type":"message","content":[` + test.item + `],"stop_reason":"end_turn"}`
			streamBody := `{"type":"content_block_start","content_block":` + test.item + `}`
			require.Equal(t, test.want, RetryFilterResponseHasValidOutput("", []byte(jsonBody)), "JSON")
			require.Equal(t, test.want, RetryFilterResponseHasValidOutput("content_block_start", []byte(streamBody)), "stream event")
		})
	}
}

func TestRetryFilterMatchRuleCoversDimensionsAndPriority(t *testing.T) {
	service := newRetryFilterTestService(
		map[int64]*model.RetryFilterPreset{1: enabledRetryFilterPreset(1)},
		map[int64][]*model.RetryFilterRule{1: {
			{ID: 10, PresetID: 1, Enabled: false, Priority: 1, StatusCodes: []int{429}, Action: model.RetryFilterActionObserve},
			{ID: 20, PresetID: 1, Enabled: true, Priority: 20, StatusCodes: []int{429}, Keywords: []string{"quota"}, UpstreamCodes: []string{"rate_limit_error"}, Models: []string{"gpt-4*"}, MatchMode: model.MatchModeAll, Action: model.RetryFilterActionObserve},
			{ID: 30, PresetID: 1, Enabled: true, Priority: 30, StatusCodes: []int{500}, Action: model.RetryFilterActionObserve},
		}},
	)

	matched := service.MatchRule(1, 429, []byte(`{"error":{"type":"rate_limit_error"},"message":"QUOTA exceeded"}`), "GPT-4o")
	require.NotNil(t, matched)
	require.Equal(t, int64(20), matched.ID, "enabled rules must be evaluated in priority order")
	require.Nil(t, service.MatchRule(1, 429, []byte(`{"error":{"type":"rate_limit_error"},"message":"quota"}`), "gpt-3.5"), "all mode must require every configured dimension")
	require.Nil(t, service.MatchRule(1, 501, []byte(`not-json quota`), "gpt-4o"), "an invalid JSON body must not match upstream code dimensions")
}

func TestRetryFilterMatchRuleAnyAndBodyLimit(t *testing.T) {
	service := newRetryFilterTestService(
		map[int64]*model.RetryFilterPreset{1: enabledRetryFilterPreset(1)},
		map[int64][]*model.RetryFilterRule{1: {
			{ID: 1, PresetID: 1, Enabled: true, Priority: 1, StatusCodes: []int{429}, Keywords: []string{"quota"}, MatchMode: model.MatchModeAny, Action: model.RetryFilterActionObserve},
		}},
	)
	require.NotNil(t, service.MatchRule(1, 429, nil, ""), "any mode must match when one configured dimension matches")
	require.NotNil(t, service.MatchRule(1, 500, []byte("quota"), ""))

	longBody := strings.Repeat("x", 8<<10) + "needle"
	needleService := newRetryFilterTestService(
		map[int64]*model.RetryFilterPreset{1: enabledRetryFilterPreset(1)},
		map[int64][]*model.RetryFilterRule{1: {{ID: 2, PresetID: 1, Enabled: true, Priority: 1, Keywords: []string{"needle"}, Action: model.RetryFilterActionObserve}}},
	)
	require.Nil(t, needleService.MatchRule(1, 500, []byte(longBody), ""), "keyword matching must stop at the first 8KB")
}

func TestRetryFilterModelWildcardAndUpstreamCodeAreExact(t *testing.T) {
	service := newRetryFilterTestService(
		map[int64]*model.RetryFilterPreset{1: enabledRetryFilterPreset(1)},
		map[int64][]*model.RetryFilterRule{1: {
			{ID: 1, PresetID: 1, Enabled: true, Priority: 1, Models: []string{"gpt-4*"}, Action: model.RetryFilterActionObserve},
		}},
	)
	require.NotNil(t, service.MatchRule(1, 500, nil, "GPT-4o"))
	require.Nil(t, service.MatchRule(1, 500, nil, "xgpt-4o"), "a trailing wildcard is a prefix match")

	codeService := newRetryFilterTestService(
		map[int64]*model.RetryFilterPreset{1: enabledRetryFilterPreset(1)},
		map[int64][]*model.RetryFilterRule{1: {{ID: 2, PresetID: 1, Enabled: true, Priority: 1, UpstreamCodes: []string{"rate_limit"}, Action: model.RetryFilterActionObserve}}},
	)
	require.NotNil(t, codeService.MatchRule(1, 500, []byte(`{"error":{"code":"rate_limit"}}`), ""))
	require.Nil(t, codeService.MatchRule(1, 500, []byte(`{"error":{"code":"rate_limit_error"}}`), ""), "upstream codes must be exact")
}

func TestRetryFilterRuntimeResolvesAccountGroupGlobalPrecedence(t *testing.T) {
	repo := &retryFilterTestRepository{
		presets: map[int64]*model.RetryFilterPreset{1: enabledRetryFilterPreset(1), 2: enabledRetryFilterPreset(2), 3: enabledRetryFilterPreset(3)},
		rules:   map[int64][]*model.RetryFilterRule{3: {{ID: 30, PresetID: 3, Enabled: true, Priority: 1, StatusCodes: []int{429}, Action: model.RetryFilterActionObserve}}},
	}
	filters := NewRetryFilterService(repo, nil)
	runtime := NewRetryFilterRuntime(filters, func(context.Context) bool { return true }, func(context.Context) (int64, error) { return 3, nil })
	account := &Account{ID: 7, Type: AccountTypeAPIKey, Credentials: map[string]any{"pool_mode": true, "retry_filter_preset_id": int64(1)}}
	groupPreset := int64(2)
	group := &Group{RetryFilterConfig: GroupRetryFilterConfig{PresetID: &groupPreset}}

	require.Equal(t, RetryFilterResolvedPreset{PresetID: 1, Source: "account"}, runtime.ResolvePreset(context.Background(), account, group))
	repo.presets[1].Enabled = false
	require.NoError(t, filters.Reload(context.Background()))
	account.Credentials["retry_filter_preset_id"] = int64(1)
	require.Equal(t, RetryFilterResolvedPreset{PresetID: 2, Source: "group"}, runtime.ResolvePreset(context.Background(), account, group), "a disabled account preset must fall through")
	repo.presets[1].Enabled = true
	require.NoError(t, filters.Reload(context.Background()))
	account.Credentials["retry_filter_preset_id"] = int64(99)
	require.Equal(t, RetryFilterResolvedPreset{PresetID: 2, Source: "group"}, runtime.ResolvePreset(context.Background(), account, group))
	group.RetryFilterConfig.PresetID = nil
	require.Equal(t, RetryFilterResolvedPreset{PresetID: 3, Source: "global"}, runtime.ResolvePreset(context.Background(), account, group))
	account.Credentials["pool_mode"] = false
	require.Nil(t, runtime.Evaluate(context.Background(), account, group, "gpt-4o", &UpstreamFailoverError{StatusCode: 429}), "non-pool accounts are outside v1 scope")
}
