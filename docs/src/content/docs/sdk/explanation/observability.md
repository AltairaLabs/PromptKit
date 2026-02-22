---
title: Observability
sidebar:
  order: 2
---
Understanding the event system in SDK.

## Overview

SDK uses an event-based observability system through the `hooks` package (in `sdk/hooks`) and the `events` package (in `runtime/events`). Events are emitted at key points during execution, allowing you to monitor, debug, audit, and evaluate your applications.

The system is built around an **EventBus** that supports pluggable persistence (`EventStore`), binary media storage (`BlobStore`), and fan-out to multiple listeners — including the evals framework.

## Event Types

Events are defined as `events.EventType` in the `runtime/events` package. There are 33 event types across 10 categories:

### Pipeline Events

```go
EventPipelineStarted   EventType = "pipeline.started"
EventPipelineCompleted EventType = "pipeline.completed"
EventPipelineFailed    EventType = "pipeline.failed"
```

### Middleware Events

```go
EventMiddlewareStarted   EventType = "middleware.started"
EventMiddlewareCompleted EventType = "middleware.completed"
EventMiddlewareFailed    EventType = "middleware.failed"
```

### Stage Events

```go
EventStageStarted   EventType = "stage.started"
EventStageCompleted EventType = "stage.completed"
EventStageFailed    EventType = "stage.failed"
```

### Provider Events

```go
EventProviderCallStarted   EventType = "provider.call.started"
EventProviderCallCompleted EventType = "provider.call.completed"
EventProviderCallFailed    EventType = "provider.call.failed"
```

### Tool Events

```go
EventToolCallStarted   EventType = "tool.call.started"
EventToolCallCompleted EventType = "tool.call.completed"
EventToolCallFailed    EventType = "tool.call.failed"
```

### Validation Events

```go
EventValidationStarted EventType = "validation.started"
EventValidationPassed  EventType = "validation.passed"
EventValidationFailed  EventType = "validation.failed"
```

### Context & State Events

```go
EventContextBuilt          EventType = "context.built"
EventTokenBudgetExceeded   EventType = "context.token_budget_exceeded"
EventStateLoaded           EventType = "state.loaded"
EventStateSaved            EventType = "state.saved"
```

### Message Events

```go
EventMessageCreated EventType = "message.created"
EventMessageUpdated EventType = "message.updated"
EventConversationStarted EventType = "conversation.started"
```

### Multimodal Events

```go
EventAudioInput         EventType = "audio.input"
EventAudioOutput        EventType = "audio.output"
EventAudioTranscription EventType = "audio.transcription"
EventVideoFrame         EventType = "video.frame"
EventScreenshot         EventType = "screenshot"
EventImageInput         EventType = "image.input"
EventImageOutput        EventType = "image.output"
```

### Stream Events

```go
EventStreamInterrupted EventType = "stream.interrupted"
```

## EventBus Architecture

The `EventBus` is the central event dispatch mechanism. It accepts published events and fans them out to registered listeners.

```
Publisher ──► EventBus ──┬──► EventStore (sync persist)
                         ├──► Listener A (async)
                         ├──► Listener B (async)
                         └──► Listener C (async)
```

**Key behaviors:**

- **Sync persistence**: When an `EventStore` is configured, events are persisted _before_ listener dispatch. This guarantees durability.
- **Async listeners**: Listeners are invoked in goroutines after persistence. Each listener call is wrapped in panic recovery.
- **Type-filtered subscriptions**: `Subscribe(eventType, listener)` registers for a specific event type. `SubscribeAll(listener)` receives every event.

```go
bus := events.NewEventBus()

// Optional: persist events to disk
bus.WithStore(events.NewFileEventStore("./recordings"))

// Subscribe to specific events
bus.Subscribe(events.EventProviderCallCompleted, func(e *events.Event) {
    log.Printf("Provider call took %v", e.Data.(*events.ProviderCallCompletedData).Duration)
})

// Subscribe to all events
bus.SubscribeAll(func(e *events.Event) {
    log.Printf("[%s] %s", e.Timestamp.Format("15:04:05"), e.Type)
})
```

### Recording & EventStore

The `EventStore` interface provides pluggable event persistence:

```go
type EventStore interface {
    Append(ctx context.Context, event *Event) error
    Query(ctx context.Context, filter *EventFilter) ([]*Event, error)
    Stream(ctx context.Context, sessionID string) (<-chan *Event, error)
    Close() error
}
```

**FileEventStore** is the built-in implementation. It persists events as JSONL (one file per session) and supports querying by session, conversation, event type, and time range.

For multimodal recordings, **BlobStore** handles large binary payloads (audio, video, images) separately from the event stream:

```go
type BlobStore interface {
    Store(ctx context.Context, sessionID string, data []byte, mimeType string) (*BinaryPayload, error)
    Load(ctx context.Context, ref string) ([]byte, error)
    Close() error
}
```

**RecordingStage** is a pipeline stage that publishes content-carrying events (like `message.created`) to the EventBus as elements flow through the pipeline. It observes without modifying data, making it safe to insert at any position:

