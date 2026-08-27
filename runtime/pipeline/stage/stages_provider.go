package stage

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"golang.org/x/sync/errgroup"

	"github.com/AltairaLabs/PromptKit/runtime/events"
	"github.com/AltairaLabs/PromptKit/runtime/hooks"
	"github.com/AltairaLabs/PromptKit/runtime/logger"
	"github.com/AltairaLabs/PromptKit/runtime/pipeline"
	"github.com/AltairaLabs/PromptKit/runtime/prompt"
	"github.com/AltairaLabs/PromptKit/runtime/providers"
	"github.com/AltairaLabs/PromptKit/runtime/selection"
	"github.com/AltairaLabs/PromptKit/runtime/statestore"
	"github.com/AltairaLabs/PromptKit/runtime/tools"
	"github.com/AltairaLabs/PromptKit/runtime/types"
)

const (
	defaultMaxRounds            = 50
	defaultMaxParallelToolCalls = 10
	defaultMaxIdenticalCalls    = 3
	toolChoiceAuto              = "auto"
	toolChoiceNone              = "none"

	// HookDeniedError fields for provider hook denials (BeforeCall/AfterCall).
	providerHookName       = "provider_hook"
	providerHookTypeBefore = "provider_before"
	providerHookTypeAfter  = "provider_after"
)

// ProviderStage implementation notes:
// - ✅ Multi-round tool execution with automatic tool result handling
// - ✅ Synchronous tool execution via toolRegistry.ExecuteAsync()
// - ✅ Human-in-the-loop approval gate via ProviderConfig.ApprovalChecker: a
//      held call is surfaced pending (ErrToolsPending / PendingTools metadata)
//      and the pipeline suspends until the caller resolves it

// ProviderStage executes LLM calls and handles tool execution.
// This is the request/response mode implementation.
type ProviderStage struct {
	BaseStage
	provider     providers.Provider
	toolRegistry *tools.Registry
	toolPolicy   *pipeline.ToolPolicy
	config       *ProviderConfig
	emitter      *events.Emitter // Optional event emitter for provider call events
	hookRegistry *hooks.Registry // Optional hook registry for policy enforcement
	// turnState is the per-Turn shared state. SystemPrompt, AllowedTools,
	// and ProviderRequestMetadata are sourced from it. Nil-safe (the
	// stage emits an empty system prompt and no allowed tools).
	turnState *TurnState
	// stateResolver advances workflow state between tool-loop rounds, so a
	// transition's destination state generates the next round instead of the
	// turn ending with the origin state still in control. Nil for
	// non-workflow runs. See state_handoff.go.
	stateResolver WorkflowStateResolver
}

// SetWorkflowStateResolver installs the resolver used to apply workflow state
// changes mid-turn. Pass nil to disable. Must be called before the stage runs.
func (s *ProviderStage) SetWorkflowStateResolver(r WorkflowStateResolver) {
	s.stateResolver = r
}

// ProviderConfig contains configuration for the provider stage.
type ProviderConfig struct {
	MaxTokens      int
	Temperature    float32
	Seed           *int
	ResponseFormat *providers.ResponseFormat // Optional response format (JSON mode)

	// StructuredOutputMode selects when ResponseFormat is applied to a tool
	// loop. Empty means final_turn — the schema is withheld from tool-calling
	// rounds and the final answer is re-asked under it, because a schema on
	// every round suppresses tool calling. See structured_output_mode.go and
	// issue #1853. Ignored when ResponseFormat is nil or the turn uses no
	// tools, where there is no loop to protect.
	StructuredOutputMode StructuredOutputMode
	Labels         map[string]string         // Optional labels propagated to events, metrics, and traces
	Source         string                    // Origin of the call: "agent" (default), "judge", "selfplay"

	// MessageLog enables per-round write-through persistence during tool loops.
	// When set, messages are appended to the log after each tool-loop round
	// completes, so they survive process crashes. Best-effort: failures are
	// logged but don't abort the loop.
	MessageLog statestore.MessageLog

	// MessageLogConvID is the conversation ID for message log operations.
	MessageLogConvID string

	// Compactor folds stale tool results between rounds when token usage
	// exceeds the configured threshold. Nil = disabled.
	Compactor CompactionStrategy

	// Streaming enables continuous multi-turn mode: instead of draining the
	// input channel and firing the provider once at close (the unary default,
	// correct for Arena's "pipeline per turn"), the stage fires the tool loop
	// on each EndOfTurn control element, emits that turn's reply, stays open,
	// and threads conversation history across turns within the session. Used by
	// the composed-VAD voice path (one continuous mic session). Default false
	// preserves today's fire-once-at-close behavior for every existing pipeline.
	Streaming bool

	// ToolSelector, when set, narrows the pack-declared allowedTools
	// each turn before tools are sent to the provider. The selector
	// receives the latest user message as its query and the current
	// allowedTools as candidates; only the IDs it returns are surfaced.
	// System tools (skill__, a2a__, workflow__, mcp__, memory__) are
	// always preserved regardless of selection. Selection is a no-op
	// when nil, when allowedTools is empty, or when the user message
	// has no text content. On any selector failure the full eligible
	// set is used (the provider stage never crashes a turn because
	// selection broke).
	ToolSelector selection.Selector

	// ApprovalChecker, when set, is consulted before each tool executes. If it
	// returns a non-nil PendingToolInfo the call is HELD pending (surfaced via
	// ErrToolsPending / PendingTools metadata) instead of executing — the
	// human-in-the-loop approval gate. Nil (default) executes every tool
	// normally. This is what makes HITL approval work on the standard
	// ProviderStage, not only the ASM duplex session.
	ApprovalChecker tools.ApprovalChecker
}

// streamingRoundParams holds parameters for a streaming round execution.
type streamingRoundParams struct {
	messages      []types.Message
	systemPrompt  string
	providerTools interface{}
	toolChoice    string
	round         int
	metadata      map[string]interface{}
	// providerCallID identifies this round's provider call, stamped on its
	// provider events and on every tool event the round produces.
	providerCallID string
}

// roundRef names the model turn that requested a set of tool calls: the 1-based
// tool-loop round, and the ID of the provider call that produced them.
//
// A tool call is made BY a turn, and consumers need that linkage to render a
// transcript. Reconstructing it from event ordering fails silently — a round's
// tool calls are dispatched before that round's provider call reports
// completion — so it is passed down explicitly rather than inferred.
type roundRef struct {
	round          int
	providerCallID string
}

// newProviderCallID mints an identifier for one provider call. Shared by the
// call's own provider events and by the tool events its tool calls produce, so
// the relationship survives retries and any change to how rounds are counted.
func newProviderCallID() string {
	return uuid.NewString()
}

// NewProviderStage creates a new provider stage for request/response mode.
func NewProviderStage(
	provider providers.Provider,
	toolRegistry *tools.Registry,
	toolPolicy *pipeline.ToolPolicy,
	config *ProviderConfig,
) *ProviderStage {
	return NewProviderStageWithEmitter(provider, toolRegistry, toolPolicy, config, nil)
}

// NewProviderStageWithEmitter creates a new provider stage with event emission support.
// The emitter is used to emit provider.call.started, provider.call.completed, and
// provider.call.failed events for observability and session recording.
func NewProviderStageWithEmitter(
	provider providers.Provider,
	toolRegistry *tools.Registry,
	toolPolicy *pipeline.ToolPolicy,
	config *ProviderConfig,
	emitter *events.Emitter,
) *ProviderStage {
	return NewProviderStageWithHooks(provider, toolRegistry, toolPolicy, config, emitter, nil)
}

// NewProviderStageWithHooks creates a provider stage with event emission and hook support.
// The hookRegistry enables synchronous interception of provider calls, streaming chunks,
// and tool execution. Pass nil for no hooks (zero overhead).
func NewProviderStageWithHooks(
	provider providers.Provider,
	toolRegistry *tools.Registry,
	toolPolicy *pipeline.ToolPolicy,
	config *ProviderConfig,
	emitter *events.Emitter,
	hookRegistry *hooks.Registry,
) *ProviderStage {
	return NewProviderStageWithTurnState(provider, toolRegistry, toolPolicy, config, emitter, hookRegistry, nil)
}

// NewProviderStageWithTurnState creates a provider stage that sources
// system_prompt, allowed_tools, and provider-bound metadata from the shared
// *TurnState. Pass nil for ad-hoc / test usage where TurnState is not wired.
func NewProviderStageWithTurnState(
	provider providers.Provider,
	toolRegistry *tools.Registry,
	toolPolicy *pipeline.ToolPolicy,
	config *ProviderConfig,
	emitter *events.Emitter,
	hookRegistry *hooks.Registry,
	turnState *TurnState,
) *ProviderStage {
	if config == nil {
		config = &ProviderConfig{}
	}
	// Hooks that emit their own events (guardrails) cannot build an emitter for
	// themselves — they are compiled from the pack before the conversation, and
	// therefore its IDs, exist. This constructor is the one place holding both,
	// so joining them here covers every consumer at once.
	hookRegistry.SetEmitter(emitter)
	return &ProviderStage{
		BaseStage:    NewBaseStage("provider", StageTypeGenerate),
		provider:     provider,
		toolRegistry: toolRegistry,
		toolPolicy:   toolPolicy,
		config:       config,
		emitter:      emitter,
		hookRegistry: hookRegistry,
		turnState:    turnState,
	}
}

// toolLabels returns the Labels from the ToolDescriptor for the given tool name,
// or nil if the tool is not found or has no labels.
func (s *ProviderStage) toolLabels(name string) map[string]string {
	if s.toolRegistry == nil {
		return nil
	}
	desc := s.toolRegistry.Get(name)
	if desc == nil {
		return nil
	}
	return desc.Labels
}

// providerInput holds accumulated input data for provider execution.
type providerInput struct {
	messages     []types.Message
	systemPrompt string
	allowedTools []string
	metadata     map[string]interface{}
}

// Process executes the LLM provider call and handles tool execution.
func (s *ProviderStage) Process(
	ctx context.Context,
	input <-chan StreamElement,
	output chan<- StreamElement,
) error {
	defer close(output)

	if s.provider == nil {
		return errors.New("provider stage: no provider configured")
	}

	if s.config != nil && s.config.Streaming {
		return s.processStreaming(ctx, input, output)
	}

	accumulated := s.accumulateInput(input)

	logger.Debug("ProviderStage accumulated input",
		"messages", len(accumulated.messages),
		"allowed_tools", accumulated.allowedTools,
		"mock_scenario_id", accumulated.metadata["mock_scenario_id"],
		"mock_turn_number", accumulated.metadata["mock_turn_number"])

	return s.executeAndEmit(ctx, accumulated, output)
}

// accumulateInput collects messages from the input channel and seeds
// per-Turn provider request metadata from TurnState.
//
// Input ends on either a closed channel (the unary "pipeline per turn" shape)
// or an EndOfStream control element. Honoring EndOfStream is what lets a duplex
// session shut down gracefully: duplexSession.Drain signals end-of-input with
// that element and then waits for the pipeline to finish *before* closing the
// channel, so waiting on channel close alone deadlocks the drain until its
// timeout expires. EndOfStream is the same session-over signal AudioTurnStage,
// ResponseVAD and processStreaming already honor. See #1638.
func (s *ProviderStage) accumulateInput(input <-chan StreamElement) *providerInput {
	acc := &providerInput{
		metadata: make(map[string]interface{}),
	}

	for elem := range input {
		if elem.Message != nil {
			acc.messages = append(acc.messages, *elem.Message)
		}
		if elem.EndOfStream {
			break
		}
	}

	if s.turnState != nil {
		acc.systemPrompt = s.turnState.SystemPrompt
		acc.allowedTools = s.turnState.AllowedTools
		if len(s.turnState.AllowedTools) > 0 {
			logger.Debug("ProviderStage allowed_tools from TurnState",
				"tools", s.turnState.AllowedTools, "count", len(s.turnState.AllowedTools))
		}
		for k, v := range s.turnState.ProviderRequestMetadata {
			acc.metadata[k] = v
		}
	}

	return acc
}

// streamingConfig holds the per-session invariants for continuous multi-turn
// mode, read once from TurnState. The metadata map is copied so per-turn
// mutation does not leak back onto TurnState.
type streamingConfig struct {
	systemPrompt string
	allowedTools []string
	baseMeta     map[string]interface{}
}

