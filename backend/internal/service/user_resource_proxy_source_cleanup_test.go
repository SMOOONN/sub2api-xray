//go:build unit

package service

import (
	"context"
	"database/sql"
	"strings"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/migrations"
	"github.com/lib/pq"
	"github.com/stretchr/testify/require"
)

func proxySourceCleanupTestDB(t *testing.T) (*sql.DB, *UserResourceService) {
	t.Helper()
	_, db := scopedAdminAccountTestDB(t)
	_, err := db.Exec(`
ALTER TABLE accounts ADD COLUMN owner_user_id BIGINT, ADD COLUMN proxy_fallback_origin_id BIGINT, ADD COLUMN extra JSONB DEFAULT '{}';
ALTER TABLE proxies ADD COLUMN owner_user_id BIGINT, ADD COLUMN is_public BOOLEAN DEFAULT false,
 ADD COLUMN name TEXT, ADD COLUMN kind TEXT DEFAULT 'standard', ADD COLUMN fallback_mode TEXT DEFAULT 'none',
 ADD COLUMN backup_proxy_id BIGINT REFERENCES proxies(id), ADD COLUMN expiry_warn_days INT DEFAULT 7,
 ADD COLUMN extra JSONB DEFAULT '{}', ADD COLUMN created_at TIMESTAMPTZ DEFAULT NOW(), ADD COLUMN updated_at TIMESTAMPTZ DEFAULT NOW();
CREATE TABLE proxy_sources(id BIGINT PRIMARY KEY, owner_user_id BIGINT, name TEXT, deleted_at TIMESTAMPTZ,
 subscription_url TEXT DEFAULT 'https://example.invalid/sub', is_public BOOLEAN DEFAULT true,
 sub_traffic_used BIGINT,sub_traffic_total BIGINT,sub_expires_at TIMESTAMPTZ,sub_info_updated_at TIMESTAMPTZ,
 last_synced_at TIMESTAMPTZ, last_sync_status TEXT, last_sync_error TEXT, last_imported_count INT, updated_at TIMESTAMPTZ DEFAULT NOW());
CREATE TABLE scheduler_outbox(id BIGSERIAL PRIMARY KEY,event_type TEXT,account_id BIGINT,group_id BIGINT,payload JSONB,created_at TIMESTAMPTZ);
INSERT INTO proxy_sources(id,owner_user_id,name) VALUES(44,9,'owned'),(45,NULL,'system');`)
	require.NoError(t, err)
	legacy, err := migrations.FS.ReadFile("186_user_resource_lifecycle_hardening.sql")
	require.NoError(t, err)
	guard, _, found := strings.Cut(string(legacy), "ALTER TABLE user_subscriptions")
	require.True(t, found)
	_, err = db.Exec(guard)
	require.NoError(t, err)
	guardSQL, err := migrations.FS.ReadFile("253_proxy_source_reference_guard.sql")
	require.NoError(t, err)
	_, err = db.Exec(string(guardSQL))
	require.NoError(t, err)
	// The additive migration must also be safe to reapply.
	_, err = db.Exec(string(guardSQL))
	require.NoError(t, err)
	return db, NewUserResourceService(db, nil, nil, nil)
}

