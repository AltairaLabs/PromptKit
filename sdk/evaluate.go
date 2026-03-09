package sdk

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/AltairaLabs/PromptKit/runtime/evals"
	"github.com/AltairaLabs/PromptKit/runtime/events"
	"github.com/AltairaLabs/PromptKit/runtime/types"
	"github.com/AltairaLabs/PromptKit/sdk/internal/pack"

	// Blank import ensures all built-in eval handlers are registered via init().
	_ "github.com/AltairaLabs/PromptKit/runtime/evals/handlers"
)

// EvaluateOpts configures standalone eval execution.
type EvaluateOpts struct {
	// --- Pack source (use one of PackPath, PackData, or EvalDefs) ---

	// PackPath loads a PromptPack from the filesystem.
	PackPath string

	// PackData parses a PromptPack from raw JSON bytes (e.g. from an API or config store).
	PackData []byte

	// EvalDefs provides pre-resolved eval definitions directly, bypassing pack loading.
	EvalDefs []evals.EvalDef

	// PromptName selects which prompt's evals to merge with pack-level evals.
	// Only used with PackPath or PackData. If empty, only pack-level evals run.
	PromptName string

	// --- Conversation snapshot ---

	// Messages is the conversation history to evaluate.
	Messages []types.Message

	// SessionID identifies the session for sampling determinism and result attribution.
	SessionID string

	// TurnIndex is the current turn index (0-based) for per-turn trigger filtering.
	TurnIndex int

	// --- Trigger filter ---

	// Trigger selects which eval trigger class to execute.
	// If empty, defaults to TriggerEveryTurn.
	Trigger evals.EvalTrigger

	// --- LLM judge support ---

	// JudgeProvider provides a pre-built judge for LLM judge evals.
	// Takes precedence over JudgeTargets.
	JudgeProvider any

	// JudgeTargets provides provider specs for LLM judge evals (Arena-style path).
	// The map keys are judge names; the SDK creates SpecJudgeProvider instances.
	JudgeTargets map[string]any

	// --- Observability ---

	// EventBus enables eval event emission (eval.completed / eval.failed).
	// If nil, events are not emitted.
	EventBus *events.EventBus

	// Logger is used for structured logging. If nil, the default logger is used.
	Logger *slog.Logger

	// --- Eval execution ---

	// Registry overrides the default eval type registry.
	// If nil, a registry with all built-in handlers is created.
	Registry *evals.EvalTypeRegistry

	// Timeout overrides the per-eval execution timeout.
	// If zero, the default (30s) is used.
	Timeout time.Duration

	// SkipSchemaValidation disables JSON schema validation when loading from PackPath.
	SkipSchemaValidation bool
}

// Evaluate runs evals from a PromptPack against a conversation snapshot.
// No live agent or provider connection is needed — just messages in, results out.
//
// Eval definitions can come from three sources (checked in order):
//  1. EvalDefs — pass pre-resolved definitions directly
//  2. PackData — parse a PromptPack from JSON bytes
//  3. PackPath — load a PromptPack from the filesystem
//
// The function builds an [evals.EvalContext] from the provided messages,
// dispatches to the appropriate runner method based on Trigger, and
// optionally emits events on the EventBus.
//
//nolint:gocritic // hugeParam: value receiver is intentional for public API ergonomics
func Evaluate(ctx context.Context, opts EvaluateOpts) ([]evals.EvalResult, error) {
	// 1. Resolve eval defs
	defs, err := resolveEvalDefs(&opts)
	if err != nil {
		return nil, fmt.Errorf("resolve eval defs: %w", err)
	}
	if len(defs) == 0 {
		return nil, nil
	}

	// 2. Build EvalContext from messages
	metadata := buildEvalMetadata(&opts)
	evalCtx := evals.BuildEvalContext(
		opts.Messages, opts.TurnIndex, opts.SessionID, opts.PromptName, metadata,
	)

	// 3. Create runner
	registry := opts.Registry
	if registry == nil {
		registry = evals.NewEvalTypeRegistry()
	}
	var runnerOpts []evals.RunnerOption
	if opts.Timeout > 0 {
		runnerOpts = append(runnerOpts, evals.WithTimeout(opts.Timeout))
	}
	runner := evals.NewEvalRunner(registry, runnerOpts...)

	// 4. Dispatch by trigger
	trigger := opts.Trigger
	if trigger == "" {
		trigger = evals.TriggerEveryTurn
	}
	results := dispatchEvals(ctx, runner, defs, evalCtx, trigger)

	// 5. Emit events (optional)
	if opts.EventBus != nil {
		emitEvalEvents(opts.EventBus, results)
	}

	return results, nil
}

