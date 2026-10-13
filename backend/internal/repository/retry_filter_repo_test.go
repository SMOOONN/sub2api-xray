package repository

import (
	"context"
	"errors"
	"testing"

	"entgo.io/ent/dialect"
	entsql "entgo.io/ent/dialect/sql"
	"github.com/DATA-DOG/go-sqlmock"
	"github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/internal/model"
	"github.com/stretchr/testify/require"
)

func TestRetryFilterReorderInvalidMembershipIsValidationError(t *testing.T) {
	for _, ids := range [][]int64{{11, 999999}, {11}} {
		db, mock, err := sqlmock.New()
		require.NoError(t, err)
		client := ent.NewClient(ent.Driver(entsql.OpenDB(dialect.Postgres, db)))
		mock.ExpectQuery(`SELECT .* FROM "retry_filter_rules"`).WithArgs(int64(1)).
			WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(int64(11)).AddRow(int64(22)))
		repo := NewRetryFilterRepository(client)
		rows, err := repo.ReorderRules(context.Background(), 1, ids)
		var validation *model.ValidationError
		require.ErrorAs(t, err, &validation)
		require.Equal(t, "rule_ids", validation.Field)
		require.Nil(t, rows)
		require.NoError(t, mock.ExpectationsWereMet(), "invalid orders must fail before opening a write transaction")
		_ = client.Close()
	}
}

func TestRetryFilterReorderDatabaseFailureRetainsServerError(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	client := ent.NewClient(ent.Driver(entsql.OpenDB(dialect.Postgres, db)))
	defer func() { _ = client.Close() }()
	dbErr := errors.New("database unavailable")
	mock.ExpectQuery(`SELECT .* FROM "retry_filter_rules"`).WithArgs(int64(1)).WillReturnError(dbErr)
	_, err = NewRetryFilterRepository(client).ReorderRules(context.Background(), 1, []int64{11})
	require.ErrorIs(t, err, dbErr)
	var validation *model.ValidationError
	require.False(t, errors.As(err, &validation))
	require.NoError(t, mock.ExpectationsWereMet())
}
