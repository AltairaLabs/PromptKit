package stage

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/AltairaLabs/PromptKit/runtime/v2/providers"
)

// The facade's wrappers of internal/compaction functions that stage's own
// tests do not call: each must hand back what compaction gives, so a stage.X
// caller sees no difference from before the move (#2876).

// facadeWindowProvider reports a context window; only BudgetTokensFor's type
// assertion is ever made on it.
type facadeWindowProvider struct {
	providers.Provider
	tokens int
}

func (p facadeWindowProvider) MaxContextTokens() int { return p.tokens }

func TestFacade_BudgetTokensFor(t *testing.T) {
	assert.Equal(t, 32000, BudgetTokensFor(facadeWindowProvider{tokens: 32000}))
	assert.Equal(t, DefaultBudgetTokens, BudgetTokensFor(facadeWindowProvider{}))
}

func TestFacade_FoldToolResults(t *testing.T) {
	rule := FoldToolResults()
	assert.Equal(t, "fold_tool_results", rule.Name())
	msg := toolResultMsg("search", "a result long enough to be worth folding into a compact summary")
	folded, extra := rule.Fold(&msg, 0, &CompactionContext{})
	assert.Equal(t, "[search: a result long enough to be worth folding into a compact summary — 63 bytes compacted]", folded)
	assert.Nil(t, extra)
}

func TestFacade_CollapsePairs(t *testing.T) {
	assert.Equal(t, "collapse_pairs", CollapsePairs().Name())
}

func TestFacade_Similarity(t *testing.T) {
	assert.InDelta(t, 1.0, CosineSimilarity([]float32{1, 2}, []float32{2, 4}), 1e-9)
	assert.Equal(t, 0.0, CosineSimilarity([]float32{1}, []float32{1, 2}))
	assert.Equal(t, []float32{0.6, 0.8}, NormalizeEmbedding([]float32{3, 4}))
	assert.Equal(t, [][]string{{"a", "b"}, {"c"}}, BatchEmbeddingTexts([]string{"a", "b", "c"}, 2))
}
