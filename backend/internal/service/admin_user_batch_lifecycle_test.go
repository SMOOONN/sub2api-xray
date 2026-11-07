//go:build unit

package service

import (
	"context"
	"errors"
	"testing"

	"entgo.io/ent/dialect"
	entsql "entgo.io/ent/dialect/sql"
	"github.com/DATA-DOG/go-sqlmock"
	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/stretchr/testify/require"
)

func newBatchDisableLifecycleClient(t *testing.T) (*dbent.Client, sqlmock.Sqlmock) {
	t.Helper()
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	client := dbent.NewClient(dbent.Driver(entsql.OpenDB(dialect.Postgres, db)))
	t.Cleanup(func() { _ = client.Close() })
	return client, mock
}

func TestBatchDisableUsersRequiresLifecycleDatabase(t *testing.T) {
	repo := &emailBlacklistUserRepoStub{userRepoStub: &userRepoStub{}, disabled: []int64{7}}
	svc := &adminServiceImpl{userRepo: repo}
	affected, err := svc.BatchDisableUsers(context.Background(), []int64{7})
	require.ErrorContains(t, err, "user resource lifecycle database is unavailable")
	require.Zero(t, affected)
	require.Zero(t, repo.disableCall)
}

func TestBatchDisableUsersRollsBackResourceFailure(t *testing.T) {
	injected := errors.New("resource query failed")
	client, mock := newFailingDeprovisionClient(t, injected)
	repo := &emailBlacklistUserRepoStub{userRepoStub: &userRepoStub{}, disabled: []int64{7}}
	repo.onDisable = func(ctx context.Context) { require.NotNil(t, dbent.TxFromContext(ctx)) }
	invalidator := &authCacheInvalidatorStub{}
	svc := &adminServiceImpl{userRepo: repo, entClient: client, authCacheInvalidator: invalidator}
	affected, err := svc.BatchDisableUsers(context.Background(), []int64{7})
	require.ErrorIs(t, err, injected)
	require.Zero(t, affected)
	require.Empty(t, invalidator.userIDs)
	require.Empty(t, invalidator.groupIDs)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestBatchDisableUsersDoesNotFinalizeFailedCommit(t *testing.T) {
	client, mock := newBatchDisableLifecycleClient(t)
	mock.ExpectBegin()
	expectEmptyOwnedResourceDeprovision(mock)
	injected := errors.New("commit failed")
	mock.ExpectCommit().WillReturnError(injected)
	repo := &emailBlacklistUserRepoStub{userRepoStub: &userRepoStub{}, disabled: []int64{7}}
	invalidator := &authCacheInvalidatorStub{}
	svc := &adminServiceImpl{userRepo: repo, entClient: client, authCacheInvalidator: invalidator}
	affected, err := svc.BatchDisableUsers(context.Background(), []int64{7})
	require.ErrorIs(t, err, injected)
	require.Zero(t, affected)
	require.Empty(t, invalidator.userIDs)
	require.Empty(t, invalidator.groupIDs)
	require.NoError(t, mock.ExpectationsWereMet())
}
