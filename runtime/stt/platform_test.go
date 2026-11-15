package stt

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/v2/credentials"
)

// openai STT runs on azure, so its credential is the platform's chain.
func TestResolveCredentialForPlatform_DeclaredPlatformUsesItsChain(t *testing.T) {
	cred, err := ResolveCredentialForPlatform(context.Background(), "openai", "", nil,
		&credentials.PlatformConfig{Type: "azure", Endpoint: "https://res.openai.azure.com"})
	require.NoError(t, err)
	assert.Equal(t, "azure", cred.Type())
}

// On a platform the type does not run on, the type's own key is resolved.
func TestResolveCredentialForPlatform_UndeclaredPlatformKeepsTheKey(t *testing.T) {
	cred, err := ResolveCredentialForPlatform(context.Background(), "openai", "",
		&credentials.CredentialConfig{APIKey: "own-key"},
		&credentials.PlatformConfig{Type: "vertex", Project: "p", Region: "r"})
	require.NoError(t, err)
	assert.Equal(t, "own-key", APIKeyFromCredential(cred))
}

func TestCreateFromSpec_AzureOpenAIWithoutEndpointFails(t *testing.T) {
	_, err := CreateFromSpec(ProviderSpec{
		ID: "ears", Type: "openai", Model: "my-transcribe", Credential: credentials.NewAPIKeyCredential("k"),
		Platform: &credentials.PlatformConfig{Type: "azure"},
	})
	require.Error(t, err)
	assert.Equal(t, `openai "ears" on azure needs platform.endpoint`, err.Error())
}
