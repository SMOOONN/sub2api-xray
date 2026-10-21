package admin

import (
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/model"
	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
)

// RetryFilterHandler exposes only administrator-owned policy data. Account
// bindings deliberately never serialize the account or its credentials.
type RetryFilterHandler struct {
	filters  *service.RetryFilterService
	settings *service.SettingService
	admin    service.AdminService
	runtime  *service.RetryFilterRuntime
	ops      *service.OpsService
}

func NewRetryFilterHandler(filters *service.RetryFilterService, settings *service.SettingService, admin service.AdminService, runtime *service.RetryFilterRuntime, ops ...*service.OpsService) *RetryFilterHandler {
	var opsService *service.OpsService
	if len(ops) > 0 {
		opsService = ops[0]
	}
	return &RetryFilterHandler{filters: filters, settings: settings, admin: admin, runtime: runtime, ops: opsService}
}

func retryFilterID(c *gin.Context, name string) (int64, bool) {
	id, err := strconv.ParseInt(c.Param(name), 10, 64)
	if err != nil || id <= 0 {
		response.BadRequest(c, "Invalid "+name)
		return 0, false
	}
	return id, true
}

func retryFilterError(c *gin.Context, err error) {
	var validation *model.ValidationError
	if errors.As(err, &validation) {
		response.BadRequest(c, validation.Error())
		return
	}
	if errors.Is(err, service.ErrRetryFilterPresetInUse) {
		response.Error(c, http.StatusConflict, "Preset is still referenced; remove its bindings first")
		return
	}
	response.ErrorFrom(c, err)
}

func (h *RetryFilterHandler) preset(c *gin.Context) *model.RetryFilterPreset {
	id, ok := retryFilterID(c, "presetID")
	if !ok {
		return nil
	}
	p, err := h.filters.GetPresetByID(c.Request.Context(), id)
	if err != nil {
		retryFilterError(c, err)
		return nil
	}
	if p == nil {
		response.NotFound(c, "Preset not found")
	}
	return p
}

func (h *RetryFilterHandler) ListPresets(c *gin.Context) {
	presets, err := h.filters.ListPresets(c.Request.Context())
	if err != nil {
		retryFilterError(c, err)
		return
	}
	response.Success(c, presets)
}

func (h *RetryFilterHandler) GetPreset(c *gin.Context) {
	if p := h.preset(c); p != nil {
		response.Success(c, p)
	}
}

