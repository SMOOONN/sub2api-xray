//go:build unit

package service

import (
	"context"
	"errors"
	"io"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/model"
	"github.com/stretchr/testify/require"
)

type retryRestoreDumperStub struct {
	DBDumper
	restore func(context.Context, io.Reader) error
}

func (d *retryRestoreDumperStub) Restore(ctx context.Context, r io.Reader) error {
	return d.restore(ctx, r)
}

type retryRestoreCacheStub struct {
	RetryFilterCache
	invalidateErr, notifyErr error
	beforeInvalidate         func(context.Context)
	notifications            int
	onUpdate                 func()
}

func (c *retryRestoreCacheStub) Set(context.Context, map[int64][]*model.RetryFilterRule) error {
	return nil
}
func (c *retryRestoreCacheStub) Invalidate(ctx context.Context) error {
	if c.beforeInvalidate != nil {
		c.beforeInvalidate(ctx)
	}
	return c.invalidateErr
}
func (c *retryRestoreCacheStub) NotifyUpdate(context.Context) error {
	c.notifications++
	return c.notifyErr
}
func (c *retryRestoreCacheStub) SubscribeUpdates(_ context.Context, f func()) { c.onUpdate = f }

type retryRestoreRepoStub struct {
	RetryFilterRepository
	listPresets func(context.Context) ([]*model.RetryFilterPreset, error)
	listRules   func(context.Context, int64) ([]*model.RetryFilterRule, error)
}

func (r *retryRestoreRepoStub) ListPresets(ctx context.Context) ([]*model.RetryFilterPreset, error) {
	return r.listPresets(ctx)
}
func (r *retryRestoreRepoStub) ListEnabledRules(ctx context.Context, id int64) ([]*model.RetryFilterRule, error) {
	return r.listRules(ctx, id)
}

func retryRestoreRule(status int) *model.RetryFilterRule {
	return &model.RetryFilterRule{ID: 1, PresetID: 1, Name: "restore-policy", Enabled: true, StatusCodes: []int{422}, Action: model.RetryFilterActionTerminate, TerminateStatusCode: &status}
}

func retryRestoreFixture(t *testing.T) (*RetryFilterService, *retryFilterTestRepository, *SettingService, *mockSettingRepo, *retryRestoreCacheStub) {
	t.Helper()
	repo := &retryFilterTestRepository{
		presets: map[int64]*model.RetryFilterPreset{1: enabledRetryFilterPreset(1), 2: enabledRetryFilterPreset(2)},
		rules:   map[int64][]*model.RetryFilterRule{1: {retryRestoreRule(410)}},
	}
	settingsRepo := newMockSettingRepo()
	require.NoError(t, settingsRepo.SetMultiple(context.Background(), map[string]string{SettingKeyRetryFilterEnabled: "false", SettingKeyRetryFilterDefaultPresetID: "2"}))
	settings := &SettingService{settingRepo: settingsRepo}
	settings.refreshRetryFilterRuntime(context.Background())
	cache := &retryRestoreCacheStub{}
	filters := ProvideRetryFilterService(repo, cache, settings)
	t.Cleanup(filters.Shutdown)
	return filters, repo, settings, settingsRepo, cache
}

