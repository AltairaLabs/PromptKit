package handlers

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/v2/evals"
)

func offeredCtx(names ...string) *evals.EvalContext {
	return &evals.EvalContext{ToolsOffered: names}
}

// assertScore compares a result's score, which is a *float64 so handlers can
// distinguish "not scored" from zero.
func assertScore(t *testing.T, want float64, res *evals.EvalResult) {
	t.Helper()
	require.NotNil(t, res.Score)
	assert.Equal(t, want, *res.Score)
}

func assertScoreDelta(t *testing.T, want float64, res *evals.EvalResult) {
	t.Helper()
	require.NotNil(t, res.Score)
	assert.InDelta(t, want, *res.Score, 0.001)
}

func TestToolsOffered_PresentPasses(t *testing.T) {
	h := &ToolsOfferedHandler{}
	res, err := h.Eval(t.Context(), offeredCtx("get_order", "refund"),
		map[string]any{"tools": []any{"refund"}})
	require.NoError(t, err)
	assertScore(t, 1.0, res)
}

func TestToolsOffered_MissingFails(t *testing.T) {
	h := &ToolsOfferedHandler{}
	res, err := h.Eval(t.Context(), offeredCtx("get_order"),
		map[string]any{"tools": []any{"refund"}})
	require.NoError(t, err)
	assertScore(t, 0.0, res)
	assert.Contains(t, res.Explanation, "refund")
}

// The absence form is the one that distinguishes a working skill grant from a
// tool that was available all along.
func TestToolsOffered_AbsentPassesWhenNotOffered(t *testing.T) {
	h := &ToolsOfferedHandler{}
	res, err := h.Eval(t.Context(), offeredCtx("get_order"),
		map[string]any{"tools": []any{"refund"}, "absent": true})
	require.NoError(t, err)
	assertScore(t, 1.0, res)
}

func TestToolsOffered_AbsentFailsWhenOffered(t *testing.T) {
	h := &ToolsOfferedHandler{}
	res, err := h.Eval(t.Context(), offeredCtx("get_order", "refund"),
		map[string]any{"tools": []any{"refund"}, "absent": true})
	require.NoError(t, err)
	assertScore(t, 0.0, res)
	assert.Contains(t, res.Explanation, "expected absent")
}

func TestToolsOffered_ExactMatch(t *testing.T) {
	h := &ToolsOfferedHandler{}

	res, err := h.Eval(t.Context(), offeredCtx("get_order", "refund"),
		map[string]any{"tools": []any{"get_order", "refund"}, "exact": true})
	require.NoError(t, err)
	assertScore(t, 1.0, res)

	res, err = h.Eval(t.Context(), offeredCtx("get_order", "refund", "escalate"),
		map[string]any{"tools": []any{"get_order", "refund"}, "exact": true})
	require.NoError(t, err)
	assertScore(t, 0.0, res)
	assert.Contains(t, res.Explanation, "escalate")
}

func TestToolsOffered_PartialCreditForSomeMissing(t *testing.T) {
	h := &ToolsOfferedHandler{}
	res, err := h.Eval(t.Context(), offeredCtx("refund"),
		map[string]any{"tools": []any{"refund", "get_order"}})
	require.NoError(t, err)
	assertScoreDelta(t, 0.5, res)
}

func TestToolsOffered_NoToolNamesIsAConfigError(t *testing.T) {
	h := &ToolsOfferedHandler{}
	res, err := h.Eval(t.Context(), offeredCtx("refund"), map[string]any{})
	require.NoError(t, err)
	assertScore(t, 0.0, res)
	assert.Contains(t, res.Explanation, "no tool_names specified")
}

// An empty set cannot be judged either way, and saying so beats guessing: a
// pass would let a host that never records the set claim the assertion held.
func TestToolsOffered_EmptySetSaysSo(t *testing.T) {
	h := &ToolsOfferedHandler{}
	res, err := h.Eval(t.Context(), offeredCtx(), map[string]any{"tools": []any{"refund"}})
	require.NoError(t, err)
	assertScore(t, 0.0, res)
	assert.Contains(t, res.Explanation, "does not populate")
}

func TestToolsOffered_AcceptsToolNamesAlias(t *testing.T) {
	h := &ToolsOfferedHandler{}
	res, err := h.Eval(t.Context(), offeredCtx("refund"),
		map[string]any{"tool_names": []any{"refund"}})
	require.NoError(t, err)
	assertScore(t, 1.0, res)
}
