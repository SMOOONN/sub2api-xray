//go:build integration

package repository

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

type retryFilterBindingCache struct {
	service.SchedulerCache
	writes int
	fail   bool
	onSet  func(context.Context, *service.Account)
}

func (c *retryFilterBindingCache) SetAccount(ctx context.Context, account *service.Account) error {
	c.writes++
	if c.onSet != nil {
		c.onSet(ctx, account)
	}
	if c.fail {
		return errors.New("injected snapshot write failure")
	}
	return c.SchedulerCache.SetAccount(ctx, account)
}

type retryFilterBindingFixture struct {
	repo     *accountRepository
	cache    *retryFilterBindingCache
	accounts []int64
	groupID  int64
}

func newRetryFilterBindingFixture(t *testing.T) *retryFilterBindingFixture {
	t.Helper()
	ctx := context.Background()
	client := testEntClient(t) // The service owns its transaction; do not nest testEntTx.
	f := &retryFilterBindingFixture{cache: &retryFilterBindingCache{SchedulerCache: NewSchedulerCache(testRedis(t))}}
	f.repo = newAccountRepositoryWithSQL(client, integrationDB, f.cache)
	t.Cleanup(func() {
		for _, id := range f.accounts {
			_, err := integrationDB.ExecContext(ctx, "DELETE FROM scheduler_outbox WHERE account_id = $1", id)
			require.NoError(t, err)
			_, err = integrationDB.ExecContext(ctx, "DELETE FROM accounts WHERE id = $1", id)
			require.NoError(t, err)
		}
		if f.groupID != 0 {
			_, err := integrationDB.ExecContext(ctx, "DELETE FROM scheduler_outbox WHERE group_id = $1", f.groupID)
			require.NoError(t, err)
			_, err = integrationDB.ExecContext(ctx, "DELETE FROM groups WHERE id = $1", f.groupID)
			require.NoError(t, err)
		}
	})
	for i := 0; i < 2; i++ {
		account := mustCreateAccount(t, client, &service.Account{
			Name:        fmt.Sprintf("retry-binding-%d-%d", time.Now().UnixNano(), i),
			Credentials: map[string]any{"retry_filter_preset_id": 100 + i, "api_key": "preserved-test-value"},
		})
		f.accounts = append(f.accounts, account.ID)
		stored, err := f.repo.GetByID(ctx, account.ID)
		require.NoError(t, err)
		require.NoError(t, f.cache.SchedulerCache.SetAccount(ctx, stored))
	}
	f.groupID = mustCreateGroup(t, client, &service.Group{Name: fmt.Sprintf("retry-binding-group-%d", time.Now().UnixNano())}).ID
	return f
}

func newRetryFilterBindingAdmin(t *testing.T, repo service.AdminAccountRepository) service.RetryFilterBindingAtomicUpdater {
	t.Helper()
	client := testEntClient(t)
	admin := service.NewAdminService(
		nil, nil, NewAdminGroupRepository(client, integrationDB), repo,
		nil, nil, nil, nil, nil, nil, nil, nil, nil,
		integrationDB, client, nil, nil, nil, nil, nil, nil, nil, nil, nil,
	)
	updater, ok := admin.(service.RetryFilterBindingAtomicUpdater)
	require.True(t, ok)
	return updater
}

func requireRetryFilterBinding(t *testing.T, account *service.Account, preset int64) {
	t.Helper()
	require.NotNil(t, account)
	if preset == 0 {
		require.NotContains(t, account.Credentials, "retry_filter_preset_id")
	} else {
		require.EqualValues(t, preset, account.Credentials["retry_filter_preset_id"])
	}
	require.Equal(t, "preserved-test-value", account.Credentials["api_key"])
}

