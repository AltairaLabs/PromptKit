package tui

import (
	"fmt"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/AltairaLabs/PromptKit/runtime/events"
	arenaaudio "github.com/AltairaLabs/PromptKit/tools/arena/audio"
	"github.com/AltairaLabs/PromptKit/tools/arena/tui/logging"
)

// audioRMSSubscribeBuffer is the per-consumer buffer requested from the
// AudioRouter for RMS frames. Slow consumers drop their own frames.
const audioRMSSubscribeBuffer = 20

// EventAdapter converts runtime events to bubbletea messages.
type EventAdapter struct {
	send  func(tea.Msg) // Delivery into the bubbletea event loop (e.g. *tea.Program.Send)
	model *Model        // For headless mode
}

// NewEventAdapter creates an adapter that forwards events via send. A
// *tea.Program's Send method satisfies func(tea.Msg), so callers pass
// program.Send to drive a running TUI program.
func NewEventAdapter(send func(tea.Msg)) *EventAdapter {
	return &EventAdapter{send: send}
}

// NewEventAdapterWithModel creates an adapter for headless mode.
func NewEventAdapterWithModel(model *Model) *EventAdapter {
	return &EventAdapter{model: model}
}

// Subscribe subscribes the adapter to an event bus.
func (a *EventAdapter) Subscribe(bus events.Bus) {
	if bus == nil {
		return
	}
	bus.SubscribeAll(a.HandleEvent)
}

// AttachAudioMonitor subscribes the TUI adapter to the process-wide audio
// Monitor's RMS stream. Frames originate from the LocalSink as it consumes
// samples, so the meter reflects whatever the user is actually hearing —
// regardless of which run is currently routed to playback.
func (a *EventAdapter) AttachAudioMonitor(monitor *arenaaudio.Monitor) {
	if a == nil || monitor == nil {
		return
	}
	rms := monitor.SubscribeRMS(audioRMSSubscribeBuffer)
	go func() {
		for frame := range rms {
			a.deliver(AudioLevelMsg{
				UserLevel:  frame.UserLevel,
				AgentLevel: frame.AgentLevel,
			})
		}
	}()
}

// HandleEvent converts runtime events into TUI messages.
func (a *EventAdapter) HandleEvent(event *events.Event) {
	msg := a.mapEvent(event)
	if msg != nil {
		a.deliver(msg)
	}
}

// mapEvent converts a runtime event to a TUI message.
func (a *EventAdapter) mapEvent(event *events.Event) tea.Msg { //nolint:gocyclo // switch on event types
	switch event.Type { //nolint:exhaustive // only map TUI-relevant events
	case events.EventProviderCallStarted:
		return a.logf("INFO", event, "Provider call started: %s %s", providerName(event), providerModel(event))
	case events.EventProviderCallCompleted:
		return a.handleProviderCallCompleted(event)
	case events.EventProviderCallFailed:
		return a.logf("ERROR", event, "Provider call failed: %s %s (%v)",
			providerName(event), providerModel(event), eventError(event))
	case events.EventMiddlewareStarted:
		return a.logf("INFO", event, "Middleware started: %s", middlewareName(event))
	case events.EventMiddlewareCompleted:
		return a.handleMiddlewareCompleted(event)
	case events.EventMiddlewareFailed:
		return a.logf("ERROR", event, "Middleware failed: %s (%v)", middlewareName(event), eventError(event))
	case events.EventToolCallStarted:
		return a.logf("INFO", event, "Tool call started: %s", toolName(event))
	case events.EventToolCallCompleted:
		return a.logf("INFO", event, "Tool call completed: %s", toolName(event))
	case events.EventToolCallFailed:
		return a.logf("ERROR", event, "Tool call failed: %s (%v)", toolName(event), eventError(event))
	case events.EventValidationStarted:
		return a.logf("INFO", event, "Validation started: %s", validationName(event))
	case events.EventValidationPassed:
		return a.logf("INFO", event, "Validation passed: %s", validationName(event))
	case events.EventValidationFailed:
		return a.logf("ERROR", event, "Validation failed: %s (%v)", validationName(event), eventError(event))
	case events.EventContextBuilt:
		return a.handleContextBuilt(event)
	case events.EventTokenBudgetExceeded:
		return a.handleTokenBudgetExceeded(event)
	case events.EventStateLoaded:
		return a.handleStateLoaded(event)
	case events.EventStateSaved:
		return a.handleStateSaved(event)
	case events.EventStreamInterrupted:
		return a.logf("ERROR", event, "Stream interrupted: %s", readString(event.Data, "reason"))
	case events.EventReasoningDelta:
		if d, ok := event.Data.(*events.ReasoningDeltaData); ok && d != nil {
			return ReasoningDeltaMsg{Text: d.Text}
		}
		return nil
	case events.EventMessageCreated:
		return a.handleMessageCreated(event)
	case events.EventMessageUpdated:
		return a.handleMessageUpdated(event)
	case events.EventConversationStarted:
		return a.handleConversationStarted(event)
	default:
		return a.handleArenaEvents(event)
	}
}

