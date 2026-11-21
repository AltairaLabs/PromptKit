package agui

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"

	"github.com/AltairaLabs/PromptKit/sdk/v2/internal/clienttool"

	aguievents "github.com/ag-ui-protocol/ag-ui/sdks/community/go/pkg/core/events"
	aguitypes "github.com/ag-ui-protocol/ag-ui/sdks/community/go/pkg/core/types"

	"github.com/AltairaLabs/PromptKit/runtime/v2/events"
	"github.com/AltairaLabs/PromptKit/runtime/v2/types"
	"github.com/AltairaLabs/PromptKit/sdk/v2"
)

// eventChannelBufferSize is the default buffer size for the AG-UI event channel.
const eventChannelBufferSize = 64

// interruptReasonToolCall is the interrupt reason used for an approval hold
// that carries no reason of its own. It is the category the AG-UI Go SDK
// documents for Interrupt.Reason.
const interruptReasonToolCall = "tool_call"

// ErrContinueUnsupported is returned by [EventAdapter.RunContinue] when the
// conversation cannot continue after an approval hold.
var ErrContinueUnsupported = errors.New("agui: conversation does not support Continue")

// Sender abstracts the conversation methods needed by the adapter. In
// production code, *sdk.Conversation satisfies this interface.
type Sender interface {
	Send(ctx context.Context, message any, opts ...sdk.SendOption) (*sdk.Response, error)
	SendToolResult(ctx context.Context, callID string, result any) error
	RejectClientTool(ctx context.Context, callID, reason string)
	Resume(ctx context.Context) (*sdk.Response, error)
}

// continuer is implemented by conversations that can continue after their
// approval-held tool calls are resolved (*sdk.Conversation does).
type continuer interface {
	Continue(ctx context.Context) (*sdk.Response, error)
}

// multimodalResultSender is implemented by conversations that accept a tool
// result made of content parts (*sdk.Conversation does).
type multimodalResultSender interface {
	SendToolResultMultimodal(ctx context.Context, callID string, parts []types.ContentPart) error
}

// clientToolFailer is implemented by conversations that record a failed client
// tool as a failure (*sdk.Conversation does).
type clientToolFailer interface {
	FailClientTool(ctx context.Context, callID string, partial any, err error) error
}

// settlerOf is sender's clienttool.Settler, which prepares an answer's tool
// result when it is given, rewritten as the model will see it: the workflow
// adapter's, or an *sdk.Conversation's. Nil for a sender that has none.
func settlerOf(sender Sender) clienttool.Settler {
	if s, ok := sender.(clienttool.Settler); ok {
		return s
	}
	return clienttool.SettlerOf(sender)
}

// streamer is implemented by conversations that stream a turn as it runs
// (*sdk.Conversation does). RunSend drives one through it, so text reaches the
// client as the model produces it.
type streamer interface {
	Stream(ctx context.Context, message any, opts ...sdk.SendOption) <-chan sdk.StreamChunk
}

// resumeStreamer is implemented by conversations that stream a resumed turn
// (*sdk.Conversation does).
type resumeStreamer interface {
	ResumeStream(ctx context.Context) <-chan sdk.StreamChunk
}

// workflowStateReader is implemented by conversations that run a workflow
// state machine (*sdk.WorkflowConversation does).
type workflowStateReader interface {
	CurrentState() string
}

// ToolResultProvider is a callback the caller implements to supply results for
// pending client tools. The adapter calls it when the LLM response contains
// deferred client tools that need fulfillment before the pipeline can continue.
// It may answer some of the pending calls and leave the rest.
type ToolResultProvider func(ctx context.Context, tools []sdk.PendingClientTool) ([]ToolResult, error)

// ToolResult carries the caller-provided outcome for a single client tool call.
type ToolResult struct {
	CallID   string // must match PendingClientTool.CallID
	Result   any    // JSON-serializable, or []types.ContentPart; ignored when Rejected is true
	Rejected bool
	Reason   string // rejection reason (used when Rejected is true)
	// Error reports that the tool failed. Result, when also set, is the
	// partial output it produced before failing.
	Error string
}

// EventBusProvider abstracts access to the conversation's event bus. The
// adapter reads the bus only for [WithRuntimeEvents]: the run's messages, tool
// calls and steps come from the turn itself, which keeps them ordered.
type EventBusProvider interface {
	EventBus() events.Bus
}

// StateProvider produces the state the run keeps in sync with the client (see
// [WithStateProvider]). If Snapshot returns an error or a nil state, the run
// sends nothing for that read and carries on.
type StateProvider interface {
	Snapshot(sender Sender) (any, error)
}

// AdapterOption configures an EventAdapter.
type AdapterOption func(*adapterConfig)

type adapterConfig struct {
	threadID           string
	runID              string
	stateProvider      StateProvider
	workflowSteps      bool
	toolResultProvider ToolResultProvider
	// runtimeEvents enables forwarding bus events as RAW events; eventTypes
	// limits it to those types (all types when empty).
	runtimeEvents bool
	eventTypes    []events.EventType
	// showReasoning sends the model's reasoning (WithReasoning).
	showReasoning bool
	// stateAfterEachTool reads the state after each tool result
	// (WithStateAfterEachTool), not only at the run's start and end.
	stateAfterEachTool bool
	// messagesSnapshot ends each run with a MESSAGES_SNAPSHOT of the
	// conversation (WithMessagesSnapshot).
	messagesSnapshot bool
	// clientHistory replaces the conversation's history before RunSend sends
	// (WithClientHistory), when clientHistorySet.
	clientHistory    []aguitypes.Message
	clientHistorySet bool
	// inputContext opens RunSend's message as a context block
	// (WithInputContext).
	inputContext []aguitypes.Context
	// frontendTools are offered to the model for the run (WithFrontendTools).
	frontendTools []aguitypes.Tool
}

// rawEventSource is the RAW event source naming PromptKit's runtime events.
const rawEventSource = "promptkit"

// WithThreadID sets the AG-UI thread ID for emitted events. By default the
// adapter generates one.
func WithThreadID(id string) AdapterOption {
	return func(c *adapterConfig) {
		c.threadID = id
	}
}

