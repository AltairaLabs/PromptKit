package engine

import (
	"context"

	"github.com/AltairaLabs/PromptKit/runtime/evals"
	"github.com/AltairaLabs/PromptKit/runtime/types"
	"github.com/AltairaLabs/PromptKit/tools/arena/assertions"
)

// PackEvalHook manages pack eval execution during Arena conversation runs.
// It wraps an EvalRunner and converts results into the assertion format
// used by Arena's reporting pipeline.
type PackEvalHook struct {
	runner   *evals.EvalRunner
	defs     []evals.EvalDef
	taskType string
	metadata map[string]any // injected into every EvalContext (e.g. judge_targets)
}

// NewPackEvalHook creates a hook for executing pack evals during Arena runs.
// If skipEvals is true, the runner is nil and all methods are no-ops.
// The evalTypeFilter, when non-empty, restricts execution to matching eval types.
func NewPackEvalHook(
	registry *evals.EvalTypeRegistry,
	defs []evals.EvalDef,
	skipEvals bool,
	evalTypeFilter []string,
	taskType string,
) *PackEvalHook {
	// Filter defs by eval type if filter is set
	filteredDefs := filterEvalDefs(defs, evalTypeFilter)

	var runner *evals.EvalRunner
	if !skipEvals {
		runner = evals.NewEvalRunner(registry)
	}

	return &PackEvalHook{
		runner:   runner,
		defs:     filteredDefs,
		taskType: taskType,
	}
}

// SetMetadata sets metadata that will be injected into every EvalContext.
// Used to pass judge_targets, prompt_registry, and other config to eval handlers.
func (h *PackEvalHook) SetMetadata(metadata map[string]any) {
	if h == nil {
		return
	}
	h.metadata = metadata
}

// HasEvals returns true if there are eval defs to execute.
func (h *PackEvalHook) HasEvals() bool {
	if h == nil {
		return false
	}
	return len(h.defs) > 0
}

// RunTurnEvals runs turn-triggered evals after a turn completes.
// Returns converted ConversationValidationResult entries.
func (h *PackEvalHook) RunTurnEvals(
	ctx context.Context,
	messages []types.Message,
	turnIndex int,
	sessionID string,
) []assertions.ConversationValidationResult {
	if h == nil || !h.HasEvals() || h.runner == nil {
		return nil
	}

	evalCtx := h.buildEvalContext(messages, turnIndex, sessionID)
	results := h.runner.RunTurnEvals(ctx, h.defs, evalCtx)
	applyDefaultPassFail(results)
	return assertions.ConvertEvalResults(results)
}

// RunSessionEvals runs session-complete evals after conversation finishes.
// Returns converted ConversationValidationResult entries.
func (h *PackEvalHook) RunSessionEvals(
	ctx context.Context,
	messages []types.Message,
	sessionID string,
) []assertions.ConversationValidationResult {
	if h == nil || !h.HasEvals() || h.runner == nil {
		return nil
	}

	turnIndex := len(messages) - 1
	if turnIndex < 0 {
		turnIndex = 0
	}
	evalCtx := h.buildEvalContext(messages, turnIndex, sessionID)
	results := h.runner.RunSessionEvals(ctx, h.defs, evalCtx)
	applyDefaultPassFail(results)
	return assertions.ConvertEvalResults(results)
}

// RunConversationEvals runs conversation-complete evals after all turns finish.
// Returns converted ConversationValidationResult entries.
func (h *PackEvalHook) RunConversationEvals(
	ctx context.Context,
	messages []types.Message,
	sessionID string,
) []assertions.ConversationValidationResult {
	if h == nil || !h.HasEvals() || h.runner == nil {
		return nil
	}

	turnIndex := len(messages) - 1
	if turnIndex < 0 {
		turnIndex = 0
	}
	evalCtx := h.buildEvalContext(messages, turnIndex, sessionID)
	results := h.runner.RunConversationEvals(ctx, h.defs, evalCtx)
	applyDefaultPassFail(results)
	return assertions.ConvertEvalResults(results)
}

