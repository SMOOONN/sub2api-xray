package service

import (
	"context"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/pkg/typesafe"
	"github.com/stretchr/testify/require"
)

func TestUserResourceNewPlatformAccountAdmission(t *testing.T) {
	svc := NewUserResourceService(nil, nil, nil, nil)
	for _, platform := range []string{PlatformTypeSafe, PlatformCommandCode, PlatformCline} {
		t.Run(platform, func(t *testing.T) {
			payload := map[string]any{
				"name": "new platform", "platform": platform, "type": AccountTypeAPIKey,
				"credentials": map[string]any{"api_key": "test-key"},
				"status":      StatusActive, "concurrency": 1, "priority": 1, "rate_multiplier": 1.0,
			}
			require.NoError(t, svc.normalizeAndValidateAccountPayload(context.Background(), 41, nil, payload))
			for _, accountType := range []string{AccountTypeOAuth, AccountTypeUpstream, AccountTypeSetupToken, AccountTypeBedrock, AccountTypeServiceAccount} {
				require.Error(t, validateUserAccountPlatformType(platform, accountType), accountType)
			}
			_, err := svc.GetGroupModelsListCandidates(context.Background(), 41, 0, platform)
			require.NoError(t, err)
		})
	}
	for _, platform := range []string{PlatformComposite, "unknown"} {
		_, err := svc.GetGroupModelsListCandidates(context.Background(), 41, 0, platform)
		require.Error(t, err, "private groups still require a concrete registered platform")
	}
}

func TestUserResourceTypeSafeTestModelsStayNative(t *testing.T) {
	models := availableUserAccountTestModels(&Account{
		Platform: PlatformTypeSafe, Type: AccountTypeAPIKey,
		Credentials: map[string]any{"model_mapping": map[string]any{"claude-sonnet-4-5": "not-a-systemone-model"}},
	})
	require.Equal(t, []UserAccountTestModel{{ID: typesafe.JevLatestModel, Type: "model", DisplayName: typesafe.JevLatestModel}}, models)
}

func TestUserResourceModelRoutedProvidersValidateProtocolRules(t *testing.T) {
	for _, platform := range []string{PlatformOpenCodeGo, PlatformCommandCode} {
		err := normalizeAndValidateUserProviderAccountCredentials(platform, map[string]any{"protocol_rules": "invalid"})
		require.Error(t, err, platform)
	}
	// Unused historical data must not become a validation failure during an
	// unrelated edit to a provider whose routing never consumes these rules.
	require.NoError(t, normalizeAndValidateUserProviderAccountCredentials(PlatformKimi, map[string]any{"protocol_rules": "unused-legacy-value"}))
}

func TestUserResourceNewPlatformModelPreviewKeepsOwnerPolicy(t *testing.T) {
	for _, platform := range []string{PlatformCommandCode, PlatformCline, PlatformKimi, PlatformMiniMax} {
		t.Run(platform, func(t *testing.T) {
			upstream := &userModelSyncHTTPUpstream{responseBody: `{"data":[{"id":"supported-model"}]}`}
			testService := &AccountTestService{httpUpstream: upstream, cfg: upstreamModelSyncTestConfig()}
			svc := NewUserResourceService(nil, nil, nil, nil)
			svc.SetAccountMaintenanceServices(testService, nil)
			protocol := APIProtocolAdaptive
			if platform == PlatformKimi || platform == PlatformMiniMax {
				protocol = APIProtocolResponses
			}
			models, err := svc.SyncAccountUpstreamModelsPreview(context.Background(), 41, UserUpstreamModelsPreviewInput{
				Platform: platform, Type: AccountTypeAPIKey, APIKey: "test-key", APIProtocol: protocol,
			})
			require.NoError(t, err)
			require.Equal(t, []string{"supported-model"}, models)
			require.Equal(t, 1, upstream.calls)
			require.True(t, upstream.publicOnly)
		})
	}

	upstream := &userModelSyncHTTPUpstream{responseBody: `{"data":[{"id":"must-not-be-fetched"}]}`}
	svc := NewUserResourceService(nil, nil, nil, nil)
	svc.SetAccountMaintenanceServices(&AccountTestService{httpUpstream: upstream, cfg: upstreamModelSyncTestConfig()}, nil)
	_, err := svc.SyncAccountUpstreamModelsPreview(context.Background(), 41, UserUpstreamModelsPreviewInput{
		Platform: PlatformTypeSafe, Type: AccountTypeAPIKey, APIKey: "test-key",
	})
	require.Error(t, err)
	require.Zero(t, upstream.calls, "TypeSafe has no generic model catalog")
}
