//go:build integration

package repository

import (
	"context"
	"fmt"
	"testing"
	"time"

	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestBatchDisableUsersHonorsOuterTransactionRollback(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	f := newMCPOAuthIntegrationFixture(t, ctx)
	repo := NewUserRepository(integrationEntClient, integrationDB).(service.UserEmailBlacklistRepository)
	tx := testEntTx(t)
	disabled, err := repo.BatchDisableUsers(dbent.NewTxContext(ctx, tx), []int64{f.userID})
	require.NoError(t, err)
	require.Equal(t, []int64{f.userID}, disabled)
	inside, err := tx.User.Get(ctx, f.userID)
	require.NoError(t, err)
	require.Equal(t, service.StatusDisabled, inside.Status)
	require.NoError(t, tx.Rollback())
	outside, err := integrationEntClient.User.Get(ctx, f.userID)
	require.NoError(t, err)
	require.Equal(t, service.StatusActive, outside.Status)
	grant, err := f.repo.GetGrant(ctx, f.grantID)
	require.NoError(t, err)
	require.Nil(t, grant.RevokedAt)
}

type batchLifecycleAuthInvalidator struct {
	userIDs  []int64
	groupIDs []int64
}

func (*batchLifecycleAuthInvalidator) InvalidateAuthCacheByKey(context.Context, string) {}

func (s *batchLifecycleAuthInvalidator) InvalidateAuthCacheByUserID(_ context.Context, id int64) {
	s.userIDs = append(s.userIDs, id)
}

func (s *batchLifecycleAuthInvalidator) InvalidateAuthCacheByGroupID(_ context.Context, id int64) {
	s.groupIDs = append(s.groupIDs, id)
}

func TestAdminBatchDisableUsersDeprovisionsPrivateResources(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	f := newMCPOAuthIntegrationFixture(t, ctx)
	client := integrationEntClient
	recipient := mustCreateUser(t, client, &service.User{Email: fmt.Sprintf("batch-recipient-%d@example.com", f.userID)})
	admin := mustCreateUser(t, client, &service.User{Email: fmt.Sprintf("batch-admin-%d@example.com", f.userID), Role: service.RoleAdmin})
	t.Cleanup(func() {
		for _, cleanup := range []struct {
			query string
			args  []any
		}{
			{`DELETE FROM scheduler_outbox WHERE account_id IN (SELECT id FROM accounts WHERE owner_user_id=$1) OR group_id IN (SELECT id FROM groups WHERE owner_user_id=$1)`, []any{f.userID}},
			{`DELETE FROM user_subscriptions WHERE managed_by_user_id=$1 OR user_id=$2`, []any{f.userID, recipient.ID}},
			{`DELETE FROM proxy_sources WHERE owner_user_id=$1`, []any{f.userID}},
			{`DELETE FROM accounts WHERE owner_user_id=$1`, []any{f.userID}},
			{`DELETE FROM groups WHERE owner_user_id IN ($1,$2)`, []any{f.userID, recipient.ID}},
			{`DELETE FROM proxies WHERE owner_user_id=$1`, []any{f.userID}},
		} {
			// Every target belongs to the synthetic users created by this test.
			_, err := integrationDB.ExecContext(context.Background(), cleanup.query, cleanup.args...)
			require.NoError(t, err)
		}
		_, err := integrationDB.ExecContext(context.Background(), `DELETE FROM users WHERE id IN ($1,$2)`, recipient.ID, admin.ID)
		require.NoError(t, err)
	})
	owned := mustCreateGroup(t, client, &service.Group{Name: "batch-owned", OwnerUserID: &f.userID, SubscriptionType: "subscription"})
	unrelated := mustCreateGroup(t, client, &service.Group{Name: "batch-other", OwnerUserID: &recipient.ID})
	proxy, err := client.Proxy.Create().SetName("batch-owned-proxy").SetOwnerUserID(f.userID).
		SetProtocol("http").SetHost("proxy.example.test").SetPort(8080).Save(ctx)
	require.NoError(t, err)
	account := mustCreateAccount(t, client, &service.Account{Name: "batch-owned-account", OwnerUserID: &f.userID, ProxyID: &proxy.ID})
	source, err := client.ProxySource.Create().SetName("batch-owned-source").SetOwnerUserID(f.userID).
		SetSubscriptionURL("https://example.invalid/fixture").Save(ctx)
	require.NoError(t, err)
	sub, err := client.UserSubscription.Create().SetUserID(recipient.ID).SetGroupID(owned.ID).
		SetManagedByUserID(f.userID).SetStartsAt(time.Now()).SetExpiresAt(time.Now().Add(time.Hour)).Save(ctx)
	require.NoError(t, err)
	invalidator := &batchLifecycleAuthInvalidator{}
	users := NewUserRepository(client, integrationDB)
	adminService := service.NewAdminService(
		nil, users, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil,
		invalidator, integrationDB, client, nil, nil, nil, nil, nil, nil, nil, nil, nil,
	)
	affected, err := adminService.BatchDisableUsers(ctx, []int64{f.userID, admin.ID, f.userID})
	require.NoError(t, err)
	require.Equal(t, 1, affected)
	user, err := client.User.Get(ctx, f.userID)
	require.NoError(t, err)
	require.Equal(t, service.StatusDisabled, user.Status)
	group, err := client.Group.Get(ctx, owned.ID)
	require.NoError(t, err)
	require.Equal(t, service.StatusDisabled, group.Status)
	currentAccount, err := client.Account.Get(ctx, account.ID)
	require.NoError(t, err)
	require.Equal(t, service.StatusDisabled, currentAccount.Status)
	require.False(t, currentAccount.Schedulable)
	currentProxy, err := client.Proxy.Get(ctx, proxy.ID)
	require.NoError(t, err)
	require.Equal(t, service.StatusDisabled, currentProxy.Status)
	var revoked bool
	require.NoError(t, integrationDB.QueryRowContext(ctx,
		`SELECT deleted_at IS NOT NULL AND revoked_by_user_id=$2 FROM user_subscriptions WHERE id=$1`, sub.ID, f.userID).Scan(&revoked))
	require.True(t, revoked)
	var sourceDisabled bool
	require.NoError(t, integrationDB.QueryRowContext(ctx,
		`SELECT deleted_at IS NOT NULL FROM proxy_sources WHERE id=$1`, source.ID).Scan(&sourceDisabled))
	require.True(t, sourceDisabled)
	grant, err := f.repo.GetGrant(ctx, f.grantID)
	require.NoError(t, err)
	require.NotNil(t, grant.RevokedAt)
	otherGroup, err := client.Group.Get(ctx, unrelated.ID)
	require.NoError(t, err)
	require.Equal(t, service.StatusActive, otherGroup.Status)
	adminUser, err := client.User.Get(ctx, admin.ID)
	require.NoError(t, err)
	require.Equal(t, service.StatusActive, adminUser.Status)
	require.ElementsMatch(t, []int64{f.userID, recipient.ID}, invalidator.userIDs)
	require.Equal(t, []int64{owned.ID}, invalidator.groupIDs)
}
