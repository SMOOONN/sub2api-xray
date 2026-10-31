package service

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/Wei-Shaw/sub2api/internal/config"
)

func TestUserGrokDiagnosticsEnforcesOwnershipBeforeReadingCatalog(t *testing.T) {
	for _, tc := range []struct {
		name        string
		owned       bool
		storedOwner int64
		wantCalls   int
	}{
		{"foreign account", false, 42, 0},
		{"ownership changed", true, 42, 1},
		{"system account", true, 0, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			db, mock, err := sqlmock.New()
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = db.Close() }()
			account := &Account{ID: 91, Platform: PlatformGrok, OwnerUserID: &tc.storedOwner}
			if tc.storedOwner == 0 {
				account.OwnerUserID = nil
			}
			repo := &userModelSyncAccountRepo{account: account}
			svc := NewUserResourceService(db, nil, nil, nil)
			svc.SetAccountMaintenanceServices(&AccountTestService{accountRepo: repo}, nil)
			mock.ExpectQuery(`SELECT EXISTS \(SELECT 1 FROM accounts WHERE id = \$1 AND owner_user_id = \$2 AND deleted_at IS NULL\)`).
				WithArgs(int64(91), int64(41)).WillReturnRows(sqlmock.NewRows([]string{"exists"}).AddRow(tc.owned))
			_, err = svc.GetAccountGrokModelDiagnostics(context.Background(), 41, 91)
			if !errors.Is(err, ErrUserResourceNotFound) {
				t.Fatalf("expected ownership rejection, got %v", err)
			}
			if repo.calls != tc.wantCalls {
				t.Fatalf("repository calls=%d, want %d", repo.calls, tc.wantCalls)
			}
			if err := mock.ExpectationsWereMet(); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestUserGrokDiagnosticsIsReadOnlyAndOmitsCredentials(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()
	ownerID := int64(41)
	account := &Account{ID: 91, OwnerUserID: &ownerID, Platform: PlatformGrok, Type: AccountTypeAPIKey,
		Credentials: map[string]any{"api_key": "fixture-secret", "base_url": "https://user:fixture-password@relay.example.com/v1?token=fixture-token"}}
	repo := &userModelSyncAccountRepo{account: account}
	upstream := &userModelSyncHTTPUpstream{}
	svc := NewUserResourceService(db, nil, nil, nil)
	svc.SetAccountMaintenanceServices(&AccountTestService{accountRepo: repo, httpUpstream: upstream,
		cfg: &config.Config{Security: config.SecurityConfig{URLAllowlist: config.URLAllowlistConfig{Enabled: false}}}}, nil)
	mock.ExpectQuery(`SELECT EXISTS \(SELECT 1 FROM accounts WHERE id = \$1 AND owner_user_id = \$2 AND deleted_at IS NULL\)`).
		WithArgs(int64(91), ownerID).WillReturnRows(sqlmock.NewRows([]string{"exists"}).AddRow(true))
	diag, err := svc.GetAccountGrokModelDiagnostics(context.Background(), ownerID, 91)
	if err != nil {
		t.Fatal(err)
	}
	if diag == nil || diag.Endpoint != "https://relay.example.com/v1/models" {
		t.Fatalf("unexpected diagnostics: %#v", diag)
	}
	raw, err := json.Marshal(diag)
	if err != nil {
		t.Fatal(err)
	}
	for _, secret := range []string{"fixture-secret", "fixture-password", "fixture-token", "user:"} {
		if strings.Contains(string(raw), secret) {
			t.Fatal("diagnostics exposed a credential")
		}
	}
	if upstream.calls != 0 || repo.calls != 1 {
		t.Fatalf("unexpected I/O: upstream=%d repository=%d", upstream.calls, repo.calls)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}
