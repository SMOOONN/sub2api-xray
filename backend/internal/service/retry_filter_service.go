package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/model"
	"github.com/tidwall/gjson"
)

var ErrRetryFilterPresetInUse = errors.New("retry filter preset is still referenced")

type RetryFilterRepository interface {
	ListPresets(context.Context) ([]*model.RetryFilterPreset, error)
	GetPresetByID(context.Context, int64) (*model.RetryFilterPreset, error)
	CreatePreset(context.Context, *model.RetryFilterPreset) (*model.RetryFilterPreset, error)
	UpdatePreset(context.Context, *model.RetryFilterPreset) (*model.RetryFilterPreset, error)
	DeletePreset(context.Context, int64) error
	HasPresetReferences(context.Context, int64) (bool, error)
	ListRules(context.Context, int64) ([]*model.RetryFilterRule, error)
	ListEnabledRules(context.Context, int64) ([]*model.RetryFilterRule, error)
	GetRuleByID(context.Context, int64) (*model.RetryFilterRule, error)
	CreateRule(context.Context, *model.RetryFilterRule) (*model.RetryFilterRule, error)
	UpdateRule(context.Context, *model.RetryFilterRule) (*model.RetryFilterRule, error)
	DeleteRule(context.Context, int64) error
	ReorderRules(context.Context, int64, []int64) ([]*model.RetryFilterRule, error)
}

type RetryFilterCache interface {
	Get(context.Context, int64) ([]*model.RetryFilterRule, bool)
	Set(context.Context, map[int64][]*model.RetryFilterRule) error
	Invalidate(context.Context) error
	NotifyUpdate(context.Context) error
	SubscribeUpdates(context.Context, func())
}

type RetryFilterService struct {
	repo                  RetryFilterRepository
	cache                 RetryFilterCache
	local                 map[int64][]*cachedRetryFilterRule
	presets               map[int64]bool
	loaded                bool
	localMu               sync.RWMutex
	lifecycleMu           sync.Mutex
	subscriptionCancel    context.CancelFunc
	reloadMu              retryFilterReloadLock
	reloadRuntimeSettings func(context.Context) error
}
type cachedRetryFilterRule struct {
	*model.RetryFilterRule
	lowerKeywords      []string
	lowerModels        []string
	lowerUpstreamCodes []string
	statusCodes        map[int]struct{}
}

func NewRetryFilterService(repo RetryFilterRepository, cache RetryFilterCache) *RetryFilterService {
	s := &RetryFilterService{repo: repo, cache: cache}
	if err := s.Reload(context.Background()); err != nil {
		log.Printf("[RetryFilterService] initial load failed; matching disabled: %v", err)
	}
	return s
}

