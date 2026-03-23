package sdk

import (
	"context"
	"sync"
	"sync/atomic"

	"github.com/AltairaLabs/PromptKit/runtime/evals"
	"github.com/AltairaLabs/PromptKit/runtime/events"
	"github.com/AltairaLabs/PromptKit/runtime/logger"
	"github.com/AltairaLabs/PromptKit/runtime/types"
)

// DefaultMaxConcurrentEvals is the default maximum number of concurrent eval goroutines.
const DefaultMaxConcurrentEvals = 10

// evalMiddleware holds dispatch state for eval execution within a conversation.
type evalMiddleware struct {
	runner       *evals.EvalRunner
	defs         []evals.EvalDef
	emitter      *events.Emitter           // nil-safe (bus may not be configured)
	metricWriter *evals.MetricResultWriter // nil-safe (recorder may not be configured)
	conv         *Conversation
	turnIndex    atomic.Int32 // atomic for thread safety across concurrent Send() calls

	// Goroutine lifecycle management for async turn evals.
	wg     sync.WaitGroup     // tracks in-flight turn eval goroutines
	ctx    context.Context    // canceled on close to stop in-flight evals
	cancel context.CancelFunc // cancels ctx

	// Bounded concurrency: sem limits how many eval goroutines run simultaneously.
	sem chan struct{}

	// cacheMu protects the cached fields below from concurrent access
	// across multiple dispatchTurnEvals goroutines.
	cacheMu          sync.Mutex
	cachedMessages   []types.Message
	cachedTurnIndex  int32
	cachedSessionID  string
	cachedLastOutput string
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
	defs = evals.FilterByGroups(defs, conv.config.evalGroups)
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

	// Build emitter from event bus (nil-safe), populating session ID if available.
	emitter := conv.newEmitter(conv.config.eventBus)

	maxConcurrent := DefaultMaxConcurrentEvals
	if conv.config.maxConcurrentEvals > 0 {
		maxConcurrent = conv.config.maxConcurrentEvals
	}

	// Build metric writer if a recorder is configured.
	// Unified MetricContext (from WithMetrics) takes precedence over legacy MetricRecorder.
	var metricWriter *evals.MetricResultWriter
	if conv.config.metricContext != nil {
		metricWriter = evals.NewMetricResultWriter(conv.config.metricContext, defs)
	} else if conv.config.metricRecorder != nil {
		metricWriter = evals.NewMetricResultWriter(conv.config.metricRecorder, defs)
	}

	logger.Info("evals: middleware created", "defs", len(defs), "max_concurrent", maxConcurrent)

	ctx, cancel := context.WithCancel(context.Background())
	// Wire the emitter into the runner so eval events are emitted as each eval completes.
	runner.SetEmitter(emitter)

	return &evalMiddleware{
		runner:       runner,
		defs:         defs,
		emitter:      emitter,
		metricWriter: metricWriter,
		conv:         conv,
		ctx:          ctx,
		cancel:       cancel,
		sem:          make(chan struct{}, maxConcurrent),
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

	turn := em.turnIndex.Add(1)

	// Try to acquire the semaphore without blocking. If all slots are taken,
	// skip this eval dispatch to prevent unbounded goroutine growth.
	select {
	case em.sem <- struct{}{}:
		// Acquired — proceed
	default:
		logger.Warn("evals: semaphore full, skipping turn eval dispatch",
			"turn", turn, "capacity", cap(em.sem))
		return
	}

	evalCtx := em.buildEvalContext(ctx)

	// Use the middleware's lifecycle context so that close() can cancel
	// in-flight evals. The middleware context is derived from
	// context.Background() so it outlives any single Send() call.
	em.wg.Add(1)
	go func() {
		defer em.wg.Done()
		defer func() { <-em.sem }()
		results := em.runner.RunTurnEvals(em.ctx, em.defs, evalCtx)
		em.recordMetrics(results)
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
	em.recordMetrics(results)
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

// recordMetrics writes eval result metrics. Event emission is handled by
// the EvalRunner (via SetEmitter).
func (em *evalMiddleware) recordMetrics(results []evals.EvalResult) {
	if em.metricWriter != nil {
		if err := em.metricWriter.WriteResults(context.Background(), results); err != nil {
			logger.Warn("evals: metric recording failed", "error", err)
		}
	}
}

// buildEvalContext creates an EvalContext from the conversation state.
// It caches messages and only reloads when the turn count changes.
func (em *evalMiddleware) buildEvalContext(ctx context.Context) *evals.EvalContext {
	em.cacheMu.Lock()
	defer em.cacheMu.Unlock()

	currentTurn := em.turnIndex.Load()
	evalCtx := &evals.EvalContext{
		TurnIndex: int(currentTurn),
		PromptID:  em.conv.promptName,
	}

	// Safely get session info — sessions may not be initialized in tests
	// or when middleware is used standalone.
	if em.conv.unarySession != nil || em.conv.duplexSession != nil {
		// Only reload messages if turn count changed since last cache
		if currentTurn != em.cachedTurnIndex || em.cachedMessages == nil {
			em.cachedMessages = em.conv.Messages(ctx)
			em.cachedSessionID = em.conv.ID()
			em.cachedTurnIndex = currentTurn
			em.cachedLastOutput = ""
			for i := len(em.cachedMessages) - 1; i >= 0; i-- {
				if em.cachedMessages[i].Role == roleAssistant {
					em.cachedLastOutput = em.cachedMessages[i].GetContent()
					break
				}
			}
		}
		evalCtx.Messages = em.cachedMessages
		evalCtx.SessionID = em.cachedSessionID
		evalCtx.CurrentOutput = em.cachedLastOutput
	}

	return evalCtx
}
