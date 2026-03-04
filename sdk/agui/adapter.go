package agui

import (
	"context"
	"encoding/json"
	"sync"

	aguievents "github.com/ag-ui-protocol/ag-ui/sdks/community/go/pkg/core/events"

	"github.com/AltairaLabs/PromptKit/runtime/events"
	"github.com/AltairaLabs/PromptKit/runtime/types"
	"github.com/AltairaLabs/PromptKit/sdk"
)

// eventChannelBufferSize is the default buffer size for the AG-UI event channel.
const eventChannelBufferSize = 64

// toolResultCompleted is the status string for a successfully resolved tool call.
const toolResultCompleted = "completed"

// Sender abstracts the conversation methods needed by the adapter. In
// production code, *sdk.Conversation satisfies this interface.
type Sender interface {
	Send(ctx context.Context, message any, opts ...sdk.SendOption) (*sdk.Response, error)
	SendToolResult(ctx context.Context, callID string, result any) error
	RejectClientTool(ctx context.Context, callID, reason string)
	Resume(ctx context.Context) (*sdk.Response, error)
}

// ToolResultProvider is a callback the caller implements to supply results for
// pending client tools. The adapter calls it when the LLM response contains
// deferred client tools that need fulfillment before the pipeline can continue.
type ToolResultProvider func(ctx context.Context, tools []sdk.PendingClientTool) ([]ToolResult, error)

// ToolResult carries the caller-provided outcome for a single client tool call.
type ToolResult struct {
	CallID   string // must match PendingClientTool.CallID
	Result   any    // JSON-serializable; ignored when Rejected is true
	Rejected bool
	Reason   string // rejection reason (used when Rejected is true)
}

