// Package web provides the HTTP server and SSE event streaming for the Arena web UI.
package web

import (
	"encoding/json"
	"sync"
	"time"

	"github.com/AltairaLabs/PromptKit/runtime/events"
)

// SSEEvent is the JSON structure sent to SSE clients.
type SSEEvent struct {
	Type           string      `json:"type"`
	Timestamp      time.Time   `json:"timestamp"`
	ExecutionID    string      `json:"executionId,omitempty"`
	ConversationID string      `json:"conversationId,omitempty"`
	Data           interface{} `json:"data,omitempty"`
}

// clientBufferSize is the channel buffer for each SSE client.
const clientBufferSize = 256

// EventAdapter subscribes to an events.Bus and fans out
// JSON-serialized events to registered SSE client channels.
type EventAdapter struct {
	mu      sync.RWMutex
	clients map[chan []byte]struct{}
}

// NewEventAdapter creates a new EventAdapter.
func NewEventAdapter() *EventAdapter {
	return &EventAdapter{
		clients: make(map[chan []byte]struct{}),
	}
}

// Subscribe subscribes the adapter to an event bus.
func (a *EventAdapter) Subscribe(bus events.Bus) {
	if bus == nil {
		return
	}
	bus.SubscribeAll(a.HandleEvent)
}

// Register adds a new SSE client and returns its event channel.
func (a *EventAdapter) Register() chan []byte {
	ch := make(chan []byte, clientBufferSize)
	a.mu.Lock()
	a.clients[ch] = struct{}{}
	a.mu.Unlock()
	return ch
}

// Unregister removes an SSE client. The caller is responsible for draining
// or discarding the channel after unregistering.
func (a *EventAdapter) Unregister(ch chan []byte) {
	a.mu.Lock()
	delete(a.clients, ch)
	a.mu.Unlock()
}

// HandleEvent converts a runtime event to JSON and broadcasts to all clients.
func (a *EventAdapter) HandleEvent(event *events.Event) {
	sse := a.mapEvent(event)
	if sse == nil {
		return
	}

	data, err := json.Marshal(sse)
	if err != nil {
		return
	}

	a.mu.RLock()
	for ch := range a.clients {
		select {
		case ch <- data:
		default:
			// Client buffer full — drop event to avoid blocking
		}
	}
	a.mu.RUnlock()
}

// mapEvent converts a runtime event to an SSEEvent.
func (a *EventAdapter) mapEvent(event *events.Event) *SSEEvent {
	sse := &SSEEvent{
		Type:           string(event.Type),
		Timestamp:      event.Timestamp,
		ExecutionID:    event.ExecutionID,
		ConversationID: event.ConversationID,
	}

	switch data := event.Data.(type) {
	case *events.ProviderCallStartedData:
		sse.Data = map[string]interface{}{
			"provider": data.Provider,
			"model":    data.Model,
		}
	case *events.ProviderCallCompletedData:
		sse.Data = map[string]interface{}{
			"provider": data.Provider,
			"model":    data.Model,
			"duration": data.Duration.Seconds(),
			"cost":     data.Cost,
		}
	case *events.ProviderCallFailedData:
		sse.Data = map[string]interface{}{
			"provider": data.Provider,
			"model":    data.Model,
			"error":    errorString(data.Error),
		}
	case events.MessageCreatedData:
		sse.Data = map[string]interface{}{
			"role":       data.Role,
			"content":    data.Content,
			"index":      data.Index,
			"toolCalls":  data.ToolCalls,
			"toolResult": data.ToolResult,
		}
	case events.MessageUpdatedData:
		sse.Data = map[string]interface{}{
			"index":        data.Index,
			"latencyMs":    data.LatencyMs,
			"inputTokens":  data.InputTokens,
			"outputTokens": data.OutputTokens,
			"totalCost":    data.TotalCost,
		}
	case events.ConversationStartedData:
		sse.Data = map[string]interface{}{
			"systemPrompt": data.SystemPrompt,
		}
	case events.MiddlewareCompletedData:
		sse.Data = map[string]interface{}{
			"name":     data.Name,
			"duration": data.Duration.Seconds(),
		}
	case events.ToolCallEventData:
		sse.Data = map[string]interface{}{
			"toolName": data.ToolName,
			"callId":   data.CallID,
			"status":   data.Status,
		}
	case *events.ToolCallEventData:
		sse.Data = map[string]interface{}{
			"toolName": data.ToolName,
			"callId":   data.CallID,
			"status":   data.Status,
		}
	case events.ValidationEventData:
		sse.Data = map[string]interface{}{
			"validatorName": data.ValidatorName,
			"validatorType": data.ValidatorType,
			"error":         errorString(data.Error),
			"monitorOnly":   data.MonitorOnly,
			"score":         data.Score,
		}
	case *events.ValidationEventData:
		sse.Data = map[string]interface{}{
			"validatorName": data.ValidatorName,
			"validatorType": data.ValidatorType,
			"error":         errorString(data.Error),
			"monitorOnly":   data.MonitorOnly,
			"score":         data.Score,
		}
	case events.CustomEventData:
		sse.Data = data.Data
	case *events.CustomEventData:
		sse.Data = data.Data
	default:
		// For unhandled event data types, still send the event type/timestamp
		sse.Data = nil
	}

	return sse
}

// errorString safely converts an error to a string.
func errorString(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}
