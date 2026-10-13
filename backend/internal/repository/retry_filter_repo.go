package repository

import (
	"context"
	"fmt"
	"strconv"

	"github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/ent/account"
	"github.com/Wei-Shaw/sub2api/ent/group"
	"github.com/Wei-Shaw/sub2api/ent/retryfilterpreset"
	"github.com/Wei-Shaw/sub2api/ent/retryfilterrule"
	"github.com/Wei-Shaw/sub2api/ent/setting"
	"github.com/Wei-Shaw/sub2api/internal/model"
	"github.com/Wei-Shaw/sub2api/internal/service"
)

type retryFilterRepository struct{ client *ent.Client }

func NewRetryFilterRepository(client *ent.Client) service.RetryFilterRepository {
	return &retryFilterRepository{client: client}
}

func (r *retryFilterRepository) ListPresets(ctx context.Context) ([]*model.RetryFilterPreset, error) {
	rows, err := r.client.RetryFilterPreset.Query().Order(ent.Asc(retryfilterpreset.FieldID)).All(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]*model.RetryFilterPreset, len(rows))
	for i, row := range rows {
		out[i] = presetToModel(row)
	}
	return out, nil
}

func (r *retryFilterRepository) GetPresetByID(ctx context.Context, id int64) (*model.RetryFilterPreset, error) {
	row, err := r.client.RetryFilterPreset.Get(ctx, id)
	if ent.IsNotFound(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return presetToModel(row), nil
}

func (r *retryFilterRepository) CreatePreset(ctx context.Context, p *model.RetryFilterPreset) (*model.RetryFilterPreset, error) {
	b := r.client.RetryFilterPreset.Create().SetName(p.Name).SetEnabled(p.Enabled)
	if p.Description != nil {
		b.SetDescription(*p.Description)
	}
	row, err := b.Save(ctx)
	if err != nil {
		return nil, err
	}
	return presetToModel(row), nil
}

func (r *retryFilterRepository) UpdatePreset(ctx context.Context, p *model.RetryFilterPreset) (*model.RetryFilterPreset, error) {
	b := r.client.RetryFilterPreset.UpdateOneID(p.ID).SetName(p.Name).SetEnabled(p.Enabled)
	if p.Description != nil {
		b.SetDescription(*p.Description)
	} else {
		b.ClearDescription()
	}
	row, err := b.Save(ctx)
	if err != nil {
		return nil, err
	}
	return presetToModel(row), nil
}

func (r *retryFilterRepository) DeletePreset(ctx context.Context, id int64) error {
	return r.client.RetryFilterPreset.DeleteOneID(id).Exec(ctx)
}

func (r *retryFilterRepository) HasPresetReferences(ctx context.Context, id int64) (bool, error) {
	wanted := strconv.FormatInt(id, 10)
	accounts, err := r.client.Account.Query().Select(account.FieldCredentials).All(ctx)
	if err != nil {
		return false, err
	}
	for _, a := range accounts {
		if value, ok := a.Credentials["retry_filter_preset_id"]; ok && retryFilterReferenceValue(value, wanted) {
			return true, nil
		}
	}
	groups, err := r.client.Group.Query().Select(group.FieldRetryFilterConfig).All(ctx)
	if err != nil {
		return false, err
	}
	for _, g := range groups {
		if g.RetryFilterConfig.PresetID != nil && *g.RetryFilterConfig.PresetID == id {
			return true, nil
		}
	}
	global, err := r.client.Setting.Query().Where(setting.KeyEQ(service.SettingKeyRetryFilterDefaultPresetID)).Only(ctx)
	if err == nil && global.Value == wanted {
		return true, nil
	}
	if ent.IsNotFound(err) {
		return false, nil
	}
	return false, err
}

func retryFilterReferenceValue(value any, wanted string) bool {
	switch v := value.(type) {
	case string:
		return v == wanted
	case float64:
		return strconv.FormatInt(int64(v), 10) == wanted
	case int:
		return strconv.Itoa(v) == wanted
	case int64:
		return strconv.FormatInt(v, 10) == wanted
	default:
		return false
	}
}

func (r *retryFilterRepository) ListRules(ctx context.Context, presetID int64) ([]*model.RetryFilterRule, error) {
	rows, err := r.client.RetryFilterRule.Query().Where(retryfilterrule.PresetIDEQ(presetID)).Order(ent.Asc(retryfilterrule.FieldPriority), ent.Asc(retryfilterrule.FieldID)).All(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]*model.RetryFilterRule, len(rows))
	for i, row := range rows {
		out[i] = ruleToModel(row)
	}
	return out, nil
}

