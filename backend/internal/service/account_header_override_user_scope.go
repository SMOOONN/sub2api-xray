package service

import "context"

// A user-owned upstream must not recover identities hidden by user resource
// diagnostics. Its owner can report their own requests, but other subscribers'
// variable headers are omitted, including any same-named client-supplied value.
func (a *Account) canReportHeaderOverrideIdentity(ctx context.Context) bool {
	if a == nil {
		return false
	}
	if a.OwnerUserID == nil {
		return true
	}
	scope := headerOverrideScopeFromContext(ctx)
	return scope != nil && scope.userID > 0 && scope.userID == *a.OwnerUserID
}