func TestRetryFilterRestoreSynchronizesRulesAndSettings(t *testing.T) {
	for _, enabled := range []string{"true", "false"} {
		t.Run(enabled, func(t *testing.T) {
			filters, repo, settings, settingsRepo, cache := retryRestoreFixture(t)
			if enabled == "false" {
				require.NoError(t, settingsRepo.Set(context.Background(), SettingKeyRetryFilterEnabled, "true"))
				settings.refreshRetryFilterRuntime(context.Background())
			}
			input := strings.NewReader("synthetic SQL")
			d := &retryFilterRestoreDumper{filters: filters, DBDumper: &retryRestoreDumperStub{restore: func(ctx context.Context, r io.Reader) error {
				require.Same(t, input, r)
				repo.rules[1] = []*model.RetryFilterRule{retryRestoreRule(409)}
				delete(repo.presets, 2)
				return settingsRepo.SetMultiple(ctx, map[string]string{SettingKeyRetryFilterEnabled: enabled, SettingKeyRetryFilterDefaultPresetID: "1"})
			}}}
			require.NoError(t, d.Restore(context.Background(), input))
			matched := filters.MatchRule(1, 422, nil, "")
			require.NotNil(t, matched)
			require.Equal(t, 409, *matched.TerminateStatusCode)
			require.False(t, filters.IsPresetUsable(2))
			require.Equal(t, enabled == "true", settings.IsRetryFilterEnabled(context.Background()))
			preset, err := settings.GetRetryFilterDefaultPresetID(context.Background())
			require.NoError(t, err)
			require.Equal(t, int64(1), preset)
			require.Equal(t, 1, cache.notifications)
			runtime := NewRetryFilterRuntime(filters, settings.IsRetryFilterEnabled, settings.GetRetryFilterDefaultPresetID)
			decision := runtime.Evaluate(context.Background(), &Account{Type: AccountTypeAPIKey, Credentials: map[string]any{"pool_mode": true}}, nil, "model", &UpstreamFailoverError{StatusCode: 422})
			if enabled == "true" {
				require.NotNil(t, decision)
			} else {
				require.Nil(t, decision)
			}
			// A delayed settings write's cache callback must not publish its old
			// input over the restored DB values.
			oldPreset := int64(2)
			settings.refreshCachedSettings(&SystemSettings{RetryFilterEnabled: enabled != "true", RetryFilterDefaultPresetID: &oldPreset})
			require.Equal(t, enabled == "true", settings.IsRetryFilterEnabled(context.Background()))
			preset, err = settings.GetRetryFilterDefaultPresetID(context.Background())
			require.NoError(t, err)
			require.Equal(t, int64(1), preset)
		})
	}
}

func TestRetryFilterRestoreSubscriberRefreshesBothOwnersWithoutRepublishing(t *testing.T) {
	filters, repo, settings, settingsRepo, cache := retryRestoreFixture(t)
	repo.rules[1] = []*model.RetryFilterRule{retryRestoreRule(409)}
	require.NoError(t, settingsRepo.SetMultiple(context.Background(), map[string]string{SettingKeyRetryFilterEnabled: "true", SettingKeyRetryFilterDefaultPresetID: "1"}))
	cache.onUpdate()
	require.Equal(t, 409, *filters.MatchRule(1, 422, nil, "").TerminateStatusCode)
	require.True(t, settings.IsRetryFilterEnabled(context.Background()))
	id, err := settings.GetRetryFilterDefaultPresetID(context.Background())
	require.NoError(t, err)
	require.Equal(t, int64(1), id)
	require.Zero(t, cache.notifications)
}

