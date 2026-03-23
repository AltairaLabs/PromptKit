package engine

import (
	"context"

	"github.com/AltairaLabs/PromptKit/runtime/evals"
	"github.com/AltairaLabs/PromptKit/runtime/events"
	"github.com/AltairaLabs/PromptKit/runtime/types"
	"github.com/AltairaLabs/PromptKit/tools/arena/assertions"
)

// WorkflowMetadataProvider is implemented by types that provide workflow state
// metadata for injection into the eval context during assertion evaluation.
type WorkflowMetadataProvider interface {
	WorkflowMetadata() map[string]any
}

// EvalOrchestrator orchestrates eval and assertion execution during Arena runs.
type EvalOrchestrator struct {
	runner               *evals.EvalRunner
	defs                 []evals.EvalDef
	taskType             string
	metadata             map[string]any           // injected into every EvalContext (e.g. judge_targets)
	eventBus             events.Bus               // optional event bus for provider call telemetry in evals
	workflowMetaProvider WorkflowMetadataProvider // optional workflow state provider
}

// NewEvalOrchestrator creates a hook for executing pack evals during Arena runs.
// If skipEvals is true, the runner is nil and all methods are no-ops.
// The evalTypeFilter, when non-empty, restricts execution to matching eval types.
func NewEvalOrchestrator(
	registry *evals.EvalTypeRegistry,
	defs []evals.EvalDef,
	skipEvals bool,
	evalTypeFilter []string,
	taskType string,
) *EvalOrchestrator {
	// Filter defs by eval type if filter is set
	filteredDefs := filterEvalDefs(defs, evalTypeFilter)

	var runner *evals.EvalRunner
	if !skipEvals {
		runner = evals.NewEvalRunner(registry)
	}

	return &EvalOrchestrator{
		runner:   runner,
		defs:     filteredDefs,
		taskType: taskType,
	}
}

// Clone creates a shallow copy suitable for per-run use. The runner and defs
// are shared (immutable after construction), but metadata and workflow provider
// are independent. This avoids data races when concurrent runs set different
// workflow metadata providers.
func (h *EvalOrchestrator) Clone() *EvalOrchestrator {
	if h == nil {
		return nil
	}
	clone := *h
	clone.workflowMetaProvider = nil
	// Clone the runner so the emitter is independent per run.
	// The emitter is wired per-call in buildEvalContext with proper session IDs.
	if h.runner != nil {
		clone.runner = h.runner.Clone()
	}
	// Copy metadata map so per-run mutations don't affect the original
	if h.metadata != nil {
		clone.metadata = make(map[string]any, len(h.metadata))
		for k, v := range h.metadata {
			clone.metadata[k] = v
		}
	}
	return &clone
}

// SetMetadata sets metadata that will be injected into every EvalContext.
// Used to pass judge_targets, prompt_registry, and other config to eval handlers.
func (h *EvalOrchestrator) SetMetadata(metadata map[string]any) {
	if h == nil {
		return
	}
	h.metadata = metadata
}

// SetEventBus configures the event bus for provider call telemetry in eval handlers.
// When set, an emitter is injected into each EvalContext's metadata so that
// LLM judge provider calls emit ProviderCallStarted/Completed/Failed events.
func (h *EvalOrchestrator) SetEventBus(bus events.Bus) {
	if h == nil {
		return
	}
	h.eventBus = bus
}

// HasEvals returns true if there are eval defs to execute.
func (h *EvalOrchestrator) HasEvals() bool {
	if h == nil {
		return false
	}
	return len(h.defs) > 0
}

// RunTurnEvals runs turn-triggered evals after a turn completes.
// Returns converted ConversationValidationResult entries.
func (h *EvalOrchestrator) RunTurnEvals(
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
func (h *EvalOrchestrator) RunSessionEvals(
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
func (h *EvalOrchestrator) RunConversationEvals(
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
func (h *EvalOrchestrator) RunAssertionsAsEvals(
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
func (h *EvalOrchestrator) RunAssertionsAsConversationResults(
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

// SetWorkflowMetadataProvider sets the workflow state provider for eval context injection.
// Called per-run for workflow scenarios so assertions can access the current workflow state.
func (h *EvalOrchestrator) SetWorkflowMetadataProvider(provider WorkflowMetadataProvider) {
	if h == nil {
		return
	}
	h.workflowMetaProvider = provider
}

// buildEvalContext constructs an EvalContext from Arena messages.
// Delegates to the shared evals.BuildEvalContext helper.
// If an event bus is configured, an emitter is injected into metadata
// so LLM judge handlers can emit provider call telemetry.
// If a workflow metadata provider is set, workflow state is injected
// so workflow assertions (state_is, transitioned_to, workflow_complete) work.
func (h *EvalOrchestrator) buildEvalContext(
	messages []types.Message,
	turnIndex int,
	sessionID string,
) *evals.EvalContext {
	metadata := h.metadata
	needsCopy := h.eventBus != nil || h.workflowMetaProvider != nil
	if needsCopy {
		merged := make(map[string]any, len(metadata)+4) //nolint:mnd // extra capacity for emitter + workflow keys
		for k, v := range metadata {
			merged[k] = v
		}
		if h.eventBus != nil {
			emitter := events.NewEmitter(h.eventBus, sessionID, sessionID, sessionID)
			merged["emitter"] = emitter
			// Wire the emitter into the runner for eval.completed/failed events.
			// Safe: each concurrent run gets its own orchestrator clone (and runner).
			if h.runner != nil {
				h.runner.SetEmitter(emitter)
			}
		}
		if h.workflowMetaProvider != nil {
			for k, v := range h.workflowMetaProvider.WorkflowMetadata() {
				merged[k] = v
			}
		}
		metadata = merged
	}
	return evals.BuildEvalContext(messages, turnIndex, sessionID, h.taskType, metadata)
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
