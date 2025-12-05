package tui

import (
	"fmt"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/AltairaLabs/PromptKit/runtime/events"
	"github.com/AltairaLabs/PromptKit/tools/arena/tui/logging"
)

// EventAdapter converts runtime events to bubbletea messages.
type EventAdapter struct {
	program *tea.Program
	model   *Model // For headless mode
}

// NewEventAdapter creates an adapter that forwards events to the TUI program.
func NewEventAdapter(program *tea.Program) *EventAdapter {
	return &EventAdapter{program: program}
}

// NewEventAdapterWithModel creates an adapter for headless mode.
func NewEventAdapterWithModel(model *Model) *EventAdapter {
	return &EventAdapter{model: model}
}

// Subscribe subscribes the adapter to an event bus.
func (a *EventAdapter) Subscribe(bus *events.EventBus) {
	if bus == nil {
		return
	}
	bus.SubscribeAll(a.HandleEvent)
}

// HandleEvent converts runtime events into TUI messages.
func (a *EventAdapter) HandleEvent(event *events.Event) {
	var msg tea.Msg

	switch event.Type { //nolint:exhaustive // only map TUI-relevant events
	case events.EventProviderCallStarted:
		msg = a.logf("INFO", event, "Provider call started: %s %s", providerName(event), providerModel(event))
	case events.EventProviderCallCompleted:
		if data, ok := event.Data.(*events.ProviderCallCompletedData); ok {
			msg = a.logf("INFO", event, "Provider call completed: %s %s (%.2fs, cost $%.4f)",
				providerName(event),
				providerModel(event),
				data.Duration.Seconds(),
				data.Cost)
		}
	case events.EventProviderCallFailed:
		msg = a.logf("ERROR", event, "Provider call failed: %s %s (%v)",
			providerName(event),
			providerModel(event),
			eventError(event))
	case events.EventMiddlewareStarted:
		msg = a.logf("INFO", event, "Middleware started: %s", middlewareName(event))
	case events.EventMiddlewareCompleted:
		if data, ok := event.Data.(events.MiddlewareCompletedData); ok {
			msg = a.logf("INFO", event, "Middleware completed: %s (%.2fs)", data.Name, data.Duration.Seconds())
		}
	case events.EventMiddlewareFailed:
		msg = a.logf("ERROR", event, "Middleware failed: %s (%v)", middlewareName(event), eventError(event))
	case events.EventToolCallStarted:
		msg = a.logf("INFO", event, "Tool call started: %s", toolName(event))
	case events.EventToolCallCompleted:
		msg = a.logf("INFO", event, "Tool call completed: %s", toolName(event))
	case events.EventToolCallFailed:
		msg = a.logf("ERROR", event, "Tool call failed: %s (%v)", toolName(event), eventError(event))
	case events.EventValidationStarted:
		msg = a.logf("INFO", event, "Validation started: %s", validationName(event))
	case events.EventValidationPassed:
		msg = a.logf("INFO", event, "Validation passed: %s", validationName(event))
	case events.EventValidationFailed:
		msg = a.logf("ERROR", event, "Validation failed: %s (%v)", validationName(event), eventError(event))
	case events.EventContextBuilt:
		if data, ok := event.Data.(events.ContextBuiltData); ok {
			msg = a.logf("INFO", event, "Context built (%d msgs, %d tokens)%s",
				data.MessageCount,
				data.TokenCount,
				ternary(data.Truncated, " [truncated]", ""))
		}
	case events.EventTokenBudgetExceeded:
		if data, ok := event.Data.(events.TokenBudgetExceededData); ok {
			msg = a.logf("ERROR", event, "Token budget exceeded: need %d, budget %d, excess %d",
				data.RequiredTokens,
				data.Budget,
				data.Excess)
		}
	case events.EventStateLoaded:
		if data, ok := event.Data.(events.StateLoadedData); ok {
			msg = a.logf("INFO", event, "State loaded: %s (%d messages)", data.ConversationID, data.MessageCount)
		}
	case events.EventStateSaved:
		if data, ok := event.Data.(events.StateSavedData); ok {
			msg = a.logf("INFO", event, "State saved: %s (%d messages)", data.ConversationID, data.MessageCount)
		}
	case events.EventStreamInterrupted:
		msg = a.logf("ERROR", event, "Stream interrupted: %s", readString(event.Data, "reason"))
	case events.EventType("arena.run.started"):
		msg = RunStartedMsg{
			RunID:    event.RunID,
			Scenario: readString(event.Data, "scenario"),
			Provider: readString(event.Data, "provider"),
			Region:   readString(event.Data, "region"),
			Time:     event.Timestamp,
		}
	case events.EventType("arena.run.completed"):
		msg = RunCompletedMsg{
			RunID:    event.RunID,
			Duration: readDuration(event.Data, "duration"),
			Cost:     readFloat(event.Data, "cost"),
			Time:     event.Timestamp,
		}
	case events.EventType("arena.run.failed"):
		msg = RunFailedMsg{
			RunID: event.RunID,
			Error: readError(event.Data, "error"),
			Time:  event.Timestamp,
		}
	case events.EventType("arena.turn.started"):
		msg = TurnStartedMsg{
			RunID:     event.RunID,
			TurnIndex: readInt(event.Data, "turn_index"),
			Role:      readString(event.Data, "role"),
			Scenario:  readString(event.Data, "scenario"),
			Time:      event.Timestamp,
		}
	case events.EventType("arena.turn.completed"), events.EventType("arena.turn.failed"):
		msg = TurnCompletedMsg{
			RunID:     event.RunID,
			TurnIndex: readInt(event.Data, "turn_index"),
			Role:      readString(event.Data, "role"),
			Scenario:  readString(event.Data, "scenario"),
			Error:     readError(event.Data, "error"),
			Time:      event.Timestamp,
		}
	case events.EventMessageCreated:
		if data, ok := event.Data.(events.MessageCreatedData); ok {
			// Map tool calls
			var toolCalls []MessageToolCall
			for _, tc := range data.ToolCalls {
				toolCalls = append(toolCalls, MessageToolCall{
					ID:   tc.ID,
					Name: tc.Name,
					Args: tc.Args,
				})
			}
			// Map tool result
			var toolResult *MessageToolResult
			if data.ToolResult != nil {
				toolResult = &MessageToolResult{
					ID:        data.ToolResult.ID,
					Name:      data.ToolResult.Name,
					Content:   data.ToolResult.Content,
					Error:     data.ToolResult.Error,
					LatencyMs: data.ToolResult.LatencyMs,
				}
			}
			msg = MessageCreatedMsg{
				ConversationID: event.ConversationID,
				Role:           data.Role,
				Content:        data.Content,
				Index:          data.Index,
				ToolCalls:      toolCalls,
				ToolResult:     toolResult,
				Time:           event.Timestamp,
			}
		}
	case events.EventMessageUpdated:
		if data, ok := event.Data.(events.MessageUpdatedData); ok {
			msg = MessageUpdatedMsg{
				ConversationID: event.ConversationID,
				Index:          data.Index,
				LatencyMs:      data.LatencyMs,
				InputTokens:    data.InputTokens,
				OutputTokens:   data.OutputTokens,
				TotalCost:      data.TotalCost,
				Time:           event.Timestamp,
			}
		}
	case events.EventConversationStarted:
		if data, ok := event.Data.(events.ConversationStartedData); ok {
			msg = ConversationStartedMsg{
				ConversationID: event.ConversationID,
				SystemPrompt:   data.SystemPrompt,
				Time:           event.Timestamp,
			}
		}
	default:
		// Ignore events we don't map yet
		return
	}

	a.send(msg)
}