// streamingTurnState snapshots the per-session invariants for a streaming run.
func (s *ProviderStage) streamingTurnState() streamingConfig {
	cfg := streamingConfig{baseMeta: map[string]interface{}{}}
	if s.turnState == nil {
		return cfg
	}
	cfg.systemPrompt = s.turnState.SystemPrompt
	cfg.allowedTools = s.turnState.AllowedTools
	cfg.baseMeta = make(map[string]interface{}, len(s.turnState.ProviderRequestMetadata))
	for k, v := range s.turnState.ProviderRequestMetadata {
		cfg.baseMeta[k] = v
	}
	return cfg
}

// processStreaming runs the continuous multi-turn mode. It accumulates Message
// elements until an EndOfTurn control element arrives, fires the existing tool
// loop on the accumulated history, emits that turn's new messages, re-emits an
// EndOfTurn boundary, and stays open for the next turn. History threads across
// turns within the session. Non-Message, non-control elements (e.g. an Interrupt
// arriving between turns) are forwarded downstream unchanged so later stages and
// follow-up work (the barge-in path) can act on them.
func (s *ProviderStage) processStreaming(
	ctx context.Context,
	input <-chan StreamElement,
	output chan<- StreamElement,
) error {
	// A concurrent reader cancels in-flight generation the instant a barge-in
	// Interrupt arrives, so a blocking provider call cannot swallow it.
	canceller := newTurnCanceller(ctx)
	defer canceller.stop()
	work := make(chan StreamElement, interruptWorkBuffer)
	go drainCancelingOnInterrupt(input, work, canceller)

	var history []types.Message
	var pending []types.Message

	for elem := range work {
		switch {
		case elem.Interrupt:
			// Barge-in: in-flight generation was already canceled out of band by
			// the reader. Drop the partial turn's input, roll a fresh context,
			// and forward the Interrupt downstream.
			pending = nil
			canceller.refresh()
			if err := sendStreamElement(ctx, elem, output); err != nil {
				return err
			}
		case elem.EndOfTurn:
			next, err := s.fireIfPending(ctx, canceller.context(), &history, pending, output)
			if err != nil {
				return err
			}
			pending = next
		case elem.EndOfStream:
			// Session over: fire any partial turn, then stop. Closing output
			// downstream signals end-of-conversation to the next stages.
			_, err := s.fireIfPending(ctx, canceller.context(), &history, pending, output)
			return err
		case elem.Message != nil:
			pending = append(pending, *elem.Message)
		default:
			// Stray content passes through unchanged.
			if err := sendStreamElement(ctx, elem, output); err != nil {
				return err
			}
		}
	}

	// Input closed without an explicit EndOfStream: fire any trailing turn.
	_, err := s.fireIfPending(ctx, canceller.context(), &history, pending, output)
	return err
}

// fireIfPending fires a streaming turn when there is buffered input, returning
// the reset pending slice. A no-op (returning pending unchanged) when empty.
// forwardCtx carries the downstream emits; genCtx (cancelable per turn) governs
// the provider call so a barge-in can abort it.
func (s *ProviderStage) fireIfPending(
	forwardCtx, genCtx context.Context,
	history *[]types.Message,
	pending []types.Message,
	output chan<- StreamElement,
) ([]types.Message, error) {
	if len(pending) == 0 {
		return pending, nil
	}
	if err := s.fireStreamingTurn(forwardCtx, genCtx, history, pending, output); err != nil {
		return pending, err
	}
	return nil, nil
}

// fireStreamingTurn runs the tool loop for one turn against (history + pending),
// emits only this turn's new messages (the turn's user transcript plus the
// assistant reply and any tool messages — the provider drains its input, so it
// must re-emit the user messages itself for the save stage), advances history to
// the full conversation, and emits an EndOfTurn boundary.
//
// genCtx governs the provider call: a barge-in cancels it, which drops the turn
// (no reply) without tearing down the session. A non-cancellation error is
// surfaced as an error element but likewise keeps the session alive — a live
// conversation survives one failed turn. forwardCtx carries the downstream emits
// so a late interrupt that cancels genCtx after a successful generation does not
// abort the emit.
func (s *ProviderStage) fireStreamingTurn(
	forwardCtx, genCtx context.Context,
	history *[]types.Message,
	pending []types.Message,
	output chan<- StreamElement,
) error {
	// Read the per-session invariants here, not before the loop: upstream
	// prompt/template stages write TurnState lazily as elements flow, and this
	// turn's messages have already passed through them by fire time, so the read
	// is safely ordered after those writes (channel happens-before).
	cfg := s.streamingTurnState()

	// Emit this turn's user transcript(s) BEFORE generation, so the UI shows what
	// the user said immediately (and the save stage persists it) instead of the
	// transcript and the reply appearing together after the LLM responds.
	if emitErr := s.emitResponseMessages(forwardCtx, pending, output); emitErr != nil {
		return emitErr
	}

	priorLen := len(*history)
	messages := make([]types.Message, 0, priorLen+len(pending))
	messages = append(messages, *history...)
	messages = append(messages, pending...)

	metadata := make(map[string]interface{}, len(cfg.baseMeta))
	for k, v := range cfg.baseMeta {
		metadata[k] = v
	}
	acc := &providerInput{
		messages:     messages,
		systemPrompt: cfg.systemPrompt,
		allowedTools: cfg.allowedTools,
		metadata:     metadata,
	}

	var full []types.Message
	var err error
	if s.provider.SupportsStreaming() {
		full, err = s.executeStreamingMultiRound(genCtx, acc, output)
	} else {
		full, err = s.executeMultiRound(genCtx, acc)
	}
	if err != nil {
		if errors.Is(err, context.Canceled) {
			// Barge-in (or shutdown) canceled this turn's generation; drop it.
			// The session continues with a fresh context.
			logger.Debug("ProviderStage streaming turn canceled (barge-in/shutdown), dropping")
			return nil
		}
		logger.Error("ProviderStage streaming turn failed", "error", err)
		return sendStreamElement(forwardCtx, NewErrorElement(err), output)
	}

	// full = (history + pending) + new assistant/tool messages. The user
	// messages (pending) were already emitted above, so emit only the new reply.
	newStart := priorLen + len(pending)
	if newStart <= len(full) {
		if emitErr := s.emitResponseMessages(forwardCtx, full[newStart:], output); emitErr != nil {
			return emitErr
		}
	}
	*history = full

	return sendStreamElement(forwardCtx, NewEndOfTurnElement(), output)
}

