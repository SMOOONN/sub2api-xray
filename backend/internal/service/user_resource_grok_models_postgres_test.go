//go:build unit

package service

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestUserAccountWritesPreserveServerModelObservations(t *testing.T) {
	_, db := scopedAdminAccountTestDB(t)
	_, err := db.Exec(`ALTER TABLE accounts ADD COLUMN owner_user_id BIGINT, ADD COLUMN extra JSONB DEFAULT '{}', ADD COLUMN created_at TIMESTAMPTZ DEFAULT NOW();
INSERT INTO accounts(id,owner_user_id,extra) VALUES(91,41,'{"grok_observed_models":{"models":["fresh"]},"upstream_model_metadata":{"fresh":true},"old_setting":1}');`)
	require.NoError(t, err)
	svc := NewUserResourceService(db, nil, nil, nil)
	ctx := context.Background()
	stale := map[string]any{"extra": map[string]any{
		"grok_observed_models":    map[string]any{"models": []string{"stale"}},
		"upstream_model_metadata": map[string]any{"forged": true}, "new_setting": 2,
	}}
	require.NoError(t, svc.updateOwnedWith(ctx, db, "accounts", 41, 91, accountWritableColumns, stale))
	var stored string
	require.NoError(t, db.QueryRow(`SELECT extra::text FROM accounts WHERE id=91`).Scan(&stored))
	require.JSONEq(t, `{"grok_observed_models":{"models":["fresh"]},"upstream_model_metadata":{"fresh":true},"new_setting":2}`, stored)

	id, err := svc.insertOwnedWith(ctx, db, "accounts", 41, accountWritableColumns, stale, nil)
	require.NoError(t, err)
	require.NoError(t, db.QueryRow(`SELECT extra::text FROM accounts WHERE id=$1`, id).Scan(&stored))
	require.JSONEq(t, `{"new_setting":2}`, stored)
	// A later form cannot introduce an observation when no server snapshot exists.
	require.NoError(t, svc.updateOwnedWith(ctx, db, "accounts", 41, id, accountWritableColumns, stale))
	require.NoError(t, db.QueryRow(`SELECT extra::text FROM accounts WHERE id=$1`, id).Scan(&stored))
	require.JSONEq(t, `{"new_setting":2}`, stored)
}
