package handlers

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/AltairaLabs/PromptKit/runtime/v2/evals"
)

// ToolsOfferedHandler checks which tools a turn handed the provider.
//
// Every other tool eval observes what the model *called*. This one observes
// what it *could* call, which is the only way to assert on a tool set the model
// may never exercise. A skill's allowed-tools grant that works and one that
// silently does nothing produce identical tool calls whenever the model does
// not go on to use the granted tool — so #1957's own plan asked for this, and
// without it the grant path shipped twice without working.
//
// Params:
//   - tool_names/tools []string — the tools to check for
//   - absent bool — when true, none of them may be offered (default false)
//   - exact bool — when true, the offered set must be exactly tool_names
//     (default false: the listed tools must be present, others may be too)
//
// The absent form is the valuable half. "refund must not be offered before the
// skill is activated" is what distinguishes a working grant from a tool that
// was available all along.
type ToolsOfferedHandler struct{}

// Result-value map keys.
const (
	keyOffered    = "offered"
	keyUnexpected = "unexpected"
)

// errNoToolNames is the explanation shared by every tool eval that requires a
// tool_names/tools param.
const errNoToolNames = "no tool_names specified"

// Type returns the eval type identifier.
func (h *ToolsOfferedHandler) Type() string { return "tools_offered" }

// Eval checks the turn's offered tool set against the expectation.
func (h *ToolsOfferedHandler) Eval(
	_ context.Context,
	evalCtx *evals.EvalContext,
	params map[string]any,
) (*evals.EvalResult, error) {
	want := extractStringSlice(params, "tool_names")
	if len(want) == 0 {
		want = extractStringSlice(params, "tools")
	}
	if len(want) == 0 {
		return &evals.EvalResult{
			Type:        h.Type(),
			Score:       boolScore(false),
			Explanation: errNoToolNames,
		}, nil
	}

	offered := evalCtx.ToolsOffered
	value := map[string]any{
		keyOffered:  offered,
		keyExpected: want,
	}

	// An unpopulated set cannot be judged. Scoring it as a pass would let a
	// host that never records the offered tools claim the assertion held;
	// scoring it as a fail would break every host that has not adopted it.
	// Saying so is the only honest option.
	if len(offered) == 0 {
		return &evals.EvalResult{
			Type:  h.Type(),
			Score: boolScore(false),
			Value: value,
			Explanation: "no tools recorded for this turn — either the turn offered none, " +
				"or the host does not populate EvalContext.ToolsOffered",
		}, nil
	}

	offeredSet := make(map[string]bool, len(offered))
	for _, name := range offered {
		offeredSet[name] = true
	}

	if extractBool(params, "absent") {
		return h.evalAbsent(want, offeredSet, value)
	}
	if extractBool(params, "exact") {
		return h.evalExact(want, offered, offeredSet, value)
	}
	return h.evalPresent(want, offeredSet, value)
}

// evalPresent requires every named tool to be offered; others may be too.
func (h *ToolsOfferedHandler) evalPresent(
	want []string, offeredSet map[string]bool, value map[string]any,
) (*evals.EvalResult, error) {
	var missing []string
	for _, name := range want {
		if !offeredSet[name] {
			missing = append(missing, name)
		}
	}
	sort.Strings(missing)
	value[keyMissing] = missing

	if len(missing) > 0 {
		return &evals.EvalResult{
			Type:        h.Type(),
			Score:       ratioScore(len(want)-len(missing), len(want)),
			Value:       value,
			Explanation: fmt.Sprintf("tools not offered: %s", strings.Join(missing, ", ")),
		}, nil
	}
	return &evals.EvalResult{
		Type:        h.Type(),
		Score:       boolScore(true),
		Value:       value,
		Explanation: "all expected tools were offered",
	}, nil
}

// evalAbsent requires none of the named tools to be offered.
func (h *ToolsOfferedHandler) evalAbsent(
	want []string, offeredSet map[string]bool, value map[string]any,
) (*evals.EvalResult, error) {
	var present []string
	for _, name := range want {
		if offeredSet[name] {
			present = append(present, name)
		}
	}
	sort.Strings(present)
	value[keyUnexpected] = present

	if len(present) > 0 {
		return &evals.EvalResult{
			Type:        h.Type(),
			Score:       ratioScore(len(want)-len(present), len(want)),
			Value:       value,
			Explanation: fmt.Sprintf("tools offered but expected absent: %s", strings.Join(present, ", ")),
		}, nil
	}
	return &evals.EvalResult{
		Type:        h.Type(),
		Score:       boolScore(true),
		Value:       value,
		Explanation: "none of the named tools were offered",
	}, nil
}

// evalExact requires the offered set to match tool_names exactly.
func (h *ToolsOfferedHandler) evalExact(
	want, offered []string, offeredSet map[string]bool, value map[string]any,
) (*evals.EvalResult, error) {
	wantSet := make(map[string]bool, len(want))
	for _, name := range want {
		wantSet[name] = true
	}

	var missing, unexpected []string
	for _, name := range want {
		if !offeredSet[name] {
			missing = append(missing, name)
		}
	}
	for _, name := range offered {
		if !wantSet[name] {
			unexpected = append(unexpected, name)
		}
	}
	sort.Strings(missing)
	sort.Strings(unexpected)
	value[keyMissing] = missing
	value[keyUnexpected] = unexpected

	if len(missing) == 0 && len(unexpected) == 0 {
		return &evals.EvalResult{
			Type:        h.Type(),
			Score:       boolScore(true),
			Value:       value,
			Explanation: "the offered tools match exactly",
		}, nil
	}

	var parts []string
	if len(missing) > 0 {
		parts = append(parts, "not offered: "+strings.Join(missing, ", "))
	}
	if len(unexpected) > 0 {
		parts = append(parts, "unexpectedly offered: "+strings.Join(unexpected, ", "))
	}
	return &evals.EvalResult{
		Type:        h.Type(),
		Score:       boolScore(false),
		Value:       value,
		Explanation: strings.Join(parts, "; "),
	}, nil
}
