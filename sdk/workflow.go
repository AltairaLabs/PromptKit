package sdk

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/AltairaLabs/PromptKit/runtime/events"
	"github.com/AltairaLabs/PromptKit/runtime/logger"
	"github.com/AltairaLabs/PromptKit/runtime/statestore"
	"github.com/AltairaLabs/PromptKit/runtime/types"
	"github.com/AltairaLabs/PromptKit/runtime/workflow"
	"github.com/AltairaLabs/PromptKit/sdk/internal/pack"
)

// Persisted-state keys for split workflow context storage.
//
// `workflowCurrentKey` lives in the conversation's user metadata hash and
// holds the small struct (current_state, visit_counts, artifacts, etc.).
// History and ArtifactHistory live in their own append-only lists keyed
// by `workflowHistoryListName` and `workflowArtifactListName`. Splitting
// keeps per-transition write cost O(1 new entry) instead of O(full
// History) every time. See plans/2026-04-28-workflow-context-incremental-history.md.
const (
	workflowCurrentKey       = "workflow.current"
	workflowHistoryListName  = "workflow.history"
	workflowArtifactListName = "workflow.artifact_history"
	// workflowContextVar is the template variable name that carries the
	// previous state's conversation summary into the new state's prompt.
	workflowContextVar = "workflow_context"
)

// WorkflowConversation manages a stateful workflow that transitions between
// different prompts in a pack based on events.
//
// Each state in the workflow maps to a prompt_task in the pack. When a
// transition occurs, the current conversation is closed and a new one is
// opened for the target state's prompt.
//
// Basic usage:
//
//	wc, err := sdk.OpenWorkflow("./support.pack.json")
//	if err != nil {
//	    log.Fatal(err)
//	}
//	defer wc.Close()
//
//	resp, _ := wc.Send(ctx, "I need help with billing")
//	fmt.Println(resp.Text())
//
//	newState, _ := wc.Transition("Escalate")
//	fmt.Println("Moved to:", newState)
type WorkflowConversation struct {
	mu                  sync.RWMutex
	machine             *workflow.StateMachine
	workflowSpec        *workflow.Spec
	packPath            string
	sdkPack             *pack.Pack
	activeConv          *Conversation
	opts                []Option
	emitter             *events.Emitter
	stateStore          statestore.Store
	workflowID          string
	contextCarryForward bool
	workflowCap         *WorkflowCapability
	transExec           *workflow.TransitionExecutor
	artifactExec        *workflow.ArtifactExecutor
	// historyAppended is the length of machine.Context().History at the
	// last persistWorkflowContext call. Used to compute the per-transition
	// delta — only the new tail is RPUSH'd to the workflow.history list.
	historyAppended int
	// artifactHistoryAppended is the analogous tracker for ArtifactHistory.
	artifactHistoryAppended int
	closed                  bool
}

// defaultMaxSummaryMessages is the max messages to include in a carry-forward summary.
const defaultMaxSummaryMessages = 10

// OpenWorkflow loads a pack file and creates a WorkflowConversation.
//
// The pack must contain a workflow section. The initial conversation is opened
// for the workflow's entry state prompt_task.
//
//	wc, err := sdk.OpenWorkflow("./support.pack.json",
//	    sdk.WithModel("gpt-4o"),
//	)
func OpenWorkflow(packPath string, opts ...Option) (*WorkflowConversation, error) {
	return openWorkflowAtState(packPath, "", opts...)
}

