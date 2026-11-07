package sdk

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/AltairaLabs/PromptKit/runtime/v2/evals"
	"github.com/AltairaLabs/PromptKit/runtime/v2/events"
	"github.com/AltairaLabs/PromptKit/runtime/v2/inference"
	"github.com/AltairaLabs/PromptKit/runtime/v2/logger"
	"github.com/AltairaLabs/PromptKit/runtime/v2/types"
)

// filterInvalidEvalDefs removes eval defs whose type is unregistered or
// whose params are unusable by their handler. Each removed def is logged
// as a warning. Returns the filtered slice (never nil).
//
// This runs at middleware creation time, so the EvalRunner never sees
// bad defs at dispatch time. Unlike pack validators — which are guardrails, and
// whose construction failures are fatal in convertPackValidatorsToHooks — an
// eval is a measurement, not a control, so dropping one degrades reporting
// rather than removing protection.
func filterInvalidEvalDefs(defs []evals.EvalDef, registry *evals.EvalTypeRegistry) []evals.EvalDef {
	if len(defs) == 0 {
		return defs
	}
	errs := evals.ValidateEvalTypes(defs, registry)
	if len(errs) == 0 {
		return defs
	}

	// ValidateEvalTypes formats each error as: eval "<id>": <reason>, with the
	// id %q-quoted. Match against the ids we actually hold rather than
	// re-parsing the quoting: an id containing a quote is escaped in the
	// message, and a parse that missed it left the rejected def running (#1950).
	badIDs := make(map[string]string, len(errs))
	for _, e := range errs {
		id, reason := matchEvalValidationError(e, defs)
		if id == "" {
			logger.Warn("Skipping unusable pack eval: unrecognized validation message", "message", e)
			continue
		}
		badIDs[id] = reason
		logger.Warn("Skipping unusable pack eval", "id", id, "reason", reason)
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

// Eval scopes passed to evals.ValidateEvals, which prefixes each message with
// them: pack-level evals are "pack", a prompt's are "prompt:<name>".
const (
	packEvalScope         = "pack"
	promptEvalScopePrefix = "prompt:"
)

// structuralEvalIssues runs evals.ValidateEvalsDetailed over one scope's defs
// and groups the findings by the index of the def each one names.
//
// With skipWhen, a `when` finding is left out, for a caller that also runs
// evals.ValidateEvalTypes, which reports it as well.
func structuralEvalIssues(defs []evals.EvalDef, scope string, skipWhen bool) map[int][]evals.EvalFinding {
	findings := evals.ValidateEvalsDetailed(defs, scope)
	if len(findings) == 0 {
		return nil
	}
	out := make(map[int][]evals.EvalFinding)
	for _, f := range findings {
		if skipWhen && f.Code == evals.EvalFindingWhenUnusable {
			continue
		}
		out[f.Index] = append(out[f.Index], f)
	}
	return out
}

// dropStructurallyInvalidEvals removes the defs evals.ValidateEvals rejects
// (a missing or duplicate id, a missing type or trigger, an unknown trigger, a
// sample_percentage out of range, a metric Prometheus cannot register, an
// unusable when), logging
// a warning for each. Like filterInvalidEvalDefs it degrades reporting rather
// than failing the open: an eval is a measurement, not a control.
func dropStructurallyInvalidEvals(defs []evals.EvalDef, scope string) []evals.EvalDef {
	issues := structuralEvalIssues(defs, scope, false)
	if len(issues) == 0 {
		return defs
	}
	kept := make([]evals.EvalDef, 0, len(defs))
	for i := range defs {
		found, bad := issues[i]
		if !bad {
			kept = append(kept, defs[i])
			continue
		}
		for _, f := range found {
			logger.Warn("Skipping unusable pack eval", "id", defs[i].ID, "reason", f.Message)
		}
	}
	return kept
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
// matchEvalValidationError resolves a ValidateEvalTypes message to the def it
// names by prefix-matching the exact %q form of each known id. Falls back to
// parseEvalValidationError for ids that need no escaping.
func matchEvalValidationError(s string, defs []evals.EvalDef) (id, reason string) {
	for i := range defs {
		prefix := fmt.Sprintf("eval %q:", defs[i].ID)
		if strings.HasPrefix(s, prefix) {
			return defs[i].ID, strings.TrimSpace(s[len(prefix):])
		}
	}
	return parseEvalValidationError(s)
}

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
	cacheMu         sync.Mutex
	cachedMessages  []types.Message
	cachedTurnIndex int32
	cachedSessionID string

	// pendingWorkflowSteps counts committed workflow transitions whose
	// on_workflow_step evals have not been dispatched yet. A transition can
	// commit mid-turn, on the pipeline's goroutine, before the turn that made it
	// has finished, so it is recorded here and dispatched once the turn is done.
	pendingWorkflowSteps atomic.Int32
	// hasWorkflowStepEvals is whether any def uses the on_workflow_step
	// trigger; without one a transition dispatches nothing.
	hasWorkflowStepEvals bool
	// hasConversationEvals is whether any def uses the on_conversation_complete
	// trigger; without one Close runs no conversation evals.
	hasConversationEvals bool
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
		packEvals = evals.Values(conv.pack.Evals)
	}
	if conv.prompt != nil {
		promptEvals = evals.Values(conv.prompt.Evals)
	}

	logger.Debug("evals: resolving defs",
		"pack_evals", len(packEvals), "prompt_evals", len(promptEvals),
		"has_pack", conv.pack != nil, "has_prompt", conv.prompt != nil)

	// Each scope is checked on its own: a prompt eval sharing a pack eval's id
	// is an override, not a duplicate.
	packEvals = dropStructurallyInvalidEvals(packEvals, packEvalScope)
	promptEvals = dropStructurallyInvalidEvals(promptEvals, promptEvalScopePrefix+conv.promptName)

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

		hasWorkflowStepEvals: hasTrigger(defs, evals.TriggerOnWorkflowStep),
		hasConversationEvals: hasTrigger(defs, evals.TriggerOnConversationComplete),
	}
}

