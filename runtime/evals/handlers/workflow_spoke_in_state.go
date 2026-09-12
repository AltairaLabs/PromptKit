package handlers

import (
	"context"
	"fmt"
	"strings"

	"github.com/AltairaLabs/PromptKit/runtime/v2/evals"
)

// workflowStateMetaKey is the message metadata key under which the runtime
// records the workflow state that produced each assistant message. Declared
// here rather than imported: runtime/pipeline/stage owns the constant, and
// evals must not depend on the pipeline.
const workflowStateMetaKey = "current_workflow_state"

// Result keys and messages for this handler.
const (
	keyState           = "state"
	keyStatesThatSpoke = "states_that_spoke"
	msgMissingState    = "missing required param 'state'"
)

// WorkflowSpokeInStateHandler checks that a workflow state actually produced
// assistant output, rather than merely being entered.
//
// This closes the blind spot that let the in-turn handoff bug ship. state_is,
// transitioned_to, workflow_complete and workflow_transition_order all read the
// state machine's history, so a transition that generates no output at all
// satisfies every one of them — which is exactly what was broken: the machine
// advanced and the destination state never spoke.
//
// Params: state string (required).
type WorkflowSpokeInStateHandler struct{}

// Type returns the eval type identifier.
func (h *WorkflowSpokeInStateHandler) Type() string { return "spoke_in_state" }

// Eval reports whether any assistant message with non-empty text was produced
// while the workflow was in the named state.
func (h *WorkflowSpokeInStateHandler) Eval(
	_ context.Context,
	evalCtx *evals.EvalContext,
	params map[string]any,
) (*evals.EvalResult, error) {
	expected, _ := params[keyState].(string)
	if expected == "" {
		return &evals.EvalResult{
			Type:        h.Type(),
			Score:       boolScore(false),
			Explanation: msgMissingState,
		}, nil
	}

	spoke, anyStamped := statesThatSpoke(evalCtx)
	for _, state := range spoke {
		if state == expected {
			return &evals.EvalResult{
				Type:        h.Type(),
				Score:       boolScore(true),
				Value:       map[string]any{keyState: expected},
				Explanation: fmt.Sprintf("state %q produced assistant output", expected),
				Details:     map[string]any{keyStatesThatSpoke: spoke},
			}, nil
		}
	}

	// Distinguish "this state was silent" from "nothing records state at all".
	// The second means the run predates the per-message stamp or the consumer
	// has not wired the resolver — a different bug, and one that would
	// otherwise look like an ordinary assertion failure.
	explanation := fmt.Sprintf("no assistant message with text was produced in state %q", expected)
	if !anyStamped {
		explanation = "no assistant message carries a workflow state: the run produced no " +
			"per-message state stamps, so the workflow resolver is probably not wired"
	}

	return &evals.EvalResult{
		Type:        h.Type(),
		Score:       boolScore(false),
		Value:       map[string]any{keyState: expected},
		Explanation: explanation,
		Details: map[string]any{
			keyExpected:         expected,
			keyStatesThatSpoke:  spoke,
			"any_state_stamped": anyStamped,
		},
	}, nil
}

// statesThatSpoke returns the states that produced assistant text, in order of
// first appearance and de-duplicated, plus whether any assistant message
// carried a state stamp at all.
//
// Whitespace does not count as speech: a state that emits only blank content
// has said nothing, which is the case this check exists to catch.
func statesThatSpoke(evalCtx *evals.EvalContext) (states []string, anyStamped bool) {
	if evalCtx == nil {
		return nil, false
	}
	seen := map[string]bool{}
	for i := range evalCtx.Messages {
		msg := &evalCtx.Messages[i]
		if !strings.EqualFold(msg.Role, roleAssistant) {
			continue
		}
		state, ok := stampedState(msg.Meta)
		if !ok {
			continue
		}
		anyStamped = true
		if strings.TrimSpace(msg.GetContent()) == "" {
			continue
		}
		if !seen[state] {
			seen[state] = true
			states = append(states, state)
		}
	}
	return states, anyStamped
}

// stampedState reads the workflow state recorded on a message.
func stampedState(meta map[string]any) (string, bool) {
	if len(meta) == 0 {
		return "", false
	}
	raw, ok := meta[workflowStateMetaKey].(map[string]any)
	if !ok {
		return "", false
	}
	state, ok := raw["current_state"].(string)
	if !ok || state == "" {
		return "", false
	}
	return state, true
}

// ValidateParams checks that the required 'state' param is set to a non-empty
// string.
func (h *WorkflowSpokeInStateHandler) ValidateParams(params map[string]any) error {
	state, _ := params[keyState].(string)
	if state == "" {
		return fmt.Errorf("%s requires a non-empty 'state' string param", h.Type())
	}
	return nil
}
