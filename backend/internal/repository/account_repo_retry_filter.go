package repository

import (
	"context"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
)

var _ service.RetryFilterBindingSnapshotSynchronizer = (*accountRepository)(nil)

// SyncRetryFilterBindingAccountSnapshots is called only after the binding
// transaction commits. Detach client cancellation and supply a short context
// deadline; cache I/O still follows its own configured timeouts. The existing
// outbox remains responsible for cache failures.
func (r *accountRepository) SyncRetryFilterBindingAccountSnapshots(ctx context.Context, accountIDs []int64) {
	propagationCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 2*time.Second)
	defer cancel()
	r.syncSchedulerAccountSnapshots(propagationCtx, accountIDs)
}