func TestProxySourceCleanupReconcilesBindingsAndOwnership(t *testing.T) {
	db, svc := proxySourceCleanupTestDB(t)
	ctx := context.Background()
	owner := userResourceOwner(9)
	initial, err := svc.syncProxySourceNodes(ctx, owner, 44, "source", "http://1.1.1.1:8080#keep\nhttp://8.8.8.8:8080#gone", true, nil)
	require.NoError(t, err)
	require.Len(t, initial.Created, 2)
	keep, gone := urToInt64(initial.Created[0]["id"]), urToInt64(initial.Created[1]["id"])
	_, err = db.Exec(`INSERT INTO accounts(id,owner_user_id,proxy_id,extra) VALUES(901,9,$1,'{"upstream_billing_probe":{"old":true}}'),(902,10,$1,'{}')`, gone)
	require.NoError(t, err)
	_, err = db.Exec(`INSERT INTO accounts(id,owner_user_id,proxy_id,proxy_fallback_origin_id) VALUES(903,9,$2,$1)`, gone, keep)
	require.NoError(t, err)
	_, err = db.Exec(`INSERT INTO proxies(id,owner_user_id,protocol,host,port,backup_proxy_id,fallback_mode,extra) VALUES
(501,9,'http','1.0.0.1',80,$1,'proxy','{}'),
(502,10,'http','1.0.0.1',80,NULL,'none','{"source_id":44,"source_node_key":"other-owner"}'),
(503,9,'http','1.0.0.1',80,NULL,'none','{"source_id":99,"source_node_key":"other-source"}')`, gone)
	require.NoError(t, err)
	_, err = db.Exec(`INSERT INTO accounts(id,owner_user_id,proxy_id) VALUES(904,9,501)`)
	require.NoError(t, err)
	result, err := svc.syncProxySourceNodes(ctx, owner, 44, "source", "http://1.0.0.1:8080#keep", true, nil)
	require.NoError(t, err)
	require.Equal(t, 1, result.DeletedCount)
	require.Equal(t, 2, result.DetachedAccountCount)
	require.Equal(t, keep, urToInt64(result.Updated[0]["id"]))
	var valid bool
	require.NoError(t, db.QueryRow(`SELECT count(*)=2 FROM accounts WHERE id IN (901,902) AND proxy_id IS NULL AND proxy_fallback_origin_id IS NULL AND NOT(extra ? 'upstream_billing_probe')`).Scan(&valid))
	require.True(t, valid)
	require.NoError(t, db.QueryRow(`SELECT proxy_id=$1 AND proxy_fallback_origin_id IS NULL FROM accounts WHERE id=903`, keep).Scan(&valid))
	require.True(t, valid)
	require.NoError(t, db.QueryRow(`SELECT proxy_id=501 FROM accounts WHERE id=904`).Scan(&valid))
	require.True(t, valid)
	require.NoError(t, db.QueryRow(`SELECT deleted_at IS NOT NULL FROM proxies WHERE id=$1`, gone).Scan(&valid))
	require.True(t, valid)
	require.NoError(t, db.QueryRow(`SELECT count(*)=3 FROM proxies WHERE id IN (501,502,503) AND deleted_at IS NULL AND backup_proxy_id IS NULL AND fallback_mode='none'`).Scan(&valid))
	require.True(t, valid)
	var payload string
	require.NoError(t, db.QueryRow(`SELECT payload::text FROM scheduler_outbox ORDER BY id DESC LIMIT 1`).Scan(&payload))
	for _, id := range []string{"901", "902", "903", "904"} {
		require.Contains(t, payload, id)
	}
	// A repeated sync neither detaches another account nor deletes a live node.
	again, err := svc.syncProxySourceNodes(ctx, owner, 44, "source", "http://1.0.0.1:8080#keep", true, nil)
	require.NoError(t, err)
	require.Zero(t, again.DeletedCount)
	require.Zero(t, again.DetachedAccountCount)
}

func TestProxySourceCleanupPreservesNodesOnPartialOrInvalidLists(t *testing.T) {
	db, svc := proxySourceCleanupTestDB(t)
	ctx := context.Background()
	owner := userResourceOwner(9)
	initial, err := svc.syncProxySourceNodes(ctx, owner, 44, "source", "http://1.1.1.1:8080#keep\nhttp://8.8.8.8:8080#gone", false, nil)
	require.NoError(t, err)
	for _, raw := range []string{
		"http://1.1.1.1:8080#keep\ninvalid-node", "invalid-node",
		`{"outbounds":[{"type":"http","tag":"keep","server":"1.1.1.1","server_port":8080},null]}`,
		`{"outbounds":[{"type":"http","tag":"keep","server":"1.1.1.1","server_port":8080}],"endpoints":"broken"}`,
		"proxies: null",
	} {
		result, err := svc.syncProxySourceNodes(ctx, owner, 44, "source", raw, false, nil)
		require.NoError(t, err)
		require.NotEmpty(t, result.Errors)
		require.Zero(t, result.DeletedCount)
		var count int
		require.NoError(t, db.QueryRow(`SELECT count(*) FROM proxies WHERE deleted_at IS NULL`).Scan(&count))
		require.Equal(t, 2, count)
	}
	_, err = db.Exec(`INSERT INTO accounts(id,proxy_id) VALUES(901,$1)`, urToInt64(initial.Created[0]["id"]))
	require.NoError(t, err)
	empty, err := svc.syncProxySourceNodes(ctx, owner, 44, "source", "", false, nil)
	require.NoError(t, err)
	require.Equal(t, 2, empty.DeletedCount)
	require.Equal(t, 1, empty.DetachedAccountCount)
}

