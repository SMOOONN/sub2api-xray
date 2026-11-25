package service

import (
	"context"
	"database/sql"
	"errors"
	"strconv"
	"time"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/lib/pq"
)

var errProxySourceChanged = infraerrors.Conflict("PROXY_SOURCE_CHANGED", "Subscription source changed during sync; refresh and retry")

type proxySourceSyncSnapshot struct {
	revision string
	info     proxySubscriptionUserInfo
}

// Retry the entire rolled-back transaction when concurrent editors or another
// source acquire related rows in the opposite order. Network fetching is outside
// this boundary, and no externally visible database changes have committed.
func retryProxySourceTransaction(ctx context.Context, apply func() error) error {
	for attempt := 0; ; attempt++ {
		err := apply()
		var pgErr *pq.Error
		if attempt >= 2 || !errors.As(err, &pgErr) || (pgErr.Code != "40P01" && pgErr.Code != "40001") {
			return err
		}
		timer := time.NewTimer(time.Duration(attempt+1) * 20 * time.Millisecond)
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case <-timer.C:
		}
	}
}

// deleteMissingProxySourceNodesWith is shared by source deletion and successful
// subscription reconciliation. The caller holds the source lock and owns the
// transaction, including outbox delivery and runtime cleanup.
func (s *UserResourceService) deleteMissingProxySourceNodesWith(ctx context.Context, db userResourceDBTX, ownerID *int64, sourceID int64, activeKeys []string) ([]int64, []int64, int, error) {
	rows, err := db.QueryContext(ctx, `
SELECT id FROM proxies
WHERE owner_user_id IS NOT DISTINCT FROM $1 AND deleted_at IS NULL
  AND extra->>'source_id' = $2
  AND NOT (COALESCE(extra->>'source_node_key', '') = ANY($3))
ORDER BY id FOR UPDATE`, userResourceOwnerValue(ownerID), strconv.FormatInt(sourceID, 10), pq.Array(append([]string{}, activeKeys...)))
	if err != nil {
		return nil, nil, 0, err
	}
	ids, err := collectProxySourceIDs(rows)
	if err != nil || len(ids) == 0 {
		return ids, nil, 0, err
	}
	// Invalidate users of a surviving proxy whose backup is about to disappear
	// before removing that reference. Do not detach those surviving primaries.
	accountIDs, err := clearProxyDependentProbeWith(ctx, db, ids)
	if err != nil {
		return nil, nil, 0, err
	}
	rows, err = db.QueryContext(ctx, `
UPDATE accounts SET proxy_id = NULL, proxy_fallback_origin_id = NULL, updated_at = NOW()
WHERE proxy_id = ANY($1) AND deleted_at IS NULL RETURNING id`, pq.Array(ids))
	if err != nil {
		return nil, nil, 0, err
	}
	detached, err := collectProxySourceIDs(rows)
	if err != nil {
		return nil, nil, 0, err
	}
	rows, err = db.QueryContext(ctx, `
UPDATE accounts SET proxy_fallback_origin_id = NULL, updated_at = NOW()
WHERE proxy_fallback_origin_id = ANY($1) AND deleted_at IS NULL RETURNING id`, pq.Array(ids))
	if err != nil {
		return nil, nil, 0, err
	}
	origins, err := collectProxySourceIDs(rows)
	if err != nil {
		return nil, nil, 0, err
	}
	if _, err := db.ExecContext(ctx, `
UPDATE proxies SET backup_proxy_id = NULL, fallback_mode = 'none', updated_at = NOW()
WHERE backup_proxy_id = ANY($1) AND deleted_at IS NULL`, pq.Array(ids)); err != nil {
		return nil, nil, 0, err
	}
	// Keep the existing soft-delete convention. The reference guard migration
	// blocks concurrent account/backup bindings until this transaction commits.
	if _, err := db.ExecContext(ctx, `
UPDATE proxies SET status = 'disabled', deleted_at = NOW(), updated_at = NOW()
WHERE id = ANY($1) AND deleted_at IS NULL`, pq.Array(ids)); err != nil {
		return nil, nil, 0, err
	}
	accountIDs = uniquePositiveInt64s(append(append(accountIDs, detached...), origins...))
	return ids, accountIDs, len(detached), nil
}

// Fully consume and close RETURNING rows before issuing the next statement on
// the transaction connection.
func collectProxySourceIDs(rows *sql.Rows) ([]int64, error) {
	defer func() { _ = rows.Close() }()
	ids := []int64{}
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return ids, rows.Close()
}