func TestRetryFilterRestoreErrorsStillAttemptLocalAndRemoteSynchronization(t *testing.T) {
	for _, which := range []string{"restore", "invalidate", "notify", "rules", "settings", "all"} {
		t.Run(which, func(t *testing.T) {
			filters, repo, _, _, cache := retryRestoreFixture(t)
			restoreErr, invalidateErr, notifyErr, rulesErr, settingsErr := errors.New("restore"), errors.New("invalidate"), errors.New("notify"), errors.New("rules"), errors.New("settings")
			var wanted []error
			settingsAttempted := false
			filters.reloadRuntimeSettings = func(context.Context) error {
				settingsAttempted = true
				if which == "settings" || which == "all" {
					return settingsErr
				}
				return nil
			}
			if which == "settings" || which == "all" {
				wanted = append(wanted, settingsErr)
			}
			if which == "invalidate" || which == "all" {
				cache.invalidateErr = invalidateErr
				wanted = append(wanted, invalidateErr)
			}
			if which == "notify" || which == "all" {
				cache.notifyErr = notifyErr
				wanted = append(wanted, notifyErr)
			}
			if which == "rules" || which == "all" {
				filters.repo = &retryRestoreRepoStub{listPresets: func(context.Context) ([]*model.RetryFilterPreset, error) { return nil, rulesErr }}
				wanted = append(wanted, rulesErr)
			}
			if which == "restore" || which == "all" {
				wanted = append(wanted, restoreErr)
			}
			cache.beforeInvalidate = func(context.Context) {
				require.True(t, settingsAttempted, "local settings must be attempted before Redis")
				if which != "rules" && which != "all" {
					require.Equal(t, 409, *filters.MatchRule(1, 422, nil, "").TerminateStatusCode)
				}
			}
			d := &retryFilterRestoreDumper{filters: filters, DBDumper: &retryRestoreDumperStub{restore: func(context.Context, io.Reader) error {
				repo.rules[1] = []*model.RetryFilterRule{retryRestoreRule(409)}
				if which == "restore" || which == "all" {
					return restoreErr
				}
				return nil
			}}}
			err := d.Restore(context.Background(), strings.NewReader(""))
			for _, target := range wanted {
				require.ErrorIs(t, err, target)
			}
			require.True(t, settingsAttempted)
			require.Equal(t, 1, cache.notifications)
			matched := filters.MatchRule(1, 422, nil, "")
			if which == "rules" || which == "all" {
				require.Nil(t, matched)
			} else {
				require.NotNil(t, matched)
				require.Equal(t, 409, *matched.TerminateStatusCode)
			}
		})
	}
}

func TestRetryFilterRestoreCancellationAndDumpPassthrough(t *testing.T) {
	filters, repo, _, _, _ := retryRestoreFixture(t)
	type contextKey struct{}
	ctx, cancel := context.WithCancel(context.WithValue(context.Background(), contextKey{}, "restore-value"))
	cancel()
	filters.reloadRuntimeSettings = func(syncCtx context.Context) error {
		require.NoError(t, syncCtx.Err())
		require.Equal(t, "restore-value", syncCtx.Value(contextKey{}))
		deadline, ok := syncCtx.Deadline()
		require.True(t, ok)
		require.Positive(t, time.Until(deadline))
		require.LessOrEqual(t, time.Until(deadline), retryFilterRuntimeDBTimeout)
		return nil
	}
	base := &mockDumper{dumpData: []byte("unchanged dump")}
	d := &retryFilterRestoreDumper{filters: filters, DBDumper: &retryRestoreDumperStub{DBDumper: base, restore: func(got context.Context, _ io.Reader) error {
		require.Same(t, ctx, got)
		repo.rules[1] = []*model.RetryFilterRule{retryRestoreRule(409)}
		return got.Err()
	}}}
	r, err := d.Dump(context.Background())
	require.NoError(t, err)
	b, err := io.ReadAll(r)
	require.NoError(t, err)
	require.NoError(t, r.Close())
	require.Equal(t, "unchanged dump", string(b))
	require.ErrorIs(t, d.Restore(ctx, strings.NewReader("")), context.Canceled)
	require.Equal(t, 409, *filters.MatchRule(1, 422, nil, "").TerminateStatusCode)
}

func TestRetryFilterRestoreAllBackupEntrypointsReportSyncFailure(t *testing.T) {
	for _, async := range []bool{false, true} {
		for _, split := range []bool{false, true} {
			name := "sync-legacy"
			if async {
				name = "async-legacy"
			}
			if split {
				name += "-parts"
			}
			t.Run(name, func(t *testing.T) {
				filters, _, _, _, cache := retryRestoreFixture(t)
				cache.notifyErr = errors.New("publish unavailable")
				data := []byte("synthetic database dump")
				base := &mockDumper{}
				repo := newMockSettingRepo()
				seedS3Config(t, repo)
				store := newMockObjectStore()
				compressed := gzipBackupBytes(t, data)
				store.objects["restore"] = compressed
				svc := newTestBackupService(repo, &retryFilterRestoreDumper{DBDumper: base, filters: filters}, store)
				record := &BackupRecord{ID: "restore", Status: "completed", S3Key: "restore"}
				if split {
					record.Parts = []BackupPart{{Index: 1, S3Key: "restore", SizeBytes: int64(len(compressed))}}
				}
				require.NoError(t, svc.saveRecord(context.Background(), record))
				if async {
					_, err := svc.StartRestore(context.Background(), record.ID)
					require.NoError(t, err)
					svc.wg.Wait()
					result, err := svc.GetBackupRecord(context.Background(), record.ID)
					require.NoError(t, err)
					require.Equal(t, "failed", result.RestoreStatus)
					require.Contains(t, result.RestoreError, "database restore succeeded")
				} else {
					require.ErrorContains(t, svc.RestoreBackup(context.Background(), record.ID), "database restore succeeded")
				}
				require.Equal(t, data, base.restored)
			})
		}
	}
}

