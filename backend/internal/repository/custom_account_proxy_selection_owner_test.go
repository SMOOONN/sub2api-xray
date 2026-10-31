package repository

import (
	"context"
	"regexp"
	"testing"

	"entgo.io/ent/dialect"
	entsql "entgo.io/ent/dialect/sql"
	"github.com/DATA-DOG/go-sqlmock"
	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/stretchr/testify/require"
)

func TestAutomaticProxySelectionRestrictsUserAccountsBeforeRanking(t *testing.T) {
	for _, mode := range []string{"random", "least_used"} {
		t.Run(mode, func(t *testing.T) {
			db, mock, err := sqlmock.New()
			require.NoError(t, err)
			t.Cleanup(func() { _ = db.Close() })
			client := dbent.NewClient(dbent.Driver(entsql.OpenDB(dialect.Postgres, db)))
			mock.ExpectBegin()
			tx, err := client.Tx(context.Background())
			require.NoError(t, err)
			ctx := dbent.NewTxContext(context.Background(), tx)
			if mode == "least_used" {
				mock.ExpectExec(`SELECT pg_advisory_xact_lock`).WillReturnResult(sqlmock.NewResult(0, 1))
			}
			mock.ExpectQuery(regexp.QuoteMeta("AND (p.owner_user_id = $1 OR p.is_public = TRUE)")).
				WithArgs(int64(41)).WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(7))
			ownerID := int64(41)
			selected, err := (&proxyRepository{}).SelectAccountProxy(ctx, mode, &ownerID)
			require.NoError(t, err)
			require.Equal(t, int64(7), selected)
			mock.ExpectRollback()
			require.NoError(t, tx.Rollback())
			require.NoError(t, mock.ExpectationsWereMet())
		})
	}
}
