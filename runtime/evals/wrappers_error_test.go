package evals

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// An inner Error means nothing was measured; its placeholder 0 must not
// satisfy a max_score gate, nor any other threshold.
func TestAssertionApplyThresholds_InnerErrorFails(t *testing.T) {
	h := &AssertionEvalHandler{}
	zero, one := 0.0, 1.0
	errored := &EvalResult{Score: &zero, Error: "tool crashed"}
	assert.False(t, h.passes(errored, ScoreThresholds{Max: &zero}), "max_score")
	assert.False(t, h.passes(errored, ScoreThresholds{Min: &zero}), "min_score")
	assert.False(t, h.passes(errored, ScoreThresholds{}), "default")

	measured := &EvalResult{Score: &zero}
	assert.True(t, h.passes(measured, ScoreThresholds{Max: &zero}))
	assert.True(t, h.passes(&EvalResult{Score: &one}, ScoreThresholds{}))
}
