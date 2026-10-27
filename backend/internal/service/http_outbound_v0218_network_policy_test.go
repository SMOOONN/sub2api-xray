package service

import (
	"context"
	"net/http"
	"testing"
	"time"
)

func TestGeminiNewOutboundRequestsApplyOwnershipNetworkPolicy(t *testing.T) {
	for _, tc := range []struct {
		name   string
		invoke func(*Account, HTTPUpstream) error
	}{
		{
			name: "explicit_cache",
			invoke: func(account *Account, upstream HTTPUpstream) error {
				svc := &GeminiCachedContentService{compat: &GeminiMessagesCompatService{httpUpstream: upstream}}
				_, err := svc.doUpstream(context.Background(), account, http.MethodPost, "https://cache.example/v1beta/cachedContents", []byte(`{}`), time.Second)
				return err
			},
		},
		{
			name: "models",
			invoke: func(account *Account, upstream HTTPUpstream) error {
				req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, "https://models.example/v1beta/models", nil)
				if err != nil {
					return err
				}
				svc := &GeminiMessagesCompatService{httpUpstream: upstream}
				_, err = svc.doGeminiModelsGET(req, account)
				return err
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			requireOwnershipNetworkPolicy(t, func() *Account {
				return &Account{ID: 118001, Platform: PlatformGemini, Type: AccountTypeAPIKey, Concurrency: 1, Credentials: map[string]any{"api_key": "test-key"}}
			}, tc.invoke)
		})
	}
}
