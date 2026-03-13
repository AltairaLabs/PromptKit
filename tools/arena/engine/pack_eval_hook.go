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
	return assertions.ConvertEvalResults(results)
}

// RunAssertionsAsEvals converts assertion configs to EvalDefs and runs them
// through the runner. Returns raw EvalResults (not converted to assertion format).
// The trigger parameter overrides the default trigger on each converted def.
//
// Each assertion is converted to an EvalDef with type "assertion", which the
// runner dispatches to AssertionEvalHandler. The wrapper resolves the inner
// eval handler from the registry, executes it, and applies min_score/max_score
// thresholds to determine pass/fail.
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

	switch trigger { //nolint:exhaustive // Only conversation and turn triggers are meaningful here
	case evals.TriggerOnConversationComplete:
		return h.runner.RunConversationEvals(ctx, defs, evalCtx)
	case evals.TriggerEveryTurn:
		return h.runner.RunTurnEvals(ctx, defs, evalCtx)
	default:
		return h.runner.RunTurnEvals(ctx, defs, evalCtx)
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
