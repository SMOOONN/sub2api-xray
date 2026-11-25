//go:build unit

package service

import (
	"context"
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestHeaderOverrideUserOwnedIdentityPrivacy(t *testing.T) {
	ownerID := int64(7)
	for _, tc := range []struct {
		name    string
		owner   *int64
		userID  int64
		allowed bool
	}{
		{name: "system account", userID: 8, allowed: true},
		{name: "owner request", owner: &ownerID, userID: 7, allowed: true},
		{name: "other subscriber", owner: &ownerID, userID: 8},
		{name: "background request", owner: &ownerID},
	} {
		t.Run(tc.name, func(t *testing.T) {
			account := headerOverrideVarsAccount(PlatformOpenAI, AccountTypeAPIKey, map[string]any{
				"x-identity": "u={{user_id}};k={{api_key_id}}",
				"x-static":   "fixed",
			})
			account.OwnerUserID = tc.owner
			ctx := WithHeaderOverrideRequestScope(context.Background(), tc.userID, 31, nil)
			headers := http.Header{"X-Identity": {"spoofed"}, "x-IDENTITY": {"second-spoof"}}
			account.ApplyHeaderOverrides(ctx, headers)
			require.Equal(t, []string{"fixed"}, headerValuesFold(headers, "x-static"))
			if tc.allowed {
				require.Len(t, headerValuesFold(headers, "x-identity"), 1)
				require.Contains(t, headerValuesFold(headers, "x-identity")[0], "k=31")
				require.NotEmpty(t, headerOverrideReportedHeaders(ctx, account.ID))
			} else {
				require.Empty(t, headerValuesFold(headers, "x-identity"))
				require.Nil(t, headerOverrideReportedHeaders(ctx, account.ID))
				require.NotContains(t, account.headerOverrideReportedIdentity(headers), "31")
			}
		})
	}
}
