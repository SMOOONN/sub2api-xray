package service

import (
	"context"
	"strings"

	dbent "github.com/Wei-Shaw/sub2api/ent"
	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
)

// RetryFilterBindingMutation describes one complete binding write. Set and
// clear operations are committed together so a failed account or group cannot
// leave a partially applied preset selection behind.
type RetryFilterBindingMutation struct {
	PresetID        *int64
	AccountIDs      []int64
	ClearAccountIDs []int64
	GroupIDs        []int64
	ClearGroupIDs   []int64
	DefaultPresetID *int64
}

// RetryFilterBindingAtomicUpdater is intentionally a narrow interface used by
// the admin handler. It keeps the large AdminService test contract unchanged.
type RetryFilterBindingAtomicUpdater interface {
	UpdateRetryFilterBindingsAtomically(context.Context, RetryFilterBindingMutation) error
}

// RetryFilterBindingSnapshotSynchronizer publishes committed account bindings
// through the repository's existing scheduler snapshot path.
type RetryFilterBindingSnapshotSynchronizer interface {
	SyncRetryFilterBindingAccountSnapshots(context.Context, []int64)
}

func (s *adminServiceImpl) UpdateRetryFilterBindingsAtomically(ctx context.Context, mutation RetryFilterBindingMutation) error {
	if s == nil || s.entClient == nil || s.accountRepo == nil || s.groupRepo == nil {
		return infraerrors.ServiceUnavailable("RETRY_FILTER_BINDING_UNAVAILABLE", "atomic retry-filter binding is unavailable")
	}

	rawAccountIDs := append(append([]int64{}, mutation.AccountIDs...), mutation.ClearAccountIDs...)
	rawGroupIDs := append(append([]int64{}, mutation.GroupIDs...), mutation.ClearGroupIDs...)
	if err := validateRetryFilterIDs(rawAccountIDs, "account"); err != nil {
		return err
	}
	if err := validateRetryFilterIDs(rawGroupIDs, "group"); err != nil {
		return err
	}
	accountIDs := uniquePositiveIDs(rawAccountIDs)
	groupIDs := uniquePositiveIDs(rawGroupIDs)
	var snapshotSyncer RetryFilterBindingSnapshotSynchronizer
	if len(accountIDs) > 0 {
		var ok bool
		snapshotSyncer, ok = s.accountRepo.(RetryFilterBindingSnapshotSynchronizer)
		if !ok {
			return infraerrors.ServiceUnavailable("RETRY_FILTER_BINDING_UNAVAILABLE", "retry-filter account snapshot synchronization is unavailable")
		}
	}

	accounts, err := s.accountRepo.GetByIDs(ctx, accountIDs)
	if err != nil {
		return err
	}
	if len(accounts) != len(accountIDs) {
		return infraerrors.NotFound("ACCOUNT_NOT_FOUND", "one or more accounts do not exist")
	}
	accountByID := make(map[int64]*Account, len(accounts))
	for _, account := range accounts {
		accountByID[account.ID] = account
	}

	groups := make(map[int64]*Group, len(groupIDs))
	for _, id := range groupIDs {
		group, getErr := s.groupRepo.GetByIDLite(ctx, id)
		if getErr != nil {
			return getErr
		}
		if group == nil {
			return infraerrors.NotFound("GROUP_NOT_FOUND", "one or more groups do not exist")
		}
		groups[id] = group
	}

	tx, err := s.entClient.Tx(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	txCtx := dbent.NewTxContext(ctx, tx)

	clearAccounts := make(map[int64]struct{}, len(mutation.ClearAccountIDs))
	for _, id := range mutation.ClearAccountIDs {
		clearAccounts[id] = struct{}{}
	}
	for _, id := range accountIDs {
		account := accountByID[id]
		credentials := cloneJSONMap(account.Credentials)
		if _, clear := clearAccounts[id]; clear {
			delete(credentials, "retry_filter_preset_id")
		} else if mutation.PresetID != nil {
			credentials["retry_filter_preset_id"] = *mutation.PresetID
		} else {
			delete(credentials, "retry_filter_preset_id")
		}
		account.Credentials = credentials
		if err := s.accountRepo.Update(txCtx, account); err != nil {
			return err
		}
	}

	clearGroups := make(map[int64]struct{}, len(mutation.ClearGroupIDs))
	for _, id := range mutation.ClearGroupIDs {
		clearGroups[id] = struct{}{}
	}
	for _, id := range groupIDs {
		group := groups[id]
		if _, clear := clearGroups[id]; clear || mutation.PresetID == nil {
			group.RetryFilterConfig.PresetID = nil
		} else {
			presetID := *mutation.PresetID
			group.RetryFilterConfig.PresetID = &presetID
		}
		if err := s.groupRepo.Update(txCtx, group); err != nil {
			return err
		}
	}

	if mutation.DefaultPresetID != nil {
		var presetID *int64
		if *mutation.DefaultPresetID > 0 {
			id := *mutation.DefaultPresetID
			presetID = &id
		}
		if s.settingService == nil {
			return infraerrors.ServiceUnavailable("RETRY_FILTER_SETTING_UNAVAILABLE", "retry-filter settings are unavailable")
		}
		if err := s.settingService.SetRetryFilterDefaultPresetIDInTx(txCtx, presetID); err != nil {
			return err
		}
	}

	if err := tx.Commit(); err != nil {
		return err
	}

	if snapshotSyncer != nil {
		snapshotSyncer.SyncRetryFilterBindingAccountSnapshots(ctx, accountIDs)
	}
	if s.authCacheInvalidator != nil {
		for _, id := range groupIDs {
			s.authCacheInvalidator.InvalidateAuthCacheByGroupID(ctx, id)
		}
	}
	if mutation.DefaultPresetID != nil && s.settingService != nil {
		s.settingService.ReloadRetryFilterRuntime(ctx)
	}
	return nil
}

func uniquePositiveIDs(ids []int64) []int64 {
	out := make([]int64, 0, len(ids))
	seen := make(map[int64]struct{}, len(ids))
	for _, id := range ids {
		if id <= 0 {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		out = append(out, id)
	}
	return out
}

func validateRetryFilterIDs(ids []int64, kind string) error {
	for _, id := range ids {
		if id <= 0 {
			return infraerrors.BadRequest("INVALID_RETRY_FILTER_"+strings.ToUpper(kind)+"_IDS", kind+" IDs must be positive")
		}
	}
	return nil
}

func cloneJSONMap(input map[string]any) map[string]any {
	if input == nil {
		return make(map[string]any)
	}
	out := make(map[string]any, len(input)+1)
	for key, value := range input {
		out[key] = value
	}
	return out
}