func (f *retryFilterBindingFixture) requireBindings(t *testing.T, presets ...int64) {
	t.Helper()
	for i, id := range f.accounts {
		stored, err := f.repo.GetByID(context.Background(), id)
		require.NoError(t, err)
		requireRetryFilterBinding(t, stored, presets[i])
		cached, err := f.cache.GetAccount(context.Background(), id)
		require.NoError(t, err)
		requireRetryFilterBinding(t, cached, presets[i])
	}
}

func TestRetryFilterBindingsPublishCommittedSnapshotsImmediately(t *testing.T) {
	f := newRetryFilterBindingFixture(t)
	f.requireBindings(t, 100, 101)
	f.cache.onSet = func(ctx context.Context, account *service.Account) {
		require.Nil(t, dbent.TxFromContext(ctx), "snapshots must be read outside the completed transaction")
		stored, err := f.repo.GetByID(context.Background(), account.ID)
		require.NoError(t, err)
		require.Equal(t, stored.Credentials, account.Credentials, "another connection must see the committed binding")
	}
	admin := newRetryFilterBindingAdmin(t, f.repo)
	preset := int64(202)
	require.NoError(t, admin.UpdateRetryFilterBindingsAtomically(context.Background(), service.RetryFilterBindingMutation{
		PresetID: &preset, AccountIDs: f.accounts, GroupIDs: []int64{f.groupID},
	}))
	// No outbox worker or sleep: the first read after the API completes must be fresh.
	f.requireBindings(t, 202, 202)
	require.Equal(t, 2, f.cache.writes)

	preset = 303
	require.NoError(t, admin.UpdateRetryFilterBindingsAtomically(context.Background(), service.RetryFilterBindingMutation{
		PresetID: &preset, AccountIDs: []int64{f.accounts[0], f.accounts[0]}, ClearAccountIDs: []int64{f.accounts[1]}, ClearGroupIDs: []int64{f.groupID},
	}))
	f.requireBindings(t, 303, 0)
	require.Equal(t, 4, f.cache.writes, "set/clear union is deduplicated")
	group, err := NewGroupRepository(testEntClient(t), integrationDB).GetByIDLite(context.Background(), f.groupID)
	require.NoError(t, err)
	require.Nil(t, group.RetryFilterConfig.PresetID)

	require.NoError(t, admin.UpdateRetryFilterBindingsAtomically(context.Background(), service.RetryFilterBindingMutation{AccountIDs: f.accounts}))
	f.requireBindings(t, 0, 0)
	require.Equal(t, 6, f.cache.writes)
}

type retryFilterFailSecondAccount struct {
	*accountRepository
	writes int
}

func (r *retryFilterFailSecondAccount) Update(ctx context.Context, account *service.Account) error {
	r.writes++
	if r.writes == 2 {
		return errors.New("injected second account failure")
	}
	return r.accountRepository.Update(ctx, account)
}

func TestRetryFilterBindingsRollbackDoesNotPublishSnapshots(t *testing.T) {
	f := newRetryFilterBindingFixture(t)
	repo := &retryFilterFailSecondAccount{accountRepository: f.repo}
	preset := int64(202)
	err := newRetryFilterBindingAdmin(t, repo).UpdateRetryFilterBindingsAtomically(context.Background(), service.RetryFilterBindingMutation{
		PresetID: &preset, AccountIDs: f.accounts,
	})
	require.EqualError(t, err, "injected second account failure")
	require.Equal(t, 2, repo.writes)
	f.requireBindings(t, 100, 101)
	require.Zero(t, f.cache.writes)
	for _, id := range f.accounts {
		var count int
		require.NoError(t, integrationDB.QueryRow("SELECT count(*) FROM scheduler_outbox WHERE account_id = $1", id).Scan(&count))
		require.Zero(t, count, "rolled-back writes must not leave an outbox event")
	}
}

// Embedding only the public admin repository contract deliberately hides the
// optional snapshot capability, even though the underlying repository has it.
type retryFilterAccountWithoutSnapshots struct {
	service.AdminAccountRepository
	writes int
}