// WithRunID sets the AG-UI run ID for emitted events. By default the adapter
// generates one.
func WithRunID(id string) AdapterOption {
	return func(c *adapterConfig) {
		c.runID = id
	}
}

// WithStateProvider sets a provider of the state the run keeps in sync with
// the client. The run sends the state as a STATE_SNAPSHOT at its start, and
// reads it again once the turn has run, before RUN_FINISHED, sending the
// change as a STATE_DELTA: an RFC 6902 JSON Patch against the state the
// client holds. A change a patch cannot carry (one replacing the whole state,
// or setting a value to null, which the AG-UI Go SDK cannot encode) is sent
// as a new STATE_SNAPSHOT. Snapshot is never called while the turn runs; see
// [WithStateAfterEachTool] to send changes as each tool's result arrives.
func WithStateProvider(sp StateProvider) AdapterOption {
	return func(c *adapterConfig) {
		c.stateProvider = sp
	}
}

// WithStateAfterEachTool sets whether the run also reads the StateProvider
// after each TOOL_CALL_RESULT, sending the change a tool made as it lands
// rather than when the turn ends. Off by default.
//
// The turn is still running then: its next tool calls can be executing, so
// Snapshot is called concurrently with the tools that change the state, and
// must be safe for that (guard the state with a lock the tools take too).
func WithStateAfterEachTool(enabled bool) AdapterOption {
	return func(c *adapterConfig) {
		c.stateAfterEachTool = enabled
	}
}

// WithWorkflowSteps enables STEP_STARTED / STEP_FINISHED events naming the
// workflow state a run executes in. It has an effect only when the
// conversation runs a workflow, as one passed to [NewWorkflowEventAdapter]
// does; [NewWorkflowEventAdapter] enables it by default.
func WithWorkflowSteps(enabled bool) AdapterOption {
	return func(c *adapterConfig) {
		c.workflowSteps = enabled
	}
}

// WithToolResultProvider sets a callback that answers pending client tools on
// the server, inside the run. When the callback answers every pending call,
// the adapter resolves them, emits a TOOL_CALL_RESULT for each, and resumes
// the turn. Calls it leaves unanswered stay pending: the run finishes with
// them unanswered, as AG-UI requires for a frontend tool, and the application
// answers them in the next run (see [EventAdapter.RunResume]). The answers it
// gave are reported before the run finishes; when the conversation has a
// ToolResultRewriter, each is reported as the model will be given it,
// rewritten.
//
// Use it only for tools the server itself can answer. A frontend tool, one the
// application advertised in RunAgentInput.tools, must not be answered by the
// producer: a call to a tool listed in [WithFrontendTools] is never handed to
// the callback, and the run ends with it pending.
func WithToolResultProvider(provider ToolResultProvider) AdapterOption {
	return func(c *adapterConfig) {
		c.toolResultProvider = provider
	}
}

// WithRuntimeEvents forwards the conversation's runtime events, read from its
// event bus ([EventBusProvider]), as AG-UI RAW events with source "promptkit"
// and the [events.Event] as the payload. Name event types to forward only
// those (for example events.EventToolCallStarted); with none, every event is
// forwarded. By default no runtime event is forwarded.
//
// The bus delivers asynchronously, so a RAW event is not ordered against the
// run's other events, and only events delivered while the run is open are
// forwarded: one the bus delivers after the run's RUN_FINISHED or RUN_ERROR is
// not sent. For a workflow conversation the bus is the active conversation's
// when the run starts. A bus shared by several conversations (sdk.WithEventBus)
// carries all of their events; filter on the payload's SessionID or
// ConversationID to keep one conversation's.
func WithRuntimeEvents(eventTypes ...events.EventType) AdapterOption {
	return func(c *adapterConfig) {
		c.runtimeEvents = true
		c.eventTypes = append([]events.EventType(nil), eventTypes...)
	}
}

// WithReasoning sets whether the run sends the model's reasoning as AG-UI
// REASONING_* events, for a provider configured to return its reasoning (a
// Claude thinking budget, Gemini's include_thoughts, OpenAI's
// reasoning_summary). Off by default: reasoning routinely holds what the
// model chose not to say, so the client gets it only when asked for. With it
// off, reasoning is also left out of the RAW events WithRuntimeEvents
// forwards; with it on, those carry the reasoning's text but never its opaque
// tokens.
//
// Only the reasoning's text is sent. A reply an output guardrail blocks keeps
// no reasoning, so a settled reply never sends the reasoning behind it. But
// reasoning that streams (sdk.WithStreamReasoning) goes out as the model
// produces it, before the output guardrails judge the reply it leads to:
// enable streamed reasoning only for a client that may see what a guardrail
// would block.
func WithReasoning(enabled bool) AdapterOption {
	return func(c *adapterConfig) {
		c.showReasoning = enabled
	}
}

// WithMessagesSnapshot sets whether each run ends with a MESSAGES_SNAPSHOT,
// immediately before RUN_FINISHED, restating the conversation's history as the agent
// holds it: for a client reconnecting to the thread, or one whose copy has
// drifted from the agent's. Off by default.
//
// The client replaces its messages with the snapshot's. Each message is keyed
// by its stable id (types.MetaMessageID), which a later run's snapshot gives
// it again: the id the client gave it, for a message that came from the
// client, or the one the conversation gave it when storing it. A message
// stored before ids were assigned is keyed by the thread id and its position
// in the history ("<thread>-0", "<thread>-1", ...), stable across runs only
// when the thread id is: set it with [WithThreadID] from the input's
// threadId. The messages the run streamed under ids of their own are replaced
// too. The history keeps no reasoning: with [WithReasoning]
// on, the snapshot restates the reasoning of the run's own turns, as reasoning
// messages before the assistant messages they led to, which AG-UI reads as
// the complete set of reasoning messages. A run whose history reads back
// empty, or shorter than the run's own turn (a store that failed or does not
// keep it), sends no snapshot rather than one that would clear the client. It
// needs a conversation that exposes its history (*sdk.Conversation does); for
// any other the option does nothing.
func WithMessagesSnapshot(enabled bool) AdapterOption {
	return func(c *adapterConfig) {
		c.messagesSnapshot = enabled
	}
}

