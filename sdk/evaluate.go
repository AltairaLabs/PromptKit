package sdk

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"go.opentelemetry.io/otel/trace"

	pkgconfig "github.com/AltairaLabs/PromptKit/pkg/config"
	"github.com/AltairaLabs/PromptKit/runtime/evals"
	"github.com/AltairaLabs/PromptKit/runtime/evals/handlers" // also registers built-in handlers via init()
	"github.com/AltairaLabs/PromptKit/runtime/events"
	"github.com/AltairaLabs/PromptKit/runtime/metrics"
	"github.com/AltairaLabs/PromptKit/runtime/telemetry"
	"github.com/AltairaLabs/PromptKit/runtime/types"
	"github.com/AltairaLabs/PromptKit/sdk/internal/pack"
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

	// --- Group filter ---

	// EvalGroups selects which eval groups to execute.
	// Each EvalDef can belong to one or more groups; evals with no explicit
	// groups belong to the "default" group. When set, only evals with at least
	// one matching group run. If nil, all evals run regardless of group.
	EvalGroups []string

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

	// TracerProvider enables OpenTelemetry trace emission for eval results.
	// When set, an OTelEventListener is automatically wired to the EventBus,
	// producing spans named "promptkit.eval.{evalID}" with GenAI SIG attributes.
	// An EventBus is created automatically if not provided.
	TracerProvider trace.TracerProvider

	// EventBus enables eval event emission (eval.completed / eval.failed).
	// If nil and TracerProvider is set, a bus is created automatically.
	EventBus *events.EventBus

	// Logger is used for structured logging. If nil, the default logger is used.
	Logger *slog.Logger

	// --- RuntimeConfig ---

	// RuntimeConfigPath loads exec eval handlers from a RuntimeConfig YAML file.
	// Exec eval bindings in the config are registered in the eval type registry,
	// enabling external subprocess evals (Python, Node.js, etc.) to run seamlessly.
	// If Registry is also provided, the exec handlers are registered into it.
	RuntimeConfigPath string

	// --- Metrics ---

	// MetricsCollector enables Prometheus eval metrics using the unified
	// Collector, mirroring the WithMetrics() pattern from the conversation API.
	// When set, the SDK calls Bind(MetricsInstanceLabels) internally and uses
	// the resulting MetricContext as the recorder.
	// Takes precedence over MetricRecorder.
	MetricsCollector *metrics.Collector

	// MetricsInstanceLabels provides per-invocation label values for the
	// MetricsCollector. Keys must match the InstanceLabels declared on the
	// Collector. If the Collector has no InstanceLabels, pass nil.
	MetricsInstanceLabels map[string]string

	// MetricRecorder records eval results as metrics (e.g. Prometheus gauges,
	// counters, histograms) based on Metric definitions in each EvalDef.
	// If nil, no metrics are recorded.
	// Prefer MetricsCollector for new code — MetricRecorder is useful when
	// you already have a custom recorder implementation.
	MetricRecorder evals.MetricRecorder

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
	// 0. Wire OTel listener if TracerProvider is set
	ownsBus := initEvalTracing(&opts)

	// 1. Resolve eval defs and apply group filter
	defs, err := resolveEvalDefs(&opts)
	if err != nil {
		return nil, fmt.Errorf("resolve eval defs: %w", err)
	}
	defs = evals.FilterByGroups(defs, opts.EvalGroups)
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

	// Register exec eval handlers from RuntimeConfig (if provided)
	if opts.RuntimeConfigPath != "" {
		if err := registerExecEvalHandlers(registry, opts.RuntimeConfigPath); err != nil {
			return nil, fmt.Errorf("load runtime config evals: %w", err)
		}
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
		emitEvalEvents(opts.EventBus, opts.SessionID, results)
	}

	// 6. Record metrics (optional)
	recorder := resolveMetricRecorder(&opts)
	if recorder != nil {
		writer := evals.NewMetricResultWriter(recorder, defs)
		if err := writer.WriteResults(ctx, results); err != nil {
			return results, fmt.Errorf("record metrics: %w", err)
		}
	}

	// 7. Close auto-created bus to flush OTel listener
	if ownsBus && opts.EventBus != nil {
		opts.EventBus.Close()
	}

	return results, nil
}

