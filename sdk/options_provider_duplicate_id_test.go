package sdk

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The programmatic provider options used to overwrite their map entry while
// appending unconditionally to the ordered ID list, so registering an ID twice
// left one provider and two identical IDs — the list stopped being a set. The
// declarative path (a runtime config's *_providers blocks) rejected the same
// mistake outright, so one spelling errored and the other quietly malformed
// itself (#2000).
func TestProviderOptions_RejectDuplicateID(t *testing.T) {
	for _, tc := range []struct {
		name string
		spec ProviderSpec
		opt  func(ProviderSpec) Option
		ids  func(*config) []string
	}{
		{
			name: "WithEmbeddingProvider",
			spec: ProviderSpec{ID: "emb", Type: "openai", Model: "text-embedding-3-small"},
			opt:  WithEmbeddingProvider,
			ids:  func(c *config) []string { return c.embeddingProviderIDs },
		},
		{
			name: "WithTTSProvider",
			spec: ProviderSpec{ID: "voice", Type: "openai", Model: "tts-1"},
			opt:  WithTTSProvider,
			ids:  func(c *config) []string { return c.ttsProviderIDs },
		},
		{
			name: "WithSTTProvider",
			spec: ProviderSpec{ID: "ears", Type: "openai", Model: "whisper-1"},
			opt:  WithSTTProvider,
			ids:  func(c *config) []string { return c.sttProviderIDs },
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("OPENAI_API_KEY", "test-key")

			cfg := &config{}
			require.NoError(t, tc.opt(tc.spec)(cfg), "first registration should succeed")

			err := tc.opt(tc.spec)(cfg)
			require.Error(t, err, "registering the same ID twice should be an error, as it is declaratively")
			assert.Contains(t, err.Error(), "duplicate ID")

			assert.Equal(t, []string{tc.spec.ID}, tc.ids(cfg),
				"the ID list must stay a set; a consumer iterating it would build or bill the provider twice")
		})
	}
}

// A different ID is not a duplicate: the second provider registers and the
// first stays the default.
func TestProviderOptions_DistinctIDsBothRegister(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "test-key")

	cfg := &config{}
	require.NoError(t, WithEmbeddingProvider(
		ProviderSpec{ID: "primary", Type: "openai", Model: "text-embedding-3-small"})(cfg))
	first := cfg.retrievalProvider

	require.NoError(t, WithEmbeddingProvider(
		ProviderSpec{ID: "secondary", Type: "openai", Model: "text-embedding-3-large"})(cfg))

	assert.Equal(t, []string{"primary", "secondary"}, cfg.embeddingProviderIDs)
	assert.Len(t, cfg.embeddingProviders, 2)
	assert.Same(t, first, cfg.retrievalProvider, "the default is first-wins")
}