// PUT is a full editable-policy replacement; IDs and timestamps are server-owned.
func (h *RetryFilterHandler) SavePreset(c *gin.Context) {
	p := &model.RetryFilterPreset{Enabled: true}
	if c.Request.Method == http.MethodPut {
		p = h.preset(c)
		if p == nil {
			return
		}
	}
	id, created := p.ID, p.CreatedAt
	var req struct {
		Name        string  `json:"name" binding:"required"`
		Enabled     *bool   `json:"enabled"`
		Description *string `json:"description"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "Invalid preset request")
		return
	}
	p.Name, p.Description = strings.TrimSpace(req.Name), req.Description
	if req.Enabled != nil {
		p.Enabled = *req.Enabled
	}
	p.ID, p.CreatedAt = id, created
	var saved *model.RetryFilterPreset
	var err error
	if id == 0 {
		saved, err = h.filters.CreatePreset(c.Request.Context(), p)
	} else {
		saved, err = h.filters.UpdatePreset(c.Request.Context(), p)
	}
	if err != nil {
		retryFilterError(c, err)
		return
	}
	response.Success(c, saved)
}

func (h *RetryFilterHandler) DeletePreset(c *gin.Context) {
	p := h.preset(c)
	if p == nil {
		return
	}
	if err := h.filters.DeletePreset(c.Request.Context(), p.ID); err != nil {
		retryFilterError(c, err)
		return
	}
	response.Success(c, gin.H{"deleted": true})
}

func (h *RetryFilterHandler) ListRules(c *gin.Context) {
	p := h.preset(c)
	if p == nil {
		return
	}
	rules, err := h.filters.ListRules(c.Request.Context(), p.ID)
	if err != nil {
		retryFilterError(c, err)
		return
	}
	response.Success(c, rules)
}

// GetPresetDetail returns the aggregate consumed by the embedded rule-set
// editor. It intentionally contains only binding IDs and counts; account
// credentials and other sensitive account fields never cross this boundary.
func (h *RetryFilterHandler) GetPresetDetail(c *gin.Context) {
	p := h.preset(c)
	if p == nil {
		return
	}
	rules, err := h.filters.ListRules(c.Request.Context(), p.ID)
	if err != nil {
		retryFilterError(c, err)
		return
	}
	summary, err := h.bindingSummary(c, p.ID)
	if err != nil {
		retryFilterError(c, err)
		return
	}
	response.Success(c, gin.H{"preset": p, "rules": rules, "bindings": summary})
}

func (h *RetryFilterHandler) rule(c *gin.Context, presetID int64) *model.RetryFilterRule {
	id, ok := retryFilterID(c, "ruleID")
	if !ok {
		return nil
	}
	r, err := h.filters.GetRuleByID(c.Request.Context(), id)
	if err != nil {
		retryFilterError(c, err)
		return nil
	}
	if r == nil || r.PresetID != presetID {
		response.NotFound(c, "Rule not found in this preset")
		return nil
	}
	return r
}

func (h *RetryFilterHandler) GetRule(c *gin.Context) {
	p := h.preset(c)
	if p == nil {
		return
	}
	if r := h.rule(c, p.ID); r != nil {
		response.Success(c, r)
	}
}

func (h *RetryFilterHandler) SaveRule(c *gin.Context) {
	p := h.preset(c)
	if p == nil {
		return
	}
	var existing *model.RetryFilterRule
	if c.Request.Method == http.MethodPut {
		existing = h.rule(c, p.ID)
		if existing == nil {
			return
		}
	}
	r := &model.RetryFilterRule{Enabled: true}
	if err := c.ShouldBindJSON(r); err != nil {
		response.BadRequest(c, "Invalid rule request")
		return
	}
	r.Name = strings.TrimSpace(r.Name)
	r.ID, r.PresetID = 0, p.ID
	if existing != nil {
		r.ID, r.CreatedAt, r.UpdatedAt = existing.ID, existing.CreatedAt, existing.UpdatedAt
	}
	var saved *model.RetryFilterRule
	var err error
	if existing == nil {
		saved, err = h.filters.CreateRule(c.Request.Context(), r)
	} else {
		saved, err = h.filters.UpdateRule(c.Request.Context(), r)
	}
	if err != nil {
		retryFilterError(c, err)
		return
	}
	response.Success(c, saved)
}

func (h *RetryFilterHandler) DeleteRule(c *gin.Context) {
	p := h.preset(c)
	if p == nil {
		return
	}
	r := h.rule(c, p.ID)
	if r == nil {
		return
	}
	if err := h.filters.DeleteRule(c.Request.Context(), r.ID); err != nil {
		retryFilterError(c, err)
		return
	}
	response.Success(c, gin.H{"deleted": true})
}

// ReorderRules persists a complete rule permutation from the drag-and-drop
// editor. The service rejects duplicate, foreign, or missing rule IDs.
func (h *RetryFilterHandler) ReorderRules(c *gin.Context) {
	p := h.preset(c)
	if p == nil {
		return
	}
	var req struct {
		RuleIDs []int64 `json:"rule_ids" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "Invalid rule order")
		return
	}
	rules, err := h.filters.ReorderRules(c.Request.Context(), p.ID, req.RuleIDs)
	if err != nil {
		retryFilterError(c, err)
		return
	}
	response.Success(c, rules)
}