// EventAdapter bridges a PromptKit conversation to an AG-UI event channel.
// It runs one conversation turn and translates the turn's messages into
// AG-UI protocol events. An adapter carries one run: its channel closes when
// the run ends.
type EventAdapter struct {
	sender Sender
	bus    EventBusProvider
	cfg    adapterConfig
	events chan aguievents.Event
	once   sync.Once
	mu     sync.RWMutex
	closed bool
}

// NewEventAdapter creates a new EventAdapter for the given conversation.
// The conversation must implement both Sender and EventBusProvider.
// *sdk.Conversation satisfies both interfaces.
func NewEventAdapter(conv interface {
	Sender
	EventBusProvider
}, opts ...AdapterOption,
) *EventAdapter {
	return newAdapter(conv, conv, opts...)
}

// NewWorkflowEventAdapter creates an EventAdapter for a workflow conversation.
// Each run opens a step named after the workflow state it executes in, and a
// transition the turn commits finishes that step and starts the next. Pass
// WithWorkflowSteps(false) to leave the steps out.
//
// Client-tool results and approval resolutions go to the workflow's active
// conversation, the one serving the current state.
func NewWorkflowEventAdapter(wc *sdk.WorkflowConversation, opts ...AdapterOption) *EventAdapter {
	ws := &workflowSender{wc: wc}
	return newAdapter(ws, ws, append([]AdapterOption{WithWorkflowSteps(true)}, opts...)...)
}

// newAdapter creates an adapter from separate sender and event bus provider.
// The bus is read only for WithRuntimeEvents.
func newAdapter(sender Sender, bus EventBusProvider, opts ...AdapterOption) *EventAdapter {
	cfg := adapterConfig{
		threadID: aguievents.GenerateThreadID(),
		runID:    aguievents.GenerateRunID(),
	}
	for _, opt := range opts {
		opt(&cfg)
	}
	return &EventAdapter{
		sender: sender,
		bus:    bus,
		cfg:    cfg,
		events: make(chan aguievents.Event, eventChannelBufferSize),
	}
}

// Events returns the read-only channel of AG-UI events. The channel is closed
// when the run ends, successfully or with an error.
//
// Drain it concurrently with RunSend, RunResume or RunContinue, for example
// from another goroutine. The adapter never drops an event. When the channel's
// buffer is full it waits for the reader, so a reader that stops reading holds
// the run until the run's context is canceled; cancel it (an HTTP handler's
// request context is canceled when the client disconnects) to release the run.
func (a *EventAdapter) Events() <-chan aguievents.Event {
	return a.events
}

// ThreadID returns the thread ID used by this adapter.
func (a *EventAdapter) ThreadID() string {
	return a.cfg.threadID
}

// RunID returns the run ID used by this adapter.
func (a *EventAdapter) RunID() string {
	return a.cfg.runID
}

// RunSend sends a message through the conversation and emits the run's AG-UI
// events.
//
// Event sequence on success:
//  1. RUN_STARTED
//  2. STATE_SNAPSHOT (if a StateProvider is configured)
//  3. STEP_STARTED for the current workflow state (workflow conversations)
//  4. For each round of the turn (an assistant message and the tool results
//     after it), in order: with WithReasoning(true), the round's reasoning
//     (REASONING_START, REASONING_MESSAGE_START, a REASONING_MESSAGE_CONTENT
//     for each chunk, REASONING_MESSAGE_END, REASONING_END) as the model
//     streams it; TEXT_MESSAGE_START, a TEXT_MESSAGE_CONTENT for each chunk of
//     text as the model streams it, TEXT_MESSAGE_END; then TOOL_CALL_START /
//     TOOL_CALL_ARGS / TOOL_CALL_END for each call it made; then a
//     TOOL_CALL_RESULT for each result the round fed back to the model, each
//     followed by a STATE_DELTA when the state changed and
//     WithStateAfterEachTool is set
//  5. With WithReasoning(true), the reasoning of the streamed rounds that did
//     not stream it, whole
//  6. STEP_FINISHED / STEP_STARTED when the turn moved the workflow on
//  7. If a ToolResultProvider answers the pending client tools: a
//     TOOL_CALL_RESULT for each, then the resumed turn from step 4
//  8. STATE_DELTA when the state changed
//  9. STEP_FINISHED for the open step
//  10. MESSAGES_SNAPSHOT (with WithMessagesSnapshot)
//  11. RUN_FINISHED, with the run's token usage
//
// Reasoning is sent only with WithReasoning(true). It streams for a
// conversation opened with sdk.WithStreamReasoning; otherwise each round's
// reasoning is sent whole (step 5). A turn that does not stream sends each
// round's reasoning before its text.
//
// A client tool call left pending ends the run with the call unanswered, and
// RUN_FINISHED's success outcome names it in pendingToolCallIds; the
// application answers it in the next run with [EventAdapter.RunResume]. An
// approval-held call (sdk.Conversation.OnToolAsync) ends the run with
// RUN_FINISHED carrying an interrupt outcome that names the held calls; the
// next run continues with [EventAdapter.RunContinue].
//
// A conversation that streams (*sdk.Conversation, through
// sdk.Conversation.Stream) sends each round's text as it arrives, so the
// client shows the reply while the model writes it, and each round's calls as
// soon as the round's reply has streamed, before they run. The deltas of a
// message make up its text. One that does not (a workflow conversation, or
// any other Sender) runs the turn with Send, and each message arrives as one
// TEXT_MESSAGE_CONTENT once the turn has run.
//
// A reply that ends differently from what streamed, as when a guardrail
// replaces it, is sent as a new text message holding the reply: the client has
// already shown the streamed text, which AG-UI cannot take back.
//
// Before the model is called, the run offers the input's frontend tools
// ([WithFrontendTools]), replaces the conversation's history with the
// client's ([WithClientHistory]) and opens msg with the input's context
// ([WithInputContext]), each only when its option is set.
//
// On error, a RUN_ERROR ends the run, with the token usage of the model calls
// made before the error. The events channel is always closed when RunSend
// returns.
//
// Read Events() concurrently with the run, or cancel ctx: the run waits for
// the reader once the channel's buffer of 64 events is full, so a caller that
// lets the run finish before reading blocks there.
func (a *EventAdapter) RunSend(ctx context.Context, msg *types.Message) error {
	return a.run(ctx, false, func(ctx context.Context, st *runState) (*sdk.Response, error) {
		opts, err := a.runInputOptions()
		if err != nil {
			return nil, err
		}
		sent := a.withInputContext(msg)
		if s, ok := a.sender.(streamer); ok {
			return st.consume(s.Stream(ctx, sent, opts...))
		}
		return a.sender.Send(ctx, sent, opts...)
	})
}