// sendStreamElement forwards one element downstream, honoring cancellation.
//
//nolint:gocritic // hugeParam: StreamElement is the channel element type, passed by value throughout the pipeline
func sendStreamElement(ctx context.Context, elem StreamElement, output chan<- StreamElement) error {
	select {
	case output <- elem:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// executeAndEmit runs provider execution and emits results.
func (s *ProviderStage) executeAndEmit(
	ctx context.Context,
	acc *providerInput,
	output chan<- StreamElement,
) error {
	var responseMessages []types.Message
	var err error

	if s.provider.SupportsStreaming() {
		responseMessages, err = s.executeStreamingMultiRound(ctx, acc, output)
	} else {
		responseMessages, err = s.executeMultiRound(ctx, acc)
	}

	if err != nil {
		// If tools are pending, emit collected messages and propagate the
		// pending list on a typed marker element so the SDK can surface
		// them to the caller.
		if ep, ok := tools.IsErrToolsPending(err); ok {
			if emitErr := s.emitResponseMessages(ctx, responseMessages, output); emitErr != nil {
				return emitErr
			}
			pendingElem := StreamElement{}
			pendingElem.Meta.PendingTools = ep.Pending
			select {
			case output <- pendingElem:
			case <-ctx.Done():
				return ctx.Err()
			}
			return nil
		}
		output <- NewErrorElement(err)
		return err
	}

	return s.emitResponseMessages(ctx, responseMessages, output)
}

// emitResponseMessages sends response messages to output channel.
func (s *ProviderStage) emitResponseMessages(
	ctx context.Context,
	messages []types.Message,
	output chan<- StreamElement,
) error {
	for i := range messages {
		elem := NewMessageElement(&messages[i])

		logger.Debug("ProviderStage emitting response message",
			"role", messages[i].Role)

		select {
		case output <- elem:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	return nil
}

func (s *ProviderStage) executeMultiRound(
	ctx context.Context,
	acc *providerInput,
) ([]types.Message, error) {
	s.applyToolSelector(ctx, acc)
	loop, err := s.newToolLoop(acc)
	if err != nil {
		return nil, err
	}
	// Pre-seed: persist history messages into the log before the tool loop
	// starts. This guarantees the full transcript is recoverable even if a
	// mid-loop error prevents the normal end-of-turn save from running.
	// LogAppend's idempotent-dedup skips messages already in the store, so
	// calling this at loop-start is safe for multi-turn conversations where
	// the history was persisted by a previous turn.
	loop.preSeedLog(ctx)
	// Reconcile before the first round. A resumed execution (HITL approval,
	// deferred client tool) re-ran PromptAssemblyStage, which reset the prompt
	// to the state the pipeline was built for even though the workflow has
	// already moved on. Nothing is "pending" by then, so only a comparison
	// against the current state recovers it.
	if stop, hErr := s.applyStateHandoff(ctx, acc, loop); hErr != nil {
		return loop.messages, hErr
	} else if stop {
		return loop.messages, nil
	}
	for round := 1; round <= loop.maxRounds; round++ {
		rr := roundRef{round: round, providerCallID: newProviderCallID()}
		response, hasToolCalls, err := s.executeRound(
			ctx, loop.messages, acc.systemPrompt, loop.providerTools, loop.toolChoice, rr, acc.metadata)
		if err != nil {
			return loop.messages, err
		}
		// Stamp before afterRound appends the response: the active state right
		// now is the one that generated this round, and applyStateHandoff may
		// move it on before the next round runs.
		s.stampWorkflowState(&response)
		if done, msgs, err := loop.afterRound(ctx, acc.allowedTools, &response, hasToolCalls, rr); done {
			return msgs, err
		}
		if stop, hErr := s.applyStateHandoff(ctx, acc, loop); hErr != nil {
			return loop.messages, hErr
		} else if stop {
			return loop.messages, nil
		}
	}
	// Unreachable: afterRound returns done with an error at round == maxRounds,
	// so every exit goes through it. Kept as a compile-time fallthrough.
	return loop.messages, nil
}

// stampWorkflowState records the workflow state that produced this round's
// assistant message. A turn can span several states once handoffs are applied
// mid-loop, so attribution has to be per message rather than per turn. No-op
// for non-workflow runs.
func (s *ProviderStage) stampWorkflowState(response *types.Message) {
	if s.stateResolver == nil || response == nil {
		return
	}
	meta := s.stateResolver.CurrentStateMeta()
	if len(meta) == 0 {
		return
	}
	if response.Meta == nil {
		response.Meta = map[string]interface{}{}
	}
	response.Meta[workflowStateMetaKey] = meta
}

// recordToolCalls reports a round's executed tool-call count to the resolver
// when it implements ToolCallRecorder, feeding RFC 0009's max_tool_calls
// budget. No-op without a resolver, or when the resolver does not enforce a
// budget — the ordinary non-workflow case.
func (s *ProviderStage) recordToolCalls(n int) {
	if n <= 0 || s.stateResolver == nil {
		return
	}
	if rec, ok := s.stateResolver.(ToolCallRecorder); ok {
		rec.RecordToolCalls(n)
	}
}

// applyStateHandoff commits a workflow transition left pending by this round's
// tool calls, then swaps the turn's system prompt and tool set to the
// destination state's so the next round runs as that state. This is what makes
// the destination state speak without waiting for a user message.
//
// No-op without a resolver, or when the resolver reports no pending transition
// (including the states it declines to advance through: external, terminal,
// composition).
// Returns stop=true when the turn must end without a further provider round.
func (s *ProviderStage) applyStateHandoff(
	ctx context.Context, acc *providerInput, loop *toolLoop,
) (stop bool, err error) {
	if s.stateResolver == nil {
		return false, nil
	}
	handoff, err := s.stateResolver.ResolveCurrentState(ctx)
	if err != nil {
		return false, fmt.Errorf("provider stage: workflow handoff: %w", err)
	}
	if handoff.Stop {
		return true, nil
	}
	// Compare rather than trust a change flag. PromptAssemblyStage re-runs on
	// every pipeline execution and resets the prompt to the one the pipeline
	// was built for, so a resumed turn (HITL, deferred client tool) can find
	// itself back on the origin state's prompt with nothing "pending" to
	// signal it. Reconciling against what is actually loaded self-corrects.
	if !handoff.Valid || handoff.SystemPrompt == acc.systemPrompt {
		return false, nil
	}
	rebuilt, _, err := s.buildProviderTools(handoff.AllowedTools, loop.excluded)
	if err != nil {
		return false, fmt.Errorf("provider stage: workflow handoff: rebuild tools: %w", err)
	}
	acc.systemPrompt = handoff.SystemPrompt
	acc.allowedTools = handoff.AllowedTools
	loop.providerTools = rebuilt

	// Write through to TurnState so anything else reading it this execution
	// sees the same state. Note this does NOT survive the next execution --
	// PromptAssemblyStage overwrites it -- which is why the comparison above,
	// not this write, is what makes resume correct.
	if s.turnState != nil {
		s.turnState.SystemPrompt = handoff.SystemPrompt
		s.turnState.AllowedTools = handoff.AllowedTools
	}
	return false, nil
}

// applyToolSelector narrows acc.allowedTools through the configured
// external Selector. Mutates acc in place so subsequent rebuilds
// (afterRound on tool rejection) see the same narrowed set. No-op
// when no selector is configured, the candidate list is empty, the
// user query is empty, the selector errors, or the selection result
// is empty — fallback is always "use the full eligible list."
func (s *ProviderStage) applyToolSelector(ctx context.Context, acc *providerInput) {
	if s.config == nil || s.config.ToolSelector == nil || s.toolRegistry == nil {
		return
	}
	query := lastUserMessageText(acc.messages)
	candidates := s.toolCandidates(acc.allowedTools)
	if query == "" || len(candidates) == 0 {
		return
	}
	ids, err := s.config.ToolSelector.Select(ctx,
		selection.Query{Text: query, Kind: "tool", K: len(candidates)},
		candidates,
	)
	if err != nil || len(ids) == 0 {
		return
	}
	if narrowed := intersectInOrder(acc.allowedTools, ids); len(narrowed) > 0 {
		acc.allowedTools = narrowed
	}
}

// toolCandidates resolves the named tool list against the registry and
// returns one Candidate per registered entry. Unknown names are
// dropped silently.
func (s *ProviderStage) toolCandidates(names []string) []selection.Candidate {
	out := make([]selection.Candidate, 0, len(names))
	for _, name := range names {
		td, err := s.toolRegistry.GetTool(name)
		if err != nil {
			continue
		}
		out = append(out, selection.Candidate{
			ID:          td.Name,
			Name:        td.Name,
			Description: td.Description,
		})
	}
	return out
}

// intersectInOrder returns the elements of source that appear in keep,
// preserving source order.
func intersectInOrder(source, keep []string) []string {
	keepSet := make(map[string]bool, len(keep))
	for _, id := range keep {
		keepSet[id] = true
	}
	out := make([]string, 0, len(keep))
	for _, name := range source {
		if keepSet[name] {
			out = append(out, name)
		}
	}
	return out
}

// lastUserMessageText returns the concatenated text of the most
// recent user message in the slice, or "" when none is found.
func lastUserMessageText(msgs []types.Message) string {
	for i := len(msgs) - 1; i >= 0; i-- {
		if msgs[i].Role != "user" {
			continue
		}
		var sb strings.Builder
		for _, p := range msgs[i].Parts {
			if p.Type == types.ContentTypeText && p.Text != nil {
				if sb.Len() > 0 {
					sb.WriteByte(' ')
				}
				sb.WriteString(*p.Text)
			}
		}
		return sb.String()
	}
	return ""
}

// getMaxRounds returns the maximum number of tool call rounds.
func (s *ProviderStage) getMaxRounds() int {
	if s.toolPolicy != nil && s.toolPolicy.MaxRounds > 0 {
		return s.toolPolicy.MaxRounds
	}
	return defaultMaxRounds
}

// getMaxIdenticalToolCalls returns the threshold for aborting a loop where the
// same tool is called repeatedly with identical arguments.
func (s *ProviderStage) getMaxIdenticalToolCalls() int {
	if s.toolPolicy != nil && s.toolPolicy.MaxIdenticalToolCalls > 0 {
		return s.toolPolicy.MaxIdenticalToolCalls
	}
	return defaultMaxIdenticalCalls
}

// canonicalArgs returns a stable, order-insensitive string representation of
// raw JSON arguments for use as an identical-call detection key. If the raw
// bytes cannot be parsed as JSON, the raw bytes are used directly so the key
// is still meaningful (just byte-order-sensitive).
func canonicalArgs(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var v any
	if err := json.Unmarshal(raw, &v); err != nil {
		return string(raw)
	}
	b, err := json.Marshal(v)
	if err != nil {
		return string(raw)
	}
	return string(b)
}

// checkIdenticalToolCalls increments per-call counters and returns an error if
// any single (tool, args) pair exceeds the configured threshold.
func (tl *toolLoop) identicalLoopCalls(toolCalls []types.MessageToolCall) []types.MessageToolCall {
	threshold := tl.stage.getMaxIdenticalToolCalls()
	var looping []types.MessageToolCall
	for i := range toolCalls {
		tc := toolCalls[i]
		key := tc.Name + "\x00" + canonicalArgs(tc.Args)
		tl.identicalCallCounts[key]++
		if tl.identicalCallCounts[key] >= threshold {
			looping = append(looping, tc)
		}
	}
	return looping
}

// loopFeedbackResults builds tool-result messages for a round where an identical
// loop was detected: the looping calls get a synthetic "you repeated this — stop"
// result (NOT executed, since the result wouldn't change), while any non-looping
// calls in the same round execute normally so every tool_use still has a result.
func (tl *toolLoop) loopFeedbackResults(
	ctx context.Context, all, looping []types.MessageToolCall, rr roundRef,
) []types.Message {
	loopSet := make(map[string]bool, len(looping))
	for _, c := range looping {
		loopSet[c.ID] = true
	}
	threshold := tl.stage.getMaxIdenticalToolCalls()
	results := make([]types.Message, 0, len(all))
	var toExecute []types.MessageToolCall
	for i := range all {
		tc := all[i]
		if !loopSet[tc.ID] {
			toExecute = append(toExecute, tc)
			continue
		}
		msg := fmt.Sprintf(
			"Repeated call ignored: %q has been called %d times with identical arguments and was NOT "+
				"executed again — the result will not change. Stop repeating this exact call: change the "+
				"arguments, use a different tool, or finish the task.",
			tc.Name, threshold)
		results = append(results, types.NewToolResultMessage(types.NewTextToolResult(tc.ID, tc.Name, msg)))
	}
	if len(toExecute) > 0 {
		executed, _ := tl.stage.executeToolCalls(ctx, toExecute, rr)
		results = append(results, executed...)
	}
	return results
}

func (s *ProviderStage) executeStreamingMultiRound(
	ctx context.Context,
	acc *providerInput,
	output chan<- StreamElement,
) ([]types.Message, error) {
	s.applyToolSelector(ctx, acc)
	loop, err := s.newToolLoop(acc)
	if err != nil {
		return nil, err
	}
	loop.output = output
	loop.preSeedLog(ctx)
	// Reconcile before the first round. A resumed execution (HITL approval,
	// deferred client tool) re-ran PromptAssemblyStage, which reset the prompt
	// to the state the pipeline was built for even though the workflow has
	// already moved on. Nothing is "pending" by then, so only a comparison
	// against the current state recovers it.
	if stop, hErr := s.applyStateHandoff(ctx, acc, loop); hErr != nil {
		return loop.messages, hErr
	} else if stop {
		return loop.messages, nil
	}
	for round := 1; round <= loop.maxRounds; round++ {
		rr := roundRef{round: round, providerCallID: newProviderCallID()}
		params := &streamingRoundParams{
			messages:       loop.messages,
			systemPrompt:   acc.systemPrompt,
			providerTools:  loop.providerTools,
			toolChoice:     loop.toolChoice,
			round:          round,
			metadata:       acc.metadata,
			providerCallID: rr.providerCallID,
		}
		response, hasToolCalls, err := s.executeStreamingRound(ctx, params, output)
		if err != nil {
			return loop.messages, err
		}
		// Same per-round stamp/handoff as the unary loop — the next round's
		// streamingRoundParams re-read acc.systemPrompt and loop.providerTools,
		// so a mid-loop swap takes effect there.
		s.stampWorkflowState(&response)
		if done, msgs, err := loop.afterRound(ctx, acc.allowedTools, &response, hasToolCalls, rr); done {
			return msgs, err
		}
		if stop, hErr := s.applyStateHandoff(ctx, acc, loop); hErr != nil {
			return loop.messages, hErr
		} else if stop {
			return loop.messages, nil
		}
	}
	// Unreachable: afterRound returns done with an error at round == maxRounds,
	// so every exit goes through it. Kept as a compile-time fallthrough.
	return loop.messages, nil
}

// toolLoop holds shared state for multi-round tool execution.
type toolLoop struct {
	stage               *ProviderStage
	messages            []types.Message
	providerTools       interface{}
	toolChoice          string
	maxRounds           int
	excluded            map[string]bool
	rejectionCounts     map[string]int
	identicalCallCounts map[string]int // keyed by "toolName\x00<canonical-args>"
	lastPersistedSeq    int            // messages persisted so far via MessageLog
	cumulativeCost      float64        // accumulated cost across rounds
	cumulativeInput     int            // accumulated input tokens across this loop's rounds
	cumulativeCached    int            // accumulated cache-read tokens across this loop's rounds
	cachingSupported    bool           // provider advertises prompt caching (gates the stall warning)
	warnedNoCaching     bool           // one-time guard for the caching-stalled warning
	nudgedLoop          bool           // already fed an identical-loop back to the model once

	// acc is the live turn input. Held as a pointer rather than copied because
	// applyStateHandoff can swap the system prompt mid-loop, and the
	// final-turn re-ask must use the prompt in force when the loop ended.
	acc *providerInput

	// schemaWithheld records that ResponseFormat was kept off this loop's
	// rounds, so the final answer still owes a constrained re-ask. Set once at
	// construction: whether a schema was withheld is a property of the loop,
	// not of the round that happens to finish it.
	schemaWithheld bool

	// output is the pipeline's element channel, set only on the streaming path.
	// Its presence is what tells the final-turn re-ask to stream its answer —
	// with every round's text suppressed, that re-ask is the only text a
	// streaming consumer receives.
	output chan<- StreamElement
}

// promptCachingProvider is optionally implemented by providers that support
// prompt caching. The caching-stall warning only fires for providers that
// advertise support, so a provider without caching (local models, etc.) never
// produces false "caching not engaging" warnings.
type promptCachingProvider interface {
	SupportsPromptCaching() bool
}

// cachingStallRounds is how many rounds of a tool loop must pass with a large,
// uncached input before we warn that prompt caching isn't engaging.
const cachingStallRounds = 3

// warnIfCachingStalled emits a one-time warning when an agent loop has run
// several rounds re-sending a large input with zero cache reads — the signature
// of prompt caching not engaging (an unstable request prefix). It is the
// operational backstop for the non-deterministic-tool-order class of bug, which
// silently re-bills the full context at full price every round. Gated on the
// provider advertising caching support so no-cache providers don't false-alarm.
// Cheap: a couple of int comparisons per round.
func (tl *toolLoop) warnIfCachingStalled(round int) {
	if tl.warnedNoCaching || !tl.cachingSupported || round < cachingStallRounds {
		return
	}
	// Average input per round as a proxy for "there is a substantial prefix that
	// caching would help" (Claude's cacheable floor is ~2048 tokens).
	avgInput := tl.cumulativeInput / (round + 1)
	if tl.cumulativeCached == 0 && avgInput >= 2048 {
		tl.warnedNoCaching = true
		logger.Warn("prompt caching not engaging: zero cache reads over a multi-round tool loop "+
			"with a large input — verify the provider supports caching and the request prefix "+
			"(tools+system) is stable across rounds",
			"rounds", round+1,
			"avg_input_tokens", avgInput,
			"cumulative_input_tokens", tl.cumulativeInput)
	}
}

func (s *ProviderStage) newToolLoop(acc *providerInput) (*toolLoop, error) {
	excluded := map[string]bool{}
	providerTools, toolChoice, err := s.buildProviderTools(acc.allowedTools, excluded)
	if err != nil {
		return nil, fmt.Errorf("provider stage: %w", err)
	}
	cachingSupported := false
	if pc, ok := s.provider.(promptCachingProvider); ok {
		cachingSupported = pc.SupportsPromptCaching()
	}
	return &toolLoop{
		stage:               s,
		acc:                 acc,
		schemaWithheld:      s.withholdsSchema(providerTools),
		messages:            acc.messages,
		providerTools:       providerTools,
		toolChoice:          toolChoice,
		maxRounds:           s.getMaxRounds(),
		excluded:            excluded,
		rejectionCounts:     map[string]int{},
		identicalCallCounts: map[string]int{},
		cachingSupported:    cachingSupported,
		lastPersistedSeq:    len(acc.messages), // history already in store
		// Seed with the cost already incurred in this conversation (prior
		// turns), so MaxCostUSD bounds the whole RUN, not just this turn's
		// loop. Arena builds a fresh pipeline (and toolLoop) per turn, so
		// without this seed the cap would reset every turn and a multi-turn
		// run could spend MaxCostUSD per turn. History cost is carried on the
		// input messages' CostInfo.
		cumulativeCost: sumHistoryCost(acc.messages),
	}, nil
}

// sumHistoryCost totals the CostInfo across the conversation history fed into
// this turn — i.e. the run's spend before this turn's loop begins.
func sumHistoryCost(messages []types.Message) float64 {
	var total float64
	for i := range messages {
		if messages[i].CostInfo != nil {
			total += messages[i].CostInfo.TotalCost
		}
	}
	return total
}

// afterRound handles tool execution, rejection tracking, and loop control after
// a provider round completes. Returns (done, messages, error). When done is true,
// the caller should return immediately with the provided messages and error.
func (tl *toolLoop) afterRound(
	ctx context.Context,
	allowedTools []string,
	response *types.Message,
	hasToolCalls bool,
	rr roundRef,
) (bool, []types.Message, error) {
	round := rr.round
	tl.messages = append(tl.messages, *response)

	// Emit this round's assembled reasoning before its tool events, matching
	// the causal order a consumer renders: the model reasons, then calls.
	//
	// This is the only emit site for both the unary and streaming paths — they
	// converge here with the response and the round in hand — so the two cannot
	// drift. It is also the signal a consumer WITHOUT a recording stage
	// depends on: message.created only exists when RecordingStage is wired, so
	// without this the accumulated trace never leaves the process and the
	// consumer is left re-accumulating reasoning.delta fragments.
	if tl.stage.emitter != nil {
		tl.stage.emitter.ReasoningCompletedCtx(ctx, response.Reasoning, rr.round, rr.providerCallID)
	}

	// Track cumulative cost for budget enforcement
	if response.CostInfo != nil {
		tl.cumulativeCost += response.CostInfo.TotalCost
		tl.cumulativeInput += response.CostInfo.InputTokens
		tl.cumulativeCached += response.CostInfo.CachedTokens
	}
	tl.warnIfCachingStalled(round)
	policy := tl.stage.toolPolicy
	if policy != nil && policy.MaxCostUSD > 0 && tl.cumulativeCost > policy.MaxCostUSD {
		tl.persistMessages(ctx, round)
		return true, tl.messages, fmt.Errorf(
			"provider stage: cost budget exceeded (%.4f > %.4f USD)", tl.cumulativeCost, policy.MaxCostUSD)
	}

	if !hasToolCalls {
		// The loop has ended. This is the first and only moment that is
		// knowable, so a withheld schema is applied here — the answer that
		// revealed the ending is regenerated under it. Before persistMessages,
		// so the log holds the constrained answer rather than the discarded one.
		if tl.schemaWithheld {
			tl.reaskUnderSchema(ctx, rr)
		}
		tl.persistMessages(ctx, round)
		return true, tl.messages, nil
	}

	// Repeated-identical-call breaker. The first time the same tool is called with
	// identical arguments too many times, feed that back to the model once (a
	// synthetic "you're repeating yourself — stop" result) and let it self-correct
	// — loops are often transient confusion (e.g. a model retrying a failing call
	// verbatim). If it loops AGAIN after the nudge, abort.
	if looping := tl.identicalLoopCalls(response.ToolCalls); len(looping) > 0 {
		if tl.nudgedLoop {
			tl.persistMessages(ctx, round)
			return true, tl.messages, fmt.Errorf(
				"provider stage: tool loop persisted after a self-correction nudge: %q called with identical arguments",
				looping[0].Name)
		}
		tl.nudgedLoop = true
		logger.Warn("identical tool-call loop detected; feeding it back to the model for one self-correction attempt",
			"tool", looping[0].Name)
		tl.messages = append(tl.messages, tl.loopFeedbackResults(ctx, response.ToolCalls, looping, rr)...)
		ResetIdleFromContext(ctx)
		tl.persistMessages(ctx, round)
		return false, tl.messages, nil
	}

	toolResults, err := tl.stage.executeToolCalls(ctx, response.ToolCalls, rr)
	if err != nil {
		if _, ok := tools.IsErrToolsPending(err); ok {
			tl.messages = append(tl.messages, toolResults...)
			tl.persistMessages(ctx, round)
			return true, tl.messages, err
		}
		return true, tl.messages, fmt.Errorf("provider stage: tool execution failed: %w", err)
	}

	tl.messages = append(tl.messages, toolResults...)
	ResetIdleFromContext(ctx)
	tl.persistMessages(ctx, round)

	if tl.stage.updateExcludedTools(toolResults, tl.rejectionCounts, tl.excluded) {
		rebuilt, _, rebuildErr := tl.stage.buildProviderTools(allowedTools, tl.excluded)
		if rebuildErr != nil {
			return true, tl.messages, fmt.Errorf("provider stage: rebuild tools: %w", rebuildErr)
		}
		tl.providerTools = rebuilt
	}

	tl.toolChoice = toolChoiceAuto

	// RFC 0010 termination.tool_called: stop cleanly after the round in which
	// the named terminal tool fired. The tool has already executed and its result
	// is recorded in tl.messages — now stop before the next provider round.
	if policy != nil && policy.StopOnTool != "" {
		for _, tc := range response.ToolCalls {
			if tc.Name == policy.StopOnTool {
				return true, tl.messages, nil
			}
		}
	}

	// Compact stale tool results before next round's provider call.
	// Pass 0 for lastInputTokens: the provider's InputTokens reflects what it
	// saw on the last call, but we've since appended the assistant response and
	// tool results — using the stale count would under-compact.
	if tl.stage.config != nil && tl.stage.config.Compactor != nil {
		cr := tl.stage.config.Compactor.Compact(tl.messages, 0)
		tl.messages = cr.Messages
		if cr.MessagesFolded > 0 && tl.stage.emitter != nil {
			tl.stage.emitter.ContextCompacted(round, cr.OriginalTokens, cr.CompactedTokens,
				cr.MessagesFolded, tl.stage.config.Compactor.TokenBudget())
		}
	}

	if round == tl.maxRounds {
		return true, tl.messages, fmt.Errorf("provider stage: max rounds (%d) exceeded", tl.maxRounds)
	}

	return false, nil, nil
}

// preSeedLog writes the history messages that were already present in
// acc.messages (i.e. messages[0:lastPersistedSeq]) into the MessageLog.
// This ensures the full initial context is persisted before the first
// tool-loop round fires, so a mid-loop error doesn't lose the transcript.
// LogAppend's idempotent-dedup skips messages already stored from prior
// turns, so this is safe to call on every turn start.
func (tl *toolLoop) preSeedLog(ctx context.Context) {
	cfg := tl.stage.config
	if cfg == nil || cfg.MessageLog == nil || tl.lastPersistedSeq == 0 {
		return
	}
	history := tl.messages[:tl.lastPersistedSeq]
	if len(history) == 0 {
		return
	}
	newTotal, err := cfg.MessageLog.LogAppend(ctx, cfg.MessageLogConvID, 0, history)
	if err != nil {
		logger.Warn("message log pre-seed failed", "error", err)
		return
	}
	// After pre-seeding, lastPersistedSeq reflects what's now in the store.
	// If the store already had these messages (multi-turn case), newTotal may
	// be > lastPersistedSeq (store has more history from previous turns).
	// Keep lastPersistedSeq as the larger of the two so we don't re-send old
	// messages in subsequent persistMessages calls.
	if newTotal > tl.lastPersistedSeq {
		tl.lastPersistedSeq = newTotal
	}
}

// persistMessages writes new messages to the MessageLog if configured.
// Best-effort: failures are logged but don't affect the tool loop.
func (tl *toolLoop) persistMessages(ctx context.Context, round int) {
	cfg := tl.stage.config
	if cfg == nil || cfg.MessageLog == nil {
		return
	}
	newMsgs := tl.messages[tl.lastPersistedSeq:]
	if len(newMsgs) == 0 {
		return
	}
	newTotal, err := cfg.MessageLog.LogAppend(ctx, cfg.MessageLogConvID, tl.lastPersistedSeq, newMsgs)
	if err != nil {
		logger.Warn("message log append failed", "round", round, "error", err)
		return
	}
	tl.lastPersistedSeq = newTotal
}

func (s *ProviderStage) executeRound(
	ctx context.Context,
	messages []types.Message,
	systemPrompt string,
	providerTools interface{},
	toolChoice string,
	rr roundRef,
	metadata map[string]interface{},
) (types.Message, bool, error) {
	round := rr.round
	ResetIdleFromContext(ctx)

	if blocked, handled, err := s.runBeforeCallHooks(
		ctx, messages, systemPrompt, round, metadata,
	); handled {
		return blocked, false, err
	}

	// Build provider request
	req := providers.PredictionRequest{
		System:         systemPrompt,
		Messages:       messages,
		MaxTokens:      s.config.MaxTokens,
		Temperature:    s.config.Temperature,
		Seed:           s.config.Seed,
		ResponseFormat: s.roundResponseFormat(providerTools),
		Metadata:       metadata,
	}

	// Normalize: merge any system-role messages from Messages into the System
	// field so all providers receive system context through the dedicated field.
	req.NormalizeMessages()

	// Count tools for event emission
	toolCount := 0
	if providerTools != nil {
		if toolDescs, ok := providerTools.([]*providers.ToolDescriptor); ok {
			toolCount = len(toolDescs)
		}
	}

	logger.Debug("Provider round starting",
		"round", round,
		"messages", len(messages),
		"tools", providerTools != nil)

	// Emit provider call started event
	if s.emitter != nil {
		s.emitter.ProviderCallStartedCtx(ctx, &events.ProviderCallStartedData{
			Provider:     s.provider.ID(),
			Model:        s.provider.Model(),
			MessageCount: len(messages),
			ToolCount:    toolCount,
			Labels:       s.config.Labels,
			Round:        rr.round,
			CallID:       rr.providerCallID,
		})
	}

	// Call provider (with or without tools)
	startTime := time.Now()
	var resp providers.PredictionResponse
	var toolCalls []types.MessageToolCall
	var err error

	toolProvider, supportsTools := s.provider.(providers.ToolSupport)
	if s.useToolPath(providerTools, req.Messages, supportsTools) {
		// Use tool-aware provider interface
		if !supportsTools {
			return types.Message{}, false, errors.New("provider does not support tools")
		}
		resp, toolCalls, err = toolProvider.PredictWithTools(ctx, req, providerTools, toolChoice)
	} else {
		// Regular prediction
		resp, err = s.provider.Predict(ctx, req)
		toolCalls = resp.ToolCalls
	}

	duration := time.Since(startTime)

	if err != nil {
		logger.Error("Provider call failed", "error", err, "duration", duration)
		// Emit provider call failed event
		if s.emitter != nil {
			s.emitter.ProviderCallFailedCtx(ctx, &events.ProviderCallFailedData{
				Provider: s.provider.ID(),
				Model:    s.provider.Model(),
				Error:    err,
				Duration: duration,
				Source:   s.config.Source,
				Labels:   s.config.Labels,
				Round:    rr.round,
				CallID:   rr.providerCallID,
			})
		}
		return types.Message{}, false, fmt.Errorf("provider call failed: %w", err)
	}

	// Emit provider call completed event
	if s.emitter != nil {
		completedData := &events.ProviderCallCompletedData{
			Provider:      s.provider.ID(),
			Model:         s.provider.Model(),
			Duration:      duration,
			ToolCallCount: len(toolCalls),
			FinishReason:  resp.FinishReason,
			Source:        s.config.Source,
			Labels:        s.config.Labels,
			Round:         rr.round,
			CallID:        rr.providerCallID,
		}
		if resp.CostInfo != nil {
			completedData.InputTokens = resp.CostInfo.InputTokens
			completedData.OutputTokens = resp.CostInfo.OutputTokens
			completedData.CachedTokens = resp.CostInfo.CachedTokens
			completedData.Cost = resp.CostInfo.TotalCost
		}
		s.emitter.ProviderCallCompletedCtx(ctx, completedData)
	}

	// Stamp the unified CostInfo identity fields if the provider didn't fill
	// them in itself. Most chat providers leave these empty since they only
	// populate the legacy token-cost fields; Imagen and post-migration
	// ancillary providers populate them at source. Either way, after this
	// stamp every assistant message's cost_info carries the provider name
	// and capability discriminator the breakdown / aggregation paths expect.
	if resp.CostInfo != nil {
		if resp.CostInfo.ProviderName == "" {
			resp.CostInfo.ProviderName = s.provider.Name()
		}
		if resp.CostInfo.Capability == "" {
			resp.CostInfo.Capability = string(s.provider.Type())
		}
		if resp.CostInfo.Latency == 0 {
			resp.CostInfo.Latency = duration
		}
	}

	// Build response message with latency and cost info
	responseMsg := types.Message{
		Role:         roleAssistant,
		Content:      resp.Content,
		Parts:        resp.Parts,
		Reasoning:    resp.Reasoning,
		ToolCalls:    toolCalls,
		Timestamp:    timeNow(),
		LatencyMs:    duration.Milliseconds(),
		CostInfo:     resp.CostInfo,
		FinishReason: resp.FinishReason,
	}

	// Run AfterCall hooks
	if err := s.runAfterCallHooks(ctx, &afterCallParams{
		messages:     messages,
		systemPrompt: systemPrompt,
		round:        round,
		metadata:     metadata,
		duration:     duration,
		responseMsg:  &responseMsg,
		toolCalls:    &toolCalls,
	}); err != nil {
		return responseMsg, false, err
	}

	logger.Debug("Provider round completed",
		"round", round,
		"duration", duration,
		"latencyMs", responseMsg.LatencyMs,
		"tool_calls", len(toolCalls))

	// Check for tool calls
	hasToolCalls := len(toolCalls) > 0

	return responseMsg, hasToolCalls, nil
}

func (s *ProviderStage) executeStreamingRound(
	ctx context.Context,
	params *streamingRoundParams,
	output chan<- StreamElement,
) (types.Message, bool, error) {
	ResetIdleFromContext(ctx)

	if blocked, handled, err := s.runBeforeCallHooks(
		ctx, params.messages, params.systemPrompt, params.round, params.metadata,
	); handled {
		if err == nil {
			if emitErr := s.emitBlockedTurnText(ctx, &blocked, output); emitErr != nil {
				return blocked, false, emitErr
			}
		}
		return blocked, false, err
	}

	// Build provider request
	req := providers.PredictionRequest{
		System:         params.systemPrompt,
		Messages:       params.messages,
		MaxTokens:      s.config.MaxTokens,
		Temperature:    s.config.Temperature,
		Seed:           s.config.Seed,
		Metadata:       params.metadata,
		ResponseFormat: s.roundResponseFormat(params.providerTools),
	}

	// Normalize: merge any system-role messages from Messages into the System
	// field so all providers receive system context through the dedicated field.
	req.NormalizeMessages()

	// Count tools for event emission
	toolCount := 0
	if params.providerTools != nil {
		if toolDescs, ok := params.providerTools.([]*providers.ToolDescriptor); ok {
			toolCount = len(toolDescs)
		}
	}

	logger.Debug("Provider streaming round starting",
		"round", params.round,
		"messages", len(params.messages),
		"tools", params.providerTools != nil)

	// Emit provider call started event
	if s.emitter != nil {
		s.emitter.ProviderCallStartedCtx(ctx, &events.ProviderCallStartedData{
			Provider:     s.provider.ID(),
			Model:        s.provider.Model(),
			MessageCount: len(params.messages),
			ToolCount:    toolCount,
			Labels:       s.config.Labels,
			Round:        params.round,
			CallID:       params.providerCallID,
		})
	}

	startTime := time.Now()

	// Start the streaming request
	streamChan, err := s.startStreamingRequest(ctx, req, params.providerTools, params.toolChoice)
	if err != nil {
		duration := time.Since(startTime)
		// Emit provider call failed event
		if s.emitter != nil {
			s.emitter.ProviderCallFailedCtx(ctx, &events.ProviderCallFailedData{
				Provider: s.provider.ID(),
				Model:    s.provider.Model(),
				Error:    err,
				Duration: duration,
				Source:   s.config.Source,
				Labels:   s.config.Labels,
				Round:    params.round,
				CallID:   params.providerCallID,
			})
		}
		return types.Message{}, false, err
	}

	// Process all chunks and collect response
	content, toolCalls, costInfo, reasoning, chunkValidations, finishReason, err :=
		s.processStreamChunks(ctx, streamChan, output,
			roundRef{round: params.round, providerCallID: params.providerCallID},
			s.withholdsSchema(params.providerTools))
	duration := time.Since(startTime)

	if err != nil {
		// Emit provider call failed event
		if s.emitter != nil {
			s.emitter.ProviderCallFailedCtx(ctx, &events.ProviderCallFailedData{
				Provider: s.provider.ID(),
				Model:    s.provider.Model(),
				Error:    err,
				Duration: duration,
				Source:   s.config.Source,
				Labels:   s.config.Labels,
				Round:    params.round,
				CallID:   params.providerCallID,
			})
		}
		return types.Message{}, false, err
	}

	// Emit provider call completed event with cost info from streaming response
	if s.emitter != nil {
		completedData := &events.ProviderCallCompletedData{
			Provider:      s.provider.ID(),
			Model:         s.provider.Model(),
			Duration:      duration,
			ToolCallCount: len(toolCalls),
			FinishReason:  finishReason,
			Source:        s.config.Source,
			Labels:        s.config.Labels,
			Round:         params.round,
			CallID:        params.providerCallID,
		}
		// Populate token counts from cost info if available (present in final chunk)
		if costInfo != nil {
			completedData.InputTokens = costInfo.InputTokens
			completedData.OutputTokens = costInfo.OutputTokens
			completedData.CachedTokens = costInfo.CachedTokens
			completedData.Cost = costInfo.TotalCost
		}
		s.emitter.ProviderCallCompletedCtx(ctx, completedData)
	}

	// Stamp identity fields onto the streaming-path cost info; mirrors the
	// non-streaming path above so every assistant message has a consistent
	// provider/capability stamp.
	if costInfo != nil {
		if costInfo.ProviderName == "" {
			costInfo.ProviderName = s.provider.Name()
		}
		if costInfo.Capability == "" {
			costInfo.Capability = string(s.provider.Type())
		}
		if costInfo.Latency == 0 {
			costInfo.Latency = duration
		}
	}

	// Build final response message with latency and cost info. Stamp any
	// chunk-interceptor firings up-front so downstream readers see streaming
	// guardrail decisions in msg.Validations the same way they see AfterCall
	// firings — without this, a guardrail that aborts mid-stream produces
	// no observable validation record.
	responseMsg := types.Message{
		Role:      roleAssistant,
		Content:   content,
		ToolCalls: toolCalls,
		Reasoning: reasoning,
		Timestamp: timeNow(),
		LatencyMs: duration.Milliseconds(),
		CostInfo:  costInfo,
		// Stamp the provider's own reason, mirroring the non-streaming path.
		// Enforcement below overwrites this with FinishReasonSafety when a
		// guardrail fires. Without it a streaming turn reports no finish
		// reason at all, so max_output_tokens and refusal are indistinguishable
		// from a normal completion once the SDK surfaces the field.
		FinishReason: finishReason,
		Validations:  chunkValidations,
	}

	// Run AfterCall hooks
	if err := s.runAfterCallHooks(ctx, &afterCallParams{
		messages:     params.messages,
		systemPrompt: params.systemPrompt,
		round:        params.round,
		metadata:     params.metadata,
		duration:     duration,
		responseMsg:  &responseMsg,
		toolCalls:    &toolCalls,
	}); err != nil {
		return responseMsg, false, err
	}

	logger.Debug("Provider streaming round completed",
		"round", params.round,
		"duration", duration,
		"latencyMs", responseMsg.LatencyMs,
		"tool_calls", len(toolCalls))

	return responseMsg, len(toolCalls) > 0, nil
}

// messagesHaveToolLinkage reports whether the history contains assistant tool
// calls or tool results. Providers keep two message serializers — a tool-aware
// one and a plain one — and only the tool-aware one can represent that linkage
// (OpenAI's plain path builds []openAIMessage, which has no tool_calls or
// tool_call_id field at all; Claude's splits []claudeMessage vs
// []claudeToolMessage the same way). Sending such a history down the plain path
// silently strips the linkage and the provider rejects the array (issue #1735).
func messagesHaveToolLinkage(messages []types.Message) bool {
	for i := range messages {
		if len(messages[i].ToolCalls) > 0 || messages[i].ToolResult != nil {
			return true
		}
	}
	return false
}

// useToolPath decides whether a round goes to the provider's tool-aware entry
// point. Having tools to declare is the obvious reason, but it is not the only
// one: buildProviderTools returns nil both when the pack never had tools and
// when the round's tools were filtered away (every tool excluded after repeated
// rejection, or tool_choice "none"). In the second case the history already
// carries tool linkage, so the tool-aware serializer is still required even
// though there is nothing left to declare. Providers accept tool history with
// no tools declared — verified against OpenAI, Anthropic and Gemini.
func (s *ProviderStage) useToolPath(
	providerTools interface{}, messages []types.Message, supportsTools bool,
) bool {
	if providerTools != nil {
		return true
	}
	return supportsTools && messagesHaveToolLinkage(messages)
}

// startStreamingRequest initiates a streaming request with or without tools.
func (s *ProviderStage) startStreamingRequest(
	ctx context.Context,
	req providers.PredictionRequest,
	providerTools interface{},
	toolChoice string,
) (<-chan providers.StreamChunk, error) {
	toolProvider, supportsTools := s.provider.(providers.ToolSupport)
	if s.useToolPath(providerTools, req.Messages, supportsTools) {
		if !supportsTools {
			return nil, errors.New("provider does not support tools")
		}
		streamChan, err := toolProvider.PredictStreamWithTools(ctx, req, providerTools, toolChoice)
		if err != nil {
			logger.Error("Provider stream failed", "error", err)
			return nil, fmt.Errorf("provider stream failed: %w", err)
		}
		return streamChan, nil
	}

	streamChan, err := s.provider.PredictStream(ctx, req)
	if err != nil {
		logger.Error("Provider stream failed", "error", err)
		return nil, fmt.Errorf("provider stream failed: %w", err)
	}
	return streamChan, nil
}

// processStreamChunks processes streaming chunks and emits elements to output.
// Returns accumulated content, tool calls, cost info (from final chunk),
// any chunk-interceptor firings (ValidationResults the caller folds into
// the final assistant message), and any error.
func (s *ProviderStage) processStreamChunks(
	ctx context.Context,
	streamChan <-chan providers.StreamChunk,
	output chan<- StreamElement,
	rr roundRef,
	suppressText bool,
) (string, []types.MessageToolCall, *types.CostInfo, *types.ReasoningTrace, []types.ValidationResult, string, error) {
	var content string
	var toolCalls []types.MessageToolCall
	var costInfo *types.CostInfo
	var reasoning strings.Builder
	var opaqueReasoning []types.OpaqueReasoning
	var pendingValidations []types.ValidationResult
	var finishReason string

	for chunk := range streamChan {
		ResetIdleFromContext(ctx)

		// Reset signal: the retry driver failed mid-stream and is
		// retrying from scratch. Discard all accumulated state so the
		// caller sees only the retry's response, not a mashup of the
		// failed attempt and the retry.
		if chunk.Reset {
			content = ""
			toolCalls = nil
			costInfo = nil
			reasoning.Reset()
			opaqueReasoning = nil
			continue
		}

		if chunk.Error != nil {
			logger.Error("Stream chunk error", "error", chunk.Error)
			return "", nil, nil, nil, nil, "", fmt.Errorf("stream chunk error: %w", chunk.Error)
		}

		content = chunk.Content
		if len(chunk.ToolCalls) > 0 {
			toolCalls = chunk.ToolCalls
		}
		// Capture cost info from final chunk (only present when FinishReason != nil)
		if chunk.CostInfo != nil {
			costInfo = chunk.CostInfo
		}
		// Capture the finish reason (last non-nil wins) so the completion event
		// carries it instead of an empty string.
		if chunk.FinishReason != nil {
			finishReason = *chunk.FinishReason
		}
		// Accumulate reasoning (deltas) separately from content; it surfaces on
		// Message.Reasoning, never as content or future context.
		reasoning.WriteString(chunk.Reasoning)
		opaqueReasoning = append(opaqueReasoning, chunk.OpaqueReasoning...)

		if err := s.emitChunkElement(ctx, &chunk, output, rr, suppressText); err != nil {
			return "", nil, nil, nil, nil, "", err
		}

		// Run chunk interceptor hooks
		if s.hookRegistry != nil && s.hookRegistry.HasChunkInterceptors() {
			if d := s.hookRegistry.RunOnChunk(ctx, &chunk); !d.Allow {
				s.recordGuardrailFiring(nil, d, hooks.DirectionOutput, 0)
				// Stamp the firing on a sentinel pending-validation slot so
				// the AfterCall path below can fold it into responseMsg.
				// Without this, streaming-only guardrails produce no
				// observable Validations entry and the guardrail_triggered
				// assertion would silently miss them.
				//
				// The stamp goes through the same builder recordGuardrailFiring
				// uses so a streaming firing carries direction and a timestamp
				// like every other firing, and copies the decision metadata
				// rather than aliasing it.
				if v, ok := guardrailValidation(d, hooks.DirectionOutput); ok {
					pendingValidations = append(pendingValidations, v)
				}
				if d.Enforced {
					// Hook enforced (e.g., truncated content) — use the
					// modified chunk content and stop reading the stream.
					content = chunk.Content
					break
				}
				return "", nil, nil, nil, nil, "", &providers.ValidationAbortError{
					Reason: d.Reason,
					Chunk:  chunk,
				}
			}
		}
	}

	var trace *types.ReasoningTrace
	if reasoning.Len() > 0 || len(opaqueReasoning) > 0 {
		trace = &types.ReasoningTrace{Text: reasoning.String(), Opaque: opaqueReasoning}
	}
	return content, toolCalls, costInfo, trace, pendingValidations, finishReason, nil
}

// emitChunkElement creates and emits streaming element(s) for a chunk.
// Handles both text (Delta) and media (MediaData) content.
func (s *ProviderStage) emitChunkElement(
	ctx context.Context,
	chunk *providers.StreamChunk,
	output chan<- StreamElement,
	rr roundRef,
	suppressText bool,
) error {
	// Emit a live, non-content reasoning element if present, so the UI can stream
	// thinking as it arrives (it also accumulates onto Message.Reasoning). Mirror
	// onto the events bus as a distinct non-content signal for live consumers.
	if chunk.Reasoning != "" || len(chunk.OpaqueReasoning) > 0 {
		if s.emitter != nil && chunk.Reasoning != "" {
			// Attributed to its round so a streaming consumer can tell which
			// model turn it is watching think, and can tie these fragments to
			// the reasoning.completed that follows them.
			s.emitter.ReasoningDeltaCtx(ctx, chunk.Reasoning, rr.round, rr.providerCallID)
		}
		select {
		case output <- StreamElement{Reasoning: &ReasoningDelta{Text: chunk.Reasoning, Opaque: chunk.OpaqueReasoning}}:
		case <-ctx.Done():
			return ctx.Err()
		}
	}

	// Emit text element if present.
	//
	// Suppressed for the whole of a schema-withheld tool loop. None of that
	// loop's prose is the deliverable: a tool-calling round's preamble is not
	// part of a JSON contract, and the closing round's answer is discarded and
	// re-asked under the schema. Suppressing unconditionally is what makes this
	// work without lookahead — a round is only known to be the last one after
	// it has finished, far too late to have withheld its deltas selectively.
	// The consumer sees exactly one thing: the constrained final answer.
	//
	// Reasoning deltas above are deliberately NOT suppressed. They are a
	// separate, non-content channel that a UI renders as thinking, and they
	// never form part of the schema-constrained answer.
	if chunk.Delta != "" && !suppressText {
		elem := NewTextElement(chunk.Delta)
		elem.Timestamp = timeNow()
		elem.Priority = PriorityNormal

		// Mark as an incremental delta so a downstream speech-out stage speaks
		// the complete assistant Message instead of each fragment (see
		// ElementMetadata.StreamingDelta).
		elem.Meta.StreamingDelta = true
		elem.Meta.TokenCount = chunk.TokenCount
		if chunk.FinishReason != nil {
			fr := *chunk.FinishReason
			elem.Meta.FinishReason = &fr
		}

		select {
		case output <- elem:
		case <-ctx.Done():
			return ctx.Err()
		}
	}

	// Emit media element if present
	if chunk.MediaData != nil && len(chunk.MediaData.Data) > 0 {
		elem := StreamMediaToElement(chunk.MediaData)

		select {
		case output <- elem:
		case <-ctx.Done():
			return ctx.Err()
		}
	}

	return nil
}

// StreamMediaToElement converts a StreamMediaData to a StreamElement.
// Routes by MIME type: audio/* → AudioData, video/* → VideoData, image/* → ImageData.
func StreamMediaToElement(media *providers.StreamMediaData) StreamElement {
	elem := StreamElement{
		Timestamp: timeNow(),
	}

	switch {
	case strings.HasPrefix(media.MIMEType, "audio/"):
		sampleRate := media.SampleRate
		if sampleRate == 0 {
			sampleRate = 16000
		}
		channels := media.Channels
		if channels == 0 {
			channels = 1
		}
		elem.Audio = &AudioData{
			Samples:    media.Data,
			SampleRate: sampleRate,
			Channels:   channels,
			Format:     AudioFormatPCM16,
		}

	case strings.HasPrefix(media.MIMEType, "video/"):
		elem.Video = &VideoData{
			Data:       media.Data,
			MIMEType:   media.MIMEType,
			Width:      media.Width,
			Height:     media.Height,
			FrameRate:  media.FrameRate,
			IsKeyFrame: media.IsKeyFrame,
			FrameNum:   media.FrameNum,
		}
		elem.Priority = PriorityHigh

	case strings.HasPrefix(media.MIMEType, "image/"):
		elem.Image = &ImageData{
			Data:     media.Data,
			MIMEType: media.MIMEType,
			Width:    media.Width,
			Height:   media.Height,
			FrameNum: media.FrameNum,
		}
	}

	return elem
}

// toolCallResult holds the outcome of a single tool call execution,
// including its original index for preserving result ordering.
type toolCallResult struct {
	index   int
	message types.Message
	pending *tools.PendingToolExecution // non-nil if tool returned pending status
}

// getMaxParallelToolCalls returns the max concurrency for parallel tool execution.
func (s *ProviderStage) getMaxParallelToolCalls() int {
	if s.toolPolicy != nil && s.toolPolicy.MaxParallelToolCalls > 0 {
		return s.toolPolicy.MaxParallelToolCalls
	}
	return defaultMaxParallelToolCalls
}

func (s *ProviderStage) executeToolCalls(
	ctx context.Context, toolCalls []types.MessageToolCall, rr roundRef,
) ([]types.Message, error) {
	if s.toolRegistry == nil {
		return nil, errors.New("tool registry not configured but tool calls present")
	}

	resultSlots := make([]toolCallResult, len(toolCalls))
	var mu sync.Mutex
	var pendingTools []tools.PendingToolExecution

	g, gctx := errgroup.WithContext(ctx)
	g.SetLimit(s.getMaxParallelToolCalls())

	for i, tc := range toolCalls {
		idx := i
		toolCall := tc

		g.Go(func() error {
			result := s.executeSingleToolCall(gctx, toolCall, rr)
			mu.Lock()
			defer mu.Unlock()
			result.index = idx
			resultSlots[idx] = result
			if result.pending != nil {
				pendingTools = append(pendingTools, *result.pending)
			}
			return nil
		})
	}

	// errgroup goroutines always return nil, so this only errors on ctx cancel.
	if err := g.Wait(); err != nil {
		return nil, err
	}

	// Collect results in original order, excluding pending entries.
	results := make([]types.Message, 0, len(toolCalls))
	for i := range resultSlots {
		if resultSlots[i].pending != nil {
			continue
		}
		results = append(results, resultSlots[i].message)
	}

	// Report what actually ran for RFC 0009's max_tool_calls budget. results
	// excludes pending calls, which have not executed and are counted when they
	// run on resume; errored and policy-blocked calls are included, since both
	// consumed a round.
	s.recordToolCalls(len(results))

	if len(pendingTools) > 0 {
		return results, &tools.ErrToolsPending{Pending: pendingTools}
	}

	return results, nil
}

// preExecCheck runs policy and hook checks before tool execution.
// Returns (hookDecision, blocked result, shouldSkip).
// When shouldSkip is true the caller should return the blocked result directly.
func (s *ProviderStage) preExecCheck(
	ctx context.Context, toolCall types.MessageToolCall,
) (hooks.Decision, toolCallResult, bool) {
	if s.toolPolicy != nil && isToolBlocked(toolCall.Name, s.toolPolicy.Blocklist) {
		errMsg := fmt.Sprintf("Tool %s is blocked by policy", toolCall.Name)
		result := types.NewTextToolResult(toolCall.ID, toolCall.Name, errMsg)
		result.Error = errMsg
		result.ErrorType = types.ToolErrorApproval
		return hooks.Decision{}, toolCallResult{
			message: types.NewToolResultMessage(result),
		}, true
	}

	var hookDecision hooks.Decision
	if s.hookRegistry != nil {
		toolReq := hooks.ToolRequest{
			Name: toolCall.Name, Args: toolCall.Args, CallID: toolCall.ID,
		}
		hookDecision = s.hookRegistry.RunBeforeToolExecution(ctx, toolReq)
		if !hookDecision.Allow {
			errMsg := fmt.Sprintf(
				"Tool %s blocked by hook: %s", toolCall.Name, hookDecision.Reason,
			)
			hookResult := types.NewTextToolResult(toolCall.ID, toolCall.Name, errMsg)
			hookResult.Error = errMsg
			hookResult.ErrorType = types.ToolErrorApproval
			msg := types.NewToolResultMessage(hookResult)
			if hookDecision.Metadata != nil {
				msg.Meta = hookDecision.Metadata
			}
			return hookDecision, toolCallResult{message: msg}, true
		}
	}
	return hookDecision, toolCallResult{}, false
}

// executeSingleToolCall handles policy checks, hooks, execution, and event
// emission for a single tool call. It never returns an error — failures are
// captured as error results in the returned message, matching the previous
// sequential behavior where one tool failure does not cancel others.
func (s *ProviderStage) executeSingleToolCall(
	ctx context.Context,
	toolCall types.MessageToolCall,
	rr roundRef,
) toolCallResult {
	hookDecision, blocked, skip := s.preExecCheck(ctx, toolCall)
	if skip {
		return blocked
	}

	// Human-in-the-loop approval gate: if a checker holds this call, surface it
	// as pending (via ErrToolsPending / PendingTools metadata) instead of
	// executing. The caller resolves it (approve / approve-with-edits / reject)
	// and resumes.
	if s.config != nil && s.config.ApprovalChecker != nil {
		var argsMap map[string]any
		if len(toolCall.Args) > 0 {
			_ = json.Unmarshal(toolCall.Args, &argsMap)
		}
		if info := s.config.ApprovalChecker(ctx, toolCall.ID, toolCall.Name, argsMap); info != nil {
			if info.ToolName == "" {
				info.ToolName = toolCall.Name
			}
			return s.buildPendingResult(ctx, toolCall, &tools.ToolExecutionResult{
				Status:      tools.ToolStatusPending,
				PendingInfo: info,
			})
		}
	}

	labels := s.toolLabels(toolCall.Name)
	s.emitToolStarted(ctx, toolCall, labels, rr)

	startTime := time.Now()
	ctx = tools.WithCallID(ctx, toolCall.ID)
	asyncResult, err := s.toolRegistry.ExecuteAsync(ctx, toolCall.Name, toolCall.Args)
	ResetIdleFromContext(ctx)
	if err != nil {
		if s.emitter != nil {
			s.emitter.ToolCallEventCtx(ctx, events.EventToolCallFailed, &events.ToolCallEventData{
				ToolName:       toolCall.Name,
				CallID:         toolCall.ID,
				Error:          err,
				Duration:       time.Since(startTime),
				Labels:         labels,
				Round:          rr.round,
				ProviderCallID: rr.providerCallID,
			})
		}
		errResult := types.NewTextToolResult(toolCall.ID, toolCall.Name, fmt.Sprintf("Error: %v", err))
		errResult.Error = err.Error()
		errResult.ErrorType = classifyToolError(err)
		return toolCallResult{
			message: types.NewToolResultMessage(errResult),
		}
	}

	if asyncResult.Status == tools.ToolStatusPending {
		return s.buildPendingResult(ctx, toolCall, asyncResult)
	}

	result := s.handleToolResult(toolCall, asyncResult)
	if s.emitter != nil {
		s.emitter.ToolCallEventCtx(ctx, events.EventToolCallCompleted, &events.ToolCallEventData{
			ToolName:       toolCall.Name,
			CallID:         toolCall.ID,
			Duration:       time.Since(startTime),
			Status:         string(asyncResult.Status),
			Parts:          result.Parts,
			Labels:         labels,
			Round:          rr.round,
			ProviderCallID: rr.providerCallID,
		})
	}
	resultMsg := types.NewToolResultMessage(result)
	if hookDecision.Metadata != nil {
		resultMsg.Meta = hookDecision.Metadata
	}

	s.runAfterToolHooks(ctx, toolCall, result, startTime)

	return toolCallResult{message: resultMsg}
}

// emitToolStarted emits the tool call started event if an emitter is configured.
func (s *ProviderStage) emitToolStarted(
	ctx context.Context, toolCall types.MessageToolCall, labels map[string]string, rr roundRef,
) {
	if s.emitter == nil {
		return
	}
	var argsMap map[string]interface{}
	if toolCall.Args != nil {
		_ = json.Unmarshal(toolCall.Args, &argsMap)
	}
	s.emitter.ToolCallEventCtx(ctx, events.EventToolCallStarted, &events.ToolCallEventData{
		ToolName:       toolCall.Name,
		CallID:         toolCall.ID,
		Args:           argsMap,
		Labels:         labels,
		Round:          rr.round,
		ProviderCallID: rr.providerCallID,
	})
}

// afterCallParams carries what the AfterCall hook chain needs from either
// round function. responseMsg and toolCalls are pointers because an enforcing
// guardrail rewrites the response and drops its tool calls in place.
type afterCallParams struct {
	messages     []types.Message
	systemPrompt string
	round        int
	metadata     map[string]interface{}
	duration     time.Duration
	responseMsg  *types.Message
	toolCalls    *[]types.MessageToolCall
}

// runAfterCallHooks runs the AfterCall hook chain, shared by the unary and
// streaming rounds so the two cannot drift.
//
// A non-nil error is a hook denial, which aborts the pipeline. An enforcing
// guardrail returns nil: it rewrites the response and clears its tool calls via
// applyEnforcedResponse, so the caller reports hasToolCalls=false and the round
// loop stops while the pipeline continues.
func (s *ProviderStage) runAfterCallHooks(ctx context.Context, p *afterCallParams) error {
	if s.hookRegistry == nil {
		return nil
	}

	hookReq := &hooks.ProviderRequest{
		ProviderID:   s.provider.ID(),
		Model:        s.provider.Model(),
		Messages:     p.messages,
		SystemPrompt: p.systemPrompt,
		Round:        p.round,
		Metadata:     p.metadata,
	}
	hookResp := &hooks.ProviderResponse{
		ProviderID: s.provider.ID(),
		Model:      s.provider.Model(),
		Message:    *p.responseMsg,
		Round:      p.round,
		LatencyMs:  p.duration.Milliseconds(),
	}

	hookStart := time.Now()
	d := s.hookRegistry.RunAfterProviderCall(ctx, hookReq, hookResp)
	if d.Allow {
		return nil
	}

	s.recordGuardrailFiring(p.responseMsg, d, hooks.DirectionOutput, time.Since(hookStart))
	if !d.Enforced {
		return &hooks.HookDeniedError{
			HookName: providerHookName,
			HookType: providerHookTypeAfter,
			Reason:   d.Reason,
			Metadata: d.Metadata,
		}
	}
	applyEnforcedResponse(p.responseMsg, p.toolCalls, hookResp, d)
	return nil
}

// runBeforeCallHooks runs the BeforeCall hook chain. It is called BEFORE the
// provider request is built, because hooks may mutate the message slice in
// place (e.g. redaction) and NormalizeMessages copies that slice when a system
// message is present — a hook running after the build would have its mutations
// silently discarded.
//
// handled is true when the round must not call the provider:
//   - a guardrail enforced: blocked is the canned assistant turn, err is nil,
//     and the caller returns hasToolCalls=false so the round loop stops
//     (afterRound returns done on !hasToolCalls). The pipeline is NOT aborted —
//     the message is emitted and every downstream stage still runs.
//   - a hook denied: err is a HookDeniedError, which does abort the pipeline.
func (s *ProviderStage) runBeforeCallHooks(
	ctx context.Context,
	messages []types.Message,
	systemPrompt string,
	round int,
	metadata map[string]interface{},
) (types.Message, bool, error) {
	if s.hookRegistry == nil {
		return types.Message{}, false, nil
	}
	hookReq := &hooks.ProviderRequest{
		ProviderID:   s.provider.ID(),
		Model:        s.provider.Model(),
		Messages:     messages,
		SystemPrompt: systemPrompt,
		Round:        round,
		Metadata:     metadata,
	}
	hookStart := time.Now()
	d := s.hookRegistry.RunBeforeProviderCall(ctx, hookReq)
	if d.Allow {
		return types.Message{}, false, nil
	}
	if !d.Enforced {
		return types.Message{}, true, &hooks.HookDeniedError{
			HookName: providerHookName,
			HookType: providerHookTypeBefore,
			Reason:   d.Reason,
			Metadata: d.Metadata,
		}
	}
	// Guardrail skipped the provider call: no request, no tokens.
	blocked := s.blockedMessage(hookReq, d)
	s.recordGuardrailFiring(&blocked, d, hooks.DirectionInput, time.Since(hookStart))
	return blocked, true, nil
}

// emitBlockedTurnText streams the canned assistant text of a guardrail-blocked
// turn as a text element, so a blocked turn reaches the caller the same way
// every unblocked turn does. Enforcement returns before startStreamingRequest,
// so no chunk ever passes through emitChunkElement: without this the caller
// received nothing at all until the whole message landed at once at the end of
// the turn, and a UI rendering text deltas showed an empty reply (#1716).
//
// The element is shaped exactly like the element a real final chunk produces
// (emitChunkElement), which is what keeps the reply delivered exactly once:
//
//   - Meta.StreamingDelta marks it as the live-text feed. A speech-out stage
//     skips deltas and speaks the complete Message (stages_speech.go,
//     stages_tts.go), so the canned reply is synthesized once; and
//     accumulateResult only folds text that arrives after the assistant message,
//     which this element precedes. Consumers that render deltas concatenate them
//     to exactly the Message content, as on any other turn.
//   - Meta.FinishReason carries the message's finish reason
//     (types.FinishReasonSafety), the element-level marker
//     PRE_LLM_GUARDRAILS_DESIGN.md §4.1.2 calls for. It mirrors how the duplex
//     stage marks an interrupted turn, and lets a consumer reading element
//     metadata rather than the message tell a policy block from a model reply.
func (s *ProviderStage) emitBlockedTurnText(
	ctx context.Context, blocked *types.Message, output chan<- StreamElement,
) error {
	if blocked == nil || blocked.Content == "" {
		return nil
	}
	elem := NewTextElement(blocked.Content)
	elem.Timestamp = timeNow()
	elem.Priority = PriorityNormal
	elem.Meta.StreamingDelta = true
	if blocked.FinishReason != "" {
		fr := blocked.FinishReason
		elem.Meta.FinishReason = &fr
	}
	return sendStreamElement(ctx, elem, output)
}

// applyEnforcedResponse folds an enforcing AfterCall decision into the round's
// response: the hook already rewrote the message (truncate or replace), so pick
// that up and stop the round loop. The pipeline itself continues — this
// response is still emitted and every downstream stage runs.
//
// Tool calls are dropped deliberately: the message goes into history, and an
// assistant message carrying tool calls with no matching tool results is a
// protocol error on the next call. Clearing the caller's toolCalls makes the
// round report hasToolCalls=false, which terminates the loop. The response is
// marked like a blocked tool call (preExecCheck) so consumers can tell the
// calls were dropped by policy.
func applyEnforcedResponse(
	responseMsg *types.Message,
	toolCalls *[]types.MessageToolCall,
	hookResp *hooks.ProviderResponse,
	d hooks.Decision,
) {
	responseMsg.Content = hookResp.Message.Content
	responseMsg.Validations = append(responseMsg.Validations, hookResp.Message.Validations...)
	responseMsg.ToolCalls = nil
	*toolCalls = nil
	responseMsg.FinishReason = types.FinishReasonSafety
	if len(d.Metadata) > 0 {
		// Meta is map[string]interface{} (types/message.go:39). Copy rather than
		// alias the decision's map so a hook cannot mutate an already-recorded
		// message afterwards (mirrors blockedMessage).
		responseMsg.Meta = make(map[string]interface{}, len(d.Metadata))
		for k, v := range d.Metadata {
			responseMsg.Meta[k] = v
		}
	}
}

// recordGuardrailFiring stamps a guardrail firing onto msg.Validations and
// emits the validation event. Shared by the before-call and after-call phases
// so an input firing is exactly as observable as an output firing — the
// guardrail_triggered assertion reads msg.Validations via
// EvalContext.PriorResults.
func (s *ProviderStage) recordGuardrailFiring(
	msg *types.Message, d hooks.Decision, direction string, duration time.Duration,
) {
	vType, _ := d.Metadata["validator_type"].(string)

	if msg != nil {
		if v, ok := guardrailValidation(d, direction); ok {
			msg.Validations = append(msg.Validations, v)
		}
	}

	if s.emitter == nil {
		return
	}
	data := &events.ValidationEventData{
		ValidatorName: vType,
		ValidatorType: vType,
		Direction:     direction,
		Duration:      duration,
		Enforced:      d.Enforced,
		Score:         metadataScore(d.Metadata),
	}
	if !d.Allow {
		data.Violations = []string{d.Reason}
	}
	s.emitter.GuardrailResult(data)
}

// guardrailValidation builds the ValidationResult recorded for a guardrail
// firing. Every path that records a firing goes through here — the before/after
// call phases via recordGuardrailFiring, and the streaming chunk interceptor,
// which cannot use recordGuardrailFiring for the stamp because no message
// exists yet at that point. Sharing the construction is what keeps a streaming
// firing shaped like every other one: same direction tag, same timestamp, same
// copied details map (§4.5 observability parity).
//
// ok is false when the decision carries no validator_type — there is no
// validation to record without a validator identity.
//
// The details map is copied, never aliased: a hook that holds a reference to
// the metadata map it returned must not be able to mutate an already-recorded
// validation afterwards (mirrors blockedMessage and applyEnforcedResponse).
func guardrailValidation(d hooks.Decision, direction string) (types.ValidationResult, bool) {
	vType, _ := d.Metadata["validator_type"].(string)
	if vType == "" {
		return types.ValidationResult{}, false
	}

	details := make(map[string]any, len(d.Metadata)+1)
	for k, v := range d.Metadata {
		details[k] = v
	}
	// Details is public API via sdk.Response.Validations(), and the guardrail
	// adapter stores evals.EvalResult.Score — a *float64. Copied verbatim, a
	// consumer doing Details["score"].(float64) gets 0, false, and the map
	// renders as an address. Dereference to the same plain float64 the event
	// payload already carries via metadataScore. Any other shape (a
	// hand-written hook's own type) is left untouched.
	if _, isPtr := details["score"].(*float64); isPtr {
		details["score"] = metadataScore(d.Metadata)
	}
	details["direction"] = direction

	return types.ValidationResult{
		ValidatorType: vType,
		Passed:        false,
		Details:       details,
		Timestamp:     timeNow(),
	}, true
}

// metadataScore extracts a guardrail's eval score from decision metadata.
//
// The guardrail adapter stores evals.EvalResult.Score, which is a *float64, so
// a plain float64 type assertion silently yields 0 and every event under-reports
// the score. Accept both shapes: a hand-written hook may reasonably put a plain
// float64 there.
func metadataScore(metadata map[string]any) float64 {
	switch v := metadata["score"].(type) {
	case *float64:
		if v != nil {
			return *v
		}
	case float64:
		return v
	}
	return 0
}

// blockedMessage builds the assistant turn substituted for a provider call that
// an enforcing BeforeCall hook skipped.
//
// This mirrors preExecCheck's handling of a blocked tool call: substitute a
// well-formed result of the same shape, mark it with a typed indicator, attach
// the decision metadata, and let the caller return normally so the pipeline
// continues. Only the provider work is skipped — downstream stages still run.
//
// Text resolution order: the hook's req.Replacement, then
// Decision.Metadata["replacement"] (the only channel an exec hook has), then
// the default blocked message.
func (s *ProviderStage) blockedMessage(
	req *hooks.ProviderRequest, d hooks.Decision,
) types.Message {
	content := ""
	if req != nil {
		content = req.Replacement
	}
	if content == "" {
		if m, ok := d.Metadata["replacement"].(string); ok {
			content = m
		}
	}
	if content == "" {
		content = prompt.DefaultBlockedMessage
	}

	msg := types.Message{
		Role:         roleAssistant,
		Content:      content,
		Timestamp:    timeNow(),
		FinishReason: types.FinishReasonSafety,
	}
	if len(d.Metadata) > 0 {
		// Meta is map[string]interface{} (types/message.go:39). Copy rather
		// than alias the decision's map so a hook cannot mutate the recorded
		// message afterwards.
		msg.Meta = make(map[string]interface{}, len(d.Metadata))
		for k, v := range d.Metadata {
			msg.Meta[k] = v
		}
	}
	return msg
}

// buildPendingResult creates a toolCallResult for a pending tool execution.
// It emits a tool.client.request event so observers know a client tool is
// awaiting fulfillment, and a tool.call.completed with status "pending" so
// every tool.call.started has a matching completion.
func (s *ProviderStage) buildPendingResult(
	ctx context.Context, toolCall types.MessageToolCall, asyncResult *tools.ToolExecutionResult,
) toolCallResult {
	var argsMap map[string]any
	if toolCall.Args != nil {
		_ = json.Unmarshal(toolCall.Args, &argsMap)
	}

	// Emit client tool request event with consent/category metadata
	if s.emitter != nil {
		reqData := &events.ClientToolRequestData{
			CallID:   toolCall.ID,
			ToolName: toolCall.Name,
			Args:     argsMap,
		}
		if asyncResult.PendingInfo != nil {
			reqData.ConsentMsg = asyncResult.PendingInfo.Message
			if cats, ok := asyncResult.PendingInfo.Metadata["categories"].([]string); ok {
				reqData.Categories = cats
			}
		}
		s.emitter.ClientToolRequest(reqData)
	}

	// Emit tool.call.completed with status "pending" so the started event is paired
	if s.emitter != nil {
		labels := s.toolLabels(toolCall.Name)
		s.emitter.ToolCallCompletedCtx(ctx, toolCall.Name, toolCall.ID, 0, "pending", nil, labels)
	}

	toolResult := s.handleToolResult(toolCall, asyncResult)
	return toolCallResult{
		pending: &tools.PendingToolExecution{
			CallID:      toolCall.ID,
			ToolName:    toolCall.Name,
			Args:        argsMap,
			PendingInfo: asyncResult.PendingInfo,
			ToolResult:  toolResult,
		},
	}
}

// runAfterToolHooks runs AfterExecution hooks if a hook registry is configured.
func (s *ProviderStage) runAfterToolHooks(
	ctx context.Context,
	toolCall types.MessageToolCall,
	result types.MessageToolResult,
	startTime time.Time,
) {
	if s.hookRegistry == nil {
		return
	}
	toolReq := hooks.ToolRequest{
		Name: toolCall.Name, Args: toolCall.Args, CallID: toolCall.ID,
	}
	toolResp := hooks.ToolResponse{
		Name:      toolCall.Name,
		CallID:    toolCall.ID,
		Content:   result.GetTextContent(),
		Error:     result.Error,
		LatencyMs: time.Since(startTime).Milliseconds(),
	}
	s.hookRegistry.RunAfterToolExecution(ctx, toolReq, toolResp)
}

// handleToolResult converts tool execution result to MessageToolResult
func (s *ProviderStage) handleToolResult(
	call types.MessageToolCall,
	asyncResult *tools.ToolExecutionResult,
) types.MessageToolResult {
	switch asyncResult.Status {
	case tools.ToolStatusPending:
		// Tool is held pending external resolution (HITL approval or a client-mode
		// tool awaiting fulfillment). The call is surfaced via ErrToolsPending /
		// PendingTools metadata and the pipeline suspends; this placeholder result
		// is only used if the pending execution is inspected before it resolves.
		pendingMsg := asyncResult.PendingInfo.Message
		if pendingMsg == "" {
			pendingMsg = fmt.Sprintf("Tool %s is awaiting approval", call.Name)
		}
		return types.NewTextToolResult(call.ID, call.Name, pendingMsg)

	case tools.ToolStatusFailed:
		failResult := types.NewTextToolResult(
			call.ID, call.Name,
			fmt.Sprintf("Tool execution failed: %s", asyncResult.Error),
		)
		failResult.Error = asyncResult.Error
		return failResult

	case tools.ToolStatusComplete:
		// Tool completed successfully
		content := string(asyncResult.Content)

		// Try to format nicely if it's JSON
		var resultValue interface{}
		if json.Unmarshal(asyncResult.Content, &resultValue) == nil {
			content = formatToolResult(resultValue)
		}

		// Enforce tool result size limit
		content = s.enforceResultSizeLimit(call.Name, content)

		// If the executor returned multimodal parts, propagate them directly;
		// otherwise wrap the text content as a single text ContentPart (legacy path).
		if len(asyncResult.Parts) > 0 {
			return types.MessageToolResult{
				ID:    call.ID,
				Name:  call.Name,
				Parts: asyncResult.Parts,
			}
		}

		return types.NewTextToolResult(call.ID, call.Name, content)

	default:
		unknownMsg := fmt.Sprintf("Unknown tool status: %v", asyncResult.Status)
		unknownResult := types.NewTextToolResult(call.ID, call.Name, unknownMsg)
		unknownResult.Error = unknownMsg
		return unknownResult
	}
}

// enforceResultSizeLimit truncates the tool result content if it exceeds the
// configured maximum size from the tool registry.
func (s *ProviderStage) enforceResultSizeLimit(toolName, content string) string {
	if s.toolRegistry == nil {
		return content
	}
	maxSize := s.toolRegistry.MaxToolResultSize()
	if maxSize <= 0 {
		return content
	}
	size := len(content)
	if size <= maxSize {
		return content
	}
	logger.Warn("Tool result truncated",
		"tool", toolName,
		"size", size,
		"limit", maxSize,
	)
	truncated := content[:maxSize]
	return fmt.Sprintf(
		"%s\n... [truncated, %d bytes exceeded limit of %d bytes]",
		truncated, size, maxSize,
	)
}

// isToolBlocked checks if a tool is in the blocklist
func isToolBlocked(toolName string, blocklist []string) bool {
	for _, blocked := range blocklist {
		if blocked == toolName {
			return true
		}
	}
	return false
}

// formatToolResult formats tool result for display
func formatToolResult(value interface{}) string {
	switch v := value.(type) {
	case string:
		return v
	case map[string]interface{}:
		// Pretty print JSON objects
		bytes, err := json.MarshalIndent(v, "", "  ")
		if err != nil {
			return fmt.Sprintf("%v", v)
		}
		return string(bytes)
	case []interface{}:
		// Pretty print JSON arrays
		bytes, err := json.MarshalIndent(v, "", "  ")
		if err != nil {
			return fmt.Sprintf("%v", v)
		}
		return string(bytes)
	default:
		return fmt.Sprintf("%v", v)
	}
}

// updateExcludedTools increments rejection counts for tools whose results have
// errors and marks them for exclusion after the second rejection. Returns true
// if the excluded set changed (caller should rebuild provider tools).
func (s *ProviderStage) updateExcludedTools(
	results []types.Message,
	rejectionCounts map[string]int,
	excluded map[string]bool,
) bool {
	changed := false
	for i := range results {
		tr := results[i].ToolResult
		if tr == nil || tr.Error == "" {
			continue
		}
		rejectionCounts[tr.Name]++
		if rejectionCounts[tr.Name] > 1 && !excluded[tr.Name] {
			excluded[tr.Name] = true
			changed = true
			logger.Warn("Tool excluded after repeated rejection",
				"tool", tr.Name, "rejections", rejectionCounts[tr.Name])
		}
	}
	return changed
}

// buildProviderTools constructs the tool descriptors sent to the provider.
// Tools in the excluded set are omitted from the result.
// classifyToolError determines the ToolErrorType from an execution error.
func classifyToolError(err error) types.ToolErrorType {
	var valErr *tools.ValidationError
	if errors.As(err, &valErr) {
		return types.ToolErrorValidation
	}
	return types.ToolErrorExecution
}

// collectProviderDescriptors builds the deterministic tool-descriptor list for a
// provider call: pack-declared tools (allowedTools) plus capability tools
// (system-namespaced). Order MUST be stable across calls within a run — the
// tools array is part of the provider's cached prefix (Anthropic caches
// tools+system+first-message), and capability tools are gathered via IterateTools
// in Go map-iteration order, which is randomized. A varying order changes the
// cached-prefix bytes and busts prompt caching entirely, re-billing the full
// context at full price every round. Sorting by name keeps the prefix byte-stable
// so caching actually engages.
func (s *ProviderStage) collectProviderDescriptors(
	allowedTools []string, excluded map[string]bool,
) []*providers.ToolDescriptor {
	seen := make(map[string]bool)
	var descriptors []*providers.ToolDescriptor
	add := func(tool *tools.ToolDescriptor) {
		seen[tool.Name] = true
		descriptors = append(descriptors, &providers.ToolDescriptor{
			Name:        tool.Name,
			Description: tool.Description,
			InputSchema: tool.InputSchema,
		})
	}

	// 1. Pack-declared tools from the prompt's allowed list (slice order). An
	//    entry ending in "*" (e.g. mcp__server__*) is a wildcard expanded against
	//    the registry: MCP servers reveal their tool sets only at connection time,
	//    so authors can't enumerate exact names (issue #1578).
	for _, entry := range allowedTools {
		if strings.HasSuffix(entry, "*") {
			s.toolRegistry.IterateTools(func(name string, tool *tools.ToolDescriptor) {
				if seen[name] || excluded[name] || !tools.MatchToolPattern(entry, name) {
					return
				}
				add(tool)
			})
			continue
		}
		if excluded[entry] {
			continue
		}
		tool, err := s.toolRegistry.GetTool(entry)
		if err != nil {
			logger.Warn("Tool not found in registry", "tool", entry, "error", err)
			continue
		}
		add(tool)
	}

	// 2. Implicit capability tools (skill__, a2a__, workflow__, memory__, media),
	//    gathered in randomized map order — sorted below to make the whole set
	//    stable. MCP tools are NOT implicit: they surface only via an explicit
	//    allowed_tools entry in loop 1 above (issue #1578).
	s.toolRegistry.IterateTools(func(name string, tool *tools.ToolDescriptor) {
		if seen[name] || excluded[name] || !tools.IsImplicitTool(name) {
			return
		}
		add(tool)
	})

	sort.Slice(descriptors, func(i, j int) bool {
		return descriptors[i].Name < descriptors[j].Name
	})
	return descriptors
}

func (s *ProviderStage) buildProviderTools(
	allowedTools []string, excluded map[string]bool,
) (providerTools interface{}, toolChoice string, err error) {
	if s.toolRegistry == nil {
		return nil, "", nil
	}

	// Check if provider supports tools
	toolProvider, ok := s.provider.(providers.ToolSupport)
	if !ok {
		return nil, "", nil
	}

	// When tool calling is disabled for this turn (tool_choice: none), send no
	// tool declarations at all. The model cannot call them, so shipping the
	// declarations only wastes input tokens and — on some models — primes
	// spurious tool calls. Returning nil routes the provider to its non-tool
	// path, which omits both the declarations and any tool_config.
	if s.toolPolicy != nil && s.toolPolicy.ToolChoice == toolChoiceNone {
		return nil, "", nil
	}

	descriptors := s.collectProviderDescriptors(allowedTools, excluded)
	if len(descriptors) == 0 {
		return nil, "", nil
	}

	// Build provider-specific tools
	providerTools, err = toolProvider.BuildTooling(descriptors)
	if err != nil {
		return nil, "", fmt.Errorf("failed to build tools: %w", err)
	}

	// Determine tool choice from policy
	toolChoice = toolChoiceAuto // default
	if s.toolPolicy != nil && s.toolPolicy.ToolChoice != "" {
		toolChoice = s.toolPolicy.ToolChoice
	}

	return providerTools, toolChoice, nil
}