// A null or zero reference means inherit. Negative and missing presets are errors.
func (h *RetryFilterHandler) bindingRequest(c *gin.Context) (*int64, bool) {
	var req struct {
		PresetID *int64 `json:"preset_id"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "Invalid preset binding")
		return nil, false
	}
	if req.PresetID == nil || *req.PresetID == 0 {
		return nil, true
	}
	if *req.PresetID < 0 {
		response.BadRequest(c, "Invalid preset ID")
		return nil, false
	}
	p, err := h.filters.GetPresetByID(c.Request.Context(), *req.PresetID)
	if err != nil {
		retryFilterError(c, err)
		return nil, false
	}
	if p == nil {
		response.BadRequest(c, "Preset does not exist")
		return nil, false
	}
	return req.PresetID, true
}

func (h *RetryFilterHandler) GetDefault(c *gin.Context) {
	s, err := h.settings.GetAllSettings(c.Request.Context())
	if err != nil {
		retryFilterError(c, err)
		return
	}
	response.Success(c, gin.H{"preset_id": s.RetryFilterDefaultPresetID, "enabled": s.RetryFilterEnabled})
}

func (h *RetryFilterHandler) SetDefault(c *gin.Context) {
	id, ok := h.bindingRequest(c)
	if !ok {
		return
	}
	if err := h.settings.SetRetryFilterDefaultPresetID(c.Request.Context(), id); err != nil {
		retryFilterError(c, err)
		return
	}
	h.GetDefault(c)
}

func (h *RetryFilterHandler) AccountBinding(c *gin.Context) {
	id, ok := retryFilterID(c, "accountID")
	if !ok {
		return
	}
	a, err := h.admin.GetAccount(c.Request.Context(), id)
	if err != nil {
		retryFilterError(c, err)
		return
	}
	if a == nil {
		response.NotFound(c, "Account not found")
		return
	}
	if c.Request.Method == http.MethodPut {
		presetID, ok := h.bindingRequest(c)
		if !ok {
			return
		}
		value := int64(0)
		if presetID != nil {
			value = *presetID
		}
		credentials := make(map[string]any, len(a.Credentials)+1)
		for key, current := range a.Credentials {
			credentials[key] = current
		}
		credentials["retry_filter_preset_id"] = value
		a, err = h.admin.UpdateAccount(c.Request.Context(), id, &service.UpdateAccountInput{Credentials: credentials})
		if err != nil {
			retryFilterError(c, err)
			return
		}
	}
	response.Success(c, gin.H{"id": a.ID, "name": a.Name, "preset_id": service.RetryFilterAccountPresetID(a), "eligible": a.IsPoolMode()})
}

func (h *RetryFilterHandler) GroupBinding(c *gin.Context) {
	id, ok := retryFilterID(c, "groupID")
	if !ok {
		return
	}
	g, err := h.admin.GetGroup(c.Request.Context(), id)
	if err != nil {
		retryFilterError(c, err)
		return
	}
	if g == nil {
		response.NotFound(c, "Group not found")
		return
	}
	if c.Request.Method == http.MethodPut {
		presetID, ok := h.bindingRequest(c)
		if !ok {
			return
		}
		g, err = h.admin.UpdateGroup(c.Request.Context(), id, &service.UpdateGroupInput{RetryFilterConfig: &service.GroupRetryFilterConfig{PresetID: presetID}})
		if err != nil {
			retryFilterError(c, err)
			return
		}
	}
	response.Success(c, gin.H{"id": g.ID, "name": g.Name, "preset_id": g.RetryFilterConfig.PresetID})
}

func (h *RetryFilterHandler) EffectiveSource(c *gin.Context) {
	var query struct {
		AccountID int64 `form:"account_id" binding:"omitempty,min=1"`
		GroupID   int64 `form:"group_id" binding:"omitempty,min=1"`
	}
	if err := c.ShouldBindQuery(&query); err != nil {
		response.BadRequest(c, "Invalid account or group ID")
		return
	}
	var a *service.Account
	var g *service.Group
	var err error
	if query.AccountID > 0 {
		a, err = h.admin.GetAccount(c.Request.Context(), query.AccountID)
		if err != nil {
			retryFilterError(c, err)
			return
		}
		if a == nil {
			response.NotFound(c, "Account not found")
			return
		}
	}
	if query.GroupID > 0 {
		g, err = h.admin.GetGroup(c.Request.Context(), query.GroupID)
		if err != nil {
			retryFilterError(c, err)
			return
		}
		if g == nil {
			response.NotFound(c, "Group not found")
			return
		}
	}
	s, err := h.settings.GetAllSettings(c.Request.Context())
	if err != nil {
		retryFilterError(c, err)
		return
	}
	resolved := h.runtime.ResolvePreset(c.Request.Context(), a, g)
	response.Success(c, gin.H{"preset_id": resolved.PresetID, "source": resolved.Source, "enabled": s.RetryFilterEnabled, "eligible": a == nil || a.IsPoolMode()})
}

type retryFilterBindingCandidate struct {
	ID       int64  `json:"id"`
	Name     string `json:"name"`
	Platform string `json:"platform,omitempty"`
	Status   string `json:"status,omitempty"`
	PresetID int64  `json:"preset_id,omitempty"`
	Selected bool   `json:"selected"`
	Eligible bool   `json:"eligible"`
}

func retryFilterSelectedPresetID(value int64, wanted *int64) bool {
	return wanted != nil && value == *wanted
}

// ListAccountBindingCandidates is deliberately backed by the existing admin
// account list contract so search, pagination, ownership filtering, and future
// account visibility rules remain centralized in AdminService.
func (h *RetryFilterHandler) ListAccountBindingCandidates(c *gin.Context) {
	page, pageSize := response.ParsePagination(c)
	q := strings.TrimSpace(c.Query("q"))
	presetID, err := optionalPositiveQueryID(c, "preset_id")
	if err != nil {
		response.BadRequest(c, err.Error())
		return
	}
	accounts, total, err := h.admin.ListAccounts(c.Request.Context(), page, pageSize, "", "", "", q, 0, "", "id", "asc")
	if err != nil {
		retryFilterError(c, err)
		return
	}
	items := make([]retryFilterBindingCandidate, 0, len(accounts))
	for _, account := range accounts {
		id := service.RetryFilterAccountPresetID(&account)
		items = append(items, retryFilterBindingCandidate{ID: account.ID, Name: account.Name, Platform: account.Platform, Status: account.Status, PresetID: id, Selected: retryFilterSelectedPresetID(id, presetID), Eligible: account.IsPoolMode()})
	}
	response.Paginated(c, items, total, page, pageSize)
}

// ListGroupBindingCandidates is the group counterpart to
// ListAccountBindingCandidates.
func (h *RetryFilterHandler) ListGroupBindingCandidates(c *gin.Context) {
	page, pageSize := response.ParsePagination(c)
	q := strings.TrimSpace(c.Query("q"))
	presetID, err := optionalPositiveQueryID(c, "preset_id")
	if err != nil {
		response.BadRequest(c, err.Error())
		return
	}
	groups, total, err := h.admin.ListGroups(c.Request.Context(), page, pageSize, "", "", q, nil, "name", "asc")
	if err != nil {
		retryFilterError(c, err)
		return
	}
	items := make([]retryFilterBindingCandidate, 0, len(groups))
	for _, group := range groups {
		id := int64(0)
		if group.RetryFilterConfig.PresetID != nil {
			id = *group.RetryFilterConfig.PresetID
		}
		items = append(items, retryFilterBindingCandidate{ID: group.ID, Name: group.Name, Platform: group.Platform, Status: group.Status, PresetID: id, Selected: retryFilterSelectedPresetID(id, presetID), Eligible: service.IsGroupBindableInSimpleMode(&group)})
	}
	response.Paginated(c, items, total, page, pageSize)
}

func optionalPositiveQueryID(c *gin.Context, name string) (*int64, error) {
	raw := strings.TrimSpace(c.Query(name))
	if raw == "" {
		return nil, nil
	}
	id, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || id <= 0 {
		return nil, errors.New("invalid " + name)
	}
	return &id, nil
}

type retryFilterBatchBindingRequest struct {
	IDs        []int64 `json:"ids"`
	Accounts   []int64 `json:"accounts"`
	AccountIDs []int64 `json:"account_ids"`
	Groups     []int64 `json:"groups"`
	GroupIDs   []int64 `json:"group_ids"`
	PresetID   *int64  `json:"preset_id"`
	Global     *bool   `json:"global"`
}

func (r *retryFilterBatchBindingRequest) accountIDs() []int64 {
	if len(r.IDs) > 0 {
		return r.IDs
	}
	if len(r.AccountIDs) > 0 {
		return r.AccountIDs
	}
	return r.Accounts
}

func (r *retryFilterBatchBindingRequest) groupIDs() []int64 {
	if len(r.IDs) > 0 {
		return r.IDs
	}
	if len(r.GroupIDs) > 0 {
		return r.GroupIDs
	}
	return r.Groups
}

func validateRetryFilterBindingPreset(ctx *gin.Context, filters *service.RetryFilterService, presetID *int64) bool {
	if presetID == nil || *presetID == 0 {
		return true
	}
	if *presetID < 0 {
		response.BadRequest(ctx, "Invalid preset ID")
		return false
	}
	p, err := filters.GetPresetByID(ctx.Request.Context(), *presetID)
	if err != nil {
		retryFilterError(ctx, err)
		return false
	}
	if p == nil {
		response.BadRequest(ctx, "Preset does not exist")
		return false
	}
	return true
}

func normalizedRetryFilterPresetID(id *int64) *int64 {
	if id == nil || *id <= 0 {
		return nil
	}
	return id
}

// BindAccountsBatch applies one preset reference to many accounts while
// preserving every unrelated credential key. A null/zero preset clears the
// retry-filter binding.
func (h *RetryFilterHandler) BindAccountsBatch(c *gin.Context) {
	var req retryFilterBatchBindingRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "Invalid account binding request")
		return
	}
	req.PresetID = normalizedRetryFilterPresetID(req.PresetID)
	if !validateRetryFilterBindingPreset(c, h.filters, req.PresetID) {
		return
	}
	ids := req.accountIDs()
	if len(ids) == 0 {
		response.BadRequest(c, "ids must not be empty")
		return
	}
	for _, id := range ids {
		if id <= 0 {
			response.BadRequest(c, "account IDs must be positive")
			return
		}
	}
	updater, ok := h.admin.(service.RetryFilterBindingAtomicUpdater)
	if !ok {
		retryFilterError(c, errors.New("atomic retry-filter binding is unavailable"))
		return
	}
	if err := updater.UpdateRetryFilterBindingsAtomically(c.Request.Context(), service.RetryFilterBindingMutation{
		PresetID:   req.PresetID,
		AccountIDs: ids,
	}); err != nil {
		retryFilterError(c, err)
		return
	}
	response.Success(c, gin.H{"updated": ids, "preset_id": req.PresetID})
}

// BindGroupsBatch applies one preset reference to many groups.
func (h *RetryFilterHandler) BindGroupsBatch(c *gin.Context) {
	var req retryFilterBatchBindingRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "Invalid group binding request")
		return
	}
	req.PresetID = normalizedRetryFilterPresetID(req.PresetID)
	if !validateRetryFilterBindingPreset(c, h.filters, req.PresetID) {
		return
	}
	ids := req.groupIDs()
	if len(ids) == 0 {
		response.BadRequest(c, "ids must not be empty")
		return
	}
	for _, id := range ids {
		if id <= 0 {
			response.BadRequest(c, "group IDs must be positive")
			return
		}
	}
	updater, ok := h.admin.(service.RetryFilterBindingAtomicUpdater)
	if !ok {
		retryFilterError(c, errors.New("atomic retry-filter binding is unavailable"))
		return
	}
	if err := updater.UpdateRetryFilterBindingsAtomically(c.Request.Context(), service.RetryFilterBindingMutation{
		PresetID: req.PresetID,
		GroupIDs: ids,
	}); err != nil {
		retryFilterError(c, err)
		return
	}
	response.Success(c, gin.H{"updated": ids, "preset_id": req.PresetID})
}

// BindPreset is the embedded editor's single-save endpoint. It updates the
// explicitly supplied account/group bindings and optionally the global default.
func (h *RetryFilterHandler) BindPreset(c *gin.Context) {
	p := h.preset(c)
	if p == nil {
		return
	}
	var req retryFilterBatchBindingRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "Invalid preset binding request")
		return
	}
	// The embedded editor has two independent selection sets.  Do not accept
	// the generic `ids` alias here: that alias is useful for the dedicated
	// account/group batch endpoints, but treating it as both sets would bind
	// the same IDs to two different resource types.
	if len(req.IDs) > 0 {
		response.BadRequest(c, "use accounts and groups for preset bindings")
		return
	}
	req.PresetID = &p.ID
	updater, ok := h.admin.(service.RetryFilterBindingAtomicUpdater)
	if !ok {
		retryFilterError(c, errors.New("atomic retry-filter binding is unavailable"))
		return
	}
	accountsProvided := req.Accounts != nil || req.AccountIDs != nil
	groupsProvided := req.Groups != nil || req.GroupIDs != nil
	existing, err := h.bindingSummary(c, p.ID)
	if err != nil {
		retryFilterError(c, err)
		return
	}
	accountIDs := make([]int64, 0)
	if accountsProvided {
		if len(req.AccountIDs) > 0 {
			accountIDs = req.AccountIDs
		} else {
			accountIDs = req.Accounts
		}
	}
	groupIDs := make([]int64, 0)
	if groupsProvided {
		if len(req.GroupIDs) > 0 {
			groupIDs = req.GroupIDs
		} else {
			groupIDs = req.Groups
		}
	}
	mutation := service.RetryFilterBindingMutation{PresetID: &p.ID}
	if accountsProvided {
		mutation.AccountIDs = accountIDs
		mutation.ClearAccountIDs = idsNotIn(accountIDs, existing.AccountIDs)
	}
	if groupsProvided {
		mutation.GroupIDs = groupIDs
		mutation.ClearGroupIDs = idsNotIn(groupIDs, existing.GroupIDs)
	}
	if req.Global != nil {
		if *req.Global {
			id := p.ID
			mutation.DefaultPresetID = &id
		} else if existing.Global {
			mutation.DefaultPresetID = new(int64)
		}
	}
	if err := updater.UpdateRetryFilterBindingsAtomically(c.Request.Context(), mutation); err != nil {
		retryFilterError(c, err)
		return
	}
	response.Success(c, gin.H{"preset_id": p.ID, "global": req.Global, "accounts": accountIDs, "groups": groupIDs})
}

func containsInt64(values []int64, wanted int64) bool {
	for _, value := range values {
		if value == wanted {
			return true
		}
	}
	return false
}

func idsNotIn(wanted, existing []int64) []int64 {
	removed := make([]int64, 0)
	for _, id := range existing {
		if !containsInt64(wanted, id) {
			removed = append(removed, id)
		}
	}
	return removed
}

type retryFilterBindingSummary struct {
	AccountIDs   []int64 `json:"account_ids"`
	GroupIDs     []int64 `json:"group_ids"`
	AccountCount int     `json:"account_count"`
	GroupCount   int     `json:"group_count"`
	Global       bool    `json:"global"`
}

func (h *RetryFilterHandler) bindingSummary(c *gin.Context, presetID int64) (*retryFilterBindingSummary, error) {
	// The paginated admin list caps page size at 1000.  Binding summaries are
	// used to reconcile a complete selection, so use the existing unpaged
	// scheduler-score projection and the all-groups projection instead of
	// silently dropping bindings after the first page.
	accounts, err := h.admin.ListAccountsForSchedulerScoreFilter(c.Request.Context(), "", "", "", "", 0, "")
	if err != nil {
		return nil, err
	}
	groups, err := h.admin.GetAllGroupsIncludingInactive(c.Request.Context())
	if err != nil {
		return nil, err
	}
	accountIDs := make([]int64, 0)
	for _, account := range accounts {
		if service.RetryFilterAccountPresetID(&account) == presetID {
			accountIDs = append(accountIDs, account.ID)
		}
	}
	groupIDs := make([]int64, 0)
	for _, group := range groups {
		if group.RetryFilterConfig.PresetID != nil && *group.RetryFilterConfig.PresetID == presetID {
			groupIDs = append(groupIDs, group.ID)
		}
	}
	globalID, err := h.settings.GetRetryFilterDefaultPresetID(c.Request.Context())
	if err != nil {
		return nil, err
	}
	return &retryFilterBindingSummary{AccountIDs: accountIDs, GroupIDs: groupIDs, AccountCount: len(accountIDs), GroupCount: len(groupIDs), Global: globalID == presetID}, nil
}

// ListMonitor returns a stable empty monitor shape until an Ops event source
// is available. The runtime still records retry-filter metadata in the existing
// Ops upstream_errors stream; no second audit table is introduced here.
func (h *RetryFilterHandler) ListMonitor(c *gin.Context) {
	page, pageSize := response.ParsePagination(c)
	if h.ops == nil {
		response.Success(c, gin.H{"items": []any{}, "total": int64(0), "page": page, "page_size": pageSize, "pages": 1, "summary": gin.H{"matched": 0, "retries": 0, "terminates": 0, "by_action": gin.H{}, "recent_errors": []any{}}})
		return
	}
	filter := &service.RetryFilterMonitorFilter{Page: page, PageSize: pageSize, Action: strings.TrimSpace(c.Query("action")), Model: strings.TrimSpace(c.Query("model")), Reason: strings.TrimSpace(c.Query("reason")), Query: strings.TrimSpace(c.Query("q"))}
	for _, item := range []struct {
		name string
		dest **int64
	}{
		{name: "account_id", dest: &filter.AccountID},
		{name: "group_id", dest: &filter.GroupID},
		{name: "preset_id", dest: &filter.PresetID},
		{name: "rule_id", dest: &filter.RuleID},
	} {
		id, err := optionalPositiveQueryID(c, item.name)
		if err != nil {
			response.BadRequest(c, err.Error())
			return
		}
		*item.dest = id
	}
	if v := strings.TrimSpace(c.Query("status_code")); v != "" {
		code, err := strconv.Atoi(v)
		if err != nil || code < 100 || code > 599 {
			response.BadRequest(c, "Invalid status_code")
			return
		}
		filter.StatusCode = &code
	}
	if raw := strings.TrimSpace(c.Query("start_time")); raw != "" {
		parsed, err := time.Parse(time.RFC3339, raw)
		if err != nil {
			response.BadRequest(c, "Invalid start_time")
			return
		}
		filter.StartTime = &parsed
	}
	if raw := strings.TrimSpace(c.Query("end_time")); raw != "" {
		parsed, err := time.Parse(time.RFC3339, raw)
		if err != nil {
			response.BadRequest(c, "Invalid end_time")
			return
		}
		filter.EndTime = &parsed
	}
	result, err := h.ops.ListRetryFilterMonitorEvents(c.Request.Context(), filter)
	if err != nil {
		retryFilterError(c, err)
		return
	}
	response.Success(c, result)
}

func (h *RetryFilterHandler) GetMonitorEvent(c *gin.Context) {
	if h.ops == nil {
		response.NotFound(c, "Retry filter monitor event not found")
		return
	}
	event, err := h.ops.GetRetryFilterMonitorEvent(c.Request.Context(), strings.TrimSpace(c.Param("id")))
	if err != nil {
		retryFilterError(c, err)
		return
	}
	if event == nil {
		response.NotFound(c, "Retry filter monitor event not found")
		return
	}
	response.Success(c, event)
}