// resolveMetricRecorder returns the MetricRecorder to use for eval metrics.
// MetricsCollector takes precedence: Bind() is called internally to create
// a MetricContext, matching the WithMetrics() pattern from the conversation API.
func resolveMetricRecorder(opts *EvaluateOpts) evals.MetricRecorder {
	if opts.MetricsCollector != nil {
		return opts.MetricsCollector.Bind(opts.MetricsInstanceLabels)
	}
	return opts.MetricRecorder
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

// initEvalTracing wires an OTelEventListener to the EventBus when TracerProvider is set.
// Creates an EventBus if one wasn't provided. Returns true if the bus was auto-created
// (caller should close it after emitting events to flush the listener).
func initEvalTracing(opts *EvaluateOpts) bool {
	if opts.TracerProvider == nil {
		return false
	}
	createdBus := opts.EventBus == nil
	if createdBus {
		opts.EventBus = events.NewEventBus()
	}
	tracer := telemetry.Tracer(opts.TracerProvider)
	listener := telemetry.NewOTelEventListener(tracer)
	opts.EventBus.SubscribeAll(listener.OnEvent)
	return createdBus
}

// registerExecEvalHandlers loads a RuntimeConfig and registers any exec eval
// handlers into the given registry.
func registerExecEvalHandlers(registry *evals.EvalTypeRegistry, path string) error {
	rc, err := pkgconfig.LoadRuntimeConfig(path)
	if err != nil {
		return fmt.Errorf("loading runtime config: %w", err)
	}
	for typeName, binding := range rc.Spec.Evals {
		if binding == nil {
			continue
		}
		handler := handlers.NewExecEvalHandler(&handlers.ExecEvalConfig{
			TypeName:  typeName,
			Command:   binding.Command,
			Args:      binding.Args,
			Env:       binding.Env,
			TimeoutMs: binding.TimeoutMs,
		})
		registry.Register(handler)
	}
	return nil
}

// ValidateEvalTypesOpts configures eval type validation.
type ValidateEvalTypesOpts struct {
	// --- Pack source (use one of PackPath, PackData, or EvalDefs) ---

	// PackPath loads a PromptPack from the filesystem.
	PackPath string

	// PackData parses a PromptPack from raw JSON bytes.
	PackData []byte

	// EvalDefs provides pre-resolved eval definitions directly.
	EvalDefs []evals.EvalDef

	// PromptName selects which prompt's evals to merge with pack-level evals.
	// Only used with PackPath or PackData. If empty, only pack-level evals are checked.
	PromptName string

	// --- Extensibility ---

	// RuntimeConfigPath registers exec eval handlers from a RuntimeConfig YAML file
	// before validation, so custom eval types are recognized.
	RuntimeConfigPath string

	// Registry overrides the default eval type registry.
	// If nil, a registry with all built-in handlers is created.
	Registry *evals.EvalTypeRegistry

	// SkipSchemaValidation disables JSON schema validation when loading from PackPath.
	SkipSchemaValidation bool
}

// ValidateEvalTypes checks that every eval type referenced in the resolved
// eval definitions has a registered handler in the EvalTypeRegistry.
// Returns a list of eval IDs whose types are missing, or nil if all are valid.
//
// This is useful as a preflight check — e.g. at startup or in CI — to catch
// configuration errors (typos, missing RuntimeConfig bindings) before evals
// are actually executed.
//
//nolint:gocritic // hugeParam: value receiver is intentional for public API ergonomics
func ValidateEvalTypes(opts ValidateEvalTypesOpts) ([]evals.EvalDef, error) {
	// Reuse the same resolution logic as Evaluate().
	resolveOpts := &EvaluateOpts{
		PackPath:             opts.PackPath,
		PackData:             opts.PackData,
		EvalDefs:             opts.EvalDefs,
		PromptName:           opts.PromptName,
		SkipSchemaValidation: opts.SkipSchemaValidation,
	}
	defs, err := resolveEvalDefs(resolveOpts)
	if err != nil {
		return nil, fmt.Errorf("resolve eval defs: %w", err)
	}

	registry := opts.Registry
	if registry == nil {
		registry = evals.NewEvalTypeRegistry()
	}
	if opts.RuntimeConfigPath != "" {
		if err := registerExecEvalHandlers(registry, opts.RuntimeConfigPath); err != nil {
			return nil, fmt.Errorf("load runtime config evals: %w", err)
		}
	}

	var missing []evals.EvalDef
	for _, def := range defs {
		if !registry.Has(def.Type) {
			missing = append(missing, def)
		}
	}
	return missing, nil
}

// emitEvalEvents emits eval results as events on the event bus.
func emitEvalEvents(bus *events.EventBus, sessionID string, results []evals.EvalResult) {
	emitter := events.NewEmitter(bus, "", sessionID, "")
	emitEvalResultsTo(emitter, results)
}

// emitEvalResultsTo emits eval results through the given emitter.
// Shared by the standalone Evaluate() path and the eval middleware.
func emitEvalResultsTo(emitter *events.Emitter, results []evals.EvalResult) {
	for i := range results {
		r := &results[i]
		passed, _ := r.Value.(bool)
		data := events.EvalEventData{
			EvalID:      r.EvalID,
			EvalType:    r.Type,
			Passed:      passed,
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
		// EventEvalFailed means the eval errored, not that the score was low.
		if r.Error != "" {
			emitter.EvalFailed(&data)
		} else {
			emitter.EvalCompleted(&data)
		}
	}
}
