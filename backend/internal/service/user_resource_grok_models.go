package service

import (
	"context"
	"errors"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
)

// ownedModelSyncAccount checks ownership before repository access, then checks
// the loaded account again so a stale ownership lookup cannot authorize it.
func (s *UserResourceService) ownedModelSyncAccount(ctx context.Context, ownerID, accountID int64) (*Account, error) {
	if err := s.ensureOwned(ctx, "accounts", ownerID, accountID); err != nil {
		return nil, err
	}
	if s.accountTestService == nil || s.accountTestService.accountRepo == nil {
		return nil, infraerrors.ServiceUnavailable("USER_MODEL_SYNC_UNAVAILABLE", "upstream model sync is unavailable")
	}
	account, err := s.accountTestService.accountRepo.GetByID(ctx, accountID)
	if err != nil {
		if errors.Is(err, ErrAccountNotFound) {
			return nil, ErrUserResourceNotFound
		}
		return nil, err
	}
	if account == nil || account.OwnerUserID == nil || *account.OwnerUserID != ownerID {
		return nil, ErrUserResourceNotFound
	}
	return account, nil
}

// GetAccountGrokModelDiagnostics reads the owner's saved catalog. The shared
// diagnostic evaluator performs no upstream request and does not mutate data.
func (s *UserResourceService) GetAccountGrokModelDiagnostics(ctx context.Context, ownerID, accountID int64) (*GrokModelDiagnostics, error) {
	account, err := s.ownedModelSyncAccount(ctx, ownerID, accountID)
	if err != nil {
		return nil, err
	}
	return s.accountTestService.GetGrokModelDiagnostics(ctx, account), nil
}

func (s *UserResourceService) SyncAccountUpstreamModels(ctx context.Context, ownerID, accountID int64) (*UpstreamModelCatalog, error) {
	account, err := s.ownedModelSyncAccount(ctx, ownerID, accountID)
	if err != nil {
		return nil, err
	}
	if !account.IsGrok() {
		models, err := s.accountTestService.FetchUpstreamSupportedModels(ctx, account)
		return &UpstreamModelCatalog{Models: models}, err
	}
	// Grok uses the same observation/persistence rules as admin and background
	// sync. Keep discovery_source so configured fallbacks cannot replace a list.
	return s.accountTestService.SyncUpstreamModelCatalog(ctx, account)
}

// Forms and imports do not own model observations. Build these expressions at
// the SQL write boundary so a sync committed while a form was open is retained
// from the row being updated, rather than from an earlier application read.
func writableUserAccountExtraSQL(value string) string {
	return "(" + value + " - 'grok_observed_models' - 'upstream_model_metadata')"
}

func preserveUserAccountModelExtraSQL(value string) string {
	return writableUserAccountExtraSQL(value) + `
 || CASE WHEN extra ? 'grok_observed_models'
    THEN jsonb_build_object('grok_observed_models', extra -> 'grok_observed_models') ELSE '{}'::jsonb END
 || CASE WHEN extra ? 'upstream_model_metadata'
    THEN jsonb_build_object('upstream_model_metadata', extra -> 'upstream_model_metadata') ELSE '{}'::jsonb END`
}
