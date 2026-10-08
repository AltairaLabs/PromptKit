package openai

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/v2/providers"
)

// A conversation's view is a separate provider, and so is the Provider it
// wraps, so its media settings cannot reach the shared one (#2216).
func TestWithMediaSettings_ReturnsASeparateView(t *testing.T) {
	shared := NewToolProvider("v", "m", "http://x", providers.ProviderDefaults{}, false, nil, nil)
	view, ok := shared.WithMediaSettings(providers.MediaSettings{AllowPrivateNetworks: true}).(*ToolProvider)
	require.True(t, ok)
	assert.NotSame(t, shared, view)
	assert.NotSame(t, shared.Provider, view.Provider, "the wrapped provider is copied too")
	assert.Equal(t, shared.ID(), view.ID())

	plain, ok := shared.Provider.WithMediaSettings(providers.MediaSettings{}).(*Provider)
	require.True(t, ok)
	assert.NotSame(t, shared.Provider, plain)
}
