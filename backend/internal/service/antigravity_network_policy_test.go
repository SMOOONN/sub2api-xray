package service

import (
	"context"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strconv"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/require"
)

func antigravityPolicyTestServer(t *testing.T) (*httptest.Server, *atomic.Int64) {
	t.Helper()
	connections := &atomic.Int64{}
	server := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, `{"models":{"gemini-3.8-flash-high":{}}}`)
	}))
	server.Config.ConnState = func(_ net.Conn, state http.ConnState) {
		if state == http.StateNew {
			connections.Add(1)
		}
	}
	server.Start()
	t.Cleanup(server.Close)
	return server, connections
}

func antigravityPolicyTestProxy(t *testing.T, server *httptest.Server, protocol string) *Proxy {
	t.Helper()
	host, portText, err := net.SplitHostPort(server.Listener.Addr().String())
	require.NoError(t, err)
	port, err := strconv.Atoi(portText)
	require.NoError(t, err)
	return &Proxy{ID: 7, Kind: "standard", Protocol: protocol, Host: host, Port: port}
}

func TestAntigravityModelSyncEnforcesOwnershipAtSocket(t *testing.T) {
	for _, tc := range []struct {
		name     string
		owned    bool
		protocol string
	}{
		{name: "user direct", owned: true},
		{name: "user HTTP proxy", owned: true, protocol: "http"},
		{name: "user SOCKS proxy", owned: true, protocol: "socks5h"},
		{name: "system direct"},
		{name: "system HTTP proxy", protocol: "http"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server, connections := antigravityPolicyTestServer(t)
			baseURL := server.URL
			account := antigravityModelSyncAccount("Free")
			if tc.owned {
				ownerID := int64(42)
				account.OwnerUserID = &ownerID
			}
			if tc.protocol != "" {
				account.Proxy = antigravityPolicyTestProxy(t, server, tc.protocol)
				account.ProxyID = &account.Proxy.ID
				baseURL = "http://catalog.example"
			}
			t.Setenv(antigravityForwardBaseURLEnv, "")
			withAntigravityModelSyncEndpoints(t, baseURL, baseURL)
			models, err := antigravityModelSyncService().FetchUpstreamSupportedModels(context.Background(), account)
			if tc.owned {
				require.Error(t, err)
				require.Empty(t, models)
				require.Zero(t, connections.Load(), "a private endpoint must be rejected before connecting")
			} else {
				require.NoError(t, err)
				require.Equal(t, []string{"gemini-3.8-flash-high"}, models)
				require.Positive(t, connections.Load())
			}
		})
	}
}

type antigravityPolicyProxyRepository struct {
	ProxyRepository
	proxy *Proxy
}

func (r *antigravityPolicyProxyRepository) GetByID(context.Context, int64) (*Proxy, error) {
	return r.proxy, nil
}

func TestAntigravityTokenPrerequisitesEnforceOwnershipAtSocket(t *testing.T) {
	for _, operation := range []string{"refresh", "project"} {
		t.Run(operation, func(t *testing.T) {
			server, connections := antigravityPolicyTestServer(t)
			proxy := antigravityPolicyTestProxy(t, server, "http")
			ownerID := int64(42)
			account := antigravityModelSyncAccount("Free")
			account.OwnerUserID = &ownerID
			account.ProxyID = &proxy.ID
			account.Credentials["refresh_token"] = "synthetic-refresh-token"
			service := &AntigravityOAuthService{proxyRepo: &antigravityPolicyProxyRepository{proxy: proxy}}
			if operation == "refresh" {
				_, err := service.RefreshAccountToken(context.Background(), account)
				require.Error(t, err)
			} else {
				_, err := service.FillProjectID(context.Background(), account, "synthetic-access-token")
				require.Error(t, err)
			}
			require.Zero(t, connections.Load(), "refresh and project backfill must not bypass ownership policy")
		})
	}
}

func TestAntigravityPrivacyEnforcesOwnershipAtSocket(t *testing.T) {
	for _, tc := range []struct {
		name     string
		ownerID  int64
		protocol string
	}{
		{name: "user A HTTP proxy", ownerID: 42, protocol: "http"},
		{name: "user A SOCKS proxy", ownerID: 42, protocol: "socks5h"},
		{name: "user B HTTP proxy", ownerID: 43, protocol: "http"},
		{name: "system HTTP proxy", protocol: "http"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server, connections := antigravityPolicyTestServer(t)
			proxy := antigravityPolicyTestProxy(t, server, tc.protocol)
			account := antigravityModelSyncAccount("Free")
			if tc.ownerID != 0 {
				account.OwnerUserID = &tc.ownerID
			}
			account.ProxyID = &proxy.ID
			repo := &upstreamModelMetadataRepoStub{}
			service := &TokenRefreshService{
				accountRepo: repo,
				proxyRepo:   &antigravityPolicyProxyRepository{proxy: proxy},
			}
			service.ensureAntigravityPrivacy(context.Background(), account)
			require.Equal(t, account.ID, repo.accountID)
			require.Equal(t, AntigravityPrivacyFailed, repo.updates["privacy_mode"])
			require.Equal(t, AntigravityPrivacyFailed, account.Extra["privacy_mode"])
			if tc.ownerID != 0 {
				require.Zero(t, connections.Load(), "post-refresh privacy must reject a private proxy before connecting")
			} else {
				require.Positive(t, connections.Load(), "system-account proxy access must remain available")
			}
		})
	}
}

func TestAntigravityPolicyAllowsOnlyResolvedXrayListener(t *testing.T) {
	allowed, allowedConnections := antigravityPolicyTestServer(t)
	other, otherConnections := antigravityPolicyTestServer(t)
	ownerID := int64(42)
	account := &Account{OwnerUserID: &ownerID}
	resolvedProxy := &Proxy{Kind: "xray"}
	ctx := antigravityAccountRequestContext(context.Background(), account, resolvedProxy, allowed.URL)
	policy := HTTPUpstreamNetworkPolicyFromContext(ctx)
	require.True(t, policy.PublicOnly)
	require.Equal(t, []string{allowed.Listener.Addr().String()}, policy.AllowedDialAddresses)

	client, err := newAntigravityClientForContext(ctx, allowed.URL)
	require.NoError(t, err)
	models, _, err := client.FetchAvailableModelsFromURL(ctx, "http://catalog.example", "synthetic-token", "project", 4096)
	require.NoError(t, err)
	require.Contains(t, models.Models, "gemini-3.8-flash-high")
	require.Positive(t, allowedConnections.Load())

	client, err = newAntigravityClientForContext(ctx, other.URL)
	require.NoError(t, err)
	_, _, err = client.FetchAvailableModelsFromURL(ctx, "http://catalog.example", "synthetic-token", "project", 4096)
	require.Error(t, err)
	require.Zero(t, otherConnections.Load())
}
