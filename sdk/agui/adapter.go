package agui

import (
	"context"
	"sync"

	aguievents "github.com/ag-ui-protocol/ag-ui/sdks/community/go/pkg/core/events"

	"github.com/AltairaLabs/PromptKit/runtime/events"
	"github.com/AltairaLabs/PromptKit/runtime/types"
	"github.com/AltairaLabs/PromptKit/sdk"
)

// eventChannelBufferSize is the default buffer size for the AG-UI event channel.
const eventChannelBufferSize = 64

// Sender abstracts the conversation Send method so the adapter can be tested
// without a real Conversation. In production code, *sdk.Conversation satisfies
// this interface.
type Sender interface {
	Send(ctx context.Context, message any, opts ...sdk.SendOption) (*sdk.Response, error)
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
	threadID      string
	runID         string
	stateProvider StateProvider
	workflowSteps bool
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

// EventAdapter bridges a PromptKit conversation to an AG-UI event channel.
// It calls Send on the underlying conversation and translates the response
// (and any event-bus tool-call events) into AG-UI protocol events.
type EventAdapter struct {
	sender   Sender
	eventBus EventBusProvider
	cfg      adapterConfig
	events   chan aguievents.Event
	once     sync.Once
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

// RunSend sends a message through the conversation and emits AG-UI events.
//
// Event sequence on success:
//  1. RunStartedEvent
//  2. StateSnapshotEvent (if StateProvider is configured)
//  3. TextMessageStartEvent
//  4. TextMessageContentEvent (full text in a single delta)
//  5. For each tool call: ToolCallStartEvent, ToolCallArgsEvent, ToolCallEndEvent
//  6. TextMessageEndEvent
//  7. RunFinishedEvent
//
// On error, a RunErrorEvent is emitted instead of steps 3-7.
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

	// 3. Subscribe to tool call completed events for result emission
	var toolResults []toolResultCapture
	var toolResultsMu sync.Mutex
	if a.eventBus != nil && a.eventBus.EventBus() != nil {
		unsub := a.eventBus.EventBus().Subscribe(events.EventToolCallCompleted, func(e *events.Event) {
			if data, ok := e.Data.(*events.ToolCallCompletedData); ok {
				toolResultsMu.Lock()
				toolResults = append(toolResults, toolResultCapture{
					callID:   data.CallID,
					toolName: data.ToolName,
					status:   data.Status,
				})
				toolResultsMu.Unlock()
			}
		})
		defer unsub()
	}

	// 4. Call Send
	resp, err := a.sender.Send(ctx, msg)
	if err != nil {
		a.emit(aguievents.NewRunErrorEvent(err.Error()))
		return err
	}

	// 5. Emit text message events
	msgID := aguievents.GenerateMessageID()
	a.emit(aguievents.NewTextMessageStartEvent(msgID, aguievents.WithRole(roleAssistant)))

	text := resp.Text()
	if text != "" {
		a.emit(aguievents.NewTextMessageContentEvent(msgID, text))
	}

	// 6. Emit tool call events
	for _, tc := range resp.ToolCalls() {
		a.emitToolCallEvents(tc, msgID, &toolResults, &toolResultsMu)
	}

	// 7. End message
	a.emit(aguievents.NewTextMessageEndEvent(msgID))

	// 8. Emit RunFinished
	a.emit(aguievents.NewRunFinishedEvent(a.cfg.threadID, a.cfg.runID))

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

// emit sends an event to the events channel. It is non-blocking; if the
// channel buffer is full, the event is dropped to avoid deadlocks.
func (a *EventAdapter) emit(event aguievents.Event) {
	select {
	case a.events <- event:
	default:
		// Drop event if buffer is full to prevent blocking
	}
}

// closeEvents closes the events channel exactly once.
func (a *EventAdapter) closeEvents() {
	a.once.Do(func() {
		close(a.events)
	})
}

// toolResultCapture holds tool result data captured from the event bus.
type toolResultCapture struct {
	callID   string
	toolName string
	status   string
}