// hasTrigger reports whether any def uses trigger.
func hasTrigger(defs []evals.EvalDef, trigger evals.EvalTrigger) bool {
	for i := range defs {
		if evals.EvalTrigger(defs[i].Trigger) == trigger {
			return true
		}
	}
	return false
}

// reportedTurn is the turn number an eval result carries.
//
// The pipeline derives it from the persisted transcript, and that is the number
// to report: it is what the guardrails inside that same pipeline reported, and
// it is what lines up with the message log.
//
// dispatchCount is only a fallback for a conversation with no pipeline turn
// state. It counts dispatches in this process, so it is wrong for a resumed
// conversation — it restarts at 1 while the transcript already holds earlier
// turns.
func (em *evalMiddleware) reportedTurn(dispatchCount int32) int {
	if em.conv != nil && em.conv.turnState.TurnIndex() > 0 {
		return em.conv.turnState.TurnIndex()
	}
	return int(dispatchCount)
}

// dispatchTurnEvals dispatches turn-level evals asynchronously.
// Nil-safe: no-op if middleware is nil.
// The goroutine is tracked via a WaitGroup and respects the middleware's
// cancellation context so that close() can drain in-flight work.
func (em *evalMiddleware) dispatchTurnEvals(ctx context.Context) {
	if em == nil {
		return
	}
	em.turnIndex.Add(1)
	em.dispatchAsync(ctx, "turn", em.runner.RunTurnEvals)
}

// recordWorkflowStep notes that a workflow state transition committed during
// this conversation. Its on_workflow_step evals run on the next
// dispatchWorkflowStepEvals. Nil-safe and safe to call from any goroutine.
func (em *evalMiddleware) recordWorkflowStep() {
	if em == nil {
		return
	}
	em.pendingWorkflowSteps.Add(1)
}

// dispatchWorkflowStepEvals dispatches the on_workflow_step evals once for each
// transition recorded since the last call, against the conversation's current
// turn. Call it once the turn that made the transitions has finished. It uses
// the same asynchronous, bounded dispatch as turn evals.
// Nil-safe: no-op if middleware is nil.
func (em *evalMiddleware) dispatchWorkflowStepEvals(ctx context.Context) {
	if em == nil {
		return
	}
	steps := em.pendingWorkflowSteps.Swap(0)
	if !em.hasWorkflowStepEvals {
		return
	}
	for range steps {
		em.dispatchAsync(ctx, "workflow step", em.runner.RunWorkflowStepEvals)
	}
}

