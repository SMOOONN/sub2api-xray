package service

import (
	"context"

	"github.com/Wei-Shaw/sub2api/internal/pkg/antigravity"
	"github.com/Wei-Shaw/sub2api/internal/util/urlvalidator"
)

// Keep ownership policy in the service layer. The resolved proxy and URL must
// come from the same lookup so only that Xray runtime listener is exempted.
func antigravityAccountRequestContext(ctx context.Context, account *Account, resolvedProxy *Proxy, proxyURL string) context.Context {
	if account == nil {
		return ctx
	}
	policyAccount := &Account{OwnerUserID: account.OwnerUserID, Proxy: resolvedProxy}
	return WithHTTPUpstreamNetworkPolicy(ctx, HTTPUpstreamNetworkPolicyForAccount(policyAccount, proxyURL))
}

func newAntigravityClientForContext(ctx context.Context, proxyURL string) (*antigravity.Client, error) {
	policy := HTTPUpstreamNetworkPolicyFromContext(ctx)
	if !policy.PublicOnly {
		return antigravity.NewClient(proxyURL)
	}
	dialer := urlvalidator.NewPublicOnlyDialer(policy.AllowedDialAddresses)
	return antigravity.NewClientWithDialer(proxyURL, dialer.DialContext)
}
