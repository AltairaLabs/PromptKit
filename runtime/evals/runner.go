package evals

import (
	"context"
	"fmt"
	"time"
)

// DefaultEvalTimeout is the per-eval execution timeout.
const DefaultEvalTimeout = 30 * time.Second

// EvalRunner executes evals in-process. It is the leaf execution unit
// used by all dispatch modes (in-proc, event-driven, worker).
type EvalRunner struct {
	registry *EvalTypeRegistry
	timeout  time.Duration
}

// RunnerOption configures an EvalRunner.
type RunnerOption func(*EvalRunner)

// WithTimeout sets the per-eval execution timeout.
func WithTimeout(d time.Duration) RunnerOption {
	return func(r *EvalRunner) { r.timeout = d }
}

// NewEvalRunner creates an EvalRunner with the given registry and options.
func NewEvalRunner(
	registry *EvalTypeRegistry, opts ...RunnerOption,
) *EvalRunner {
	r := &EvalRunner{
		registry: registry,
		timeout:  DefaultEvalTimeout,
	}
	for _, opt := range opts {
		opt(r)
	}
	return r
}

// RunTurnEvals runs turn-level evals (every_turn and sample_turns triggers).
// It filters by enabled state and trigger, then executes matching handlers.
func (r *EvalRunner) RunTurnEvals(
	ctx context.Context,
	defs []EvalDef,
	evalCtx *EvalContext,
) []EvalResult {
	trigCtx := &TriggerContext{
		SessionID:         evalCtx.SessionID,
		TurnIndex:         evalCtx.TurnIndex,
		IsSessionComplete: false,
	}
	return r.runEvals(ctx, defs, evalCtx, trigCtx, turnTriggers)
}

// RunSessionEvals runs session-level evals (on_session_complete and
// sample_sessions triggers). Call this when a session ends.
func (r *EvalRunner) RunSessionEvals(
	ctx context.Context,
	defs []EvalDef,
	evalCtx *EvalContext,
) []EvalResult {
	trigCtx := &TriggerContext{
		SessionID:         evalCtx.SessionID,
		TurnIndex:         evalCtx.TurnIndex,
		IsSessionComplete: true,
	}
	return r.runEvals(ctx, defs, evalCtx, trigCtx, sessionTriggers)
}

// turnTriggers is the set of triggers that fire for turn-level evals.
var turnTriggers = map[EvalTrigger]bool{
	TriggerEveryTurn:   true,
	TriggerSampleTurns: true,
}

// sessionTriggers is the set of triggers that fire for session-level evals.
var sessionTriggers = map[EvalTrigger]bool{
	TriggerOnSessionComplete: true,
	TriggerSampleSessions:    true,
}

// runEvals is the shared implementation for both turn and session evals.
func (r *EvalRunner) runEvals(
	ctx context.Context,
	defs []EvalDef,
	evalCtx *EvalContext,
	trigCtx *TriggerContext,
	allowedTriggers map[EvalTrigger]bool,
) []EvalResult {
	var results []EvalResult
	for i := range defs {
		if ctx.Err() != nil {
			break
		}
		result := r.runOne(ctx, &defs[i], evalCtx, trigCtx, allowedTriggers)
		if result != nil {
			results = append(results, *result)
		}
	}
	return results
}

// runOne executes a single eval with filtering, timeout, and panic recovery.
func (r *EvalRunner) runOne(
	ctx context.Context,
	def *EvalDef,
	evalCtx *EvalContext,
	trigCtx *TriggerContext,
	allowedTriggers map[EvalTrigger]bool,
) *EvalResult {
	// Skip disabled evals
	if !def.IsEnabled() {
		return nil
	}

	// Skip evals whose trigger doesn't match this execution mode
	if !allowedTriggers[def.Trigger] {
		return nil
	}

	// Check sampling
	if !ShouldRun(def.Trigger, def.GetSamplePercentage(), trigCtx) {
		return nil
	}

	// Look up handler
	handler, err := r.registry.Get(def.Type)
	if err != nil {
		return &EvalResult{
			EvalID: def.ID,
			Type:   def.Type,
			Error:  fmt.Sprintf("handler not found: %v", err),
		}
	}

	// Execute with timeout and panic recovery
	return r.executeHandler(ctx, handler, def, evalCtx)
}

// executeHandler runs a handler with timeout and panic recovery.
func (r *EvalRunner) executeHandler(
	ctx context.Context,
	handler EvalTypeHandler,
	def *EvalDef,
	evalCtx *EvalContext,
) *EvalResult {
	evalCtx2, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()

	start := time.Now()

	// Panic recovery
	var result *EvalResult
	var evalErr error
	func() {
		defer func() {
			if rec := recover(); rec != nil {
				evalErr = fmt.Errorf("panic in eval %q: %v", def.ID, rec)
			}
		}()
		result, evalErr = handler.Eval(evalCtx2, evalCtx, def.Params)
	}()

	durationMs := time.Since(start).Milliseconds()

	if evalErr != nil {
		return &EvalResult{
			EvalID:     def.ID,
			Type:       def.Type,
			Error:      evalErr.Error(),
			DurationMs: durationMs,
		}
	}

	if result == nil {
		return &EvalResult{
			EvalID:     def.ID,
			Type:       def.Type,
			Error:      "handler returned nil result",
			DurationMs: durationMs,
		}
	}

	// Fill in metadata from the def
	result.EvalID = def.ID
	result.Type = def.Type
	result.DurationMs = durationMs
	return result
}