// RunResume answers the client tool calls a previous run left pending and
// emits the continued turn as a new run.
//
// Read Events() concurrently with the run, or cancel ctx: the run waits for
// the reader once the channel's buffer of 64 events is full, so a caller that
// lets the run finish before reading blocks there.
//
// In AG-UI the application answers a frontend tool call in the next run's
// input, as a tool message per call; [ToolResultsFromAGUI] extracts them. The
// answers are the application's own, so the run does not echo them back as
// TOOL_CALL_RESULT events. Nor does it repeat answers a ToolResultProvider
// gave in the earlier run, which reported them then: the resumed turn feeds
// every answer to the model, but none of them is new to the client.
//
// A conversation that streams a resumed turn (sdk.Conversation.ResumeStream)
// sends it as RunSend sends a streamed turn.
func (a *EventAdapter) RunResume(ctx context.Context, results []ToolResult) error {
	return a.run(ctx, true, func(ctx context.Context, st *runState) (*sdk.Response, error) {
		if err := a.resolveClientTools(ctx, results); err != nil {
			return nil, err
		}
		if s, ok := a.sender.(resumeStreamer); ok {
			return st.consume(s.ResumeStream(ctx))
		}
		return a.sender.Resume(ctx)
	})
}

// RunContinue continues a turn whose approval-held tool calls were resolved
// (sdk.Conversation.ResolveTool / RejectTool) and emits it as a new run.
//
// Read Events() concurrently with the run, or cancel ctx: the run waits for
// the reader once the channel's buffer of 64 events is full, so a caller that
// lets the run finish before reading blocks there.
//
// The held calls' results are emitted as TOOL_CALL_RESULT events, since only
// the agent knows them.
//
// It returns [ErrContinueUnsupported] (after a RUN_ERROR) when the
// conversation has no Continue method.
func (a *EventAdapter) RunContinue(ctx context.Context) error {
	return a.run(ctx, false, func(ctx context.Context, _ *runState) (*sdk.Response, error) {
		c, ok := a.sender.(continuer)
		if !ok {
			return nil, ErrContinueUnsupported
		}
		return c.Continue(ctx)
	})
}

// runState tracks what one run has emitted.
type runState struct {
	a   *EventAdapter
	ctx context.Context
	// answered holds the calls of the current round that already have a
	// result emitted in this run. A round is an assistant message and the tool
	// results after it; call ids are only unique within one (Gemini numbers
	// them call_0, call_1, ... per response), so each round has its own set.
	answered map[string]bool
	// rounds are the rounds of the turn being streamed, as they were emitted
	// live; the turn's response settles them (emitTurn).
	rounds   []*liveRound
	step     string // the open step, when stepOpen
	stepOpen bool
	// runtime forwards bus events while the run is open (WithRuntimeEvents).
	runtime *runtimeForwarder
	// state is the state the client holds (WithStateProvider).
	state stateSync
	// usage totals the token usage of the run's model calls.
	usage runUsage
	// turn holds the messages the run's turns produced, in order.
	turn []types.Message
	// lateReasoning is the reasoning of the streamed rounds that did not
	// stream it, held until the turn's text is complete.
	lateReasoning []lateReasoning
}

// run emits one AG-UI run around start, which produces the turn's response.
func (a *EventAdapter) run(
	ctx context.Context,
	resuming bool,
	start func(ctx context.Context, st *runState) (*sdk.Response, error),
) error {
	defer a.closeEvents()

	st := &runState{a: a, ctx: ctx, answered: map[string]bool{}}

	st.emit(aguievents.NewRunStartedEvent(a.cfg.threadID, a.cfg.runID))
	st.runtime = a.forwardRuntimeEvents(st)
	defer st.runtime.stop()
	st.syncState()
	st.syncStep()

	resp, err := start(ctx, st)
	if err != nil {
		// A stream that failed part-way leaves its message open; it ends
		// before the error, so the client knows the text it has is all of it.
		st.closeStreamedText()
		st.finish(st.runError(err))
		return err
	}

	// A resumed turn opens with the answers to the calls the earlier run left
	// pending. The client already has every one of them, from its own input or
	// from the earlier run's TOOL_CALL_RESULT, so none is emitted.
	skipLeadingResults := resuming
	for {
		st.usage.addResponse(resp)
		st.turn = append(st.turn, resp.TurnMessages()...)
		st.emitTurn(resp, skipLeadingResults)
		skipLeadingResults = false
		st.syncStep()
		// A call held for approval cannot be answered here, and resuming
		// without its result would send the model a tool call with no
		// result. The run ends with the hold's interrupt; the client calls
		// stay pending for the application.
		if !resp.HasPendingClientTools() || a.cfg.toolResultProvider == nil || len(resp.PendingTools()) > 0 {
			break
		}
		next, err := st.fulfillAndResume(resp.ClientTools())
		if err != nil {
			st.finish(st.runError(err))
			return err
		}
		if next == nil {
			break // calls left pending for the application
		}
		resp = next
	}

	st.syncState()
	st.closeStep()
	st.emitMessagesSnapshot()
	st.finish(st.runFinished(resp))
	return nil
}

// runError builds the RUN_ERROR ending a failed run, with the usage of the
// model calls it made before failing.
func (st *runState) runError(err error) *aguievents.RunErrorEvent {
	var opts []aguievents.RunErrorOption
	if usage := st.usage.tokenUsage(); usage != nil {
		opts = append(opts, aguievents.WithErrorUsage(usage))
	}
	return aguievents.NewRunErrorEvent(err.Error(), opts...)
}