func TestRetryFilterRestoreWaitsForOldRuleReaderBeforePublishing(t *testing.T) {
	started, release, committed := make(chan struct{}), make(chan struct{}), make(chan struct{})
	var mu sync.Mutex
	firstRead := true
	stored := retryRestoreRule(410)
	repo := &retryRestoreRepoStub{
		listPresets: func(context.Context) ([]*model.RetryFilterPreset, error) {
			return []*model.RetryFilterPreset{enabledRetryFilterPreset(1)}, nil
		},
		listRules: func(context.Context, int64) ([]*model.RetryFilterRule, error) {
			mu.Lock()
			captured, block := stored, firstRead
			firstRead = false
			mu.Unlock()
			if block {
				close(started)
				<-release
			}
			return []*model.RetryFilterRule{captured}, nil
		},
	}
	filters := &RetryFilterService{repo: repo}
	first := make(chan error, 1)
	go func() { first <- filters.Reload(context.Background()) }()
	<-started
	restored := make(chan error, 1)
	d := &retryFilterRestoreDumper{filters: filters, DBDumper: &retryRestoreDumperStub{restore: func(context.Context, io.Reader) error {
		mu.Lock()
		stored = retryRestoreRule(409)
		mu.Unlock()
		close(committed)
		return nil
	}}}
	go func() { restored <- d.Restore(context.Background(), strings.NewReader("")) }()
	<-committed
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	err := filters.Reload(ctx)
	close(release)
	require.NoError(t, <-first)
	require.NoError(t, <-restored)
	require.ErrorIs(t, err, context.DeadlineExceeded)
	require.Equal(t, 409, *filters.MatchRule(1, 422, nil, "").TerminateStatusCode)
}

type retryRestoreMutationRepoStub struct {
	RetryFilterRepository
	cancel context.CancelFunc
}

func (r *retryRestoreMutationRepoStub) UpdateRule(ctx context.Context, rule *model.RetryFilterRule) (*model.RetryFilterRule, error) {
	updated, err := r.RetryFilterRepository.UpdateRule(ctx, rule)
	r.cancel()
	return updated, err
}

