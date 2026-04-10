package evals

import "context"

// EvalHook observes eval results as they are produced by the runner.
//
// Unlike ProviderHook or ToolHook, EvalHook is purely observational —
// evals compute scores, they do not gate execution, so there is no
// allow/deny semantics. Hooks are invoked once per executed eval, after
// the handler runs (or after a skip/error result is synthesized), and
// before the result is emitted as an event.
//
// Hooks may mutate the result in place (e.g. to redact sensitive content
// from Explanation, annotate Details, or attach tracing metadata). The
// mutated result is what the runner returns to its caller and emits on
// the event bus.
//
// Typical uses:
//   - Push results to external systems (metrics, tracing, logs)
//   - Redact or enrich result fields
//   - Shell out to a subprocess for custom scoring pipelines
type EvalHook interface {
	// Name returns a stable identifier for the hook (used in logs).
	Name() string
	// OnEvalResult is called once per eval result, after the handler runs.
	// The hook may mutate result in place.
	OnEvalResult(ctx context.Context, def *EvalDef, evalCtx *EvalContext, result *EvalResult)
}
