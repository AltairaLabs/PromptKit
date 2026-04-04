package credentials

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestResolvePlatformCredential_UnsupportedType(t *testing.T) {
	cfg := ResolverConfig{
		ProviderType: "openai",
		PlatformConfig: &PlatformConfig{
			Type: "unsupported-cloud",
		},
	}

	_, err := Resolve(context.Background(), cfg)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unsupported platform type")
	assert.Contains(t, err.Error(), "unsupported-cloud")
}

func TestResolvePlatformCredential_CaseInsensitive(t *testing.T) {
	// These should all attempt the correct dispatch path (and fail because
	// we don't have cloud credentials, but they should NOT return
	// "unsupported platform type").
	tests := []struct {
		name         string
		platformType string
	}{
		{"bedrock lowercase", "bedrock"},
		{"bedrock uppercase", "BEDROCK"},
		{"bedrock mixed", "Bedrock"},
		{"vertex lowercase", "vertex"},
		{"vertex uppercase", "VERTEX"},
		{"vertex mixed", "Vertex"},
		{"azure lowercase", "azure"},
		{"azure uppercase", "AZURE"},
		{"azure mixed", "Azure"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := ResolverConfig{
				ProviderType: "claude",
				PlatformConfig: &PlatformConfig{
					Type:    tt.platformType,
					Region:  "us-east-1",
					Project: "test-project",
				},
			}

			// The call may fail due to missing cloud credentials, but the error
			// should NOT be "unsupported platform type"
			_, err := Resolve(context.Background(), cfg)
			if err != nil {
				assert.NotContains(t, err.Error(), "unsupported platform type",
					"platform type %q should be recognized", tt.platformType)
			}
			// If it succeeds (e.g., AWS credentials happen to be available), that's fine too
		})
	}
}

func TestResolvePlatformCredential_EmptyType(t *testing.T) {
	// Empty platform type should fall through to API key resolution, not platform dispatch
	cfg := ResolverConfig{
		ProviderType: "openai",
		PlatformConfig: &PlatformConfig{
			Type: "",
		},
		CredentialConfig: &CredentialConfig{
			APIKey: "sk-fallback",
		},
	}

	cred, err := Resolve(context.Background(), cfg)
	require.NoError(t, err)
	require.NotNil(t, cred)

	// Should have resolved as API key, not platform
	assert.Equal(t, "api_key", cred.Type())
}

func TestResolvePlatformCredential_NilPlatformConfig(t *testing.T) {
	// Nil platform config should fall through to API key resolution
	cfg := ResolverConfig{
		ProviderType:   "openai",
		PlatformConfig: nil,
		CredentialConfig: &CredentialConfig{
			APIKey: "sk-test",
		},
	}

	cred, err := Resolve(context.Background(), cfg)
	require.NoError(t, err)
	require.NotNil(t, cred)
	assert.Equal(t, "api_key", cred.Type())
}
