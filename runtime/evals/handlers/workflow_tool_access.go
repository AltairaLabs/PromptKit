package handlers

import (
	"context"
	"fmt"
	"strings"

	"github.com/AltairaLabs/PromptKit/runtime/evals"
)

// WorkflowToolAccessHandler enforces that tools are only called when the
// workflow is in a state that permits them.
// Params: rules []map[string]any — each with "state" (string) and "allowed" ([]string).
type WorkflowToolAccessHandler struct{}

// Type returns the eval type identifier.
func (h *WorkflowToolAccessHandler) Type() string { return "workflow_tool_access" }

// Eval checks that every tool call occurred in a workflow state that allows it.
func (h *WorkflowToolAccessHandler) Eval(
	_ context.Context,
	evalCtx *evals.EvalContext,
	params map[string]any,
) (*evals.EvalResult, error) {
	rules := parseAccessRules(params)
	if len(rules) == 0 {
		return &evals.EvalResult{
			Type:        h.Type(),
			Score:       boolScore(true),
			Explanation: "no access rules specified",
		}, nil
	}

	// Build state -> set-of-allowed-tools map.
	stateAllowed := make(map[string]map[string]bool, len(rules))
	for _, r := range rules {
		set := make(map[string]bool, len(r.allowed))
		for _, t := range r.allowed {
			set[t] = true
		}
		stateAllowed[r.state] = set
	}

	// Build turn-index-to-state mapping from workflow_transitions.
	turnState := buildTurnStateMap(evalCtx)

	var violations []map[string]any
	for _, tc := range evalCtx.ToolCalls {
		state, ok := turnState[tc.TurnIndex]
		if !ok {
			// No state known for this turn — open policy.
			continue
		}
		allowed, hasRule := stateAllowed[state]
		if !hasRule {
			// No rule for this state — open policy.
			continue
		}
		if !allowed[tc.ToolName] {
			violations = append(violations, map[string]any{
				"tool":       tc.ToolName,
				"state":      state,
				"turn_index": tc.TurnIndex,
			})
		}
	}

	passed := len(violations) == 0

	if !passed {
		msgs := make([]string, len(violations))
		for i, v := range violations {
			msgs[i] = fmt.Sprintf("%s called in state %q (turn %d)",
				v["tool"], v["state"], v["turn_index"])
		}
		return &evals.EvalResult{
			Type:        h.Type(),
			Score:       boolScore(false),
			Value:       map[string]any{"violations": violations, "violation_count": len(violations)},
			Explanation: fmt.Sprintf("%d violation(s): %s", len(violations), strings.Join(msgs, "; ")),
			Details:     map[string]any{"violations": violations},
		}, nil
	}

	return &evals.EvalResult{
		Type:        h.Type(),
		Score:       boolScore(true),
		Value:       map[string]any{"violations": []map[string]any{}, "violation_count": 0},
		Explanation: fmt.Sprintf("all tool calls comply with %d access rule(s)", len(rules)),
	}, nil
}

type accessRule struct {
	state   string
	allowed []string
}

func parseAccessRules(params map[string]any) []accessRule {
	raw, ok := params["rules"].([]any)
	if !ok {
		return nil
	}
	rules := make([]accessRule, 0, len(raw))
	for _, item := range raw {
		m, ok := item.(map[string]any)
		if !ok {
			continue
		}
		state, _ := m["state"].(string)
		if state == "" {
			continue
		}
		allowed := extractStringSlice(m, "allowed")
		rules = append(rules, accessRule{state: state, allowed: allowed})
	}
	return rules
}

// buildTurnStateMap returns a map from turn index to the workflow state active
// at that turn. It uses workflow_transitions when available, falling back to
// the scalar workflow_state for all turns.
func buildTurnStateMap(evalCtx *evals.EvalContext) map[int]string {
	turnState := make(map[int]string)

	raw, ok := evalCtx.Extras["workflow_transitions"]
	if !ok {
		// No transitions — fall back to current workflow_state for every tool call turn.
		return fallbackState(evalCtx)
	}

	transitions, ok := raw.([]any)
	if !ok || len(transitions) == 0 {
		return fallbackState(evalCtx)
	}

	// Check if transitions carry turn_index information.
	hasTurnIndex := false
	parsed := make([]transitionInfo, 0, len(transitions))
	for _, t := range transitions {
		tr, ok := t.(map[string]any)
		if !ok {
			continue
		}
		to, _ := tr["to"].(string)
		from, _ := tr["from"].(string)
		ti := transitionInfo{from: from, to: to}
		if idx, ok := extractTurnIndex(tr); ok {
			ti.turnIndex = idx
			hasTurnIndex = true
		}
		parsed = append(parsed, ti)
	}

	if !hasTurnIndex {
		// No per-transition turn indices — use the last "to" state as current state for all turns.
		if len(parsed) > 0 {
			last := parsed[len(parsed)-1]
			return constantState(evalCtx, last.to)
		}
		return fallbackState(evalCtx)
	}

	// Build timeline: determine state at each tool call's turn index.
	// Collect all tool call turn indices we need to resolve.
	for _, tc := range evalCtx.ToolCalls {
		state := resolveStateAtTurn(parsed, tc.TurnIndex)
		if state != "" {
			turnState[tc.TurnIndex] = state
		}
	}

	return turnState
}

// resolveStateAtTurn finds the workflow state active at a given turn index.
// The state after a transition at turnIndex T is the "to" state.
// Before the first transition, the state is the "from" field of the first transition.
func resolveStateAtTurn(transitions []transitionInfo, turn int) string {
	// Find the last transition at or before this turn.
	var state string
	for _, t := range transitions {
		if t.turnIndex <= turn {
			state = t.to
		} else {
			break
		}
	}
	// If no transition has happened yet, use the "from" of the first transition.
	if state == "" && len(transitions) > 0 && transitions[0].from != "" {
		state = transitions[0].from
	}
	return state
}

type transitionInfo struct {
	from      string
	to        string
	turnIndex int
}

func extractTurnIndex(tr map[string]any) (int, bool) {
	switch v := tr["turn_index"].(type) {
	case int:
		return v, true
	case float64:
		return int(v), true
	default:
		return 0, false
	}
}

func fallbackState(evalCtx *evals.EvalContext) map[int]string {
	state, _ := evalCtx.Extras["workflow_state"].(string)
	if state == "" {
		return nil
	}
	return constantState(evalCtx, state)
}

func constantState(evalCtx *evals.EvalContext, state string) map[int]string {
	turnState := make(map[int]string)
	for _, tc := range evalCtx.ToolCalls {
		turnState[tc.TurnIndex] = state
	}
	return turnState
}