func (a *EventAdapter) handleProviderCallCompleted(event *events.Event) tea.Msg {
	data, ok := event.Data.(*events.ProviderCallCompletedData)
	if !ok {
		return nil
	}
	return a.logf("INFO", event, "Provider call completed: %s %s (%.2fs, cost $%.4f)",
		providerName(event), providerModel(event), data.Duration.Seconds(), data.Cost)
}

func (a *EventAdapter) handleMiddlewareCompleted(event *events.Event) tea.Msg {
	data, ok := event.Data.(events.MiddlewareCompletedData)
	if !ok {
		return nil
	}
	return a.logf("INFO", event, "Middleware completed: %s (%.2fs)", data.Name, data.Duration.Seconds())
}

func (a *EventAdapter) handleContextBuilt(event *events.Event) tea.Msg {
	data, ok := event.Data.(events.ContextBuiltData)
	if !ok {
		return nil
	}
	return a.logf("INFO", event, "Context built (%d msgs, %d tokens)%s",
		data.MessageCount, data.TokenCount, ternary(data.Truncated, " [truncated]", ""))
}

func (a *EventAdapter) handleTokenBudgetExceeded(event *events.Event) tea.Msg {
	data, ok := event.Data.(events.TokenBudgetExceededData)
	if !ok {
		return nil
	}
	return a.logf("ERROR", event, "Token budget exceeded: need %d, budget %d, excess %d",
		data.RequiredTokens, data.Budget, data.Excess)
}

func (a *EventAdapter) handleStateLoaded(event *events.Event) tea.Msg {
	data, ok := event.Data.(events.StateLoadedData)
	if !ok {
		return nil
	}
	return a.logf("INFO", event, "State loaded: %s (%d messages)", data.ConversationID, data.MessageCount)
}

func (a *EventAdapter) handleStateSaved(event *events.Event) tea.Msg {
	data, ok := event.Data.(events.StateSavedData)
	if !ok {
		return nil
	}
	return a.logf("INFO", event, "State saved: %s (%d messages)", data.ConversationID, data.MessageCount)
}

func (a *EventAdapter) handleMessageCreated(event *events.Event) tea.Msg {
	// emitter.MessageCreated emits *events.MessageCreatedData (pointer).
	// Asserting to the value type returned a silent nil, which is why
	// the conversation panel stopped updating mid-2026 — no message
	// ever reached the model. Accept both shapes for safety.
	var data events.MessageCreatedData
	switch d := event.Data.(type) {
	case *events.MessageCreatedData:
		if d == nil {
			return nil
		}
		data = *d
	case events.MessageCreatedData:
		data = d
	default:
		return nil
	}
	return MessageCreatedMsg{
		ConversationID: event.ConversationID,
		Role:           data.Role,
		Content:        data.Content,
		Index:          data.Index,
		ToolCalls:      data.ToolCalls,
		ToolResult:     data.ToolResult,
		Reasoning:      data.Reasoning,
		Time:           event.Timestamp,
	}
}

func (a *EventAdapter) handleMessageUpdated(event *events.Event) tea.Msg {
	data, ok := event.Data.(events.MessageUpdatedData)
	if !ok {
		return nil
	}
	return MessageUpdatedMsg{
		ConversationID: event.ConversationID,
		Index:          data.Index,
		LatencyMs:      data.LatencyMs,
		InputTokens:    data.InputTokens,
		OutputTokens:   data.OutputTokens,
		TotalCost:      data.TotalCost,
		Time:           event.Timestamp,
	}
}

func (a *EventAdapter) handleConversationStarted(event *events.Event) tea.Msg {
	data, ok := event.Data.(events.ConversationStartedData)
	if !ok {
		return nil
	}
	return ConversationStartedMsg{
		ConversationID: event.ConversationID,
		SystemPrompt:   data.SystemPrompt,
		Time:           event.Timestamp,
	}
}

