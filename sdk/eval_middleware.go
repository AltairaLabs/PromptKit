package sdk

import (
	"context"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/AltairaLabs/PromptKit/runtime/evals"
	"github.com/AltairaLabs/PromptKit/runtime/events"
	"github.com/AltairaLabs/PromptKit/runtime/logger"
	"github.com/AltairaLabs/PromptKit/runtime/types"
)

// filterInvalidEvalDefs removes eval defs whose type is unregistered or
// whose params are unusable by their handler. Each removed def is logged
// as a warning. Returns the filtered slice (never nil).
//
// This runs at middleware creation time, so the EvalRunner never sees
// bad defs at dispatch time. Mirrors the validator warn-and-skip loop in
// sdk.go:convertPackValidatorsToHooks.
func filterInvalidEvalDefs(defs []evals.EvalDef, registry *evals.EvalTypeRegistry) []evals.EvalDef {
	if len(defs) == 0 {
		return defs
	}
	errs := evals.ValidateEvalTypes(defs, registry)
	if len(errs) == 0 {
		return defs
	}

	// ValidateEvalTypes formats each error as: eval "<id>": <reason>
	badIDs := make(map[string]string, len(errs))
	for _, e := range errs {
		id, reason := parseEvalValidationError(e)
		if id != "" {
			badIDs[id] = reason
			logger.Warn("Skipping unusable pack eval", "id", id, "reason", reason)
		}
	}

	filtered := make([]evals.EvalDef, 0, len(defs))
	for i := range defs {
		if _, bad := badIDs[defs[i].ID]; bad {
			continue
		}
		filtered = append(filtered, defs[i])
	}
	return filtered
}

// resolveRunnerAndFilter returns the EvalRunner to use (creating one if
// necessary) and the eval defs filtered for type/param validity.
//
// Behavior:
//   - No explicit runner: build one from the configured (or default)
//     registry and filter defs against it.
//   - Explicit runner + explicit registry: trust the caller's runner, but
//     filter against the explicit registry.
//   - Explicit runner only: trust the caller and skip filtering (we
//     cannot inspect the runner's internal registry).
func resolveRunnerAndFilter(
	cfg *config, defs []evals.EvalDef,
) (*evals.EvalRunner, []evals.EvalDef) {
	if runner := cfg.evalRunner; runner != nil {
		if reg := cfg.evalRegistry; reg != nil {
			return runner, filterInvalidEvalDefs(defs, reg)
		}
		return runner, defs
	}
	registry := cfg.evalRegistry
	if registry == nil {
		registry = evals.NewEvalTypeRegistry()
	}
	filtered := filterInvalidEvalDefs(defs, registry)
	if len(filtered) == 0 {
		return nil, filtered
	}
	return evals.NewEvalRunner(registry), filtered
}

// parseEvalValidationError extracts id and reason from an error produced
// by evals.ValidateEvalTypes. The format is: eval "<id>": <reason>.
// If the format doesn't match, the whole string is returned as reason
// and id is empty.
func parseEvalValidationError(s string) (id, reason string) {
	const prefix = `eval "`
	if !strings.HasPrefix(s, prefix) {
		return "", s
	}
	rest := s[len(prefix):]
	end := strings.Index(rest, `":`)
	if end < 0 {
		return "", s
	}
	id = rest[:end]
	reason = strings.TrimSpace(rest[end+2:])
	return id, reason
}

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

	runner, filteredDefs := resolveRunnerAndFilter(conv.config, defs)
	if len(filteredDefs) == 0 {
		logger.Debug("evals: middleware skipped, all defs filtered", "reason", "no valid defs")
		return nil
	}
	defs = filteredDefs

	// Clone the runner so per-conversation wiring (emitter, hooks) never
	// mutates a user-supplied or otherwise shared instance. Mirrors the
	// Arena EvalOrchestrator.Clone() pattern and makes repeated Open()
	// calls safe when the same base runner is passed via WithEvalRunner.
	runner = runner.Clone()

	// Attach any hooks configured via WithEvalHook to the cloned runner.
	// Hooks that were already attached to the base runner are copied
	// into the clone by Clone() itself.
	for _, h := range conv.config.evalHooks {
		runner.AddHook(h)
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