func TestProxySourceCleanupAcceptsStructuredEmptyLists(t *testing.T) {
	for _, raw := range []string{"proxies: []", `{"outbounds":[]}`, `{"outbounds":[],"endpoints":[]}`, `[]`} {
		t.Run(raw, func(t *testing.T) {
			db, svc := proxySourceCleanupTestDB(t)
			ctx := context.Background()
			_, err := svc.syncProxySourceNodes(ctx, nil, 45, "system", "http://1.1.1.1:8080#node", true, nil)
			require.NoError(t, err)
			result, err := svc.syncProxySourceNodes(ctx, nil, 45, "system", raw, true, nil)
			require.NoError(t, err)
			require.Empty(t, result.Errors)
			require.Equal(t, 1, result.DeletedCount)
			var count int
			require.NoError(t, db.QueryRow(`SELECT count(*) FROM proxies WHERE deleted_at IS NULL`).Scan(&count))
			require.Zero(t, count)
		})
	}
}

func TestProxySourceCleanupAllowsNetReplacementAtQuota(t *testing.T) {
	db, svc := proxySourceCleanupTestDB(t)
	ctx := context.Background()
	owner := userResourceOwner(9)
	_, err := svc.syncProxySourceNodes(ctx, owner, 44, "source", "http://1.1.1.1:8080#keep\nhttp://8.8.8.8:8080#gone", false, nil)
	require.NoError(t, err)
	_, err = db.Exec(`INSERT INTO proxies(owner_user_id,name,protocol,host,port) SELECT 9,'manual','http','1.0.0.1',80 FROM generate_series(1,$1)`, userResourceMaxProxies-2)
	require.NoError(t, err)
	result, err := svc.syncProxySourceNodes(ctx, owner, 44, "source", "http://1.1.1.1:8080#keep\nhttp://9.9.9.9:8080#new", false, nil)
	require.NoError(t, err)
	require.Equal(t, 1, result.DeletedCount)
	require.Len(t, result.Created, 1)
	var count int
	require.NoError(t, db.QueryRow(`SELECT count(*) FROM proxies WHERE deleted_at IS NULL`).Scan(&count))
	require.Equal(t, userResourceMaxProxies, count)
	_, err = svc.syncProxySourceNodes(ctx, owner, 44, "source", "http://1.1.1.1:8080#keep\nhttp://8.8.8.8:8080#another\ninvalid-node", false, nil)
	require.Error(t, err, "partial lists cannot free capacity by assuming missing nodes were removed")
}