// finish emits the run's terminal event, after which no runtime event is
// forwarded.
func (st *runState) finish(event aguievents.Event) {
	st.runtime.stop()
	st.emit(event)
}

// runtimeForwarder sends the bus events one run receives as RAW events, until
// the run stops it.
type runtimeForwarder struct {
	st     *runState
	mu     sync.Mutex
	open   bool
	unsubs []func()
}

// forwardRuntimeEvents subscribes the run to the conversation's event bus when
// WithRuntimeEvents is set. The forwarder it returns is safe to stop when it
// subscribed to nothing.
func (a *EventAdapter) forwardRuntimeEvents(st *runState) *runtimeForwarder {
	f := &runtimeForwarder{st: st}
	if !a.cfg.runtimeEvents || a.bus == nil {
		return f
	}
	bus := a.bus.EventBus()
	if bus == nil {
		return f
	}
	f.open = true
	if len(a.cfg.eventTypes) == 0 {
		f.unsubs = append(f.unsubs, bus.SubscribeAll(f.forward))
		return f
	}
	for _, t := range a.cfg.eventTypes {
		f.unsubs = append(f.unsubs, bus.Subscribe(t, f.forward))
	}
	return f
}

// forward emits one bus event as a RAW event, while the run is open. The
// event is copied, since the bus hands the same one to every listener; its
// reasoning is left out unless the run shows reasoning (rawPayload).
func (f *runtimeForwarder) forward(e *events.Event) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if !f.open || e == nil {
		return
	}
	payload, ok := rawPayload(e, f.st.a.cfg.showReasoning)
	if !ok {
		return
	}
	f.st.emit(aguievents.NewRawEvent(payload, aguievents.WithSource(rawEventSource)))
}

// rawPayload is the copy of e a RAW event carries, or false for an event that
// is not sent. Reasoning follows WithReasoning: with it off, the reasoning
// events are not sent and a created message carries no reasoning; with it on,
// they carry the reasoning's text alone. Opaque reasoning (signatures,
// encrypted or redacted blocks) is never sent.
func rawPayload(e *events.Event, showReasoning bool) (events.Event, bool) {
	payload := *e
	switch data := e.Data.(type) {
	case *events.ReasoningDeltaData:
		return payload, showReasoning
	case *events.ReasoningCompletedData:
		if !showReasoning {
			return payload, false
		}
		d := *data
		d.Trace = textOnly(data.Trace)
		if d.Trace == nil {
			return payload, false // opaque reasoning alone: nothing to show
		}
		payload.Data = &d
	case *events.MessageCreatedData:
		if data.Reasoning == nil {
			return payload, true
		}
		d := *data
		d.Reasoning = nil
		if showReasoning {
			d.Reasoning = textOnly(data.Reasoning)
		}
		payload.Data = &d
	}
	return payload, true
}

// textOnly is a reasoning trace's text, without its opaque tokens; nil when
// it has no text.
func textOnly(r *types.ReasoningTrace) *types.ReasoningTrace {
	if r == nil || r.Text == "" {
		return nil
	}
	return &types.ReasoningTrace{Text: r.Text}
}

// stop ends forwarding: it unsubscribes, and waits for a forward in progress,
// so nothing is forwarded once it returns. It is safe to call more than once.
func (f *runtimeForwarder) stop() {
	f.mu.Lock()
	f.open = false
	unsubs := f.unsubs
	f.unsubs = nil
	f.mu.Unlock()
	for _, unsub := range unsubs {
		unsub()
	}
}

// runFinished builds the RUN_FINISHED event, with the run's usage. A turn
// held for approval ends with the interrupt outcome naming each held call:
// AG-UI requires a run that stopped to ask for outside input to say so rather
// than report success. A turn that left client tool calls unanswered ends
// with the success outcome naming them (pendingToolCallIds), for the
// application to answer in the next run.
func (st *runState) runFinished(resp *sdk.Response) *aguievents.RunFinishedEvent {
	threadID, runID := st.a.cfg.threadID, st.a.cfg.runID
	var opts []aguievents.RunFinishedOption
	if usage := st.usage.tokenUsage(); usage != nil {
		opts = append(opts, aguievents.WithUsage(usage))
	}
	held := resp.PendingTools()
	if len(held) == 0 {
		if pending := st.unansweredCalls(resp); len(pending) > 0 {
			opts = append(opts, aguievents.WithOutcome(aguievents.RunFinishedOutcome{
				Type: aguievents.RunFinishedOutcomeTypeSuccess, PendingToolCallIDs: pending,
			}))
		}
		return aguievents.NewRunFinishedEventWithOptions(threadID, runID, opts...)
	}
	interrupts := make([]aguitypes.Interrupt, len(held))
	for i, pt := range held {
		reason := pt.Reason
		if reason == "" {
			reason = interruptReasonToolCall
		}
		interrupts[i] = aguitypes.Interrupt{
			ID:         pt.ID,
			Reason:     reason,
			Message:    pt.Message,
			ToolCallID: pt.ID,
		}
	}
	opts = append(opts, aguievents.WithOutcome(
		aguievents.RunFinishedOutcome{Type: aguievents.RunFinishedOutcomeTypeInterrupt, Interrupts: interrupts},
	))
	return aguievents.NewRunFinishedEventWithOptions(threadID, runID, opts...)
}

// unansweredCalls names the client tool calls resp left pending that the run
// did not answer, in the order the model made them.
func (st *runState) unansweredCalls(resp *sdk.Response) []string {
	var ids []string
	for _, ct := range resp.ClientTools() {
		if !st.answered[ct.CallID] {
			ids = append(ids, ct.CallID)
		}
	}
	return ids
}

// emitTurn emits the messages a turn produced that it has not emitted live,
// in the order the turn produced them. With skipLeadingResults, the tool
// results before the turn's first assistant message are not emitted.
func (st *runState) emitTurn(resp *sdk.Response, skipLeadingResults bool) {
	msgs := resp.TurnMessages()
	if len(msgs) == 0 {
		msgs = fallbackTurn(resp)
	}
	assistants := 0
	for i := range msgs {
		switch msgs[i].Role {
		case roleAssistant:
			st.settleRound(assistants, &msgs[i])
			assistants++
		case roleTool:
			if skipLeadingResults && assistants == 0 {
				continue
			}
			st.emitToolMessage(&msgs[i])
		}
	}
	st.closeStreamedText()
	for _, lr := range st.lateReasoning {
		st.settleReasoning(lr.round, lr.trace)
	}
	st.lateReasoning = nil
}

