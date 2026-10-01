package agui

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"

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

// workflowStateReader is implemented by conversations that run a workflow
// state machine (*sdk.WorkflowConversation does).
type workflowStateReader interface {
	CurrentState() string
}

// ToolResultProvider is a callback the caller implements to supply results for
// pending client tools. The adapter calls it when the LLM response contains
// deferred client tools that need fulfillment before the pipeline can continue.
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

// EventBusProvider abstracts access to the conversation's event bus.
type EventBusProvider interface {
	EventBus() events.Bus
}

// StateProvider produces a state snapshot for the AG-UI StateSnapshotEvent.
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
}

// WithThreadID sets the AG-UI thread ID for emitted events.
func WithThreadID(id string) AdapterOption {
	return func(c *adapterConfig) {
		c.threadID = id
	}
}

// WithRunID sets the AG-UI run ID for emitted events.
func WithRunID(id string) AdapterOption {
	return func(c *adapterConfig) {
		c.runID = id
	}
}

// WithStateProvider sets a provider that produces state snapshots.
func WithStateProvider(sp StateProvider) AdapterOption {
	return func(c *adapterConfig) {
		c.stateProvider = sp
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
// answers them in the next run (see [EventAdapter.RunResume]).
//
// Use it only for tools the server itself can answer. A frontend tool, one the
// application advertised in RunAgentInput.tools, must not be answered by the
// producer.
func WithToolResultProvider(provider ToolResultProvider) AdapterOption {
	return func(c *adapterConfig) {
		c.toolResultProvider = provider
	}
}

// EventAdapter bridges a PromptKit conversation to an AG-UI event channel.
// It runs one conversation turn and translates the turn's messages into
// AG-UI protocol events. An adapter carries one run: its channel closes when
// the run ends.
type EventAdapter struct {
	sender Sender
	cfg    adapterConfig
	events chan aguievents.Event
	once   sync.Once
	mu     sync.RWMutex
	closed bool
}

// NewEventAdapter creates a new EventAdapter for the given conversation.
// The conversation must implement both Sender and EventBusProvider.
// In practice, *sdk.Conversation satisfies both interfaces.
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
// The event bus is no longer read: every event the adapter emits comes from
// the turn's own messages, which keeps the stream ordered.
func newAdapter(sender Sender, _ EventBusProvider, opts ...AdapterOption) *EventAdapter {
	cfg := adapterConfig{
		threadID: aguievents.GenerateThreadID(),
		runID:    aguievents.GenerateRunID(),
	}
	for _, opt := range opts {
		opt(&cfg)
	}
	return &EventAdapter{
		sender: sender,
		cfg:    cfg,
		events: make(chan aguievents.Event, eventChannelBufferSize),
	}
}

// Events returns the read-only channel of AG-UI events. The channel is closed
// when the run ends, successfully or with an error.
//
// The adapter never drops an event. When the channel's buffer is full it waits
// for the reader, so a reader that stops reading holds the run until the run's
// context is canceled; cancel it (an HTTP handler's request context is
// canceled when the client disconnects) to release the run.
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
//  4. For each assistant message the turn produced, in order:
//     TEXT_MESSAGE_START / TEXT_MESSAGE_CONTENT / TEXT_MESSAGE_END, then
//     TOOL_CALL_START / TOOL_CALL_ARGS / TOOL_CALL_END for each call it made;
//     each tool result the turn fed back to the model is a TOOL_CALL_RESULT
//  5. STEP_FINISHED / STEP_STARTED when the turn moved the workflow on
//  6. If a ToolResultProvider answers the pending client tools: a
//     TOOL_CALL_RESULT for each, then the resumed turn from step 4
//  7. STEP_FINISHED for the open step
//  8. RUN_FINISHED
//
// A client tool call left pending ends the run with the call unanswered; the
// application answers it in the next run with [EventAdapter.RunResume]. An
// approval-held call (sdk.Conversation.OnToolAsync) ends the run with
// RUN_FINISHED carrying an interrupt outcome that names the held calls; the
// next run continues with [EventAdapter.RunContinue].
//
// Text arrives as one TEXT_MESSAGE_CONTENT per message once the turn has run;
// it is not streamed token by token.
//
// On error, a RUN_ERROR ends the run. The events channel is always closed when
// RunSend returns.
func (a *EventAdapter) RunSend(ctx context.Context, msg *types.Message) error {
	return a.run(ctx, nil, func(ctx context.Context) (*sdk.Response, error) {
		return a.sender.Send(ctx, msg)
	})
}

// RunResume answers the client tool calls a previous run left pending and
// emits the continued turn as a new run.
//
// In AG-UI the application answers a frontend tool call in the next run's
// input, as a tool message per call; [ToolResultsFromAGUI] extracts them. The
// answers are the application's own, so the run does not echo them back as
// TOOL_CALL_RESULT events.
func (a *EventAdapter) RunResume(ctx context.Context, results []ToolResult) error {
	answered := make(map[string]bool, len(results))
	for _, r := range results {
		answered[r.CallID] = true
	}
	return a.run(ctx, answered, func(ctx context.Context) (*sdk.Response, error) {
		if err := a.resolveClientTools(ctx, results); err != nil {
			return nil, err
		}
		return a.sender.Resume(ctx)
	})
}

// RunContinue continues a turn whose approval-held tool calls were resolved
// (sdk.Conversation.ResolveTool / RejectTool) and emits it as a new run. The
// held calls' results are emitted as TOOL_CALL_RESULT events, since only the
// agent knows them.
//
// It returns [ErrContinueUnsupported] (after a RUN_ERROR) when the
// conversation has no Continue method.
func (a *EventAdapter) RunContinue(ctx context.Context) error {
	return a.run(ctx, nil, func(ctx context.Context) (*sdk.Response, error) {
		c, ok := a.sender.(continuer)
		if !ok {
			return nil, ErrContinueUnsupported
		}
		return c.Continue(ctx)
	})
}

// runState tracks what one run has emitted.
type runState struct {
	a        *EventAdapter
	ctx      context.Context
	answered map[string]bool // calls answered in this run, or by the run's input
	step     string          // the open step, when stepOpen
	stepOpen bool
}

// run emits one AG-UI run around start, which produces the turn's response.
func (a *EventAdapter) run(
	ctx context.Context,
	answeredByInput map[string]bool,
	start func(ctx context.Context) (*sdk.Response, error),
) error {
	defer a.closeEvents()

	st := &runState{a: a, ctx: ctx, answered: map[string]bool{}}
	for id := range answeredByInput {
		st.answered[id] = true
	}

	st.emit(aguievents.NewRunStartedEvent(a.cfg.threadID, a.cfg.runID))
	if a.cfg.stateProvider != nil {
		if snapshot, err := a.cfg.stateProvider.Snapshot(a.sender); err == nil {
			st.emit(aguievents.NewStateSnapshotEvent(snapshot))
		}
	}
	st.syncStep()

	resp, err := start(ctx)
	if err != nil {
		st.emit(aguievents.NewRunErrorEvent(err.Error()))
		return err
	}

	for {
		st.emitTurn(resp)
		st.syncStep()
		if !resp.HasPendingClientTools() || a.cfg.toolResultProvider == nil {
			break
		}
		next, err := st.fulfillAndResume(resp.ClientTools())
		if err != nil {
			st.emit(aguievents.NewRunErrorEvent(err.Error()))
			return err
		}
		if next == nil {
			break // calls left pending for the application
		}
		resp = next
	}

	st.closeStep()
	st.emit(runFinished(a.cfg.threadID, a.cfg.runID, resp))
	return nil
}

// runFinished builds the RUN_FINISHED event. A turn held for approval ends
// with the interrupt outcome naming each held call: AG-UI requires a run that
// stopped to ask for outside input to say so rather than report success.
func runFinished(threadID, runID string, resp *sdk.Response) *aguievents.RunFinishedEvent {
	held := resp.PendingTools()
	if len(held) == 0 {
		return aguievents.NewRunFinishedEvent(threadID, runID)
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
	return aguievents.NewRunFinishedEventWithOptions(threadID, runID, aguievents.WithOutcome(
		aguievents.RunFinishedOutcome{Type: aguievents.RunFinishedOutcomeTypeInterrupt, Interrupts: interrupts},
	))
}

// emitTurn emits the messages a turn produced, in the order it produced them.
func (st *runState) emitTurn(resp *sdk.Response) {
	msgs := resp.TurnMessages()
	if len(msgs) == 0 {
		msgs = fallbackTurn(resp)
	}
	for i := range msgs {
		switch msgs[i].Role {
		case roleAssistant:
			st.emitAssistant(&msgs[i])
		case roleTool:
			st.emitToolMessage(&msgs[i])
		}
	}
}

// fallbackTurn reconstructs a one-message turn for a response that carries no
// turn messages, from its final text, its tool calls and its pending client
// tools.
func fallbackTurn(resp *sdk.Response) []types.Message {
	msg := types.Message{Role: roleAssistant, Content: resp.Text()}
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

// emitAssistant emits one assistant message and the tool calls it made. A
// message that only calls tools emits no text message; its calls still name it
// as their parent.
func (st *runState) emitAssistant(msg *types.Message) {
	msgID := aguievents.GenerateMessageID()
	text := msg.GetContent()
	if text != "" || len(msg.ToolCalls) == 0 {
		st.emit(aguievents.NewTextMessageStartEvent(msgID, aguievents.WithRole(roleAssistant)))
		if text != "" {
			st.emit(aguievents.NewTextMessageContentEvent(msgID, text))
		}
		st.emit(aguievents.NewTextMessageEndEvent(msgID))
	}
	for _, tc := range msg.ToolCalls {
		st.emit(aguievents.NewToolCallStartEvent(tc.ID, tc.Name, aguievents.WithParentMessageID(msgID)))
		if len(tc.Args) > 0 {
			st.emit(aguievents.NewToolCallArgsEvent(tc.ID, string(tc.Args)))
		}
		st.emit(aguievents.NewToolCallEndEvent(tc.ID))
	}
}

// emitToolMessage emits the result a tool message carries, unless the call
// was already answered — by this run, or by the input that started it.
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
}

// toolResultText is the TOOL_CALL_RESULT content for a tool result: its text,
// or its error when it carries no text.
func toolResultText(r *types.MessageToolResult) string {
	if text := r.GetTextContent(); text != "" {
		return text
	}
	return r.Error
}

// fulfillAndResume asks the ToolResultProvider for the pending calls' results
// and resolves them. When every call is answered it resumes the turn and
// returns the resumed response; otherwise it returns nil, leaving the rest
// for the application to answer in the next run.
func (st *runState) fulfillAndResume(pending []sdk.PendingClientTool) (*sdk.Response, error) {
	a := st.a
	results, err := a.cfg.toolResultProvider(st.ctx, pending)
	if err != nil {
		return nil, err
	}
	if resolveErr := a.resolveClientTools(st.ctx, results); resolveErr != nil {
		return nil, resolveErr
	}
	if !answersAll(pending, results) {
		for i := range results {
			st.emitResult(results[i].CallID, resultText(&results[i]))
		}
		return nil, nil
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

func toolMessagesByCall(msgs []types.Message) map[string]string {
	out := map[string]string{}
	for i := range msgs {
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
		return valueText(r.Result)
	}
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
		r := &results[i]
		var err error
		switch {
		case r.Rejected:
			a.sender.RejectClientTool(ctx, r.CallID, r.Reason)
		case r.Error != "":
			err = a.failClientTool(ctx, r)
		default:
			err = a.sendResult(ctx, r)
		}
		if err != nil {
			return err
		}
	}
	return nil
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
