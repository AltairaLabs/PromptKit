package evals

import (
	"context"
	"fmt"
	"time"

	"github.com/AltairaLabs/PromptKit/runtime/events"
	"github.com/AltairaLabs/PromptKit/runtime/logger"
)

// DefaultEvalTimeout is the per-eval execution timeout.
const DefaultEvalTimeout = 30 * time.Second

// EvalRunner executes evals in-process. It is the leaf execution unit
// used by all dispatch modes (in-proc, event-driven, worker).
type EvalRunner struct {
	registry *EvalTypeRegistry
	timeout  time.Duration
	emitter  *events.Emitter // optional — emits eval.completed/failed events
}

// RunnerOption configures an EvalRunner.
type RunnerOption func(*EvalRunner)

// WithTimeout sets the per-eval execution timeout.
func WithTimeout(d time.Duration) RunnerOption {
	return func(r *EvalRunner) { r.timeout = d }
}

// WithEmitter configures the runner to emit eval.completed/eval.failed events.
func WithEmitter(e *events.Emitter) RunnerOption {
	return func(r *EvalRunner) { r.emitter = e }
}

// Clone creates a copy of the runner with the same registry and timeout but no emitter.
func (r *EvalRunner) Clone() *EvalRunner {
	return &EvalRunner{
		registry: r.registry,
		timeout:  r.timeout,
	}
}

// SetEmitter sets (or clears) the event emitter. Must be called before running evals.
func (r *EvalRunner) SetEmitter(e *events.Emitter) {
	r.emitter = e
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

// RunConversationEvals runs conversation-level evals (on_conversation_complete trigger).
// Call this when a multi-turn conversation ends (e.g., Arena self-play completion).
func (r *EvalRunner) RunConversationEvals(
	ctx context.Context,
	defs []EvalDef,
	evalCtx *EvalContext,
) []EvalResult {
	trigCtx := &TriggerContext{
		SessionID:         evalCtx.SessionID,
		TurnIndex:         evalCtx.TurnIndex,
		IsSessionComplete: true,
	}
	return r.runEvals(ctx, defs, evalCtx, trigCtx, conversationTriggers)
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

// conversationTriggers is the set of triggers that fire for conversation-level evals.
var conversationTriggers = map[EvalTrigger]bool{
	TriggerOnConversationComplete: true,
}

// runEvals is the shared implementation for both turn and session evals.
// TODO(perf): Consider running independent evals concurrently with a worker pool
// to reduce total eval latency, especially for external (REST/LLM) eval types.
func (r *EvalRunner) runEvals(
	ctx context.Context,
	defs []EvalDef,
	evalCtx *EvalContext,
	trigCtx *TriggerContext,
	allowedTriggers map[EvalTrigger]bool,
) []EvalResult {
	logger.Debug("evals: running evals", "count", len(defs), "session_id", evalCtx.SessionID)
	// Preserve any PriorResults seeded by BuildEvalContext (e.g., from
	// message.Validations) so they're visible to evals in this batch.
	seeded := evalCtx.PriorResults
	var results []EvalResult
	for i := range defs {
		if ctx.Err() != nil {
			break
		}
		prior := make([]EvalResult, 0, len(seeded)+len(results))
		prior = append(prior, seeded...)
		prior = append(prior, results...)
		evalCtx.PriorResults = prior
		result := r.runOne(ctx, &defs[i], evalCtx, trigCtx, allowedTriggers)
		if result != nil {
			result.SessionID = evalCtx.SessionID
			result.TurnIndex = evalCtx.TurnIndex
			r.emitResult(result)
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
		logger.Debug("evals: skipping disabled eval", "eval_id", def.ID)
		return nil
	}

	// Skip evals whose trigger doesn't match this execution mode
	if !allowedTriggers[def.Trigger] {
		logger.Debug("evals: skipping eval, trigger mismatch", "eval_id", def.ID, "trigger", def.Trigger)
		return nil
	}

	// Check sampling
	if !ShouldRun(def.Trigger, def.GetSamplePercentage(), trigCtx) {
		logger.Debug("evals: skipping eval, sampling excluded", "eval_id", def.ID)
		return nil
	}

	// Check when-conditions (tool call preconditions)
	if def.When != nil {
		if shouldRun, reason := ShouldRunWhen(def.When, evalCtx.ToolCalls); !shouldRun {
			logger.Debug("evals: skipping eval, when-condition not met", "eval_id", def.ID, "reason", reason)
			return &EvalResult{
				EvalID:     def.ID,
				Type:       def.Type,
				Skipped:    true,
				SkipReason: reason,
			}
		}
	}

	// Look up handler
	handler, err := r.registry.Get(def.Type)
	if err != nil {
		logger.Warn("evals: handler not found", "eval_id", def.ID, "type", def.Type, "error", err)
		return &EvalResult{
			EvalID: def.ID,
			Type:   def.Type,
			Error:  fmt.Sprintf("handler not found: %v", err),
		}
	}

	logger.Debug("evals: executing eval", "eval_id", def.ID, "type", def.Type)

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
				logger.Warn("evals: handler panic recovered", "eval_id", def.ID, "panic", rec)
				evalErr = fmt.Errorf("panic in eval %q: %v", def.ID, rec)
			}
		}()
		result, evalErr = handler.Eval(evalCtx2, evalCtx, def.Params)
	}()

	durationMs := time.Since(start).Milliseconds()

	if evalErr != nil {
		logger.Warn("evals: eval error", "eval_id", def.ID, "type", def.Type, "error", evalErr, "duration_ms", durationMs)
		return &EvalResult{
			EvalID:     def.ID,
			Type:       def.Type,
			Error:      evalErr.Error(),
			DurationMs: durationMs,
		}
	}

	if result == nil {
		logger.Warn("evals: handler returned nil result", "eval_id", def.ID, "type", def.Type)
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

	var scoreVal any = "<nil>"
	if result.Score != nil {
		scoreVal = *result.Score
	}
	logger.Info("evals: eval completed",
		"eval_id", def.ID, "type", def.Type,
		"score", scoreVal, "duration_ms", durationMs,
	)
	return result
}

// emitResult publishes eval.completed or eval.failed via the emitter (if set).
func (r *EvalRunner) emitResult(result *EvalResult) {
	if r.emitter == nil {
		return
	}
	data := &events.EvalEventData{
		EvalID:      result.EvalID,
		EvalType:    result.Type,
		Score:       result.Score,
		Explanation: result.Explanation,
		DurationMs:  result.DurationMs,
		Error:       result.Error,
		Message:     result.Message,
		Skipped:     result.Skipped,
		SkipReason:  result.SkipReason,
	}
	// Determine passed status: skip skipped evals, use handler's Value (bool)
	// if available (accounts for min_score/max_score thresholds), otherwise
	// default to score >= 1.0.
	if !result.Skipped {
		if passed, ok := result.Value.(bool); ok {
			data.Passed = passed
		} else if result.Score == nil || *result.Score >= 1.0 {
			data.Passed = true
		}
	}
	for _, v := range result.Violations {
		data.Violations = append(data.Violations, v.Description)
	}
	if result.Error != "" {
		r.emitter.EvalFailed(data)
	} else {
		r.emitter.EvalCompleted(data)
	}
}