// lateReasoning is the reasoning of a streamed round, reported after the
// turn's text.
type lateReasoning struct {
	round *liveRound
	trace *types.ReasoningTrace
}

// closeStreamedText ends any text message the streamed rounds left open, and
// forgets the rounds.
func (st *runState) closeStreamedText() {
	for _, r := range st.rounds {
		st.closeReasoning(r)
		st.closeText(r)
	}
	st.rounds = nil
}

// fallbackTurn reconstructs a one-message turn for a response that carries no
// turn messages, from its final text, its tool calls and its pending client
// tools.
func fallbackTurn(resp *sdk.Response) []types.Message {
	msg := types.Message{Role: roleAssistant, Content: resp.Text()}
	if m := resp.Message(); m != nil {
		msg.Reasoning = m.Reasoning
	}
	msg.ToolCalls = append(msg.ToolCalls, resp.ToolCalls()...)
	seen := make(map[string]bool, len(msg.ToolCalls))
	for _, tc := range msg.ToolCalls {
		seen[tc.ID] = true
	}
	for _, ct := range resp.ClientTools() {
		if seen[ct.CallID] {
			continue
		}
		var args json.RawMessage
		if len(ct.Args) > 0 {
			args, _ = json.Marshal(ct.Args)
		}
		msg.ToolCalls = append(msg.ToolCalls, types.MessageToolCall{ID: ct.CallID, Name: ct.ToolName, Args: args})
	}
	return []types.Message{msg}
}

// settleRound emits what the turn's index'th assistant message, msg, adds to
// the round streamed for it, or the whole round when none was: the rest of
// its text and its tool calls. A message that only calls tools emits no text
// message; its calls still name it as their parent.
func (st *runState) settleRound(index int, msg *types.Message) {
	var r *liveRound
	if index < len(st.rounds) {
		r = st.rounds[index]
		st.answered = r.answered
		// A streamed round whose reasoning did not stream reports it once
		// the turn's text is complete (emitTurn), not inside a later round's
		// open text.
		st.closeReasoning(r)
		if msg.Reasoning != nil {
			st.lateReasoning = append(st.lateReasoning, lateReasoning{r, msg.Reasoning})
		}
	} else {
		r = st.newRound()
		// A round that did not stream reports its reasoning before its text.
		st.settleReasoning(r, msg.Reasoning)
	}
	st.settleText(r, msg.GetContent(), len(msg.ToolCalls) == 0)
	if r.called {
		return
	}
	for i := range msg.ToolCalls {
		st.emitCall(r.msgID, &msg.ToolCalls[i])
	}
	r.called = true
}

// emitToolMessage emits the result a tool message carries, unless its call was
// already answered in this round.
func (st *runState) emitToolMessage(msg *types.Message) {
	if msg.ToolResult == nil {
		return
	}
	st.emitResult(msg.ToolResult.ID, toolResultText(msg.ToolResult))
}

func (st *runState) emitResult(callID, content string) {
	if callID == "" || st.answered[callID] {
		return
	}
	st.answered[callID] = true
	st.emit(aguievents.NewToolCallResultEvent(aguievents.GenerateMessageID(), callID, content))
	// A tool is what usually changes the state: with WithStateAfterEachTool
	// the client sees the change with the result.
	if st.a.cfg.stateAfterEachTool {
		st.syncState()
	}
}

// TOOL_CALL_RESULT content is never empty: the AG-UI Go SDK rejects an event
// whose content is, so a Go consumer could not decode the stream. A result
// with no text is described instead: by its error, by the media it holds
// ("[image/png image]"), or by the JSON encoding of its value ("null" for no
// value, "\"\"" for the empty string).

// toolResultText is the TOOL_CALL_RESULT content for a tool result: its text;
// with none, its error, a description of its media, or the empty string as
// JSON.
func toolResultText(r *types.MessageToolResult) string {
	if text := r.GetTextContent(); text != "" {
		return text
	}
	if r.Error != "" {
		return r.Error
	}
	if r.HasMedia() {
		return describeMedia(r.Parts)
	}
	return `""`
}

// describeMedia names each media part, as "[<mime type> <kind>]".
func describeMedia(parts []types.ContentPart) string {
	var names []string
	for _, p := range parts {
		if p.Type == types.ContentTypeText {
			continue
		}
		mime := ""
		if p.Media != nil {
			mime = p.Media.MIMEType + " "
		}
		names = append(names, "["+mime+p.Type+"]")
	}
	return strings.Join(names, " ")
}

// fulfillAndResume asks the ToolResultProvider for the pending calls' results
// and resolves them. When every call is answered it resumes the turn and
// returns the resumed response; otherwise it returns nil, leaving the rest
// for the application to answer in the next run.
//
// A call to one of the run's frontend tools (WithFrontendTools) is never given
// to the provider: the application executes it, and answers it in the next
// run, so the run ends with it pending.
func (st *runState) fulfillAndResume(pending []sdk.PendingClientTool) (*sdk.Response, error) {
	a := st.a
	serverSide := make([]sdk.PendingClientTool, 0, len(pending))
	for i := range pending {
		if !a.isFrontendTool(pending[i].ToolName) {
			serverSide = append(serverSide, pending[i])
		}
	}
	if len(serverSide) == 0 {
		return nil, nil
	}
	results, err := a.cfg.toolResultProvider(st.ctx, serverSide)
	if err != nil {
		return nil, err
	}
	if !answersAll(pending, results) {
		return nil, st.reportAnswers(results)
	}
	if resolveErr := a.resolveClientTools(st.ctx, results); resolveErr != nil {
		return nil, resolveErr
	}
	resp, err := a.sender.Resume(st.ctx)
	if err != nil {
		return nil, err
	}
	// Each result as the model saw it, before the resumed turn's reply.
	seen := toolMessagesByCall(resp.TurnMessages())
	for i := range results {
		content, ok := seen[results[i].CallID]
		if !ok {
			content = resultText(&results[i])
		}
		st.emitResult(results[i].CallID, content)
	}
	return resp, nil
}