func (r *retryFilterRepository) ListEnabledRules(ctx context.Context, presetID int64) ([]*model.RetryFilterRule, error) {
	rows, err := r.client.RetryFilterRule.Query().Where(retryfilterrule.PresetIDEQ(presetID), retryfilterrule.EnabledEQ(true)).Order(ent.Asc(retryfilterrule.FieldPriority), ent.Asc(retryfilterrule.FieldID)).All(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]*model.RetryFilterRule, len(rows))
	for i, row := range rows {
		out[i] = ruleToModel(row)
	}
	return out, nil
}

func (r *retryFilterRepository) GetRuleByID(ctx context.Context, id int64) (*model.RetryFilterRule, error) {
	row, err := r.client.RetryFilterRule.Get(ctx, id)
	if ent.IsNotFound(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return ruleToModel(row), nil
}

func (r *retryFilterRepository) CreateRule(ctx context.Context, rule *model.RetryFilterRule) (*model.RetryFilterRule, error) {
	b := r.client.RetryFilterRule.Create().SetPresetID(rule.PresetID).SetName(rule.Name).SetEnabled(rule.Enabled).SetPriority(rule.Priority).SetMatchMode(rule.MatchMode).SetAction(rule.Action).SetEmptyRequestMode(rule.EmptyRequestMode).SetEmptyResponseMode(rule.EmptyResponseMode)
	setRuleJSON(b, rule)
	setRuleOptional(b, rule)
	row, err := b.Save(ctx)
	if err != nil {
		return nil, err
	}
	return ruleToModel(row), nil
}

func (r *retryFilterRepository) UpdateRule(ctx context.Context, rule *model.RetryFilterRule) (*model.RetryFilterRule, error) {
	b := r.client.RetryFilterRule.UpdateOneID(rule.ID).SetPresetID(rule.PresetID).SetName(rule.Name).SetEnabled(rule.Enabled).SetPriority(rule.Priority).SetMatchMode(rule.MatchMode).SetAction(rule.Action).SetEmptyRequestMode(rule.EmptyRequestMode).SetEmptyResponseMode(rule.EmptyResponseMode)
	setRuleJSONUpdate(b, rule)
	if rule.RetryCountOverride != nil {
		b.SetRetryCountOverride(*rule.RetryCountOverride)
	} else {
		b.ClearRetryCountOverride()
	}
	if rule.RetryDelayMs != nil {
		b.SetRetryDelayMs(*rule.RetryDelayMs)
	} else {
		b.ClearRetryDelayMs()
	}
	if rule.TerminateStatusCode != nil {
		b.SetTerminateStatusCode(*rule.TerminateStatusCode)
	} else {
		b.ClearTerminateStatusCode()
	}
	if rule.TerminateMessage != nil {
		b.SetTerminateMessage(*rule.TerminateMessage)
	} else {
		b.ClearTerminateMessage()
	}
	if rule.Description != nil {
		b.SetDescription(*rule.Description)
	} else {
		b.ClearDescription()
	}
	row, err := b.Save(ctx)
	if err != nil {
		return nil, err
	}
	return ruleToModel(row), nil
}

func (r *retryFilterRepository) DeleteRule(ctx context.Context, id int64) error {
	return r.client.RetryFilterRule.DeleteOneID(id).Exec(ctx)
}

func (r *retryFilterRepository) ReorderRules(ctx context.Context, presetID int64, ruleIDs []int64) ([]*model.RetryFilterRule, error) {
	if presetID <= 0 {
		return nil, &model.ValidationError{Field: "preset_id", Message: "preset_id must be positive"}
	}
	current, err := r.client.RetryFilterRule.Query().Where(retryfilterrule.PresetIDEQ(presetID)).Order(ent.Asc(retryfilterrule.FieldID)).IDs(ctx)
	if err != nil {
		return nil, err
	}
	if len(current) != len(ruleIDs) {
		return nil, &model.ValidationError{Field: "rule_ids", Message: "rule_ids must contain every rule in the preset"}
	}
	owned := make(map[int64]struct{}, len(current))
	for _, id := range current {
		owned[id] = struct{}{}
	}
	for _, id := range ruleIDs {
		if _, ok := owned[id]; !ok {
			return nil, &model.ValidationError{Field: "rule_ids", Message: fmt.Sprintf("rule %d does not belong to preset %d", id, presetID)}
		}
	}

	tx, err := r.client.Tx(ctx)
	if err != nil {
		return nil, err
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback()
		}
	}()
	for priority, id := range ruleIDs {
		if _, err := tx.RetryFilterRule.UpdateOneID(id).
			Where(retryfilterrule.PresetIDEQ(presetID)).
			SetPriority(priority).
			Save(ctx); err != nil {
			return nil, err
		}
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	committed = true
	return r.ListRules(ctx, presetID)
}

func setRuleJSON(b *ent.RetryFilterRuleCreate, r *model.RetryFilterRule) {
	b.SetStatusCodes(r.StatusCodes).SetKeywords(r.Keywords).SetUpstreamCodes(r.UpstreamCodes).SetUpstreamCodePaths(r.UpstreamCodePaths).SetModels(r.Models)
}
func setRuleJSONUpdate(b *ent.RetryFilterRuleUpdateOne, r *model.RetryFilterRule) {
	b.SetStatusCodes(r.StatusCodes).SetKeywords(r.Keywords).SetUpstreamCodes(r.UpstreamCodes).SetUpstreamCodePaths(r.UpstreamCodePaths).SetModels(r.Models)
}
func setRuleOptional(b *ent.RetryFilterRuleCreate, r *model.RetryFilterRule) {
	if r.RetryCountOverride != nil {
		b.SetRetryCountOverride(*r.RetryCountOverride)
	}
	if r.RetryDelayMs != nil {
		b.SetRetryDelayMs(*r.RetryDelayMs)
	}
	if r.TerminateStatusCode != nil {
		b.SetTerminateStatusCode(*r.TerminateStatusCode)
	}
	if r.TerminateMessage != nil {
		b.SetTerminateMessage(*r.TerminateMessage)
	}
	if r.Description != nil {
		b.SetDescription(*r.Description)
	}
}
func presetToModel(e *ent.RetryFilterPreset) *model.RetryFilterPreset {
	return &model.RetryFilterPreset{ID: int64(e.ID), Name: e.Name, Enabled: e.Enabled, Description: e.Description, CreatedAt: e.CreatedAt, UpdatedAt: e.UpdatedAt}
}
func ruleToModel(e *ent.RetryFilterRule) *model.RetryFilterRule {
	return &model.RetryFilterRule{ID: int64(e.ID), PresetID: e.PresetID, Name: e.Name, Enabled: e.Enabled, Priority: e.Priority, StatusCodes: e.StatusCodes, Keywords: e.Keywords, UpstreamCodes: e.UpstreamCodes, UpstreamCodePaths: e.UpstreamCodePaths, Models: e.Models, MatchMode: e.MatchMode, Action: e.Action, RetryCountOverride: e.RetryCountOverride, RetryDelayMs: e.RetryDelayMs, EmptyRequestMode: e.EmptyRequestMode, EmptyResponseMode: e.EmptyResponseMode, TerminateStatusCode: e.TerminateStatusCode, TerminateMessage: e.TerminateMessage, Description: e.Description, CreatedAt: e.CreatedAt, UpdatedAt: e.UpdatedAt}
}
