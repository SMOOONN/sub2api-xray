package service

import (
	"context"
	"errors"
	"fmt"

	dbent "github.com/Wei-Shaw/sub2api/ent"
)

// batchDisableUsersAndOwnedResources keeps batch suspension on the same resource
// lifecycle as single-user suspension, including subscriptions assigned to others.
func (s *adminServiceImpl) batchDisableUsersAndOwnedResources(ctx context.Context, repo UserEmailBlacklistRepository, userIDs []int64) ([]int64, error) {
	if s.entClient == nil {
		return nil, errors.New("user resource lifecycle database is unavailable")
	}
	tx, err := s.entClient.Tx(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()
	opCtx := dbent.NewTxContext(ctx, tx)
	disabled, err := repo.BatchDisableUsers(opCtx, userIDs)
	if err != nil {
		return nil, err
	}
	resources := make([]*userOwnedResourceDeprovision, 0, len(disabled))
	for _, userID := range disabled {
		result, err := s.disableUserOwnedResources(opCtx, tx.Client(), userID)
		if err != nil {
			return nil, fmt.Errorf("disable user-owned resources for user %d: %w", userID, err)
		}
		resources = append(resources, result)
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	// A disconnected administrator must not leave committed resources in caches.
	finalizeCtx := context.WithoutCancel(ctx)
	for i, userID := range disabled {
		s.finalizeUserOwnedResourceDeprovision(finalizeCtx, userID, resources[i])
	}
	return disabled, nil
}
