package sdk

import (
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/providers"
)

// closingProvider is a Provider that records Close invocations. The embedded
// interface keeps the type definition minimal — tests below only call Close
// and ID on these instances.
type closingProvider struct {
	providers.Provider
	id     string
	closeN atomic.Int32
}

func (p *closingProvider) ID() string   { return p.id }
func (p *closingProvider) Close() error { p.closeN.Add(1); return nil }

// TestSDK_ProviderPool_RegistersBothAgentAndSummarize pins the contract that
// WithProvider and WithAutoSummarize both feed the same providers.Registry.
// The pool closes every registered provider exactly once when invoked.
func TestSDK_ProviderPool_RegistersBothAgentAndSummarize(t *testing.T) {
	agent := &closingProvider{id: "agent-test"}
	summarizer := &closingProvider{id: "summarizer-test"}

	c := &config{}
	require.NoError(t, WithProvider(agent)(c))
	require.NoError(t, WithAutoSummarize(summarizer, 100, 50)(c))

	require.NotNil(t, c.providers, "pool should be initialised by With* options")
	assert.Equal(t, agent, c.getAgentProvider())
	assert.Equal(t, summarizer, c.getSummarizeProvider())
	assert.True(t, c.agentSet)
	assert.True(t, c.summarizeSet)

	// Pool.Close fans out to every registered provider exactly once.
	require.NoError(t, c.providers.Close())
	assert.Equal(t, int32(1), agent.closeN.Load(),
		"agent provider closed exactly once")
	assert.Equal(t, int32(1), summarizer.closeN.Load(),
		"summarizer provider closed exactly once")
}

// TestSDK_ProviderPool_LiftsLegacyAgentField pins the back-compat path: a
// *config built directly via struct literal with the deprecated `provider`
// field continues to work because getAgentProvider lifts it into the pool
// transparently.
func TestSDK_ProviderPool_LiftsLegacyAgentField(t *testing.T) {
	legacy := &closingProvider{id: "legacy-agent"}
	c := &config{provider: legacy}

	got := c.getAgentProvider()
	assert.Equal(t, legacy, got)
	assert.True(t, c.agentSet, "lift should mark agentSet true")
	assert.NotNil(t, c.providers, "pool should be initialised by lift")

	// Subsequent calls hit the pool, not the field — verify by mutating
	// the legacy field and confirming the resolved provider doesn't change.
	other := &closingProvider{id: "other-agent"}
	c.provider = other
	assert.Equal(t, legacy, c.getAgentProvider(),
		"already-lifted provider takes precedence over later legacy-field writes")
}

// TestSDK_ProviderPool_LiftsLegacySummarizeField mirrors the agent test
// for the summarizer path.
func TestSDK_ProviderPool_LiftsLegacySummarizeField(t *testing.T) {
	legacy := &closingProvider{id: "legacy-summarizer"}
	c := &config{summarizeProvider: legacy}

	got := c.getSummarizeProvider()
	assert.Equal(t, legacy, got)
	assert.True(t, c.summarizeSet)
	assert.NotNil(t, c.providers)
}

// TestSDK_ProviderPool_EmptyIDProvidersAreSetCorrectly pins the design
// decision to track presence via agentSet/summarizeSet rather than
// relying on a non-empty ID. Minimal mock providers may legitimately
// return an empty ID; they must still resolve through the pool.
func TestSDK_ProviderPool_EmptyIDProvidersAreSetCorrectly(t *testing.T) {
	emptyAgent := &closingProvider{id: ""}
	c := &config{}
	require.NoError(t, WithProvider(emptyAgent)(c))

	assert.True(t, c.agentSet)
	assert.Equal(t, emptyAgent, c.getAgentProvider(),
		"empty-ID providers must resolve via the presence flag, not ID matching")
}

// TestSDK_ProviderPool_NilWithProviderIsNoOp pins the prior behaviour:
// WithProvider(nil) must not error and must not register anything.
func TestSDK_ProviderPool_NilWithProviderIsNoOp(t *testing.T) {
	c := &config{}
	require.NoError(t, WithProvider(nil)(c))
	assert.False(t, c.agentSet)
	assert.Nil(t, c.getAgentProvider())
}
