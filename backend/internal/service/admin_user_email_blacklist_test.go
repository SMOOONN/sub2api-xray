//go:build unit

package service

import (
	"context"
	"errors"
	"testing"

	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/stretchr/testify/require"
)

type emailBlacklistUserRepoStub struct {
	*userRepoStub
	probeKeys   []string
	candidates  []User
	listErr     error
	disableIDs  []int64
	disabled    []int64
	disableErr  error
	listCalls   int
	disableCall int
	onDisable   func(context.Context)
}

func (s *emailBlacklistUserRepoStub) ListActiveUsersByEmailAliasProbeKeys(_ context.Context, keys []string) ([]User, error) {
	s.listCalls++
	s.probeKeys = append([]string(nil), keys...)
	return s.candidates, s.listErr
}

func (s *emailBlacklistUserRepoStub) BatchDisableUsers(ctx context.Context, userIDs []int64) ([]int64, error) {
	s.disableCall++
	s.disableIDs = append([]int64(nil), userIDs...)
	if s.onDisable != nil {
		s.onDisable(ctx)
	}
	return s.disabled, s.disableErr
}

func TestAdminService_FindActiveUsersByBlacklistedEmails_FiltersProbeOvermatch(t *testing.T) {
	repo := &emailBlacklistUserRepoStub{
		userRepoStub: &userRepoStub{},
		candidates: []User{
			{ID: 1, Email: "a.b@gmail.com"},
			{ID: 2, Email: "ab+x@googlemail.com"},
			{ID: 3, Email: "first.last@company.example"},
			{ID: 4, Email: "firstlast@company.example"},
		},
	}
	svc := &adminServiceImpl{userRepo: repo}

	users, err := svc.FindActiveUsersByBlacklistedEmails(context.Background(), []string{
		"AB@googlemail.com", "firstlast@company.example", "not-an-email", "ab@gmail.com",
	})
	require.NoError(t, err)
	ids := make([]int64, 0, len(users))
	for _, user := range users {
		ids = append(ids, user.ID)
	}
	require.Equal(t, []int64{1, 2, 4}, ids)
	require.ElementsMatch(t, []string{"ab@gmailcom", "ab@googlemailcom", "firstlast@companyexample"}, repo.probeKeys)
}

func TestAdminService_FindActiveUsersByBlacklistedEmails_NoValidEmailSkipsQuery(t *testing.T) {
	repo := &emailBlacklistUserRepoStub{userRepoStub: &userRepoStub{}}
	svc := &adminServiceImpl{userRepo: repo}

	users, err := svc.FindActiveUsersByBlacklistedEmails(context.Background(), []string{"", "bad"})
	require.NoError(t, err)
	require.Empty(t, users)
	require.Zero(t, repo.listCalls)
}

func TestAdminService_FindActiveUsersByBlacklistedEmails_RequiresRepositoryCapability(t *testing.T) {
	svc := &adminServiceImpl{userRepo: &userRepoStub{}}
	_, err := svc.FindActiveUsersByBlacklistedEmails(context.Background(), []string{"a@gmail.com"})
	require.ErrorIs(t, err, ErrServiceUnavailable)
}

func TestAdminService_BatchDisableUsers_InvalidatesOnlyDisabledUsers(t *testing.T) {
	client, mock := newEmptyDeprovisionClient(t, true)
	repo := &emailBlacklistUserRepoStub{userRepoStub: &userRepoStub{}, disabled: []int64{7}}
	invalidator := &authCacheInvalidatorStub{}
	svc := &adminServiceImpl{userRepo: repo, entClient: client, authCacheInvalidator: invalidator}

	affected, err := svc.BatchDisableUsers(context.Background(), []int64{7, 0, 7, 9, -3})
	require.NoError(t, err)
	require.Equal(t, 1, affected)
	require.Equal(t, []int64{7, 9}, repo.disableIDs)
	require.Equal(t, []int64{7}, invalidator.userIDs)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestAdminService_BatchDisableUsers_EmptyAndErrors(t *testing.T) {
	client, mock := newBatchDisableLifecycleClient(t)
	repo := &emailBlacklistUserRepoStub{userRepoStub: &userRepoStub{}}
	svc := &adminServiceImpl{userRepo: repo, entClient: client}

	affected, err := svc.BatchDisableUsers(context.Background(), []int64{0, -1})
	require.NoError(t, err)
	require.Zero(t, affected)
	require.Zero(t, repo.disableCall)

	repo.disableErr = errors.New("db down")
	mock.ExpectBegin()
	mock.ExpectRollback()
	_, err = svc.BatchDisableUsers(context.Background(), []int64{5})
	require.Error(t, err)
	require.NoError(t, mock.ExpectationsWereMet())
}

type ctxRecordingAuthCacheInvalidator struct {
	authCacheInvalidatorStub
	ctxErrs []error
}

func (s *ctxRecordingAuthCacheInvalidator) InvalidateAuthCacheByUserID(ctx context.Context, userID int64) {
	s.ctxErrs = append(s.ctxErrs, ctx.Err())
	s.authCacheInvalidatorStub.InvalidateAuthCacheByUserID(ctx, userID)
}

func TestAdminService_BatchDisableUsers_InvalidatesCacheAfterRequestCanceled(t *testing.T) {
	client, mock := newBatchDisableLifecycleClient(t)
	mock.ExpectBegin()
	expectEmptyOwnedResourceDeprovision(mock)
	expectEmptyOwnedResourceDeprovision(mock)
	mock.ExpectCommit()
	repo := &emailBlacklistUserRepoStub{userRepoStub: &userRepoStub{}, disabled: []int64{4, 5}}
	invalidator := &ctxRecordingAuthCacheInvalidator{}
	svc := &adminServiceImpl{userRepo: repo, entClient: client, authCacheInvalidator: invalidator}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	repo.onDisable = func(opCtx context.Context) {
		tx := dbent.TxFromContext(opCtx)
		require.NotNil(t, tx)
		tx.OnCommit(func(next dbent.Committer) dbent.Committer {
			return dbent.CommitFunc(func(commitCtx context.Context, tx *dbent.Tx) error {
				if err := next.Commit(commitCtx, tx); err != nil {
					return err
				}
				cancel()
				return nil
			})
		})
	}
	affected, err := svc.BatchDisableUsers(ctx, []int64{4, 5})
	require.NoError(t, err)
	require.Equal(t, 2, affected)
	require.Equal(t, []int64{4, 5}, invalidator.userIDs)
	require.Equal(t, []error{nil, nil}, invalidator.ctxErrs)
	require.ErrorIs(t, ctx.Err(), context.Canceled)
	require.NoError(t, mock.ExpectationsWereMet())
}