func (a *EventAdapter) logf(level string, event *events.Event, format string, args ...interface{}) tea.Msg {
	return logging.Msg{
		Timestamp: event.Timestamp,
		Level:     level,
		Message:   fmt.Sprintf(format, args...),
	}
}

func providerName(event *events.Event) string {
	if data, ok := event.Data.(events.ProviderCallStartedData); ok {
		return data.Provider
	}
	if data, ok := event.Data.(events.ProviderCallCompletedData); ok {
		return data.Provider
	}
	if data, ok := event.Data.(events.ProviderCallFailedData); ok {
		return data.Provider
	}
	return ""
}

func providerModel(event *events.Event) string {
	if data, ok := event.Data.(events.ProviderCallStartedData); ok {
		return data.Model
	}
	if data, ok := event.Data.(events.ProviderCallCompletedData); ok {
		return data.Model
	}
	if data, ok := event.Data.(events.ProviderCallFailedData); ok {
		return data.Model
	}
	return ""
}

func middlewareName(event *events.Event) string {
	if data, ok := event.Data.(events.MiddlewareStartedData); ok {
		return data.Name
	}
	if data, ok := event.Data.(events.MiddlewareCompletedData); ok {
		return data.Name
	}
	if data, ok := event.Data.(events.MiddlewareFailedData); ok {
		return data.Name
	}
	return ""
}

func toolName(event *events.Event) string {
	if data, ok := event.Data.(events.ToolCallStartedData); ok {
		return data.ToolName
	}
	if data, ok := event.Data.(events.ToolCallCompletedData); ok {
		return data.ToolName
	}
	if data, ok := event.Data.(events.ToolCallFailedData); ok {
		return data.ToolName
	}
	return ""
}

func validationName(event *events.Event) string {
	if data, ok := event.Data.(events.ValidationStartedData); ok {
		return data.ValidatorName
	}
	if data, ok := event.Data.(events.ValidationPassedData); ok {
		return data.ValidatorName
	}
	if data, ok := event.Data.(events.ValidationFailedData); ok {
		return data.ValidatorName
	}
	return ""
}

func eventError(event *events.Event) error {
	switch data := event.Data.(type) {
	case events.ProviderCallFailedData:
		return data.Error
	case events.MiddlewareFailedData:
		return data.Error
	case events.ToolCallFailedData:
		return data.Error
	case events.ValidationFailedData:
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

func (a *EventAdapter) send(msg tea.Msg) {
	if a.program != nil {
		a.program.Send(msg)
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
