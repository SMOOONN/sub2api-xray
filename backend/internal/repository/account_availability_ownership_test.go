package repository

import (
	"context"
	"testing"

	"entgo.io/ent/dialect"
	entsql "entgo.io/ent/dialect/sql"
	"github.com/DATA-DOG/go-sqlmock"
	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestSystemModelAvailabilityRequiresSystemOwner(t *testing.T) {
	for _, scope := range []struct {
		name           string
		includeGrouped bool
	}{{"ungrouped", false}, {"simple", true}} {
		t.Run(scope.name, func(t *testing.T) {
			db, mock, err := sqlmock.New()
			require.NoError(t, err)
			client := dbent.NewClient(dbent.Driver(entsql.OpenDB(dialect.Postgres, db)))
			t.Cleanup(func() { _ = client.Close() })
			repo := newAccountRepositoryWithSQL(client, db, nil)
			mock.ExpectQuery(`WHERE .*"owner_user_id" IS NULL`).WillReturnRows(sqlmock.NewRows([]string{"id"}))
			_, err = repo.ListModelAvailabilityCandidates(context.Background(), nil, []string{service.PlatformOpenAI}, scope.includeGrouped)
			require.NoError(t, err)
			require.NoError(t, mock.ExpectationsWereMet())
		})
	}
}