```go
pipeline := stage.NewPipelineBuilder().
    Chain(
        stage.NewRecordingStage(eventBus, stage.RecordingStageConfig{Position: "input"}),
        stage.NewProviderStage(provider, tools, policy, config),
        stage.NewRecordingStage(eventBus, stage.RecordingStageConfig{Position: "output"}),
    ).
    Build()
```

### Eval Integration

The `EventBusEvalListener` subscribes to `message.created` events on the EventBus and automatically triggers pack evals:

```
EventBus ──► EventBusEvalListener ──► SessionAccumulator ──► EvalDispatcher ──► EvalRunner
```

1. **SessionAccumulator** accumulates messages per session, building conversation context incrementally
2. On each **assistant message**, turn evals are dispatched asynchronously
3. On **session close**, session-level evals run synchronously
4. Results flow to configured `ResultWriters` (MetricCollector, metadata attachment)

This pattern enables evals without explicit SDK middleware — events from RecordingStage or any other publisher are automatically evaluated. See [Arena Eval Framework](/arena/explanation/eval-framework/) for details.

## Event Flow

```
conv.Send(ctx, "Hello")
        │
        ▼
   PipelineStarted ──────────► EventBus ──► Listeners
        │
        ▼
   MiddlewareStarted ────────► EventBus
        │
        ▼
   ProviderCallStarted ─────► EventBus
        │
        ▼
   ProviderCallCompleted ───► EventBus
        │
        │ (if tool call)
        ├────────────────┐
        │                ▼
        │     ToolCallStarted ──► EventBus
        │                │
        │         Handler executes
        │                │
        │     ToolCallCompleted ─► EventBus
        │                │
        └────────────────┘
        │
        ▼
   MessageCreated ───────────► EventBus ──► EventStore (persist)
        │                                ──► EvalListener (trigger evals)
        ▼
   PipelineCompleted ────────► EventBus
        │
        ▼
   Return Response
```

## Subscribing to Events

```go
import (
    "github.com/AltairaLabs/PromptKit/sdk/hooks"
    "github.com/AltairaLabs/PromptKit/runtime/events"
)

// Subscribe to a specific event type
hooks.On(conv, events.EventProviderCallCompleted, func(e *events.Event) {
    log.Printf("Provider call completed")
})

// Subscribe to all events
hooks.OnEvent(conv, func(e *events.Event) {
    log.Printf("Event: %s", e.Type)
})

// Subscribe to tool calls specifically
hooks.OnToolCall(conv, func(name string, args map[string]any) {
    log.Printf("Tool: %s", name)
})

// Subscribe to provider calls
hooks.OnProviderCall(conv, func(model string, inputTokens, outputTokens int, cost float64) {
    log.Printf("Model %s: %d in, %d out, $%.4f", model, inputTokens, outputTokens, cost)
})
```

## Event Structure

```go
// From runtime/events package
type Event struct {
    Type           EventType
    Timestamp      time.Time
    RunID          string
    SessionID      string
    ConversationID string
    Data           EventData  // Type-specific payload
}
```

Each event type has a corresponding `Data` struct. For example, `ProviderCallCompletedData` includes `Duration`, `InputTokens`, `OutputTokens`, `Cost`, and `FinishReason`.

## Use Cases

### Logging

```go
func attachLogger(conv *sdk.Conversation) {
    hooks.OnEvent(conv, func(e *events.Event) {
        log.Printf("[%s] %s",
            e.Timestamp.Format("15:04:05"),
            e.Type,
        )
    })
}
```

### Metrics

```go
type Metrics struct {
    ToolCalls int64
    Errors    int64
    mu        sync.Mutex
}

func (m *Metrics) Attach(conv *sdk.Conversation) {
    hooks.On(conv, events.EventToolCallStarted, func(e *events.Event) {
        m.mu.Lock()
        m.ToolCalls++
        m.mu.Unlock()
    })

    hooks.On(conv, events.EventToolCallFailed, func(e *events.Event) {
        m.mu.Lock()
        m.Errors++
        m.mu.Unlock()
    })
}
```

### Debugging

```go
func enableDebug(conv *sdk.Conversation) {
    hooks.OnEvent(conv, func(e *events.Event) {
        log.Printf("[DEBUG] %s: %s", e.Timestamp.Format("15:04:05"), e.Type)
    })

    hooks.OnToolCall(conv, func(name string, args map[string]any) {
        log.Printf("[DEBUG] Tool: %s(%v)", name, args)
    })
}
```

## Thread Safety

Event handlers are called asynchronously in a separate goroutine (see `EventBus.Publish` in `runtime/events/bus.go`). Use appropriate synchronization if handlers access shared state, as they run concurrently with the calling code.

## See Also

- [How-To: Monitor Events](../how-to/monitor-events)
- [How-To: Export Traces with OTLP](/runtime/how-to/export-traces-otlp) — send session traces to OpenTelemetry backends
- [Telemetry Reference](/runtime/reference/telemetry) — OTLP exporter API, span attributes, and semantic conventions
- [Arena Eval Framework](/arena/explanation/eval-framework/)
- [Session Recording](/arena/explanation/session-recording/)
- [Tutorial 6: Observability](../tutorials/06-media-storage)
