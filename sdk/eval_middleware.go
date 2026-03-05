package sdk

import (
	"context"
	"sync"

	"github.com/AltairaLabs/PromptKit/runtime/evals"
	"github.com/AltairaLabs/PromptKit/runtime/events"
	"github.com/AltairaLabs/PromptKit/runtime/logger"
)

// evalMiddleware holds dispatch state for eval execution within a conversation.
type evalMiddleware struct {
	runner    *evals.EvalRunner
	defs      []evals.EvalDef
	emitter   *events.Emitter // nil-safe (bus may not be configured)
	conv      *Conversation
	turnIndex int

	// Goroutine lifecycle management for async turn evals.
	wg     sync.WaitGroup     // tracks in-flight turn eval goroutines
	ctx    context.Context    // canceled on close to stop in-flight evals
	cancel context.CancelFunc // cancels ctx
}

// newEvalMiddleware creates eval middleware for a conversation.
// Returns nil if evals are disabled, no runner is available, or no eval defs are resolved.
func newEvalMiddleware(conv *Conversation) *evalMiddleware {
	if conv.config == nil || conv.config.evalsDisabled {
		logger.Debug("evals: middleware skipped",
			"has_config", conv.config != nil,
			"disabled", conv.config != nil && conv.config.evalsDisabled)
		return nil
	}

	// Resolve eval defs from pack + prompt
	var packEvals, promptEvals []evals.EvalDef
	if conv.pack != nil {
		packEvals = conv.pack.Evals
	}
	if conv.prompt != nil {
		promptEvals = conv.prompt.Evals
	}

	logger.Debug("evals: resolving defs",
		"pack_evals", len(packEvals), "prompt_evals", len(promptEvals),
		"has_pack", conv.pack != nil, "has_prompt", conv.prompt != nil)

	defs := evals.ResolveEvals(packEvals, promptEvals)
	if len(defs) == 0 {
		logger.Debug("evals: middleware skipped, no eval defs resolved", "reason", "no defs resolved")
		return nil
	}

	// Get or create runner
	runner := conv.config.evalRunner
	if runner == nil {
		registry := conv.config.evalRegistry
		if registry == nil {
			registry = evals.NewEvalTypeRegistry()
		}
		runner = evals.NewEvalRunner(registry)
	}

	// Build emitter from event bus (nil-safe)
	var emitter *events.Emitter
	if conv.config.eventBus != nil {
		emitter = events.NewEmitter(conv.config.eventBus, "", "", "")
	}

	logger.Info("evals: middleware created", "defs", len(defs))

	ctx, cancel := context.WithCancel(context.Background())
	return &evalMiddleware{
		runner:  runner,
		defs:    defs,
		emitter: emitter,
		conv:    conv,
		ctx:     ctx,
		cancel:  cancel,
	}
}

// dispatchTurnEvals dispatches turn-level evals asynchronously.
// Nil-safe: no-op if middleware is nil.
// The goroutine is tracked via a WaitGroup and respects the middleware's
// cancellation context so that close() can drain in-flight work.
func (em *evalMiddleware) dispatchTurnEvals(ctx context.Context) {
	if em == nil {
		return
	}

	em.turnIndex++
	evalCtx := em.buildEvalContext(ctx)

	// Use the middleware's lifecycle context so that close() can cancel
	// in-flight evals. The middleware context is derived from
	// context.Background() so it outlives any single Send() call.
	em.wg.Add(1)
	go func() {
		defer em.wg.Done()
		results := em.runner.RunTurnEvals(em.ctx, em.defs, evalCtx)
		em.emitResults(results)
	}()
}

// dispatchSessionEvals dispatches session-complete evals synchronously.
// Nil-safe: no-op if middleware is nil.
// Runs synchronously during Close() to ensure completion.
func (em *evalMiddleware) dispatchSessionEvals(ctx context.Context) {
	if em == nil {
		return
	}

	evalCtx := em.buildEvalContext(ctx)
	results := em.runner.RunSessionEvals(ctx, em.defs, evalCtx)
	em.emitResults(results)
}

// wait blocks until all in-flight turn eval goroutines have completed.
// Nil-safe: no-op if middleware is nil.
func (em *evalMiddleware) wait() {
	if em == nil {
		return
	}
	em.wg.Wait()
}

// close cancels the middleware's context and waits for all in-flight turn
// eval goroutines to finish. It should be called during conversation Close()
// to prevent goroutine leaks.
// Nil-safe: no-op if middleware is nil.
func (em *evalMiddleware) close() {
	if em == nil {
		return
	}
	em.cancel()
	em.wg.Wait()
}

// emitResults emits eval results as events on the event bus.
func (em *evalMiddleware) emitResults(results []evals.EvalResult) {
	if em.emitter == nil {
		return
	}
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
			em.emitter.EvalCompleted(&data)
		} else {
			em.emitter.EvalFailed(&data)
		}
	}
}

// buildEvalContext creates an EvalContext from the conversation state.
func (em *evalMiddleware) buildEvalContext(ctx context.Context) *evals.EvalContext {
	evalCtx := &evals.EvalContext{
		TurnIndex: em.turnIndex,
		PromptID:  em.conv.promptName,
	}

	// Safely get session info — sessions may not be initialized in tests
	// or when middleware is used standalone.
	if em.conv.unarySession != nil || em.conv.duplexSession != nil {
		evalCtx.Messages = em.conv.Messages(ctx)
		evalCtx.SessionID = em.conv.ID()

		for i := len(evalCtx.Messages) - 1; i >= 0; i-- {
			if evalCtx.Messages[i].Role == roleAssistant {
				evalCtx.CurrentOutput = evalCtx.Messages[i].GetContent()
				break
			}
		}
	}

	return evalCtx
}
