package service

import (
	"context"
	"errors"
	"fmt"
	"io"
	"sync"
)

// retryFilterReloadLock serializes database reads and snapshot publication,
// including a canceled waiter. Its zero value is ready for use.
type retryFilterReloadLock struct {
	once sync.Once
	gate chan struct{}
}

func (m *retryFilterReloadLock) lock(ctx context.Context) error {
	m.once.Do(func() { m.gate = make(chan struct{}, 1) })
	if err := ctx.Err(); err != nil {
		return err
	}
	select {
	case m.gate <- struct{}{}:
		if err := ctx.Err(); err != nil {
			m.unlock()
			return err
		}
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (m *retryFilterReloadLock) unlock() { <-m.gate }

// retryFilterRestoreDumper connects all backup restore paths to the existing
// retry-filter cache owners. Backup archive and SQL handling stay in DBDumper.
type retryFilterRestoreDumper struct {
	DBDumper
	filters *RetryFilterService
}

func (d *retryFilterRestoreDumper) Restore(ctx context.Context, data io.Reader) error {
	restoreErr := d.DBDumper.Restore(ctx, data)
	// Even an error or cancellation may race a database commit. The mutation
	// synchronization owner provides its own bounded, uncanceled context.
	syncErr := d.filters.invalidate(ctx)
	if syncErr == nil {
		return restoreErr
	}
	if restoreErr == nil {
		return fmt.Errorf("database restore succeeded, but retry-filter synchronization failed: %w", syncErr)
	}
	return errors.Join(restoreErr, fmt.Errorf("retry-filter synchronization after restore failed: %w", syncErr))
}

// reloadRuntimeState is shared by mutation/restore and the existing Redis
// subscriber. It never publishes another notification.
func (s *RetryFilterService) reloadRuntimeState(ctx context.Context) error {
	var settingsErr error
	if s.reloadRuntimeSettings != nil {
		settingsCtx, cancel := context.WithTimeout(ctx, retryFilterRuntimeDBTimeout)
		settingsErr = s.reloadRuntimeSettings(settingsCtx)
		cancel()
		if settingsErr != nil {
			settingsErr = fmt.Errorf("reload retry-filter settings: %w", settingsErr)
		}
	}
	return errors.Join(settingsErr, s.Reload(ctx))
}