// resolveEvalDefs resolves eval definitions from the opts.
func resolveEvalDefs(opts *EvaluateOpts) ([]evals.EvalDef, error) {
	// Direct defs take precedence (including empty slice — caller explicitly provided defs)
	if opts.EvalDefs != nil {
		return opts.EvalDefs, nil
	}

	// Load pack from bytes or path
	var p *pack.Pack
	var err error

	switch {
	case len(opts.PackData) > 0:
		p, err = pack.Parse(opts.PackData)
		if err != nil {
			return nil, fmt.Errorf("parse pack data: %w", err)
		}
	case opts.PackPath != "":
		loadOpts := pack.LoadOptions{SkipSchemaValidation: opts.SkipSchemaValidation}
		p, err = pack.Load(opts.PackPath, loadOpts)
		if err != nil {
			return nil, fmt.Errorf("load pack: %w", err)
		}
	default:
		return nil, fmt.Errorf("one of EvalDefs, PackData, or PackPath must be provided")
	}

	// Merge pack-level and prompt-level evals
	var promptEvals []evals.EvalDef
	if opts.PromptName != "" {
		if prompt := p.GetPrompt(opts.PromptName); prompt != nil {
			promptEvals = prompt.Evals
		}
	}

	return evals.ResolveEvals(p.Evals, promptEvals), nil
}

// buildEvalMetadata assembles the metadata map for the EvalContext.
func buildEvalMetadata(opts *EvaluateOpts) map[string]any {
	if opts.JudgeProvider == nil && opts.JudgeTargets == nil {
		return nil
	}
	metadata := make(map[string]any)
	if opts.JudgeProvider != nil {
		metadata["judge_provider"] = opts.JudgeProvider
	}
	if opts.JudgeTargets != nil {
		metadata["judge_targets"] = opts.JudgeTargets
	}
	return metadata
}

// dispatchEvals calls the appropriate runner method based on trigger.
func dispatchEvals(
	ctx context.Context,
	runner *evals.EvalRunner,
	defs []evals.EvalDef,
	evalCtx *evals.EvalContext,
	trigger evals.EvalTrigger,
) []evals.EvalResult {
	switch trigger { //nolint:exhaustive // Callers filter to meaningful triggers
	case evals.TriggerOnSessionComplete, evals.TriggerSampleSessions:
		return runner.RunSessionEvals(ctx, defs, evalCtx)
	case evals.TriggerOnConversationComplete:
		return runner.RunConversationEvals(ctx, defs, evalCtx)
	default:
		return runner.RunTurnEvals(ctx, defs, evalCtx)
	}
}

// emitEvalEvents emits eval results as events on the event bus.
func emitEvalEvents(bus *events.EventBus, results []evals.EvalResult) {
	emitter := events.NewEmitter(bus, "", "", "")
	for i := range results {
		r := &results[i]
		data := events.EvalEventData{
			EvalID:      r.EvalID,
			EvalType:    r.Type,
			Passed:      r.Passed,
			Score:       r.Score,
			Explanation: r.Explanation,
			DurationMs:  r.DurationMs,
			Error:       r.Error,
			Message:     r.Message,
			Skipped:     r.Skipped,
			SkipReason:  r.SkipReason,
		}
		for _, v := range r.Violations {
			data.Violations = append(data.Violations, v.Description)
		}
		if r.Passed {
			emitter.EvalCompleted(&data)
		} else {
			emitter.EvalFailed(&data)
		}
	}
}
