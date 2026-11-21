package openai

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/v2/providers"
	"github.com/AltairaLabs/PromptKit/runtime/v2/types"
)

// azureRealtimeDeployment builds an Azure OpenAI provider through the factory, as a
// provider config does, for a deployment named model.
func azureRealtimeDeployment(t *testing.T, model string, extra map[string]any) providers.StreamInputSupport {
	t.Helper()
	p, err := providers.CreateProviderFromSpec(providers.ProviderSpec{
		ID: "voice", Type: "openai", Model: model, Credential: &tokenCredential{},
		Platform:         "azure",
		PlatformConfig:   &providers.PlatformConfig{Type: "azure", Endpoint: "https://res.openai.azure.com"},
		AdditionalConfig: extra,
	})
	require.NoError(t, err)
	si, ok := p.(providers.StreamInputSupport)
	require.True(t, ok, "the openai provider supports stream input")
	return si
}

// An Azure realtime deployment is named by whoever deployed it, so "realtime"
// need not appear in it. additional_config.realtime declares what the name
// cannot say, and the provider then offers stream input.
func TestDeclaredRealtime_DeploymentWithoutTheWordGetsStreamInput(t *testing.T) {
	got := azureRealtimeDeployment(t, "voice-prod", map[string]any{"realtime": true}).SupportsStreamInput()

	assert.Equal(t, []string{types.ContentTypeAudio}, got)
}

// The declaration wins both ways: realtime: false withholds stream input from
// a deployment whose name happens to contain "realtime".
func TestDeclaredRealtime_FalseWinsOverTheName(t *testing.T) {
	got := azureRealtimeDeployment(t, "gpt-realtime", map[string]any{"realtime": false}).SupportsStreamInput()

	assert.Equal(t, []string{}, got)
}

// Undeclared, the name rule decides, as before.
func TestUndeclaredRealtime_NameRuleDecides(t *testing.T) {
	assert.Equal(t, []string{types.ContentTypeAudio}, azureRealtimeDeployment(t, "gpt-realtime", nil).SupportsStreamInput())
	assert.Equal(t, []string{}, azureRealtimeDeployment(t, "voice-prod", nil).SupportsStreamInput())
}

// A value that is not a bool is ignored, and the name rule decides.
func TestDeclaredRealtime_NonBoolFallsBackToTheName(t *testing.T) {
	got := azureRealtimeDeployment(t, "voice-prod", map[string]any{"realtime": "yes"}).SupportsStreamInput()

	assert.Equal(t, []string{}, got)
}

// The declaration cannot open a session the platform refuses.
func TestDeclaredRealtime_PlatformStillGates(t *testing.T) {
	p, err := providers.CreateProviderFromSpec(providers.ProviderSpec{
		ID: "v", Type: "openai", Model: "voice-prod", Credential: &tokenCredential{},
		Platform:         "vertex",
		PlatformConfig:   &providers.PlatformConfig{Type: "vertex", Project: "p", Region: "r"},
		AdditionalConfig: map[string]any{"realtime": true},
	})
	require.NoError(t, err)

	assert.Equal(t, []string{}, p.(providers.StreamInputSupport).SupportsStreamInput())
}
