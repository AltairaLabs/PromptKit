package compaction

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/v2/providers"
)

// windowProvider is a provider that reports a context window. Only the type
// assertions BudgetTokensFor makes are ever called on it.
type windowProvider struct {
	providers.Provider
	tokens int
}

func (p windowProvider) MaxContextTokens() int { return p.tokens }

// plainProvider reports no context window.
type plainProvider struct{ providers.Provider }

func TestBudgetTokensFor(t *testing.T) {
	assert.Equal(t, 32000, BudgetTokensFor(windowProvider{tokens: 32000}))
	assert.Equal(t, DefaultBudgetTokens, BudgetTokensFor(windowProvider{tokens: 0}),
		"a window of zero is no window")
	assert.Equal(t, DefaultBudgetTokens, BudgetTokensFor(plainProvider{}))
	assert.Equal(t, 128000, DefaultBudgetTokens)
}

// With BudgetFromProvider the compactor re-budgets for the provider a round
// runs on, on a copy; otherwise, or when compaction is off, it stays as it is.
func TestContextCompactor_ForProvider(t *testing.T) {
	c := &ContextCompactor{BudgetTokens: 64000, BudgetFromProvider: true, PinRecentCount: 2}

	got := c.ForProvider(windowProvider{tokens: 32000})
	moved, ok := got.(*ContextCompactor)
	require.True(t, ok)
	assert.NotSame(t, c, moved)
	assert.Equal(t, ContextCompactor{BudgetTokens: 32000, BudgetFromProvider: true, PinRecentCount: 2}, *moved)
	assert.Equal(t, 64000, c.BudgetTokens, "the configured compactor is not written")

	assert.Same(t, c, c.ForProvider(windowProvider{tokens: 64000}), "the same budget needs no copy")
	assert.Same(t, c, c.ForProvider(nil))

	fixed := &ContextCompactor{BudgetTokens: 64000}
	assert.Same(t, fixed, fixed.ForProvider(windowProvider{tokens: 32000}), "a fixed budget stays fixed")

	off := &ContextCompactor{BudgetFromProvider: true}
	assert.Same(t, off, off.ForProvider(windowProvider{tokens: 32000}), "a zero budget means off, and stays off")

	var none *ContextCompactor
	assert.Equal(t, (*ContextCompactor)(nil), none.ForProvider(windowProvider{tokens: 32000}))
}

func TestContextCompactor_TokenBudget(t *testing.T) {
	assert.Equal(t, 5000, (&ContextCompactor{BudgetTokens: 5000}).TokenBudget())
	var none *ContextCompactor
	assert.Equal(t, 0, none.TokenBudget())
}

func TestContextCompactor_EffectiveThresholdAndPin(t *testing.T) {
	threshold, pin := (&ContextCompactor{Threshold: 0.5, PinRecentCount: 7}).effectiveThresholdAndPin()
	assert.Equal(t, 0.5, threshold)
	assert.Equal(t, 7, pin)

	threshold, pin = (&ContextCompactor{Threshold: -1, PinRecentCount: -1}).effectiveThresholdAndPin()
	assert.Equal(t, 0.70, threshold)
	assert.Equal(t, 4, pin)

	threshold, pin = (&ContextCompactor{}).effectiveThresholdAndPin()
	assert.Equal(t, 0.70, threshold, "unset is the default")
	assert.Equal(t, 4, pin, "unset is the default")
}
