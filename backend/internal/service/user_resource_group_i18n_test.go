package service

import (
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestPrivateGroupI18nUsesSharedValidation(t *testing.T) {
	for _, tc := range []struct {
		name    string
		value   any
		want    GroupI18n
		invalid bool
	}{
		{name: "canonicalize", value: map[string]any{"EN": map[string]any{"name": " Group ", "description": " Text "}}, want: GroupI18n{"en": {Name: "Group", Description: "Text"}}},
		{name: "clear", value: map[string]any{}, want: GroupI18n{}},
		{name: "blank_translation", value: map[string]any{"en": map[string]any{"name": " "}}, want: GroupI18n{}},
		{name: "invalid_locale", value: map[string]any{"en_uk": map[string]any{"name": "Name"}}, invalid: true},
		{name: "duplicate_locale", value: map[string]any{"en": map[string]any{}, "EN": map[string]any{}}, invalid: true},
		{name: "oversized_name", value: map[string]any{"en": map[string]any{"name": strings.Repeat("x", 101)}}, invalid: true},
		{name: "wrong_shape", value: []any{}, invalid: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			payload := map[string]any{"i18n": tc.value}
			err := normalizeUserResourceGroupI18n(payload)
			if tc.invalid {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			require.Equal(t, tc.want, payload["i18n"])
		})
	}
	for _, payload := range []map[string]any{{}, {"i18n": nil}} {
		require.NoError(t, normalizeUserResourceGroupI18n(payload))
		require.NotContains(t, payload, "i18n", "omission and null must not clear stored translations")
	}
}

func TestPrivateGroupImageDefaultsOnlyApplyOnCreate(t *testing.T) {
	for _, platform := range []string{PlatformAnthropic, PlatformGemini, PlatformAntigravity, PlatformGrok, PlatformOpenAI} {
		for _, groupID := range []int64{0, 99} {
			payload := map[string]any{
				"name": "Group", "platform": platform, "status": StatusActive,
				"subscription_type": SubscriptionTypeStandard, "rate_multiplier": 1.0,
				"image_rate_multiplier": 1.0, "video_rate_multiplier": 1.0,
				"batch_image_discount_multiplier": 0.5, "batch_image_hold_multiplier": 0.6,
				"default_validity_days": 30, "rpm_limit": 0, "peak_rate_multiplier": 1.0,
				"allow_image_generation": false,
			}
			svc := &UserResourceService{}
			require.NoError(t, svc.normalizeAndValidateGroupPayload(context.Background(), 41, groupID, nil, payload))
			want := groupID == 0 && (platform == PlatformGemini || platform == PlatformAntigravity || platform == PlatformGrok)
			require.Equal(t, want, payload["allow_image_generation"], "platform=%s groupID=%d", platform, groupID)
		}
	}
}