// reportAnswers resolves answers that leave calls pending and emits a
// TOOL_CALL_RESULT for each, before the turn resumes. With a
// ToolResultRewriter on the conversation, each answer is settled now, as the
// model will be given it (clienttool.Settler), and that is
// what is reported: rewritten once, never for a second answer to a call or
// one the call-rate limit refuses. Otherwise the answer is reported as given.
func (st *runState) reportAnswers(results []ToolResult) error {
	var settler clienttool.Settler
	if s := settlerOf(st.a.sender); s != nil && s.HasToolResultRewriters() {
		settler = s
	}
	for i := range results {
		if err := st.reportAnswer(settler, &results[i]); err != nil {
			return err
		}
	}
	return nil
}

// reportAnswer resolves one answer that leaves calls pending and emits its
// TOOL_CALL_RESULT. With settler (the conversation has a ToolResultRewriter),
// the answer is settled now and the result the model will be given is
// reported, or nothing for a second answer to a call. Otherwise the answer is
// recorded and reported as given.
func (st *runState) reportAnswer(settler clienttool.Settler, r *ToolResult) error {
	if settler == nil {
		if err := st.a.resolveClientTool(st.ctx, r); err != nil {
			return err
		}
		st.emitResult(r.CallID, resultText(r))
		return nil
	}
	var prepared types.MessageToolResult
	var answered bool
	var err error
	switch {
	case r.Rejected:
		prepared, answered, err = settler.SettleClientToolRejection(st.ctx, r.CallID, r.Reason)
	case r.Error != "":
		prepared, answered, err = settler.SettleClientTool(st.ctx, r.CallID, r.Result, errors.New(r.Error))
	default:
		prepared, answered, err = settler.SettleClientTool(st.ctx, r.CallID, r.Result, nil)
	}
	if err != nil {
		return err
	}
	if answered {
		st.emitResult(r.CallID, toolResultText(&prepared))
	}
	return nil
}

func answersAll(pending []sdk.PendingClientTool, results []ToolResult) bool {
	answered := make(map[string]bool, len(results))
	for _, r := range results {
		answered[r.CallID] = true
	}
	for _, p := range pending {
		if !answered[p.CallID] {
			return false
		}
	}
	return true
}

// toolMessagesByCall maps call ids to the results that open a resumed turn:
// the answers to the calls the turn was suspended on. It stops at the turn's
// first assistant message, since a later round can reuse a call id (Gemini
// numbers them call_0, call_1, ... per response).
func toolMessagesByCall(msgs []types.Message) map[string]string {
	out := map[string]string{}
	for i := range msgs {
		if msgs[i].Role == roleAssistant {
			break
		}
		if msgs[i].Role == roleTool && msgs[i].ToolResult != nil {
			out[msgs[i].ToolResult.ID] = toolResultText(msgs[i].ToolResult)
		}
	}
	return out
}

// resultText renders a caller-supplied result the way the conversation hands
// it to the model, for a TOOL_CALL_RESULT that has no tool message to copy.
func resultText(r *ToolResult) string {
	switch {
	case r.Rejected:
		return "Tool rejected: " + r.Reason
	case r.Error != "":
		return toolErrorText(r)
	default:
		return nonEmptyValueText(r.Result)
	}
}

// nonEmptyValueText is valueText, falling back when that is empty to a
// description of the value's media or its JSON encoding ("null", "\"\"").
func nonEmptyValueText(v any) string {
	if text := valueText(v); text != "" {
		return text
	}
	if parts, ok := v.([]types.ContentPart); ok && hasMedia(parts) {
		return describeMedia(parts)
	}
	data, err := json.Marshal(v)
	if err != nil || len(data) == 0 {
		return "null"
	}
	return string(data)
}

func valueText(v any) string {
	switch val := v.(type) {
	case nil:
		return ""
	case string:
		return val
	case json.RawMessage:
		return string(val)
	case []types.ContentPart:
		var sb strings.Builder
		for _, p := range val {
			if p.Type == types.ContentTypeText && p.Text != nil {
				sb.WriteString(*p.Text)
			}
		}
		return sb.String()
	default:
		data, err := json.Marshal(val)
		if err != nil {
			return fmt.Sprint(val)
		}
		return string(data)
	}
}

// toolErrorText is what the model is told about a failed tool: its partial
// output, encoded as JSON as the conversation encodes any result, then the
// error. It matches the text sdk.Conversation.FailClientTool records.
func toolErrorText(r *ToolResult) string {
	if r.Result != nil {
		if data, err := json.Marshal(r.Result); err == nil {
			return string(data) + "\n\nTool error: " + r.Error
		}
	}
	return "Tool error: " + r.Error
}

// resolveClientTools hands each result to the conversation.
func (a *EventAdapter) resolveClientTools(ctx context.Context, results []ToolResult) error {
	for i := range results {
		if err := a.resolveClientTool(ctx, &results[i]); err != nil {
			return err
		}
	}
	return nil
}

// resolveClientTool hands one result to the conversation.
func (a *EventAdapter) resolveClientTool(ctx context.Context, r *ToolResult) error {
	switch {
	case r.Rejected:
		a.sender.RejectClientTool(ctx, r.CallID, r.Reason)
		return nil
	case r.Error != "":
		return a.failClientTool(ctx, r)
	default:
		return a.sendResult(ctx, r)
	}
}

// failClientTool records a failed client tool: as a failure when the
// conversation supports it, otherwise as a result saying it failed.
func (a *EventAdapter) failClientTool(ctx context.Context, r *ToolResult) error {
	if f, ok := a.sender.(clientToolFailer); ok {
		return f.FailClientTool(ctx, r.CallID, r.Result, errors.New(r.Error))
	}
	return a.sender.SendToolResult(ctx, r.CallID, toolErrorText(r))
}