// openWorkflowAtState builds a WorkflowConversation whose state machine starts
// at startState. When startState is empty it starts at the workflow's entry
// (the OpenWorkflow behavior). RFC 0011 state-backed agents enter the workflow
// at the agent's declared state via this helper.
func openWorkflowAtState(packPath, startState string, opts ...Option) (*WorkflowConversation, error) {
	absPath, err := resolvePackPath(packPath)
	if err != nil {
		return nil, fmt.Errorf("failed to resolve pack path: %w", err)
	}

	// Determine skip-schema from options
	cfg := &config{}
	for _, opt := range opts {
		if optErr := opt(cfg); optErr != nil {
			return nil, fmt.Errorf("failed to apply option: %w", optErr)
		}
	}

	// Set custom logger before any logging occurs — only once
	if cfg.logger != nil {
		setLoggerOnce(cfg.logger)
	}

	p, err := pack.Load(absPath, pack.LoadOptions{
		SkipSchemaValidation: cfg.skipSchemaValidation,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to load pack: %w", err)
	}

	if p.Workflow == nil {
		return nil, ErrNoWorkflow
	}

	// Convert SDK workflow spec to runtime workflow spec
	spec := convertWorkflowSpec(p.Workflow)
	machine := workflow.NewStateMachine(spec)
	if startState != "" {
		machine = workflow.NewStateMachineFromContext(spec, workflow.NewContext(startState, time.Now()))
	}

	// Open initial conversation for the current state's prompt_task
	promptName := machine.CurrentPromptTask()
	conv, err := Open(packPath, promptName, opts...)
	if err != nil {
		return nil, fmt.Errorf("failed to open initial conversation for state %q (prompt %q): %w",
			machine.CurrentState(), promptName, err)
	}

	// Create and init WorkflowCapability
	wfCap := NewWorkflowCapability()
	if err := wfCap.Init(newCapabilityContext(p, promptName, cfg)); err != nil {
		_ = conv.Close()
		return nil, fmt.Errorf("workflow capability init failed: %w", err)
	}

	wc := &WorkflowConversation{
		machine:             machine,
		workflowSpec:        spec,
		packPath:            packPath,
		sdkPack:             p,
		activeConv:          conv,
		opts:                opts,
		emitter:             conv.newEmitter(cfg.eventBus),
		stateStore:          cfg.stateStore,
		workflowID:          cfg.conversationID,
		contextCarryForward: cfg.contextCarryForward,
		workflowCap:         wfCap,
	}

	// Register workflow tools for the initial state
	wc.registerWorkflowTools()

	return wc, nil
}

// ResumeWorkflow restores a WorkflowConversation from a previously persisted state.
//
// The workflow context is loaded from the state store's metadata["workflow"] key.
// A state store must be configured via WithStateStore.
//
//	wc, err := sdk.ResumeWorkflow("workflow-123", "./support.pack.json",
//	    sdk.WithStateStore(store),
//	)
func ResumeWorkflow(workflowID, packPath string, opts ...Option) (*WorkflowConversation, error) {
	cfg := &config{}
	for _, opt := range opts {
		if optErr := opt(cfg); optErr != nil {
			return nil, fmt.Errorf("failed to apply option: %w", optErr)
		}
	}

	if cfg.stateStore == nil {
		return nil, ErrNoStateStore
	}

	absPath, err := resolvePackPath(packPath)
	if err != nil {
		return nil, fmt.Errorf("failed to resolve pack path: %w", err)
	}

	p, err := pack.Load(absPath, pack.LoadOptions{
		SkipSchemaValidation: cfg.skipSchemaValidation,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to load pack: %w", err)
	}

	if p.Workflow == nil {
		return nil, ErrNoWorkflow
	}

	// Load workflow context from state store
	ctx := context.Background()
	state, err := cfg.stateStore.Load(ctx, workflowID)
	if err != nil {
		return nil, fmt.Errorf("failed to load workflow state: %w", err)
	}
	if state == nil {
		return nil, ErrConversationNotFound
	}

	// Extract the small workflow.current view from metadata.
	wfCtx, err := extractWorkflowContext(state.Metadata)
	if err != nil {
		return nil, fmt.Errorf("failed to restore workflow context: %w", err)
	}

	// Hydrate the append-only History / ArtifactHistory collections from
	// their dedicated lists. The returned lengths seed the per-instance
	// delta trackers so subsequent persists only RPUSH new entries.
	historyLen, artifactHistoryLen, err := hydrateWorkflowContextLists(ctx, cfg.stateStore, workflowID, wfCtx)
	if err != nil {
		return nil, fmt.Errorf("failed to hydrate workflow context lists: %w", err)
	}

	spec := convertWorkflowSpec(p.Workflow)
	machine := workflow.NewStateMachineFromContext(spec, wfCtx)

	// Open conversation for current state's prompt_task
	promptName := machine.CurrentPromptTask()
	optsWithID := make([]Option, len(opts), len(opts)+1)
	copy(optsWithID, opts)
	optsWithID = append(optsWithID, WithConversationID(workflowID))
	conv, err := Open(packPath, promptName, optsWithID...)
	if err != nil {
		return nil, fmt.Errorf("failed to open conversation for state %q (prompt %q): %w",
			machine.CurrentState(), promptName, err)
	}

	// Create and init WorkflowCapability
	wfCap := NewWorkflowCapability()
	if err := wfCap.Init(newCapabilityContext(p, promptName, cfg)); err != nil {
		_ = conv.Close()
		return nil, fmt.Errorf("workflow capability init failed: %w", err)
	}

	wc := &WorkflowConversation{
		machine:                 machine,
		workflowSpec:            spec,
		packPath:                packPath,
		sdkPack:                 p,
		activeConv:              conv,
		opts:                    opts,
		stateStore:              cfg.stateStore,
		workflowID:              workflowID,
		contextCarryForward:     cfg.contextCarryForward,
		workflowCap:             wfCap,
		historyAppended:         historyLen,
		artifactHistoryAppended: artifactHistoryLen,
	}
	wc.emitter = conv.newEmitter(cfg.eventBus)

	// Register workflow tools for the current state
	wc.registerWorkflowTools()

	return wc, nil
}

// Send sends a message to the active state's conversation and returns the response.
// If the LLM calls the workflow__transition tool, the transition is processed
// after the Send completes.
//
//	resp, err := wc.Send(ctx, "Hello!")
//	fmt.Println(resp.Text())
func (wc *WorkflowConversation) Send(ctx context.Context, message any, opts ...SendOption) (*Response, error) {
	wc.mu.Lock()
	if wc.closed {
		wc.mu.Unlock()
		return nil, ErrWorkflowClosed
	}
	if wc.transExec != nil {
		wc.transExec.ClearPending()
	}
	// Heal any state-machine / active-conv drift left over from a prior
	// Send that errored mid-pipeline after an eager (control: agent)
	// commit. Without this, the next Send runs in the stale conv for
	// one full turn before end-of-Send reconciliation kicks in.
	// Carry-forward summary intentionally empty here: the prior Send's
	// conv history is what would have been summarized, and we don't
	// retroactively rebuild it on the recovery path.
	if err := wc.reconcileActiveConv(""); err != nil {
		wc.mu.Unlock()
		return nil, err
	}
	conv := wc.activeConv
	wc.mu.Unlock()

	resp, err := conv.Send(ctx, message, opts...)
	if err != nil {
		return nil, err
	}

	// Commit any pending transition from a workflow__transition tool call.
	// The TransitionExecutor deferred the ProcessEvent until now.
	wc.mu.Lock()
	defer wc.mu.Unlock()
	if wc.closed {
		return nil, ErrWorkflowClosed
	}
	if err := wc.commitDeferredTransition(); err != nil {
		return resp, err
	}
	return resp, nil
}

// commitDeferredTransition finalizes any workflow transitions that fired
// during the just-completed Send. Two paths converge here:
//
//   - control: user (default) — the workflow__transition tool stored a
//     pending transition during the pipeline; CommitPending now runs
//     ProcessEvent and fires OnCommit (or OnCommitError on failure).
//   - control: agent — the transition already committed eagerly inside
//     Execute and the hooks have already run; CommitPending is a no-op.
//
// Either way, if the machine has moved past the active conversation's
// prompt task, reconcileActiveConv closes the old conv and opens a new
// one for the destination state's prompt. Failure observability events
// are emitted by the OnCommitError hook wired in registerWorkflowTools,
// not from this function. Caller must hold wc.mu.
//
// Note: capturing pending.ContextSummary before CommitPending is safe —
// the caller holds wc.mu and the runtime executor's only mutator under
// its own lock is Execute, which can only be invoked by the pipeline
// tool loop, and the pipeline is no longer running at this point.
func (wc *WorkflowConversation) commitDeferredTransition() error {
	if wc.transExec == nil {
		return nil
	}
	var contextSummary string
	if pending := wc.transExec.Pending(); pending != nil {
		contextSummary = pending.ContextSummary
		if _, commitErr := wc.transExec.CommitPending(); commitErr != nil {
			return commitErr
		}
	}
	return wc.reconcileActiveConv(contextSummary)
}

// reconcileActiveConv brings the active conversation in line with the
// workflow machine's current state. If they already match (no transition
// happened during the just-completed Send) this is a no-op. Otherwise the
// active conv is closed and a new one is opened for the destination
// state's prompt_task, with the optional context summary injected as the
// {{workflow_context}} template variable.
//
// Multi-transition Sends (the agent fired one or more control: agent
// eager commits and possibly a final deferred commit) all funnel through
// here once at end-of-Send; intermediate states never get their own
// conversation, which is the whole point of control: agent.
func (wc *WorkflowConversation) reconcileActiveConv(contextSummary string) error {
	if wc.activeConv == nil {
		return nil
	}
	// A closed activeConv is its own failure mode — the caller (typically
	// Send) will observe ErrConversationClosed from the next operation.
	// Don't conflate that with state drift; reconcile only when the conv
	// is open but its prompt has fallen behind the machine.
	if wc.activeConv.closed {
		return nil
	}
	targetPrompt := wc.machine.CurrentPromptTask()
	if targetPrompt == "" {
		return nil
	}
	if wc.activeConv.promptName == targetPrompt {
		return nil
	}
	return wc.openConvForCurrentState(contextSummary)
}

// openConvForCurrentState closes the active conversation (if any) and opens
// a new one for the workflow machine's current prompt_task. The new conv
// receives carry-forward context (when non-empty) and artifact values as
// template variables, and workflow tools are re-registered on its registry.
//
// Shared between applyTransition (manual Transition path) and
// reconcileActiveConv (post-Send / post-CommitPending). The two paths
// differ only in what they do AFTER the conv is opened — applyTransition
// also fires transition events and persists context, since it bypasses
// the OnCommit hook. reconcileActiveConv relies on the hook having
// already done that work.
//
// Caller must hold wc.mu.
func (wc *WorkflowConversation) openConvForCurrentState(contextSummary string) error {
	targetPrompt := wc.machine.CurrentPromptTask()
	if wc.activeConv != nil {
		_ = wc.activeConv.Close()
	}

	opts := wc.opts
	if contextSummary != "" {
		opts = append(append([]Option{}, opts...), WithVariables(map[string]string{
			workflowContextVar: contextSummary,
		}))
	}
	if arts := wc.machine.Artifacts(); len(arts) > 0 {
		artVars := make(map[string]string, len(arts))
		for k, v := range arts {
			artVars["artifacts."+k] = v
		}
		opts = append(append([]Option{}, opts...), WithVariables(artVars))
	}

	conv, err := Open(wc.packPath, targetPrompt, opts...)
	if err != nil {
		return fmt.Errorf("failed to open conversation for state %q (prompt %q): %w",
			wc.machine.CurrentState(), targetPrompt, err)
	}
	wc.activeConv = conv
	wc.registerWorkflowTools()
	return nil
}

// Transition processes an event and moves to the next state.
//
// The current conversation is closed and a new one is opened for the target
// state's prompt_task. Returns the new state name.
//
//	newState, err := wc.Transition("Escalate")
//	if errors.Is(err, workflow.ErrInvalidEvent) {
//	    fmt.Println("Available events:", wc.AvailableEvents())
//	}
func (wc *WorkflowConversation) Transition(event string) (string, error) {
	wc.mu.Lock()
	defer wc.mu.Unlock()

	if wc.closed {
		return "", ErrWorkflowClosed
	}

	if wc.machine.IsTerminal() {
		return "", ErrWorkflowTerminal
	}

	// Build context summary from active conversation if carry-forward is enabled
	var contextSummary string
	if wc.contextCarryForward && wc.activeConv != nil {
		contextSummary = buildContextSummary(wc.machine.CurrentState(), wc.activeConv)
	}

	return wc.transitionInternal(event, contextSummary)
}

// registerWorkflowTools registers the workflow tool executors and descriptors
// for the current state on the active conversation's tool registry.
//
// Also installs the OnCommit hook on the TransitionExecutor so post-commit
// work (re-register transition tool for new state events, emit observability
// events, persist workflow context) runs from a single place regardless of
// whether the commit was eager (control: agent target, fires inside Execute)
// or deferred (control: user target, fires from CommitPending after Send).
//
// The hook updates the active conversation's tool registry in place; the
// active conv itself stays unchanged for the rest of the Send so the
// pipeline tool loop can keep looking up the same registry. The post-Send
// reconcileActiveConv step opens a new conversation if the workflow has
// moved on.
func (wc *WorkflowConversation) registerWorkflowTools() {
	registry := wc.activeConv.ToolRegistry()
	state := wc.workflowSpec.States[wc.machine.CurrentState()]

	// Create and register transition executor (deferred commit pattern)
	wc.transExec = workflow.NewTransitionExecutor(wc.machine, wc.workflowSpec)
	registry.RegisterExecutor(wc.transExec)
	wc.transExec.RegisterForState(registry, state)
	wc.transExec.SetOnCommit(wc.onTransitionCommitted)
	// OnCommitError covers both eager (control: agent) and deferred
	// (control: user) ProcessEvent failures so max_visits_exceeded /
	// budget_exhausted observability events fire regardless of path.
	wc.transExec.SetOnCommitError(wc.emitWorkflowError)

	// Create and register artifact executor if spec declares artifacts
	wc.artifactExec = workflow.NewArtifactExecutor(wc.machine)
	registry.RegisterExecutor(wc.artifactExec)
	workflow.RegisterArtifactTool(registry, wc.workflowSpec)
}

// onTransitionCommitted runs the post-commit work shared between eager and
// deferred commits. It updates the active conversation's transition tool
// for the new state's events so subsequent tool-loop iterations in the same
// pipeline turn can use them, fires observability events, and persists the
// updated workflow context.
//
// Closing the active conversation and opening a new one for the destination
// state's prompt is deferred to reconcileActiveConv, which runs once at the
// end of Send / Transition so multi-transition pipeline turns don't churn
// through intermediate convs.
func (wc *WorkflowConversation) onTransitionCommitted(result *workflow.TransitionResult) {
	if result == nil {
		return
	}
	if wc.activeConv != nil && wc.transExec != nil {
		if newState := wc.workflowSpec.States[result.To]; newState != nil {
			wc.transExec.RegisterForState(wc.activeConv.ToolRegistry(), newState)
		}
	}
	wc.emitTransitionEvents(result, result.To, wc.machine.CurrentPromptTask())
	if wc.stateStore != nil && wc.workflowID != "" {
		wc.persistWorkflowContext()
	}
}

// applyTransition handles post-commit transition logic for the manual
// Transition() path: open a fresh conversation for the new state, persist
// the workflow context, and emit transition events.
//
// Only the manual path (Transition / transitionInternal) goes through here
// because it bypasses the TransitionExecutor (calls ProcessEvent directly),
// so the OnCommit hook never fires. The LLM-initiated path
// (commitDeferredTransition) flows through onTransitionCommitted +
// reconcileActiveConv instead.
func (wc *WorkflowConversation) applyTransition(
	result *workflow.TransitionResult, contextSummary string,
) (string, error) {
	toState := result.To
	if err := wc.openConvForCurrentState(contextSummary); err != nil {
		return "", err
	}
	if wc.stateStore != nil && wc.workflowID != "" {
		wc.persistWorkflowContext()
	}
	wc.emitTransitionEvents(result, toState, wc.machine.CurrentPromptTask())
	return toState, nil
}

// CurrentState returns the current workflow state name.
func (wc *WorkflowConversation) CurrentState() string {
	wc.mu.RLock()
	defer wc.mu.RUnlock()
	return wc.machine.CurrentState()
}

// CurrentPromptTask returns the prompt_task for the current state.
func (wc *WorkflowConversation) CurrentPromptTask() string {
	wc.mu.RLock()
	defer wc.mu.RUnlock()
	return wc.machine.CurrentPromptTask()
}

// IsComplete returns true if the workflow is in a terminal state (no outgoing transitions).
func (wc *WorkflowConversation) IsComplete() bool {
	wc.mu.RLock()
	defer wc.mu.RUnlock()
	return wc.machine.IsTerminal()
}

// AvailableEvents returns the events available in the current state, sorted alphabetically.
func (wc *WorkflowConversation) AvailableEvents() []string {
	wc.mu.RLock()
	defer wc.mu.RUnlock()
	return wc.machine.AvailableEvents()
}

// Context returns a snapshot of the workflow execution context including
// transition history and metadata.
func (wc *WorkflowConversation) Context() *workflow.Context {
	wc.mu.RLock()
	defer wc.mu.RUnlock()
	return wc.machine.Context()
}

// OrchestrationMode returns the orchestration mode of the current state.
// External orchestration means transitions are driven by outside callers
// (e.g., HTTP handlers, message queues) rather than from within the conversation loop.
func (wc *WorkflowConversation) OrchestrationMode() workflow.Orchestration {
	wc.mu.RLock()
	defer wc.mu.RUnlock()

	if wc.workflowSpec == nil {
		return workflow.OrchestrationInternal
	}
	state := wc.workflowSpec.States[wc.machine.CurrentState()]
	if state == nil || state.Orchestration == "" {
		return workflow.OrchestrationInternal
	}
	return state.Orchestration
}

// ActiveConversation returns the current state's Conversation.
// Use this to access conversation-specific methods like SetVar, OnTool, etc.
func (wc *WorkflowConversation) ActiveConversation() *Conversation {
	wc.mu.RLock()
	defer wc.mu.RUnlock()
	return wc.activeConv
}

// Close closes the active conversation and marks the workflow as closed.
func (wc *WorkflowConversation) Close() error {
	wc.mu.Lock()
	defer wc.mu.Unlock()

	if wc.closed {
		return nil
	}
	wc.closed = true

	if wc.activeConv != nil {
		return wc.activeConv.Close()
	}
	return nil
}

// roleSystem is the role string for system messages (extracted as a constant to satisfy goconst).
const roleSystem = "system"

// extractMessageText returns the text content of a message, checking Content
// first and falling back to the first text Part.
func extractMessageText(msg *types.Message) string {
	if msg.Content != "" {
		return msg.Content
	}
	for _, part := range msg.Parts {
		if part.Text != nil {
			return *part.Text
		}
	}
	return ""
}

// persistWorkflowContext saves the workflow context to the state store
// using the split storage layout: the small "current" view (everything
// except the unbounded history collections) goes to a single field of the
// metadata hash, while History and ArtifactHistory deltas are RPUSH'd to
// their own append-only lists. This keeps per-transition write cost
// O(1 new entry) regardless of conversation length.
//
// It respects the persistence hint on the current state: transient states
// skip writes.
//
// Requires the state store to implement both MetadataAccessor and
// ListAccessor. Stores that don't satisfy both interfaces log an error
// and skip the write — the workflow continues in memory but won't be
// recoverable. All built-in stores (MemoryStore, RedisStore, ArenaStateStore)
// satisfy both.
func (wc *WorkflowConversation) persistWorkflowContext() {
	currentState := wc.machine.CurrentState()
	if wc.workflowSpec != nil {
		if st, ok := wc.workflowSpec.States[currentState]; ok {
			if st.Persistence == workflow.PersistenceTransient {
				return
			}
		}
	}

	ctx := context.Background()
	wfCtx := wc.machine.Context()

	accessor, hasMeta := wc.stateStore.(statestore.MetadataAccessor)
	listAcc, hasList := wc.stateStore.(statestore.ListAccessor)
	if !hasMeta || !hasList {
		logger.Error("workflow context not persisted: state store missing required interfaces",
			"workflow_id", wc.workflowID,
			"has_metadata_accessor", hasMeta,
			"has_list_accessor", hasList)
		return
	}

	// 1. Write the small "current" view — everything except the
	// append-only history collections, which live in their own lists.
	current := *wfCtx
	current.History = nil
	current.ArtifactHistory = nil
	if err := accessor.MergeMetadata(ctx, wc.workflowID, map[string]any{
		workflowCurrentKey: current,
	}); err != nil {
		logger.Warn("failed to persist workflow.current",
			"workflow_id", wc.workflowID, "error", err)
	}

	// 2. Append History delta — only entries added since the last persist.
	if err := appendWorkflowListDelta(ctx, listAcc, wc.workflowID, workflowHistoryListName,
		wfCtx.History, &wc.historyAppended,
	); err != nil {
		logger.Warn("failed to append workflow.history delta",
			"workflow_id", wc.workflowID, "error", err)
	}

	// 3. Append ArtifactHistory delta.
	if err := appendWorkflowListDelta(ctx, listAcc, wc.workflowID, workflowArtifactListName,
		wfCtx.ArtifactHistory, &wc.artifactHistoryAppended,
	); err != nil {
		logger.Warn("failed to append workflow.artifact_history delta",
			"workflow_id", wc.workflowID, "error", err)
	}
}

// appendWorkflowListDelta marshals only the new tail of items (those past
// *appended) and appends them to the named list. On success it advances
// *appended to len(items) so the next call computes the correct delta.
// Returns nil with no write when there are no new items.
func appendWorkflowListDelta[T any](
	ctx context.Context,
	listAcc statestore.ListAccessor,
	id, listName string,
	items []T,
	appended *int,
) error {
	if *appended >= len(items) {
		return nil
	}
	delta := items[*appended:]
	encoded := make([][]byte, len(delta))
	for i, item := range delta {
		b, err := json.Marshal(item)
		if err != nil {
			return fmt.Errorf("marshal item %d in %q: %w", *appended+i, listName, err)
		}
		encoded[i] = b
	}
	if err := listAcc.AppendList(ctx, id, listName, encoded); err != nil {
		return err
	}
	*appended = len(items)
	return nil
}

// extractWorkflowContext extracts and deserializes the small workflow
// "current" view from state metadata. The returned Context has nil
// History and ArtifactHistory — those are stored in append-only lists
// and must be hydrated separately via hydrateWorkflowContextLists.
func extractWorkflowContext(metadata map[string]any) (*workflow.Context, error) {
	raw, ok := metadata[workflowCurrentKey]
	if !ok {
		return nil, fmt.Errorf("no workflow context in metadata")
	}

	switch v := raw.(type) {
	case *workflow.Context:
		return v, nil
	case workflow.Context:
		return &v, nil
	case map[string]any:
		// Re-serialize and deserialize through JSON for type safety —
		// stores that round-trip through JSON (RedisStore) hand back a
		// generic map.
		data, err := json.Marshal(v)
		if err != nil {
			return nil, fmt.Errorf("failed to marshal workflow context: %w", err)
		}
		var wfCtx workflow.Context
		if err := json.Unmarshal(data, &wfCtx); err != nil {
			return nil, fmt.Errorf("failed to unmarshal workflow context: %w", err)
		}
		return &wfCtx, nil
	default:
		return nil, fmt.Errorf("unexpected workflow context type: %T", raw)
	}
}

// hydrateWorkflowContextLists loads History and ArtifactHistory from the
// state store's append-only lists and assigns them onto wfCtx. Returns
// the loaded slice lengths so the caller can initialize the per-instance
// delta trackers (historyAppended, artifactHistoryAppended) — without
// these the next persist would re-RPUSH the entire history.
func hydrateWorkflowContextLists(
	ctx context.Context,
	store statestore.Store,
	id string,
	wfCtx *workflow.Context,
) (historyLen, artifactHistoryLen int, err error) {
	listAcc, ok := store.(statestore.ListAccessor)
	if !ok {
		return 0, 0, fmt.Errorf("workflow restore: state store does not implement ListAccessor")
	}

	history, err := loadWorkflowList[workflow.StateTransition](ctx, listAcc, id, workflowHistoryListName)
	if err != nil {
		return 0, 0, fmt.Errorf("load workflow history: %w", err)
	}
	wfCtx.History = history

	artHist, err := loadWorkflowList[workflow.ArtifactSnapshot](ctx, listAcc, id, workflowArtifactListName)
	if err != nil {
		return 0, 0, fmt.Errorf("load workflow artifact history: %w", err)
	}
	wfCtx.ArtifactHistory = artHist

	return len(history), len(artHist), nil
}

func loadWorkflowList[T any](
	ctx context.Context,
	listAcc statestore.ListAccessor,
	id, listName string,
) ([]T, error) {
	items, err := listAcc.LoadList(ctx, id, listName)
	if err != nil && !errors.Is(err, statestore.ErrNotFound) {
		return nil, err
	}
	if len(items) == 0 {
		return nil, nil
	}
	out := make([]T, len(items))
	for i, raw := range items {
		if err := json.Unmarshal(raw, &out[i]); err != nil {
			return nil, fmt.Errorf("decode item %d in %q: %w", i, listName, err)
		}
	}
	return out, nil
}

// convertWorkflowSpec converts the SDK's internal pack.WorkflowSpec to a
// runtime workflow.Spec for use with the state machine.
func convertWorkflowSpec(sdkSpec *pack.WorkflowSpec) *workflow.Spec {
	states := make(map[string]*workflow.State, len(sdkSpec.States))
	for name, s := range sdkSpec.States {
		states[name] = &workflow.State{
			PromptTask:    s.PromptTask,
			Description:   s.Description,
			OnEvent:       s.OnEvent,
			Persistence:   workflow.Persistence(s.Persistence),
			Orchestration: workflow.Orchestration(s.Orchestration),
			Composition:   s.Composition,
		}
	}
	return &workflow.Spec{
		Version: sdkSpec.Version,
		Entry:   sdkSpec.Entry,
		States:  states,
		Engine:  sdkSpec.Engine,
	}
}

// emitTransitionEvents and maxVisitsForState live in sdk/workflow_events.go.