// Start subscribes to cross-instance cache invalidations. It is idempotent and
// separate from construction so the application owns the worker lifetime.
func (s *RetryFilterService) Start() {
	if s == nil || s.cache == nil {
		return
	}
	s.lifecycleMu.Lock()
	if s.subscriptionCancel != nil {
		s.lifecycleMu.Unlock()
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	s.subscriptionCancel = cancel
	s.lifecycleMu.Unlock()
	s.cache.SubscribeUpdates(ctx, func() {
		if err := s.reloadRuntimeState(ctx); err != nil {
			log.Printf("[RetryFilterService] cache invalidation synchronization failed: %v", err)
		}
	})
}

// Shutdown stops the cache invalidation subscription. It is safe to call more
// than once and before Start.
func (s *RetryFilterService) Shutdown() {
	if s == nil {
		return
	}
	s.lifecycleMu.Lock()
	cancel := s.subscriptionCancel
	s.subscriptionCancel = nil
	s.lifecycleMu.Unlock()
	if cancel != nil {
		cancel()
	}
}

// Reload serializes database reads with publication. A failed database read
// disables matching; a canceled lock waiter leaves the active reload intact.
func (s *RetryFilterService) Reload(ctx context.Context) error {
	if s == nil || s.repo == nil {
		return errors.New("retry filter repository is not configured")
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if err := s.reloadMu.lock(ctx); err != nil {
		return err
	}
	defer s.reloadMu.unlock()
	presets, err := s.repo.ListPresets(ctx)
	if err != nil {
		s.localMu.Lock()
		s.loaded = false
		s.localMu.Unlock()
		return err
	}
	nextPresets := make(map[int64]bool, len(presets))
	raw := make(map[int64][]*model.RetryFilterRule, len(presets))
	for _, preset := range presets {
		nextPresets[preset.ID] = preset.Enabled
		if !preset.Enabled {
			continue
		}
		rules, err := s.repo.ListEnabledRules(ctx, preset.ID)
		if err != nil {
			s.localMu.Lock()
			s.loaded = false
			s.localMu.Unlock()
			return err
		}
		raw[preset.ID] = rules
	}
	nextLocal := make(map[int64][]*cachedRetryFilterRule, len(raw))
	for id, rules := range raw {
		nextLocal[id] = prepareRetryRules(rules)
	}
	s.localMu.Lock()
	s.presets = nextPresets
	s.local = nextLocal
	s.loaded = true
	s.localMu.Unlock()
	if s.cache != nil {
		if err := s.cache.Set(ctx, raw); err != nil {
			log.Printf("[RetryFilterService] cache warm failed: %v", err)
		}
	}
	return nil
}

func (s *RetryFilterService) IsPresetUsable(id int64) bool {
	s.localMu.RLock()
	defer s.localMu.RUnlock()
	return s.loaded && s.presets[id]
}

func (s *RetryFilterService) ListPresets(ctx context.Context) ([]*model.RetryFilterPreset, error) {
	return s.repo.ListPresets(ctx)
}

func (s *RetryFilterService) GetPresetByID(ctx context.Context, id int64) (*model.RetryFilterPreset, error) {
	return s.repo.GetPresetByID(ctx, id)
}

func (s *RetryFilterService) CreatePreset(ctx context.Context, p *model.RetryFilterPreset) (*model.RetryFilterPreset, error) {
	if err := p.Validate(); err != nil {
		return nil, err
	}
	created, err := s.repo.CreatePreset(ctx, p)
	_ = s.invalidate(ctx)
	return created, err
}

func (s *RetryFilterService) UpdatePreset(ctx context.Context, p *model.RetryFilterPreset) (*model.RetryFilterPreset, error) {
	if err := p.Validate(); err != nil {
		return nil, err
	}
	updated, err := s.repo.UpdatePreset(ctx, p)
	_ = s.invalidate(ctx)
	return updated, err
}

func (s *RetryFilterService) DeletePreset(ctx context.Context, id int64) error {
	if used, err := s.repo.HasPresetReferences(ctx, id); err != nil {
		return err
	} else if used {
		return ErrRetryFilterPresetInUse
	}
	err := s.repo.DeletePreset(ctx, id)
	_ = s.invalidate(ctx)
	return err
}

func (s *RetryFilterService) ListRules(ctx context.Context, id int64) ([]*model.RetryFilterRule, error) {
	return s.repo.ListRules(ctx, id)
}
func (s *RetryFilterService) GetRuleByID(ctx context.Context, id int64) (*model.RetryFilterRule, error) {
	return s.repo.GetRuleByID(ctx, id)
}
func (s *RetryFilterService) CreateRule(ctx context.Context, r *model.RetryFilterRule) (*model.RetryFilterRule, error) {
	if err := r.Validate(); err != nil {
		return nil, err
	}
	created, err := s.repo.CreateRule(ctx, r)
	_ = s.invalidate(ctx)
	return created, err
}
func (s *RetryFilterService) UpdateRule(ctx context.Context, r *model.RetryFilterRule) (*model.RetryFilterRule, error) {
	if err := r.Validate(); err != nil {
		return nil, err
	}
	updated, err := s.repo.UpdateRule(ctx, r)
	_ = s.invalidate(ctx)
	return updated, err
}
func (s *RetryFilterService) DeleteRule(ctx context.Context, id int64) error {
	err := s.repo.DeleteRule(ctx, id)
	_ = s.invalidate(ctx)
	return err
}

// ReorderRules stores the drag-and-drop order as contiguous priorities. The
// repository validates the preset ownership inside the same transaction.
func (s *RetryFilterService) ReorderRules(ctx context.Context, presetID int64, ruleIDs []int64) ([]*model.RetryFilterRule, error) {
	if presetID <= 0 {
		return nil, &model.ValidationError{Field: "preset_id", Message: "preset_id must be positive"}
	}
	if len(ruleIDs) == 0 {
		return nil, &model.ValidationError{Field: "rule_ids", Message: "rule_ids must not be empty"}
	}
	seen := make(map[int64]struct{}, len(ruleIDs))
	for _, id := range ruleIDs {
		if id <= 0 {
			return nil, &model.ValidationError{Field: "rule_ids", Message: "rule IDs must be positive"}
		}
		if _, ok := seen[id]; ok {
			return nil, &model.ValidationError{Field: "rule_ids", Message: "rule IDs must be unique"}
		}
		seen[id] = struct{}{}
	}
	rows, err := s.repo.ReorderRules(ctx, presetID, ruleIDs)
	if err == nil {
		_ = s.invalidate(ctx)
	}
	return rows, err
}
func (s *RetryFilterService) invalidate(ctx context.Context) error {
	// The database mutation may already have committed even when its request
	// was canceled. Reconcile from storage before releasing this operation.
	ctx, cancelSync := context.WithTimeout(context.WithoutCancel(ctx), 30*time.Second)
	defer cancelSync()
	// Refresh local owners first so a Redis failure cannot starve the database
	// reads. Each remote operation has its own budget; all stages are attempted.
	var syncErrors []error
	if err := s.reloadRuntimeState(ctx); err != nil {
		log.Printf("[RetryFilterService] synchronization after mutation failed: %v", err)
		syncErrors = append(syncErrors, err)
	}
	if s.cache != nil {
		invalidateCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
		if err := s.cache.Invalidate(invalidateCtx); err != nil {
			log.Printf("[RetryFilterService] cache invalidate failed: %v", err)
			syncErrors = append(syncErrors, fmt.Errorf("invalidate retry-filter cache: %w", err))
		}
		cancel()
		notifyCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
		if err := s.cache.NotifyUpdate(notifyCtx); err != nil {
			log.Printf("[RetryFilterService] cache update notification failed: %v", err)
			syncErrors = append(syncErrors, fmt.Errorf("notify retry-filter update: %w", err))
		}
		cancel()
	}
	return errors.Join(syncErrors...)
}

func (s *RetryFilterService) MatchRule(presetID int64, statusCode int, body []byte, modelName string) *model.RetryFilterRule {
	return s.MatchRuleWithEvent(presetID, statusCode, body, modelName, "")
}

// MatchRuleWithEvent extends the legacy four-dimensional matcher with the
// optional request/response semantic event dimensions. An empty event type
// keeps the old behaviour exactly: event conditions simply do not match.
func (s *RetryFilterService) MatchRuleWithEvent(presetID int64, statusCode int, body []byte, modelName, eventType string) *model.RetryFilterRule {
	rules := s.cachedRules(presetID)
	if len(rules) == 0 {
		return nil
	}
	var bodyLower string
	bodyLowerDone := false
	for _, r := range rules {
		if !r.Enabled {
			continue
		}
		configured, matched := 0, 0
		if len(r.StatusCodes) > 0 {
			configured++
			if _, ok := r.statusCodes[statusCode]; ok {
				matched++
			}
		}
		if len(r.Keywords) > 0 {
			configured++
			if !bodyLowerDone {
				if len(body) > retryFilterMaxBodyMatchLen {
					body = body[:retryFilterMaxBodyMatchLen]
				}
				bodyLower = strings.ToLower(string(body))
				bodyLowerDone = true
			}
			if anySubstring(bodyLower, r.lowerKeywords) {
				matched++
			}
		}
		if len(r.UpstreamCodes) > 0 {
			configured++
			if upstreamCodeMatches(body, r.UpstreamCodes, r.UpstreamCodePaths) {
				matched++
			}
		}
		if len(r.Models) > 0 {
			configured++
			if modelMatches(modelName, r.lowerModels) {
				matched++
			}
		}
		if mode := strings.ToLower(strings.TrimSpace(r.EmptyRequestMode)); mode != "" && mode != "none" {
			configured++
			if retryFilterRequestEventMatches(mode, eventType) {
				matched++
			}
		}
		if mode := strings.ToLower(strings.TrimSpace(r.EmptyResponseMode)); mode != "" && mode != "none" {
			configured++
			if retryFilterResponseEventMatches(mode, eventType) {
				matched++
			}
		}
		if configured > 0 && ((r.MatchMode == model.MatchModeAll && matched == configured) || (r.MatchMode != model.MatchModeAll && matched > 0)) {
			return r.RetryFilterRule
		}
	}
	return nil
}

func retryFilterRequestEventMatches(mode, eventType string) bool {
	switch RetryFilterEventType(strings.TrimSpace(eventType)) {
	case RetryFilterEventRequestEmptyBody:
		return mode == "body" || mode == "both"
	case RetryFilterEventRequestEmptyStructured:
		return mode == "structured" || mode == "both"
	default:
		return false
	}
}

func retryFilterResponseEventMatches(mode, eventType string) bool {
	switch RetryFilterEventType(strings.TrimSpace(eventType)) {
	case RetryFilterEventEmptyCompletedResponse:
		return mode == "completed" || mode == "both"
	case RetryFilterEventEmptyInterruptedStream:
		return mode == "interrupted" || mode == "both"
	default:
		return false
	}
}

const retryFilterMaxBodyMatchLen = 8 << 10

func (s *RetryFilterService) cachedRules(id int64) []*cachedRetryFilterRule {
	s.localMu.RLock()
	defer s.localMu.RUnlock()
	if !s.loaded || !s.presets[id] {
		return nil
	}
	return s.local[id]
}
func prepareRetryRules(rules []*model.RetryFilterRule) []*cachedRetryFilterRule {
	out := make([]*cachedRetryFilterRule, len(rules))
	for i, rule := range rules {
		cached := &cachedRetryFilterRule{RetryFilterRule: rule, statusCodes: map[int]struct{}{}}
		for _, code := range rule.StatusCodes {
			cached.statusCodes[code] = struct{}{}
		}
		for _, keyword := range rule.Keywords {
			cached.lowerKeywords = append(cached.lowerKeywords, strings.ToLower(strings.TrimSpace(keyword)))
		}
		for _, modelName := range rule.Models {
			cached.lowerModels = append(cached.lowerModels, strings.ToLower(strings.TrimSpace(modelName)))
		}
		for j, code := range rule.UpstreamCodes {
			rule.UpstreamCodes[j] = strings.TrimSpace(code)
			cached.lowerUpstreamCodes = append(cached.lowerUpstreamCodes, strings.ToLower(rule.UpstreamCodes[j]))
		}
		if len(rule.UpstreamCodePaths) == 0 {
			rule.UpstreamCodePaths = append([]string(nil), model.DefaultRetryFilterUpstreamCodePaths...)
		}
		out[i] = cached
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Priority < out[j].Priority })
	return out
}
func anySubstring(body string, needles []string) bool {
	for _, n := range needles {
		if strings.Contains(body, n) {
			return true
		}
	}
	return false
}
func modelMatches(name string, patterns []string) bool {
	name = strings.ToLower(name)
	if name == "" {
		return false
	}
	for _, p := range patterns {
		p = strings.ToLower(p)
		if strings.HasSuffix(p, "*") {
			if strings.HasPrefix(name, strings.TrimSuffix(p, "*")) {
				return true
			}
		} else if name == p {
			return true
		}
	}
	return false
}
func upstreamCodeMatches(body []byte, codes, paths []string) bool {
	if len(body) > retryFilterMaxBodyMatchLen {
		body = body[:retryFilterMaxBodyMatchLen]
	}
	if len(body) == 0 || !json.Valid(body) {
		return false
	}
	if len(paths) == 0 {
		paths = model.DefaultRetryFilterUpstreamCodePaths
	}
	for _, path := range paths {
		value := gjson.GetBytes(body, path)
		if !value.Exists() || value.Type != gjson.String || value.String() == "" {
			continue
		}
		for _, code := range codes {
			if value.String() == code {
				return true
			}
		}
	}
	return false
}
