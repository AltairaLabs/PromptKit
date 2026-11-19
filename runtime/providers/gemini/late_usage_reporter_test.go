package gemini

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/v2/providers"
)

// A Gemini provider built from a spec declares that its Live sessions report
// a call's usage after the call ends, so the streaming pipeline holds a
// completion with no usage for it.
func TestProvider_ReportsUsageAfterCallEnds(t *testing.T) {
	p, err := providers.CreateProviderFromSpec(providers.ProviderSpec{
		ID: "live", Type: "gemini", Model: "gemini-live", BaseURL: "http://127.0.0.1:0",
	})
	require.NoError(t, err)
	r, ok := p.(providers.LateUsageReporter)
	require.True(t, ok, "the Gemini provider is a providers.LateUsageReporter")
	assert.True(t, r.ReportsUsageAfterCallEnds())
}