func (a *EventAdapter) sendResult(ctx context.Context, r *ToolResult) error {
	if parts, ok := r.Result.([]types.ContentPart); ok {
		if len(parts) == 0 {
			// Every part was dropped: there is nothing to send as parts.
			return a.sender.SendToolResult(ctx, r.CallID, nil)
		}
		if mm, ok := a.sender.(multimodalResultSender); ok {
			return mm.SendToolResultMultimodal(ctx, r.CallID, parts)
		}
		return a.sender.SendToolResult(ctx, r.CallID, valueText(parts))
	}
	return a.sender.SendToolResult(ctx, r.CallID, r.Result)
}

// syncStep keeps the open step in line with the workflow's current state:
// it finishes the step for a state the workflow has left and starts one for
// the state it is in.
func (st *runState) syncStep() {
	if !st.a.cfg.workflowSteps {
		return
	}
	wr, ok := st.a.sender.(workflowStateReader)
	if !ok {
		return
	}
	state := wr.CurrentState()
	if st.stepOpen && st.step == state {
		return
	}
	st.closeStep()
	if state == "" {
		return
	}
	st.emit(aguievents.NewStepStartedEvent(state))
	st.step, st.stepOpen = state, true
}

func (st *runState) closeStep() {
	if !st.stepOpen {
		return
	}
	st.emit(aguievents.NewStepFinishedEvent(st.step))
	st.stepOpen = false
}

func (st *runState) emit(event aguievents.Event) {
	st.a.emit(st.ctx, event)
}

// emit sends an event to the events channel. It waits for the reader when the
// buffer is full, and gives up only when ctx is done, since a dropped event
// leaves the consumer with a run that never ends or a message that never
// closes.
func (a *EventAdapter) emit(ctx context.Context, event aguievents.Event) {
	a.mu.RLock()
	defer a.mu.RUnlock()
	if a.closed {
		return
	}
	select {
	case a.events <- event:
	case <-ctx.Done():
	}
}

// closeEvents closes the events channel exactly once. It waits for any
// in-flight emit calls to complete before closing the channel.
func (a *EventAdapter) closeEvents() {
	a.once.Do(func() {
		a.mu.Lock()
		a.closed = true
		a.mu.Unlock()
		close(a.events)
	})
}

// workflowSender adapts a WorkflowConversation to the adapter. Turns go
// through the workflow, which commits the transitions they make; tool results
// and approvals go to the conversation serving the current state.
type workflowSender struct {
	wc *sdk.WorkflowConversation
}

var errNoActiveConversation = errors.New("agui: workflow has no active conversation")

// Send runs the turn through the workflow, which applies any transition it makes.
func (w *workflowSender) Send(ctx context.Context, message any, opts ...sdk.SendOption) (*sdk.Response, error) {
	return w.wc.Send(ctx, message, opts...)
}

// SendToolResult answers a client tool on the active conversation.
func (w *workflowSender) SendToolResult(ctx context.Context, callID string, result any) error {
	conv := w.wc.ActiveConversation()
	if conv == nil {
		return errNoActiveConversation
	}
	return conv.SendToolResult(ctx, callID, result)
}

// SendToolResultMultimodal answers a client tool with content parts on the active conversation.
func (w *workflowSender) SendToolResultMultimodal(ctx context.Context, callID string, parts []types.ContentPart) error {
	conv := w.wc.ActiveConversation()
	if conv == nil {
		return errNoActiveConversation
	}
	return conv.SendToolResultMultimodal(ctx, callID, parts)
}

// FailClientTool reports a failed client tool on the active conversation.
func (w *workflowSender) FailClientTool(ctx context.Context, callID string, partial any, err error) error {
	conv := w.wc.ActiveConversation()
	if conv == nil {
		return errNoActiveConversation
	}
	return conv.FailClientTool(ctx, callID, partial, err)
}

// RejectClientTool declines a client tool on the active conversation.
func (w *workflowSender) RejectClientTool(ctx context.Context, callID, reason string) {
	if conv := w.wc.ActiveConversation(); conv != nil {
		conv.RejectClientTool(ctx, callID, reason)
	}
}

// HasToolResultRewriters reports whether the active conversation rewrites
// tool results.
func (w *workflowSender) HasToolResultRewriters() bool {
	conv := w.wc.ActiveConversation()
	return conv != nil && clienttool.SettlerOf(conv).HasToolResultRewriters()
}

// SettleClientTool settles a client tool's answer on the active conversation.
func (w *workflowSender) SettleClientTool(
	ctx context.Context, callID string, result any, failure error,
) (types.MessageToolResult, bool, error) {
	conv := w.wc.ActiveConversation()
	if conv == nil {
		return types.MessageToolResult{}, false, errNoActiveConversation
	}
	return clienttool.SettlerOf(conv).SettleClientTool(ctx, callID, result, failure)
}

// SettleClientToolRejection settles a client tool's rejection on the active
// conversation.
func (w *workflowSender) SettleClientToolRejection(
	ctx context.Context, callID, reason string,
) (types.MessageToolResult, bool, error) {
	conv := w.wc.ActiveConversation()
	if conv == nil {
		return types.MessageToolResult{}, false, errNoActiveConversation
	}
	return clienttool.SettlerOf(conv).SettleClientToolRejection(ctx, callID, reason)
}

// Resume continues the active conversation after its client tools are answered.
func (w *workflowSender) Resume(ctx context.Context) (*sdk.Response, error) {
	conv := w.wc.ActiveConversation()
	if conv == nil {
		return nil, errNoActiveConversation
	}
	return conv.Resume(ctx)
}

// Continue continues the active conversation after its approval holds are resolved.
func (w *workflowSender) Continue(ctx context.Context) (*sdk.Response, error) {
	conv := w.wc.ActiveConversation()
	if conv == nil {
		return nil, errNoActiveConversation
	}
	return conv.Continue(ctx)
}

// EventBus returns the active conversation's event bus.
func (w *workflowSender) EventBus() events.Bus {
	if conv := w.wc.ActiveConversation(); conv != nil {
		return conv.EventBus()
	}
	return nil
}

// CurrentState returns the workflow state the conversation is in.
func (w *workflowSender) CurrentState() string {
	return w.wc.CurrentState()
}