func (r *retryFilterAccountWithoutSnapshots) Update(ctx context.Context, account *service.Account) error {
	r.writes++
	return r.AdminAccountRepository.Update(ctx, account)
}

func TestRetryFilterBindingsRequireSnapshotsBeforeAccountWrites(t *testing.T) {
	f := newRetryFilterBindingFixture(t)
	repo := &retryFilterAccountWithoutSnapshots{AdminAccountRepository: f.repo}
	admin := newRetryFilterBindingAdmin(t, repo)
	preset := int64(202)
	err := admin.UpdateRetryFilterBindingsAtomically(context.Background(), service.RetryFilterBindingMutation{PresetID: &preset, AccountIDs: f.accounts})
	require.ErrorContains(t, err, "snapshot synchronization is unavailable")
	require.Zero(t, repo.writes)
	f.requireBindings(t, 100, 101)
	require.NoError(t, admin.UpdateRetryFilterBindingsAtomically(context.Background(), service.RetryFilterBindingMutation{
		PresetID: &preset, GroupIDs: []int64{f.groupID},
	}))
	group, err := NewGroupRepository(testEntClient(t), integrationDB).GetByIDLite(context.Background(), f.groupID)
	require.NoError(t, err)
	require.NotNil(t, group.RetryFilterConfig.PresetID)
	require.Equal(t, preset, *group.RetryFilterConfig.PresetID)
}

func TestRetryFilterBindingsCacheFailureKeepsCommitAndOutbox(t *testing.T) {
	f := newRetryFilterBindingFixture(t)
	f.cache.fail = true
	preset := int64(202)
	require.NoError(t, newRetryFilterBindingAdmin(t, f.repo).UpdateRetryFilterBindingsAtomically(context.Background(), service.RetryFilterBindingMutation{
		PresetID: &preset, AccountIDs: f.accounts,
	}))
	require.Equal(t, 2, f.cache.writes)
	for i, id := range f.accounts {
		stored, err := f.repo.GetByID(context.Background(), id)
		require.NoError(t, err)
		requireRetryFilterBinding(t, stored, 202)
		cached, err := f.cache.GetAccount(context.Background(), id)
		require.NoError(t, err)
		requireRetryFilterBinding(t, cached, int64(100+i))
		var count int
		require.NoError(t, integrationDB.QueryRow("SELECT count(*) FROM scheduler_outbox WHERE account_id = $1", id).Scan(&count))
		require.Equal(t, 1, count, "the committed outbox event remains available for recovery")
	}
}

type retryFilterCancelOnSnapshotSync struct {
	*accountRepository
	cancel context.CancelFunc
}

func (r *retryFilterCancelOnSnapshotSync) SyncRetryFilterBindingAccountSnapshots(ctx context.Context, ids []int64) {
	r.cancel() // Simulate the client leaving immediately after the transaction commits.
	r.accountRepository.SyncRetryFilterBindingAccountSnapshots(ctx, ids)
}

func TestRetryFilterBindingsPropagateAfterCancellationWithDeadline(t *testing.T) {
	f := newRetryFilterBindingFixture(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	f.cache.onSet = func(ctx context.Context, _ *service.Account) {
		require.NoError(t, ctx.Err())
		deadline, ok := ctx.Deadline()
		require.True(t, ok)
		require.Positive(t, time.Until(deadline))
		require.LessOrEqual(t, time.Until(deadline), 2*time.Second)
	}
	preset := int64(202)
	repo := &retryFilterCancelOnSnapshotSync{accountRepository: f.repo, cancel: cancel}
	require.NoError(t, newRetryFilterBindingAdmin(t, repo).UpdateRetryFilterBindingsAtomically(ctx, service.RetryFilterBindingMutation{
		PresetID: &preset, AccountIDs: f.accounts,
	}))
	require.ErrorIs(t, ctx.Err(), context.Canceled)
	f.requireBindings(t, 202, 202)
	require.Equal(t, 2, f.cache.writes)
}