// handleArenaEvents handles arena-specific custom events.
func (a *EventAdapter) handleArenaEvents(event *events.Event) tea.Msg {
	switch event.Type { //nolint:exhaustive // only map arena-specific custom events
	case events.EventType("arena.run.started"):
		return RunStartedMsg{
			RunID:    event.ExecutionID,
			Scenario: readString(event.Data, "scenario"),
			Provider: readString(event.Data, "provider"),
			Region:   readString(event.Data, "region"),
			Time:     event.Timestamp,
		}
	case events.EventType("arena.run.completed"):
		return RunCompletedMsg{
			RunID:    event.ExecutionID,
			Duration: readDuration(event.Data, "duration"),
			Cost:     readFloat(event.Data, "cost"),
			Time:     event.Timestamp,
		}
	case events.EventType("arena.run.failed"):
		return RunFailedMsg{
			RunID: event.ExecutionID,
			Error: readError(event.Data, "error"),
			Time:  event.Timestamp,
		}
	case events.EventType("arena.turn.started"):
		return TurnStartedMsg{
			RunID:     event.ExecutionID,
			TurnIndex: readInt(event.Data, "turn_index"),
			Role:      readString(event.Data, "role"),
			Scenario:  readString(event.Data, "scenario"),
			Time:      event.Timestamp,
		}
	case events.EventType("arena.turn.completed"), events.EventType("arena.turn.failed"):
		return TurnCompletedMsg{
			RunID:     event.ExecutionID,
			TurnIndex: readInt(event.Data, "turn_index"),
			Role:      readString(event.Data, "role"),
			Scenario:  readString(event.Data, "scenario"),
			Error:     readError(event.Data, "error"),
			Time:      event.Timestamp,
		}
	default:
		return nil
	}
}

func (a *EventAdapter) logf(level string, event *events.Event, format string, args ...interface{}) tea.Msg {
	return logging.Msg{
		Timestamp: event.Timestamp,
		Level:     level,
		Message:   fmt.Sprintf(format, args...),
	}
}

func providerName(event *events.Event) string {
	if data, ok := event.Data.(*events.ProviderCallStartedData); ok {
		return data.Provider
	}
	if data, ok := event.Data.(*events.ProviderCallCompletedData); ok {
		return data.Provider
	}
	if data, ok := event.Data.(*events.ProviderCallFailedData); ok {
		return data.Provider
	}
	return ""
}

func providerModel(event *events.Event) string {
	if data, ok := event.Data.(*events.ProviderCallStartedData); ok {
		return data.Model
	}
	if data, ok := event.Data.(*events.ProviderCallCompletedData); ok {
		return data.Model
	}
	if data, ok := event.Data.(*events.ProviderCallFailedData); ok {
		return data.Model
	}
	return ""
}

func middlewareName(event *events.Event) string {
	if data, ok := event.Data.(events.MiddlewareEventData); ok {
		return data.Name
	}
	if data, ok := event.Data.(*events.MiddlewareEventData); ok {
		return data.Name
	}
	return ""
}

func toolName(event *events.Event) string {
	if data, ok := event.Data.(events.ToolCallEventData); ok {
		return data.ToolName
	}
	if data, ok := event.Data.(*events.ToolCallEventData); ok {
		return data.ToolName
	}
	return ""
}

func validationName(event *events.Event) string {
	if data, ok := event.Data.(events.ValidationEventData); ok {
		return data.ValidatorName
	}
	if data, ok := event.Data.(*events.ValidationEventData); ok {
		return data.ValidatorName
	}
	return ""
}

func eventError(event *events.Event) error {
	switch data := event.Data.(type) {
	case *events.ProviderCallFailedData:
		return data.Error
	case events.MiddlewareEventData:
		return data.Error
	case events.ToolCallEventData:
		return data.Error
	case events.ValidationEventData:
		return data.Error
	default:
		return nil
	}
}

func ternary[T any](cond bool, a, b T) T {
	if cond {
		return a
	}
	return b
}

func (a *EventAdapter) deliver(msg tea.Msg) {
	if a.send != nil {
		a.send(msg)
	} else if a.model != nil {
		a.model.Update(msg)
	}
}

func readString(data events.EventData, key string) string {
	if d, ok := data.(events.CustomEventData); ok {
		if v, ok := d.Data[key].(string); ok {
			return v
		}
	}
	return ""
}

func readDuration(data events.EventData, key string) time.Duration {
	if d, ok := data.(events.CustomEventData); ok {
		if v, ok := d.Data[key].(time.Duration); ok {
			return v
		}
	}
	return 0
}

func readFloat(data events.EventData, key string) float64 {
	if d, ok := data.(events.CustomEventData); ok {
		if v, ok := d.Data[key].(float64); ok {
			return v
		}
	}
	return 0
}

func readError(data events.EventData, key string) error {
	if d, ok := data.(events.CustomEventData); ok {
		if err, ok := d.Data[key].(error); ok {
			return err
		}
		if msg, ok := d.Data[key].(string); ok && msg != "" {
			return fmt.Errorf("%s", msg)
		}
	}
	return nil
}

func readInt(data events.EventData, key string) int {
	if d, ok := data.(events.CustomEventData); ok {
		if v, ok := d.Data[key].(int); ok {
			return v
		}
	}
	return 0
}
