package service

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"time"
)

// cachedRetryFilterRuntime is deliberately kept separate from SystemSettings.
// Gateway requests read these two values frequently, so they must not perform a
// database round trip when the settings snapshot is stale.
type cachedRetryFilterRuntime struct {
	enabled   bool
	presetID  int64
	hasPreset bool
	expiresAt int64
}

const (
	retryFilterRuntimeCacheTTL  = 60 * time.Second
	retryFilterRuntimeErrorTTL  = 5 * time.Second
	retryFilterRuntimeDBTimeout = 5 * time.Second
)

// IsRetryFilterEnabled is the request-hot-path reader used by the runtime
// adapter. It serves the last snapshot while an expired snapshot refreshes in
// the background, preserving the fail-closed default on a cold cache.
func (s *SettingService) IsRetryFilterEnabled(ctx context.Context) bool {
	if s == nil {
		return false
	}
	cached, _ := s.retryFilterRuntimeCache.Load().(*cachedRetryFilterRuntime)
	if cached == nil {
		s.refreshRetryFilterRuntime(ctx)
		cached, _ = s.retryFilterRuntimeCache.Load().(*cachedRetryFilterRuntime)
		return cached != nil && cached.enabled
	}
	if time.Now().UnixNano() >= cached.expiresAt && s.retryFilterRuntimeRefreshing.CompareAndSwap(false, true) {
		go func() {
			defer s.retryFilterRuntimeRefreshing.Store(false)
			s.refreshRetryFilterRuntime(context.Background())
		}()
	}
	return cached.enabled
}

// GetRetryFilterDefaultPresetID returns the global fallback preset reference
// from the same atomic snapshot as IsRetryFilterEnabled.
func (s *SettingService) GetRetryFilterDefaultPresetID(ctx context.Context) (int64, error) {
	if s == nil {
		return 0, nil
	}
	cached, _ := s.retryFilterRuntimeCache.Load().(*cachedRetryFilterRuntime)
	if cached == nil {
		s.refreshRetryFilterRuntime(ctx)
		cached, _ = s.retryFilterRuntimeCache.Load().(*cachedRetryFilterRuntime)
	} else if time.Now().UnixNano() >= cached.expiresAt && s.retryFilterRuntimeRefreshing.CompareAndSwap(false, true) {
		go func() {
			defer s.retryFilterRuntimeRefreshing.Store(false)
			s.refreshRetryFilterRuntime(context.Background())
		}()
	}
	if cached == nil || !cached.hasPreset {
		return 0, nil
	}
	return cached.presetID, nil
}

// refreshRetryFilterRuntime loads both global values together. Any read error
// keeps the previous value (or the disabled/empty default) for a short retry
// window, so a transient database outage never enables stale or partial policy.
func (s *SettingService) refreshRetryFilterRuntime(ctx context.Context) {
	dbCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), retryFilterRuntimeDBTimeout)
	defer cancel()
	_ = s.reloadRetryFilterRuntime(dbCtx)
}

// reloadRetryFilterRuntime is the synchronous owner used after a restore. It
// reports failures and respects the caller's cancellation and deadline.
func (s *SettingService) reloadRetryFilterRuntime(ctx context.Context) error {
	if s == nil || s.settingRepo == nil {
		return errors.New("retry filter settings repository is not configured")
	}
	if err := s.retryFilterRuntimeReloadMu.lock(ctx); err != nil {
		return err
	}
	defer s.retryFilterRuntimeReloadMu.unlock()
	previous, _ := s.retryFilterRuntimeCache.Load().(*cachedRetryFilterRuntime)
	next := &cachedRetryFilterRuntime{expiresAt: time.Now().Add(retryFilterRuntimeCacheTTL).UnixNano()}
	if previous != nil {
		next.enabled, next.presetID, next.hasPreset = previous.enabled, previous.presetID, previous.hasPreset
	}
	settings, err := s.settingRepo.GetMultiple(ctx, []string{SettingKeyRetryFilterEnabled, SettingKeyRetryFilterDefaultPresetID})
	if err != nil {
		next.expiresAt = time.Now().Add(retryFilterRuntimeErrorTTL).UnixNano()
		s.retryFilterRuntimeCache.Store(next)
		return err
	}
	next.enabled = strings.TrimSpace(settings[SettingKeyRetryFilterEnabled]) == "true"
	next.presetID, next.hasPreset = 0, false
	if raw := strings.TrimSpace(settings[SettingKeyRetryFilterDefaultPresetID]); raw != "" {
		if id, parseErr := strconv.ParseInt(raw, 10, 64); parseErr == nil && id > 0 {
			next.presetID, next.hasPreset = id, true
		}
	}
	s.retryFilterRuntimeCache.Store(next)
	return nil
}

func (s *SettingService) SetRetryFilterDefaultPresetID(ctx context.Context, id *int64) error {
	value := ""
	if id != nil && *id > 0 {
		value = strconv.FormatInt(*id, 10)
	}
	if err := s.settingRepo.Set(ctx, SettingKeyRetryFilterDefaultPresetID, value); err != nil {
		return err
	}
	if s.onUpdate != nil {
		s.onUpdate()
	}
	// Reflect a single-setting update immediately; the next full settings
	// refresh will reconcile the feature switch and preserve the same TTL.
	s.refreshRetryFilterRuntime(ctx)
	return nil
}

// SetRetryFilterDefaultPresetIDInTx writes the setting through the transaction
// carried by ctx and deliberately defers cache/callback side effects until the
// caller commits the surrounding transaction.
func (s *SettingService) SetRetryFilterDefaultPresetIDInTx(ctx context.Context, id *int64) error {
	if s == nil || s.settingRepo == nil {
		return nil
	}
	value := ""
	if id != nil && *id > 0 {
		value = strconv.FormatInt(*id, 10)
	}
	return s.settingRepo.Set(ctx, SettingKeyRetryFilterDefaultPresetID, value)
}

// ReloadRetryFilterRuntime refreshes the process-local retry-filter snapshot
// after an externally managed transaction has committed.
func (s *SettingService) ReloadRetryFilterRuntime(ctx context.Context) {
	if s == nil {
		return
	}
	if s.onUpdate != nil {
		s.onUpdate()
	}
	s.refreshRetryFilterRuntime(ctx)
}
