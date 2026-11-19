package service

import "testing"

// A deployed Xray v28 group snapshot predates force_openai_ultrafast. Reusing
// one after the upgrade would silently clear an administrator's new policy.
func TestAPIKeyServiceRejectsXrayV28SnapshotBeforeUltrafast(t *testing.T) {
	svc := &APIKeyService{}
	entry := &APIKeyAuthCacheEntry{Snapshot: &APIKeyAuthSnapshot{
		Version: 28,
		Group:   &APIKeyAuthGroupSnapshot{ID: 17},
	}}
	key, used, err := svc.applyAuthCacheEntry("cached", entry)
	if err != nil || used || key != nil {
		t.Fatalf("expected the v28 cache entry to miss, got used=%t err=%v", used, err)
	}
}