// RunAssertionsAsEvals converts assertion configs to EvalDefs and runs them
// through the runner. Returns raw EvalResults (not converted to assertion format).
// The trigger parameter overrides the default trigger on each converted def.
//
// After the runner returns scores, this method applies assertion pass/fail
// logic: min_score/max_score thresholds from assertion params take precedence,
// falling back to IsPassed() (score >= 1.0) when no thresholds are configured.
func (h *PackEvalHook) RunAssertionsAsEvals(
	ctx context.Context,
	assertionConfigs []assertions.AssertionConfig,
	messages []types.Message,
	turnIndex int,
	sessionID string,
	trigger evals.EvalTrigger,
) []evals.EvalResult {
	if h == nil || h.runner == nil || len(assertionConfigs) == 0 {
		return nil
	}

	defs := make([]evals.EvalDef, len(assertionConfigs))
	for i, cfg := range assertionConfigs {
		defs[i] = assertions.ToEvalDef(cfg, i)
		defs[i].Trigger = trigger
	}

	evalCtx := h.buildEvalContext(messages, turnIndex, sessionID)

	var results []evals.EvalResult
	switch trigger { //nolint:exhaustive // Only conversation and turn triggers are meaningful here
	case evals.TriggerOnConversationComplete:
		results = h.runner.RunConversationEvals(ctx, defs, evalCtx)
	case evals.TriggerEveryTurn:
		results = h.runner.RunTurnEvals(ctx, defs, evalCtx)
	default:
		results = h.runner.RunTurnEvals(ctx, defs, evalCtx)
	}

	// Apply assertion pass/fail from score thresholds.
	// Eval handlers return scores only; assertion configs carry
	// min_score/max_score thresholds that determine pass/fail.
	applyAssertionPassFail(results, assertionConfigs)
	return results
}

// applyDefaultPassFail sets Passed on pack eval results using IsPassed()
// (score >= 1.0 or nil). This bridges the gap between score-only handlers
// and the Passed field used by ConvertEvalResults.
func applyDefaultPassFail(results []evals.EvalResult) {
	for i := range results {
		results[i].Passed = results[i].IsPassed() //nolint:staticcheck // bridge score→Passed for ConvertEvalResults
	}
}

// applyAssertionPassFail sets Passed on each result based on assertion
// config thresholds (min_score, max_score). When no thresholds are
// configured, falls back to IsPassed() (score >= 1.0).
func applyAssertionPassFail(results []evals.EvalResult, configs []assertions.AssertionConfig) {
	for i := range results {
		if i >= len(configs) {
			break
		}
		r := &results[i]
		if r.Skipped || r.Error != "" {
			continue
		}
		r.Passed = evalPassedWithThresholds(r, configs[i].Params) //nolint:staticcheck // assertion threshold→Passed
	}
}

// evalPassedWithThresholds checks min_score/max_score from params against the
// result score. Returns IsPassed() when no thresholds are configured.
func evalPassedWithThresholds(r *evals.EvalResult, params map[string]any) bool {
	minScore := extractFloat64Param(params, "min_score")
	maxScore := extractFloat64Param(params, "max_score")

	if minScore == nil && maxScore == nil {
		return r.IsPassed()
	}
	if r.Score == nil {
		return true
	}
	if minScore != nil && *r.Score < *minScore {
		return false
	}
	if maxScore != nil && *r.Score > *maxScore {
		return false
	}
	return true
}

// extractFloat64Param extracts a float64 from a params map, handling int→float64 coercion.
func extractFloat64Param(params map[string]any, key string) *float64 {
	if params == nil {
		return nil
	}
	v, ok := params[key]
	if !ok {
		return nil
	}
	switch n := v.(type) {
	case float64:
		return &n
	case int:
		f := float64(n)
		return &f
	case int64:
		f := float64(n)
		return &f
	default:
		return nil
	}
}

// RunAssertionsAsConversationResults converts assertion configs to EvalDefs,
// runs them through the runner, and wraps results in ConversationValidationResult.
// The results use the original assertion type names (not pack_eval: prefixed).
func (h *PackEvalHook) RunAssertionsAsConversationResults(
	ctx context.Context,
	assertionConfigs []assertions.AssertionConfig,
	messages []types.Message,
	turnIndex int,
	sessionID string,
	trigger evals.EvalTrigger,
) []assertions.ConversationValidationResult {
	if h == nil {
		return nil
	}
	results := h.RunAssertionsAsEvals(ctx, assertionConfigs, messages, turnIndex, sessionID, trigger)
	converted := assertions.ConvertEvalResults(results)
	// Restore original assertion type names — ConvertEvalResults adds pack_eval:
	// prefix which is only appropriate for pack-defined evals, not scenario assertions.
	for i := range converted {
		if i < len(assertionConfigs) {
			converted[i].Type = assertionConfigs[i].Type
		}
	}
	return converted
}

// buildEvalContext constructs an EvalContext from Arena messages.
// Delegates to the shared evals.BuildEvalContext helper.
func (h *PackEvalHook) buildEvalContext(
	messages []types.Message,
	turnIndex int,
	sessionID string,
) *evals.EvalContext {
	return evals.BuildEvalContext(messages, turnIndex, sessionID, h.taskType, h.metadata)
}

// filterEvalDefs filters eval defs to only include types in the filter list.
// If the filter is empty, all defs are returned.
func filterEvalDefs(defs []evals.EvalDef, filter []string) []evals.EvalDef {
	if len(filter) == 0 {
		return defs
	}

	allowed := make(map[string]bool, len(filter))
	for _, t := range filter {
		allowed[t] = true
	}

	var filtered []evals.EvalDef
	for i := range defs {
		if allowed[defs[i].Type] {
			filtered = append(filtered, defs[i])
		}
	}
	return filtered
}