func TestRetryFilterMutationCancellationStillSynchronizesCommittedRule(t *testing.T) {
	filters, repo, _, _, cache := retryRestoreFixture(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	filters.repo = &retryRestoreMutationRepoStub{RetryFilterRepository: repo, cancel: cancel}
	_, err := filters.UpdateRule(ctx, retryRestoreRule(409))
	require.NoError(t, err)
	require.ErrorIs(t, ctx.Err(), context.Canceled)
	require.Equal(t, 409, *filters.MatchRule(1, 422, nil, "").TerminateStatusCode)
	require.Equal(t, 1, cache.notifications)
}

type retryFilterCancelOnSelectContext struct {
	context.Context
	cancel context.CancelFunc
}

func (c retryFilterCancelOnSelectContext) Done() <-chan struct{} {
	c.cancel()
	return c.Context.Done()
}

func TestRetryFilterReloadLockCancellationDoesNotConsumeToken(t *testing.T) {
	var gate retryFilterReloadLock
	for i := 0; i < 100; i++ {
		ctx, cancel := context.WithCancel(context.Background())
		err := gate.lock(retryFilterCancelOnSelectContext{Context: ctx, cancel: cancel})
		cancel()
		require.ErrorIs(t, err, context.Canceled)
		probe, stop := context.WithTimeout(context.Background(), time.Second)
		require.NoError(t, gate.lock(probe))
		gate.unlock()
		stop()
	}
}

type retryRestoreSettingsRepoStub struct {
	SettingRepository
	getMultiple func(context.Context) (map[string]string, error)
}

func (r *retryRestoreSettingsRepoStub) GetMultiple(ctx context.Context, _ []string) (map[string]string, error) {
	return r.getMultiple(ctx)
}

func TestRetryFilterRestoreSettingsReadErrorsAndCanceledWaiter(t *testing.T) {
	started, release := make(chan struct{}), make(chan struct{})
	var once sync.Once
	repo := &retryRestoreSettingsRepoStub{getMultiple: func(context.Context) (map[string]string, error) {
		once.Do(func() { close(started); <-release })
		return map[string]string{SettingKeyRetryFilterEnabled: "true", SettingKeyRetryFilterDefaultPresetID: "1"}, nil
	}}
	s := &SettingService{settingRepo: repo}
	first := make(chan error, 1)
	go func() { first <- s.reloadRetryFilterRuntime(context.Background()) }()
	<-started
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	require.ErrorIs(t, s.reloadRetryFilterRuntime(ctx), context.DeadlineExceeded)
	close(release)
	require.NoError(t, <-first)
	wantErr := errors.New("settings database unavailable")
	repo.getMultiple = func(context.Context) (map[string]string, error) { return nil, wantErr }
	require.ErrorIs(t, s.reloadRetryFilterRuntime(context.Background()), wantErr)
	require.True(t, s.IsRetryFilterEnabled(context.Background()), "retain existing transient-error policy")
	id, err := s.GetRetryFilterDefaultPresetID(context.Background())
	require.NoError(t, err)
	require.Equal(t, int64(1), id)
}

func TestRetryFilterRestoreWaitsForOldSettingsReaderBeforePublishing(t *testing.T) {
	started, release, committed := make(chan struct{}), make(chan struct{}), make(chan struct{})
	var mu sync.Mutex
	firstRead := true
	stored := map[string]string{SettingKeyRetryFilterEnabled: "false", SettingKeyRetryFilterDefaultPresetID: "2"}
	repo := &retryRestoreSettingsRepoStub{getMultiple: func(context.Context) (map[string]string, error) {
		mu.Lock()
		captured, block := stored, firstRead
		firstRead = false
		mu.Unlock()
		if block {
			close(started)
			<-release
		}
		return captured, nil
	}}
	s := &SettingService{settingRepo: repo}
	filters, _, _, _, _ := retryRestoreFixture(t)
	filters.reloadRuntimeSettings = s.reloadRetryFilterRuntime
	first := make(chan error, 1)
	go func() { first <- s.reloadRetryFilterRuntime(context.Background()) }()
	<-started
	restored := make(chan error, 1)
	d := &retryFilterRestoreDumper{filters: filters, DBDumper: &retryRestoreDumperStub{restore: func(context.Context, io.Reader) error {
		mu.Lock()
		stored = map[string]string{SettingKeyRetryFilterEnabled: "true", SettingKeyRetryFilterDefaultPresetID: "1"}
		mu.Unlock()
		close(committed)
		return nil
	}}}
	go func() { restored <- d.Restore(context.Background(), strings.NewReader("")) }()
	<-committed
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	err := s.reloadRetryFilterRuntime(ctx)
	close(release)
	require.NoError(t, <-first)
	require.NoError(t, <-restored)
	require.ErrorIs(t, err, context.DeadlineExceeded)
	require.True(t, s.IsRetryFilterEnabled(context.Background()))
	id, err := s.GetRetryFilterDefaultPresetID(context.Background())
	require.NoError(t, err)
	require.Equal(t, int64(1), id)
}
