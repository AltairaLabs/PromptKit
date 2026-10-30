package handlers

import (
	"context"
	"fmt"
	"strings"

	"github.com/AltairaLabs/PromptKit/runtime/v2/evals"
)

// WorkflowTransitionOrderHandler checks that workflow state transitions happen in an expected order.
// Params: sequence []string (required) — expected ordered list of states that must appear as a subsequence.
type WorkflowTransitionOrderHandler struct{}

// Type returns the eval type identifier.
func (h *WorkflowTransitionOrderHandler) Type() string { return "workflow_transition_order" }

// Eval checks that the expected sequence appears as a subsequence of actual workflow transitions.
func (h *WorkflowTransitionOrderHandler) Eval(
	_ context.Context,
	evalCtx *evals.EvalContext,
	params map[string]any,
) (*evals.EvalResult, error) {
	sequence := extractStringSlice(params, "sequence")
	if len(sequence) == 0 {
		return &evals.EvalResult{
			Type:        h.Type(),
			Score:       boolScore(false),
			Explanation: "missing or empty required param 'sequence'",
		}, nil
	}

	raw, ok := evalCtx.Extras["workflow_transitions"]
	if !ok {
		return &evals.EvalResult{
			Type:        h.Type(),
			Score:       boolScore(false),
			Explanation: msgNoTransitions,
		}, nil
	}

	transitions, ok := raw.([]any)
	if !ok {
		return &evals.EvalResult{
			Type:        h.Type(),
			Score:       boolScore(false),
			Explanation: msgInvalidTransitions,
		}, nil
	}

	// Extract "to" values from each transition record.
	actual := make([]string, 0, len(transitions))
	for _, t := range transitions {
		tr, ok := t.(map[string]any)
		if !ok {
			continue
		}
		if to, _ := tr["to"].(string); to != "" {
			actual = append(actual, to)
		}
	}

	// Check subsequence: walk through actual transitions matching sequence elements in order.
	seqIdx := 0
	for _, state := range actual {
		if seqIdx < len(sequence) && state == sequence[seqIdx] {
			seqIdx++
		}
	}

	value := map[string]any{
		keyMatchedSteps:      seqIdx,
		keyTotalSteps:        len(sequence),
		keyExpectedSequence:  sequence,
		keyActualTransitions: actual,
	}

	if seqIdx < len(sequence) {
		return &evals.EvalResult{
			Type:  h.Type(),
			Score: ratioScore(seqIdx, len(sequence)),
			Value: value,
			Explanation: fmt.Sprintf(
				"sequence incomplete: matched %d/%d, missing %q",
				seqIdx, len(sequence), sequence[seqIdx],
			),
			Details: map[string]any{
				keyMatchedSteps:      seqIdx,
				keyTotalSteps:        len(sequence),
				keyExpectedSequence:  sequence,
				keyActualTransitions: actual,
			},
		}, nil
	}

	return &evals.EvalResult{
		Type:        h.Type(),
		Score:       ratioScore(seqIdx, len(sequence)),
		Value:       value,
		Explanation: fmt.Sprintf("sequence [%s] fully matched", strings.Join(sequence, " → ")),
		Details: map[string]any{
			keyMatchedSteps:      seqIdx,
			keyExpectedSequence:  sequence,
			keyActualTransitions: actual,
		},
	}, nil
}
