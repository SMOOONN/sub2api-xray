package service

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestNewPlatformOutboundRequestsApplyOwnershipNetworkPolicy(t *testing.T) {
	gin.SetMode(gin.TestMode)
	tests := []struct {
		name     string
		platform string
		invoke   func(*Account, HTTPUpstream) error
	}{
		{
			name: "typesafe_connection_test", platform: PlatformTypeSafe,
			invoke: func(account *Account, upstream HTTPUpstream) error {
				svc := &AccountTestService{cfg: &config.Config{}, httpUpstream: upstream}
				return svc.testTypeSafeAccountConnection(newOutboundPolicyGinContext("/api/v1/my/accounts/1/test", nil), account, "connection test")
			},
		},
		{
			name: "typesafe_gateway", platform: PlatformTypeSafe,
			invoke: func(account *Account, upstream HTTPUpstream) error {
				svc := &GatewayService{cfg: &config.Config{}, httpUpstream: upstream}
				body := []byte(`{"model":"jev-latest","state":"test","questions":{"test":{"type":"noul"}}}`)
				_, err := svc.ForwardSystemOne(context.Background(), newOutboundPolicyGinContext("/v1/systemone", body), account, body)
				return err
			},
		},
		{
			name: "cline_account_api", platform: PlatformCline,
			invoke: func(account *Account, upstream HTTPUpstream) error {
				client := newClineAccountClient(&config.Config{}, upstream, account, "", "CN_BALANCE")
				_, err := client.get(context.Background(), "/api/v1/users/me")
				return err
			},
		},
		{
			name: "commandcode_usage_api", platform: PlatformCommandCode,
			invoke: func(account *Account, upstream HTTPUpstream) error {
				client := &commandCodeUsageClient{cfg: &config.Config{}, httpUpstream: upstream, account: account, apiKey: "test-key"}
				var response map[string]any
				return client.get(context.Background(), "/alpha/whoami", nil, &response)
			},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			requireOwnershipNetworkPolicy(t, func() *Account {
				return &Account{
					ID: 110001, Platform: tc.platform, Type: AccountTypeAPIKey, Concurrency: 1,
					Credentials: map[string]any{"api_key": "test-key"},
				}
			}, tc.invoke)
		})
	}
}

type modelCatalogOwnershipRecorder struct {
	HTTPUpstream
	requests chan *http.Request
}

func (r *modelCatalogOwnershipRecorder) Do(req *http.Request, _ string, _ int64, _ int) (*http.Response, error) {
	r.requests <- req
	return nil, context.Canceled
}

func TestModelProtocolCatalogRefreshPreservesOwnershipNetworkPolicy(t *testing.T) {
	for index, tc := range []struct {
		name       string
		userOwned  bool
		localProxy bool
	}{
		{name: "user", userOwned: true},
		{name: "system"},
		{name: "user_local_standard_proxy", userOwned: true, localProxy: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			account := &Account{
				ID: int64(110101 + index), Platform: PlatformCommandCode, Type: AccountTypeAPIKey, Concurrency: 1,
				Credentials: map[string]any{"api_key": "test-key", "base_url": "https://ownership-catalog.example/v1"},
			}
			if tc.userOwned {
				ownerID := int64(41)
				account.OwnerUserID = &ownerID
			}
			if tc.localProxy {
				proxyID := int64(51)
				account.ProxyID = &proxyID
				account.Proxy = &Proxy{ID: proxyID, Kind: "standard", Protocol: "socks5", Host: "127.0.0.1", Port: 12451}
			}
			catalogURL := buildOpenAIModelsURL(account.GetCNProtocolBaseURL(APIProtocolChatCompletions))
			catalogKey := modelProtocolCatalogKey(account, catalogURL)
			t.Cleanup(func() {
				upstreamModelProtocols.mu.Lock()
				defer upstreamModelProtocols.mu.Unlock()
				delete(upstreamModelProtocols.entries, catalogKey)
			})
			upstream := &modelCatalogOwnershipRecorder{requests: make(chan *http.Request, 1)}
			svc := &OpenAIGatewayService{cfg: &config.Config{}, httpUpstream: upstream}
			require.Empty(t, svc.modelCatalogProtocols(context.Background(), account, "catalog-policy-model"))
			select {
			case request := <-upstream.requests:
				policy := HTTPUpstreamNetworkPolicyFromContext(request.Context())
				require.Equal(t, tc.userOwned, policy.PublicOnly)
				require.Empty(t, policy.AllowedDialAddresses, "a standard loopback proxy must not get the Xray runtime exception")
			case <-time.After(3 * time.Second):
				t.Fatal("model catalog refresh did not reach the outbound client")
			}
		})
	}
}
