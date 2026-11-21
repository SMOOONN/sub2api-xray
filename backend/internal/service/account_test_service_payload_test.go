//go:build unit

package service

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCreateTestPayload_Temperature(t *testing.T) {
	tests := []struct {
		model           string
		wantTemperature bool
	}{
		{model: "claude-sonnet-4-6", wantTemperature: true},
		{model: "claude-opus-4-6", wantTemperature: true},
		{model: "claude-sonnet-5-5", wantTemperature: true},
		{model: "claude-haiku-5-5", wantTemperature: true},
		{model: "claude-opus-4-7", wantTemperature: false},
		{model: "claude-opus-4-8", wantTemperature: false},
		{model: "claude-opus-5-5", wantTemperature: false},
		{model: "claude-fable-5-2", wantTemperature: false},
		{model: "claude-mythos-5-2", wantTemperature: false},
	}
	for _, tt := range tests {
		t.Run(tt.model, func(t *testing.T) {
			payload, err := createTestPayload(tt.model, "")
			require.NoError(t, err)

			temperature, present := payload["temperature"]
			require.Equal(t, tt.wantTemperature, present)
			if tt.wantTemperature {
				require.Equal(t, 1, temperature)
			}
			require.Equal(t, tt.model, payload["model"])
		})
	}
}

func TestBuildBedrockTestPayload_Temperature(t *testing.T) {
	tests := []struct {
		model           string
		wantTemperature bool
	}{
		{model: "us.anthropic.claude-sonnet-4-6", wantTemperature: true},
		{model: "us.anthropic.claude-opus-4-8-v1", wantTemperature: false},
	}
	for _, tt := range tests {
		t.Run(tt.model, func(t *testing.T) {
			payload := buildBedrockTestPayload(tt.model, "  Check this account.  ")
			_, present := payload["temperature"]
			require.Equal(t, tt.wantTemperature, present)
			messages, ok := payload["messages"].([]map[string]any)
			require.True(t, ok)
			require.Len(t, messages, 1)
			content, ok := messages[0]["content"].([]map[string]any)
			require.True(t, ok)
			require.Len(t, content, 1)
			require.Equal(t, "Check this account.", content[0]["text"])
		})
	}
}