func TestProxySourceCleanupAllowsConcurrentNameEditOfBoundAccount(t *testing.T) {
	db, svc := proxySourceCleanupTestDB(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	result, err := svc.syncProxySourceNodes(ctx, nil, 45, "source", "http://1.1.1.1:8080#node", true, nil)
	require.NoError(t, err)
	id := urToInt64(result.Created[0]["id"])
	_, err = db.Exec(`INSERT INTO accounts(id,proxy_id) VALUES(901,$1)`, id)
	require.NoError(t, err)
	editor, err := db.BeginTx(ctx, nil)
	require.NoError(t, err)
	defer func() { _ = editor.Rollback() }()
	var lockedID int64
	require.NoError(t, editor.QueryRowContext(ctx, `SELECT id FROM accounts WHERE id=901 FOR NO KEY UPDATE`).Scan(&lockedID))
	cleanup, err := db.BeginTx(ctx, nil)
	require.NoError(t, err)
	defer func() { _ = cleanup.Rollback() }()
	require.NoError(t, cleanup.QueryRowContext(ctx, `SELECT id FROM proxies WHERE id=$1 FOR UPDATE`, id).Scan(&lockedID))
	// Admin account persistence writes proxy_id even for a name-only edit.
	// Keeping the same reference must not wait on the proxy held by cleanup.
	_, err = editor.ExecContext(ctx, `UPDATE accounts SET name='edited',proxy_id=$1 WHERE id=901`, id)
	require.NoError(t, err)
	require.NoError(t, editor.Commit())
	_, _, detached, err := svc.deleteMissingProxySourceNodesWith(ctx, cleanup, nil, 45, nil)
	require.NoError(t, err)
	require.Equal(t, 1, detached)
	require.NoError(t, cleanup.Commit())
}

func TestProxySourceCleanupDeleteSourceIsAtomic(t *testing.T) {
	db, svc := proxySourceCleanupTestDB(t)
	ctx := context.Background()
	initial, err := svc.syncProxySourceNodes(ctx, nil, 45, "system", "http://1.1.1.1:8080#node", true, nil)
	require.NoError(t, err)
	id := urToInt64(initial.Created[0]["id"])
	_, err = db.Exec(`UPDATE proxies SET status='disabled' WHERE id=$1`, id)
	require.NoError(t, err)
	_, err = db.Exec(`INSERT INTO accounts(id,proxy_id) VALUES(901,$1)`, id)
	require.NoError(t, err)
	require.ErrorIs(t, svc.DeleteProxySource(ctx, 9, 45), ErrUserResourceNotFound)
	_, err = db.Exec(`CREATE FUNCTION fail_source_cleanup() RETURNS TRIGGER AS $$ BEGIN RAISE EXCEPTION 'fixture cleanup failure'; END; $$ LANGUAGE plpgsql;
CREATE TRIGGER fail_source_cleanup BEFORE UPDATE OF deleted_at ON proxies FOR EACH ROW EXECUTE FUNCTION fail_source_cleanup();`)
	require.NoError(t, err)
	require.Error(t, svc.DeleteSystemProxySource(ctx, 45))
	var valid bool
	require.NoError(t, db.QueryRow(`SELECT deleted_at IS NULL FROM proxy_sources WHERE id=45`).Scan(&valid))
	require.True(t, valid)
	require.NoError(t, db.QueryRow(`SELECT proxy_id=$1 FROM accounts WHERE id=901`, id).Scan(&valid))
	require.True(t, valid)
	_, err = db.Exec(`DROP TRIGGER fail_source_cleanup ON proxies`)
	require.NoError(t, err)
	require.NoError(t, svc.DeleteSystemProxySource(ctx, 45))
	require.NoError(t, db.QueryRow(`SELECT deleted_at IS NOT NULL FROM proxies WHERE id=$1`, id).Scan(&valid))
	require.True(t, valid)
	require.NoError(t, db.QueryRow(`SELECT proxy_id IS NULL FROM accounts WHERE id=901`).Scan(&valid))
	require.True(t, valid)
}

func TestProxySourceCleanupRejectsStaleConcurrentBindings(t *testing.T) {
	db, svc := proxySourceCleanupTestDB(t)
	ctx := context.Background()
	initial, err := svc.syncProxySourceNodes(ctx, nil, 45, "system", "http://1.1.1.1:8080#node", true, nil)
	require.NoError(t, err)
	id := urToInt64(initial.Created[0]["id"])
	_, err = db.Exec(`INSERT INTO accounts(id) VALUES(901); INSERT INTO proxies(id,protocol,host,port) VALUES(501,'http','1.0.0.1',80)`)
	require.NoError(t, err)
	tx, err := db.BeginTx(ctx, nil)
	require.NoError(t, err)
	defer func() { _ = tx.Rollback() }()
	_, _, _, err = svc.deleteMissingProxySourceNodesWith(ctx, tx, nil, 45, nil)
	require.NoError(t, err)
	// The write must wait on the node lock instead of installing a stale binding.
	conn, err := db.Conn(ctx)
	require.NoError(t, err)
	defer func() { _ = conn.Close() }()
	_, err = conn.ExecContext(ctx, `SET lock_timeout = '100ms'`)
	require.NoError(t, err)
	_, err = conn.ExecContext(ctx, `UPDATE accounts SET proxy_id=$1 WHERE id=901`, id)
	var pgErr *pq.Error
	require.ErrorAs(t, err, &pgErr)
	require.Equal(t, pq.ErrorCode("55P03"), pgErr.Code)
	require.NoError(t, tx.Commit())
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	for _, query := range []string{
		`UPDATE accounts SET proxy_id=$1 WHERE id=901`,
		`UPDATE accounts SET proxy_fallback_origin_id=$1 WHERE id=901`,
		`UPDATE proxies SET backup_proxy_id=$1 WHERE id=501`,
	} {
		_, err = db.ExecContext(ctx, query, id)
		require.ErrorAs(t, err, &pgErr)
		require.Equal(t, pq.ErrorCode("23514"), pgErr.Code)
	}
}

func TestProxySourceCleanupDiscardsResponsesFromOlderSourceRevisions(t *testing.T) {
	for _, change := range []string{"url changed during fetch", "newer sync completed first"} {
		t.Run(change, func(t *testing.T) {
			db, svc := proxySourceCleanupTestDB(t)
			ctx := context.Background()
			initial, err := svc.syncProxySourceNodes(ctx, nil, 45, "source", "http://1.1.1.1:8080#old", true, nil)
			require.NoError(t, err)
			originalID := urToInt64(initial.Created[0]["id"])
			stale := &proxySourceSyncSnapshot{info: parseProxySubscriptionUserInfo("total=1")}
			require.NoError(t, db.QueryRow(`SELECT xmin::text FROM proxy_sources WHERE id=45`).Scan(&stale.revision))
			currentID := originalID
			if change == "url changed during fetch" {
				_, err = db.Exec(`UPDATE proxy_sources SET subscription_url='https://example.invalid/changed' WHERE id=45`)
				require.NoError(t, err)
			} else {
				fresh := &proxySourceSyncSnapshot{revision: stale.revision, info: parseProxySubscriptionUserInfo("total=999")}
				result, syncErr := svc.syncProxySourceNodes(ctx, nil, 45, "source", "http://8.8.8.8:8080#new", true, fresh)
				require.NoError(t, syncErr)
				currentID = urToInt64(result.Created[0]["id"])
			}
			_, err = db.Exec(`INSERT INTO accounts(id,proxy_id) VALUES(901,$1)`, currentID)
			require.NoError(t, err)
			var before string
			require.NoError(t, db.QueryRow(`SELECT row_to_json(proxy_sources)::text FROM proxy_sources WHERE id=45`).Scan(&before))
			// The old HTTP response is empty. Applying it would delete the current
			// node and switch the account to direct connection.
			_, err = svc.syncProxySourceNodes(ctx, nil, 45, "source", "", true, stale)
			require.ErrorIs(t, err, errProxySourceChanged)
			// A late network failure must not overwrite the newer sync status either.
			require.ErrorIs(t, svc.recordProxySourceSyncError(ctx, nil, 45, stale.revision, context.DeadlineExceeded), errProxySourceChanged)
			var after string
			require.NoError(t, db.QueryRow(`SELECT row_to_json(proxy_sources)::text FROM proxy_sources WHERE id=45`).Scan(&after))
			require.JSONEq(t, before, after)
			var preserved bool
			require.NoError(t, db.QueryRow(`SELECT a.proxy_id=$1 AND p.deleted_at IS NULL FROM accounts a JOIN proxies p ON p.id=a.proxy_id WHERE a.id=901`, currentID).Scan(&preserved))
			require.True(t, preserved)
		})
	}
}
