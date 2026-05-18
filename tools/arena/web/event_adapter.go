// Package web provides the HTTP server and SSE event streaming for the Arena web UI.
package web

import (
	"encoding/base64"
	"encoding/json"
	"sync"
	"time"

	"github.com/AltairaLabs/PromptKit/runtime/events"
	arenaaudio "github.com/AltairaLabs/PromptKit/tools/arena/audio"
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

	audioMu      sync.RWMutex
	audioClients map[chan []byte]struct{}
}

// NewEventAdapter creates a new EventAdapter.
func NewEventAdapter() *EventAdapter {
	return &EventAdapter{
		clients:      make(map[chan []byte]struct{}),
		audioClients: make(map[chan []byte]struct{}),
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

// RegisterAudio adds a client channel that receives audio SSE messages.
// Audio clients are distinct from regular event clients — only clients that
// explicitly opted in (via ?audio=1) receive audio frames. The same channel
// may also be registered via Register; the browser EventSource demuxes
// regular vs audio events by their `event:` line.
func (a *EventAdapter) RegisterAudio(ch chan []byte) {
	a.audioMu.Lock()
	a.audioClients[ch] = struct{}{}
	a.audioMu.Unlock()
}

// UnregisterAudio removes an audio client channel.
func (a *EventAdapter) UnregisterAudio(ch chan []byte) {
	a.audioMu.Lock()
	delete(a.audioClients, ch)
	a.audioMu.Unlock()
}

// AttachAudioRouter subscribes the adapter to a per-run AudioRouter.
// Frames are encoded as SSE "audio" events for clients that opted in
// via ?audio=1. The subscription lives until the router closes.
func (a *EventAdapter) AttachAudioRouter(runID string, router *arenaaudio.AudioRouter, rate int) {
	if a == nil || router == nil {
		return
	}
	consumer := router.Subscribe("sse-"+runID, audioConsumerBuffer)
	go func() {
		seq := int64(0)
		for frame := range consumer {
			seq++
			msg := encodeAudioSSE(runID, frame, rate, seq)
			a.broadcastAudio(msg)
		}
	}()
}

func (a *EventAdapter) broadcastAudio(msg []byte) {
	a.audioMu.RLock()
	for ch := range a.audioClients {
		select {
		case ch <- msg:
		default:
			// drop — client stalled
		}
	}
	a.audioMu.RUnlock()
}

// audioConsumerBuffer is the per-run AudioRouter subscription buffer for the
// SSE relay. Sized to absorb short bursts without dropping at the router edge.
const audioConsumerBuffer = 50

// bytesPerSample is the byte width of an int16 PCM sample (s16le).
const bytesPerSample = 2

// highByteShift is the bit shift used to extract the high byte of a uint16
// when encoding little-endian s16le.
const highByteShift = 8

// audioSSEPayload is the JSON envelope for an audio frame sent over SSE.
type audioSSEPayload struct {
	Type      string `json:"type"`
	RunID     string `json:"run_id"`
	Direction string `json:"direction"`
	Seq       int64  `json:"seq"`
	Rate      int    `json:"rate"`
	Samples   string `json:"samples"`
}

// encodeAudioSSE produces the `event: audio\ndata: {...}\n\n` byte slice for
// a single frame. JSON envelope keeps the wire format homogeneous with other
// SSE events.
func encodeAudioSSE(runID string, frame arenaaudio.Frame, rate int, seq int64) []byte {
	payload := audioSSEPayload{
		Type:      "audio",
		RunID:     runID,
		Direction: string(frame.Direction),
		Seq:       seq,
		Rate:      rate,
		Samples:   samplesToBase64(frame.Samples),
	}
	data, err := json.Marshal(payload)
	if err != nil {
		return nil
	}
	out := make([]byte, 0, len("event: audio\ndata: ")+len(data)+len("\n\n"))
	out = append(out, "event: audio\ndata: "...)
	out = append(out, data...)
	out = append(out, '\n', '\n')
	return out
}

// samplesToBase64 encodes int16 PCM samples as little-endian s16le bytes,
// then base64. This matches the wire format expected by the browser's
// AudioWorklet/decoder. The int16→uint16 cast is bit-pattern preserving
// (two's complement) and intentional for s16le encoding.
func samplesToBase64(samples []int16) string {
	b := make([]byte, bytesPerSample*len(samples))
	for i, s := range samples {
		//nolint:gosec // s16le wire format requires bit-pattern reinterpretation of int16 as uint16.
		u := uint16(s)
		b[bytesPerSample*i] = byte(u)
		b[bytesPerSample*i+1] = byte(u >> highByteShift)
	}
	return base64.StdEncoding.EncodeToString(b)
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
	case *events.MessageCreatedData:
		// runtime/events/emitter.go emits a pointer; without this case the
		// payload was silently dropped (nil data) and the frontend reducer
		// never aggregated messages into liveRun.messages.
		if data != nil {
			sse.Data = map[string]interface{}{
				"role":       data.Role,
				"content":    data.Content,
				"index":      data.Index,
				"toolCalls":  data.ToolCalls,
				"toolResult": data.ToolResult,
			}
		}
	case events.MessageUpdatedData:
		sse.Data = map[string]interface{}{
			"index":        data.Index,
			"latencyMs":    data.LatencyMs,
			"inputTokens":  data.InputTokens,
			"outputTokens": data.OutputTokens,
			"totalCost":    data.TotalCost,
		}
	case *events.MessageUpdatedData:
		if data != nil {
			sse.Data = map[string]interface{}{
				"index":        data.Index,
				"latencyMs":    data.LatencyMs,
				"inputTokens":  data.InputTokens,
				"outputTokens": data.OutputTokens,
				"totalCost":    data.TotalCost,
			}
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
		sse.Data = validationPayload(&data)
	case *events.ValidationEventData:
		sse.Data = validationPayload(data)
	case *events.TemplateStartedData:
		sse.Data = map[string]interface{}{
			"taskType":      data.TaskType,
			"variableCount": data.VariableCount,
			"modelOverride": data.ModelOverride,
		}
	case *events.TemplateRenderedData:
		sse.Data = map[string]interface{}{
			"taskType":      data.TaskType,
			"promptHash":    data.PromptHash,
			"variablesUsed": data.VariablesUsed,
			"renderPasses":  data.RenderPasses,
		}
	case *events.TemplateFailedData:
		sse.Data = map[string]interface{}{
			"taskType":   data.TaskType,
			"error":      data.Error,
			"unresolved": data.UnresolvedPlaceholders,
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

// validationPayload renders a guardrail firing for the SSE channel.
// Two switch cases (value + pointer) used to inline the same map literal,
// which trips goconst on the "enforced" key.
func validationPayload(data *events.ValidationEventData) map[string]interface{} {
	return map[string]interface{}{
		"validatorName": data.ValidatorName,
		"validatorType": data.ValidatorType,
		"error":         errorString(data.Error),
		"enforced":      data.Enforced,
		"score":         data.Score,
	}
}