// dispatchAsync builds the eval context for the current turn and runs run on a
// goroutine tracked by the WaitGroup, bounded by the semaphore.
//
// The semaphore is acquired without blocking: if every slot is taken the
// dispatch is skipped, to prevent unbounded goroutine growth.
func (em *evalMiddleware) dispatchAsync(
	ctx context.Context,
	kind string,
	run func(context.Context, []evals.EvalDef, *evals.EvalContext) []evals.EvalResult,
) {
	select {
	case em.sem <- struct{}{}:
		// Acquired — proceed
	default:
		logger.Warn("evals: semaphore full, skipping eval dispatch",
			"kind", kind, "turn", em.turnIndex.Load(), "capacity", cap(em.sem))
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
		results := run(em.evalContextWithBinding(em.evalContextWithRegistry(em.ctx)), em.defs, evalCtx)
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
	results := em.runner.RunSessionEvals(em.evalContextWithBinding(em.evalContextWithRegistry(ctx)), em.defs, evalCtx)
	em.recordMetrics(results)
}

// dispatchConversationEvals dispatches on_conversation_complete evals
// synchronously, against the whole transcript. Close() is the end of a
// Conversation, so every Close runs them, right after the session evals: a
// host that closes and later resumes a conversation runs them again, over the
// history the resumed conversation reloaded. An empty transcript runs none.
// Nil-safe: no-op if middleware is nil.
func (em *evalMiddleware) dispatchConversationEvals(ctx context.Context) {
	if em == nil || !em.hasConversationEvals {
		return
	}

	evalCtx := em.buildEvalContext(ctx)
	if len(evalCtx.Messages) == 0 {
		// Nothing was said: a conversation closed on a failed open, or never
		// used, has no transcript to score.
		return
	}
	results := em.runner.RunConversationEvals(
		em.evalContextWithBinding(em.evalContextWithRegistry(ctx)), em.defs, evalCtx)
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
// evalContextWithRegistry attaches the conversation's inference registry to
// base so inference-backed eval handlers (audio_emotion, text_toxicity, …)
// can resolve their provider via inference.FromContext. Turn evals run on the
// middleware's background-derived lifecycle context and session evals on the
// Close() context; neither passes through the pipeline's execCtx where the
// registry is otherwise attached, so the wiring has to happen here too.
// No-op (returns base) when no inference provider was configured.
func (em *evalMiddleware) evalContextWithRegistry(base context.Context) context.Context {
	if em.conv == nil || em.conv.config == nil || em.conv.config.inferenceRegistry == nil {
		return base
	}
	return inference.WithRegistry(base, em.conv.config.inferenceRegistry)
}

// evalContextWithBinding attaches the host's provider binding, so a per-turn
// eval resolves the logical provider names its pack declared exactly as a
// guardrail does. Turn evals run off the middleware's own context rather than
// the pipeline's, so the wiring has to happen here as well.
func (em *evalMiddleware) evalContextWithBinding(base context.Context) context.Context {
	if em.conv == nil || em.conv.config == nil {
		return base
	}
	return evals.WithProviderBinding(base, newHostBinding(em.conv.config))
}

func (em *evalMiddleware) buildEvalContext(ctx context.Context) *evals.EvalContext {
	em.cacheMu.Lock()
	defer em.cacheMu.Unlock()

	currentTurn := em.turnIndex.Load()
	reported := em.reportedTurn(currentTurn)

	// Safely get session info — sessions may not be initialized in tests
	// or when middleware is used standalone.
	if em.conv.unarySession == nil && em.conv.duplexSession == nil {
		return &evals.EvalContext{
			TurnIndex: reported,
			PromptID:  em.conv.promptName,
		}
	}

	// Only reload messages if turn count changed since last cache
	if currentTurn != em.cachedTurnIndex || em.cachedMessages == nil {
		em.cachedMessages = em.conv.Messages(ctx)
		em.cachedSessionID = em.conv.ID()
		em.cachedTurnIndex = currentTurn
	}

	// Delegate rather than assemble a literal.
	//
	// This function used to build the context inline and set four of its
	// fields, leaving ToolCalls, Extras, Metadata and PriorResults nil while
	// holding the messages every one of them derives from. The failure mode is
	// the dangerous direction: handlers do not error on a nil slice, they
	// report ABSENCE. tools_called_session failed runs that had called every
	// tool, tools_not_called_session passed trivially, and cost_budget computed
	// zero spend.
	//
	// The same mistake was fixed once before in BuildGuardrailEvalContext
	// (#1704). Delegating is what stops a third occurrence: a field added to
	// EvalContext now reaches this path automatically instead of needing a
	// second edit nobody remembers to make.
	return evals.BuildEvalContext(
		em.cachedMessages,
		reported,
		em.cachedSessionID,
		em.conv.promptName,
		em.hostDefaultJudge(),
	)
}

// hostDefaultJudge carries a host-supplied default judge to checks whose pack
// names no provider of its own. A check that names one resolves it through the
// binding on the context instead, and never reads this.
func (em *evalMiddleware) hostDefaultJudge() map[string]any {
	if em.conv == nil || em.conv.config == nil || em.conv.config.judgeProvider == nil {
		return nil
	}
	return map[string]any{"judge_provider": em.conv.config.judgeProvider}
}