// EventBusProvider abstracts access to the conversation's event bus.
type EventBusProvider interface {
	EventBus() *events.EventBus
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

// WithWorkflowSteps enables emission of StepStarted/StepFinished events
// for workflow state transitions observed on the event bus.
func WithWorkflowSteps(enabled bool) AdapterOption {
	return func(c *adapterConfig) {
		c.workflowSteps = enabled
	}
}

// WithToolResultProvider sets a callback that supplies results for pending
// client tools. When configured, the adapter will suspend, call the provider,
// resolve each tool, then call Resume to continue the pipeline.
func WithToolResultProvider(provider ToolResultProvider) AdapterOption {
	return func(c *adapterConfig) {
		c.toolResultProvider = provider
	}
}

// EventAdapter bridges a PromptKit conversation to an AG-UI event channel.
// It calls Send on the underlying conversation and translates the response
// (and any event-bus tool-call events) into AG-UI protocol events.
type EventAdapter struct {
	sender   Sender
	eventBus EventBusProvider
	cfg      adapterConfig
	events   chan aguievents.Event
	once     sync.Once
	mu       sync.RWMutex
	closed   bool
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

// newAdapter creates an adapter from separate sender and event bus provider.
func newAdapter(sender Sender, ebp EventBusProvider, opts ...AdapterOption) *EventAdapter {
	cfg := adapterConfig{
		threadID: aguievents.GenerateThreadID(),
		runID:    aguievents.GenerateRunID(),
	}
	for _, opt := range opts {
		opt(&cfg)
	}
	return &EventAdapter{
		sender:   sender,
		eventBus: ebp,
		cfg:      cfg,
		events:   make(chan aguievents.Event, eventChannelBufferSize),
	}
}

// Events returns the read-only channel of AG-UI events. The channel is closed
// after RunSend completes (either successfully or with an error).
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

// clientToolsPendingEvent is the custom event name emitted when the adapter
// suspends to wait for client tool results.
const clientToolsPendingEvent = "promptkit.client_tools_pending"

// RunSend sends a message through the conversation and emits AG-UI events.
//
// Event sequence on success:
//  1. RunStartedEvent
//  2. StateSnapshotEvent (if StateProvider is configured)
//  3. TextMessageStartEvent
//  4. TextMessageContentEvent (full text in a single delta)
//  5. For each server-side tool call: ToolCallStartEvent, ToolCallArgsEvent, ToolCallEndEvent
//  6. If pending client tools (and ToolResultProvider configured):
//     a. ToolCallStart/Args/End for each pending tool
//     b. CustomEvent("promptkit.client_tools_pending")
//     c. Provider is called → ToolCallResult per resolved tool
//     d. Resume → loop back to step 4
//  7. TextMessageEndEvent
//  8. RunFinishedEvent
//
// On error, a RunErrorEvent is emitted instead of steps 3-8.
// The events channel is always closed when RunSend returns.
func (a *EventAdapter) RunSend(ctx context.Context, msg *types.Message) error {
	defer a.closeEvents()

	// 1. Emit RunStarted
	a.emit(aguievents.NewRunStartedEvent(a.cfg.threadID, a.cfg.runID))

	// 2. State snapshot
	if a.cfg.stateProvider != nil {
		snapshot, err := a.cfg.stateProvider.Snapshot(a.sender)
		if err == nil {
			a.emit(aguievents.NewStateSnapshotEvent(snapshot))
		}
	}

	// 3. Subscribe to event bus for tool results and workflow transitions
	var toolResults []toolResultCapture
	var toolResultsMu sync.Mutex
	unsubs := a.subscribeEventBus(&toolResults, &toolResultsMu)
	defer func() {
		for _, fn := range unsubs {
			fn()
		}
	}()

	// 4. Call Send
	resp, err := a.sender.Send(ctx, msg)
	if err != nil {
		a.emit(aguievents.NewRunErrorEvent(err.Error()))
		return err
	}

	// 5. Emit text message events (may loop if client tools require resume)
	msgID := aguievents.GenerateMessageID()
	a.emit(aguievents.NewTextMessageStartEvent(msgID, aguievents.WithRole(roleAssistant)))

	for {
		a.emitResponseContent(resp, msgID, &toolResults, &toolResultsMu)

		if !resp.HasPendingClientTools() {
			break
		}

		a.emitPendingClientTools(resp.ClientTools(), msgID)

		if a.cfg.toolResultProvider == nil {
			break // no provider — emit events only (backward-compat)
		}

		var resumeErr error
		resp, resumeErr = a.fulfillAndResume(ctx, resp.ClientTools())
		if resumeErr != nil {
			a.emit(aguievents.NewRunErrorEvent(resumeErr.Error()))
			return resumeErr
		}
	}

	// 7. End message
	a.emit(aguievents.NewTextMessageEndEvent(msgID))

	// 8. Emit RunFinished
	a.emit(aguievents.NewRunFinishedEvent(a.cfg.threadID, a.cfg.runID))

	return nil
}

// emitResponseContent emits text content and server-side tool call events from a response.
func (a *EventAdapter) emitResponseContent(
	resp *sdk.Response,
	msgID string,
	toolResults *[]toolResultCapture,
	toolResultsMu *sync.Mutex,
) {
	text := resp.Text()
	if text != "" {
		a.emit(aguievents.NewTextMessageContentEvent(msgID, text))
	}
	for _, tc := range resp.ToolCalls() {
		a.emitToolCallEvents(tc, msgID, toolResults, toolResultsMu)
	}
}

// fulfillAndResume calls the ToolResultProvider, resolves each tool result via the
// sender, then calls Resume to continue the pipeline. Returns the new response.
func (a *EventAdapter) fulfillAndResume(
	ctx context.Context, pendingTools []sdk.PendingClientTool,
) (*sdk.Response, error) {
	results, err := a.cfg.toolResultProvider(ctx, pendingTools)
	if err != nil {
		return nil, err
	}
	if err := a.resolveClientTools(ctx, results); err != nil {
		return nil, err
	}
	return a.sender.Resume(ctx)
}

// emitPendingClientTools emits ToolCallStart/Args/End for each pending client tool,
// then a CustomEvent signaling the frontend that these tools need client fulfillment.
func (a *EventAdapter) emitPendingClientTools(tools []sdk.PendingClientTool, parentMsgID string) {
	type pendingToolInfo struct {
		CallID   string         `json:"callID"`
		ToolName string         `json:"toolName"`
		Args     map[string]any `json:"args,omitempty"`
	}

	var infos []pendingToolInfo
	for _, t := range tools {
		a.emit(aguievents.NewToolCallStartEvent(
			t.CallID,
			t.ToolName,
			aguievents.WithParentMessageID(parentMsgID),
		))
		if len(t.Args) > 0 {
			argsJSON, _ := json.Marshal(t.Args)
			a.emit(aguievents.NewToolCallArgsEvent(t.CallID, string(argsJSON)))
		}
		a.emit(aguievents.NewToolCallEndEvent(t.CallID))
		infos = append(infos, pendingToolInfo{
			CallID:   t.CallID,
			ToolName: t.ToolName,
			Args:     t.Args,
		})
	}

	a.emit(aguievents.NewCustomEvent(
		clientToolsPendingEvent,
		aguievents.WithValue(map[string]any{"tools": infos}),
	))
}

// resolveClientTools calls SendToolResult or RejectClientTool on the sender for
// each result, and emits a ToolCallResult event per tool.
func (a *EventAdapter) resolveClientTools(ctx context.Context, results []ToolResult) error {
	for _, r := range results {
		if r.Rejected {
			a.sender.RejectClientTool(ctx, r.CallID, r.Reason)
		} else {
			if err := a.sender.SendToolResult(ctx, r.CallID, r.Result); err != nil {
				return err
			}
		}
		resultMsgID := aguievents.GenerateMessageID()
		content := toolResultCompleted
		if r.Rejected {
			content = "rejected"
			if r.Reason != "" {
				content = "rejected: " + r.Reason
			}
		}
		a.emit(aguievents.NewToolCallResultEvent(resultMsgID, r.CallID, content))
	}
	return nil
}

// emitToolCallEvents emits the AG-UI tool call event sequence for a single tool call.
func (a *EventAdapter) emitToolCallEvents(
	tc types.MessageToolCall,
	parentMsgID string,
	toolResults *[]toolResultCapture,
	toolResultsMu *sync.Mutex,
) {
	// ToolCallStart
	a.emit(aguievents.NewToolCallStartEvent(
		tc.ID,
		tc.Name,
		aguievents.WithParentMessageID(parentMsgID),
	))

	// ToolCallArgs - emit the full arguments as a single delta
	args := string(tc.Args)
	if args != "" {
		a.emit(aguievents.NewToolCallArgsEvent(tc.ID, args))
	}

	// ToolCallEnd
	a.emit(aguievents.NewToolCallEndEvent(tc.ID))

	// ToolCallResult - check if we captured a result from the event bus
	toolResultsMu.Lock()
	defer toolResultsMu.Unlock()
	for _, tr := range *toolResults {
		if tr.callID != tc.ID {
			continue
		}
		resultMsgID := aguievents.GenerateMessageID()
		content := tr.status
		if content == "" {
			content = "completed"
		}
		a.emit(aguievents.NewToolCallResultEvent(resultMsgID, tc.ID, content))
		break
	}
}

// subscribeEventBus sets up event bus subscriptions for tool results and
// workflow transitions. Returns a slice of unsubscribe functions to call on cleanup.
func (a *EventAdapter) subscribeEventBus(
	toolResults *[]toolResultCapture,
	toolResultsMu *sync.Mutex,
) []func() {
	if a.eventBus == nil || a.eventBus.EventBus() == nil {
		return nil
	}

	bus := a.eventBus.EventBus()
	var unsubs []func()

	unsubs = append(unsubs, bus.Subscribe(events.EventToolCallCompleted, func(e *events.Event) {
		if data, ok := e.Data.(*events.ToolCallCompletedData); ok {
			toolResultsMu.Lock()
			*toolResults = append(*toolResults, toolResultCapture{
				callID:   data.CallID,
				toolName: data.ToolName,
				status:   data.Status,
			})
			toolResultsMu.Unlock()
		}
	}))

	if a.cfg.workflowSteps {
		unsubs = append(unsubs, bus.Subscribe(events.EventWorkflowTransitioned, func(e *events.Event) {
			if data, ok := e.Data.(*events.WorkflowTransitionedData); ok {
				if data.FromState != "" {
					a.emit(aguievents.NewStepFinishedEvent(data.FromState))
				}
				a.emit(aguievents.NewStepStartedEvent(data.ToState))
			}
		}))
	}

	return unsubs
}

// emit sends an event to the events channel. It is non-blocking; if the
// channel buffer is full or the adapter is closed, the event is dropped.
func (a *EventAdapter) emit(event aguievents.Event) {
	a.mu.RLock()
	defer a.mu.RUnlock()
	if a.closed {
		return
	}
	select {
	case a.events <- event:
	default:
		// Drop event if buffer is full to prevent blocking
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

// toolResultCapture holds tool result data captured from the event bus.
type toolResultCapture struct {
	callID   string
	toolName string
	status   string
}
