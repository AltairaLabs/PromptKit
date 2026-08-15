package handlers

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/evals"
	"github.com/AltairaLabs/PromptKit/runtime/types"
)

// stamped builds an assistant message carrying the per-message workflow state
// the runtime records (stage.workflowStateMetaKey).
func stamped(content, state string) types.Message {
	return types.Message{
		Role:    "assistant",
		Content: content,
		Meta: map[string]any{
			"current_workflow_state": map[string]any{"current_state": state},
		},
	}
}

func spokeCtx(msgs ...types.Message) *evals.EvalContext {
	return &evals.EvalContext{Messages: msgs}
}

func evalSpoke(t *testing.T, ctx *evals.EvalContext, state string) *evals.EvalResult {
	t.Helper()
	h := &WorkflowSpokeInStateHandler{}
	res, err := h.Eval(context.Background(), ctx, map[string]any{"state": state})
	require.NoError(t, err)
	return res
}

func TestSpokeInState_PassesWhenStateProducedText(t *testing.T) {
	res := evalSpoke(t, spokeCtx(
		stamped("How can I help?", "triage"),
		stamped("I'm the escalations agent.", "handoff"),
	), "handoff")

	assert.Equal(t, 1.0, *res.Score)
	assert.Contains(t, res.Explanation, "handoff")
}

// The whole point of the check: #1747's bug was a transition that advanced the
// state machine while the destination said nothing. transitioned_to passes on
// that; this must not.
func TestSpokeInState_FailsWhenStateWasEnteredButSilent(t *testing.T) {
	res := evalSpoke(t, spokeCtx(
		stamped("How can I help?", "triage"),
		stamped("", "handoff"), // entered the state, produced no text
	), "handoff")

	assert.Equal(t, 0.0, *res.Score)
	assert.Contains(t, res.Explanation, "no assistant message")
}

// Whitespace is not speech.
func TestSpokeInState_TreatsWhitespaceAsSilence(t *testing.T) {
	res := evalSpoke(t, spokeCtx(stamped("   \n\t ", "handoff")), "handoff")

	assert.Equal(t, 0.0, *res.Score)
}

// A user message tagged with the state is not the agent speaking.
func TestSpokeInState_IgnoresNonAssistantMessages(t *testing.T) {
	user := types.Message{
		Role:    "user",
		Content: "hello",
		Meta:    map[string]any{"current_workflow_state": map[string]any{"current_state": "handoff"}},
	}
	res := evalSpoke(t, spokeCtx(user), "handoff")

	assert.Equal(t, 0.0, *res.Score)
}

// Diagnosing a failure needs to distinguish "that state never spoke" from
// "nothing is stamping states at all" — the latter means the run predates the
// per-message stamp or the resolver is not wired, which is a different bug.
func TestSpokeInState_DistinguishesUnstampedRun(t *testing.T) {
	res := evalSpoke(t, spokeCtx(
		types.Message{Role: "assistant", Content: "hello"},
	), "handoff")

	assert.Equal(t, 0.0, *res.Score)
	assert.Contains(t, res.Explanation, "no assistant message carries a workflow state")
}

// The failure detail should name the states that did speak, so the author can
// see what happened instead of guessing.
func TestSpokeInState_ReportsStatesThatDidSpeak(t *testing.T) {
	res := evalSpoke(t, spokeCtx(
		stamped("hi", "triage"),
		stamped("still here", "triage"),
		stamped("done", "confirmation"),
	), "handoff")

	assert.Equal(t, 0.0, *res.Score)
	require.NotNil(t, res.Details)
	spoke, ok := res.Details["states_that_spoke"].([]string)
	require.True(t, ok, "details = %v", res.Details)
	assert.Equal(t, []string{"triage", "confirmation"}, spoke, "in order, de-duplicated")
}

func TestSpokeInState_RequiresStateParam(t *testing.T) {
	h := &WorkflowSpokeInStateHandler{}
	res, err := h.Eval(context.Background(), spokeCtx(stamped("hi", "a")), map[string]any{})
	require.NoError(t, err)
	assert.Equal(t, 0.0, *res.Score)
	assert.Contains(t, res.Explanation, "state")

	assert.Error(t, h.ValidateParams(map[string]any{}))
	assert.NoError(t, h.ValidateParams(map[string]any{"state": "handoff"}))
}

func TestSpokeInState_IsRegistered(t *testing.T) {
	reg := evals.NewEvalTypeRegistry()
	h, err := reg.Get("spoke_in_state")
	require.NoError(t, err)
	assert.Equal(t, "spoke_in_state", h.Type())
}
