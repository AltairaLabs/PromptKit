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

Events are defined as `events.EventType` in the `runtime/events` package. There are 34 event types across 10 categories:

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
EventContextCompacted      EventType = "context.compacted"
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
store, _ := events.NewFileEventStore("./recordings")
bus.SubscribeAll(store.OnEvent)

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

**MessageBroadcastStage** publishes `message.created` on the EventBus as each complete message arrives. It is added whenever an event bus is configured — no `EventStore`, no `WithRecording()`, no state store — so subscribing to `message.created` is the supported way to watch a conversation unfold.

**RecordingStage** writes the same event type **directly to the EventStore, bypassing the EventBus**. That is what lets it keep full binary data for session replay, and why it is synchronous and lossless where the bus is async and lossy. RecordingStages observe without modifying data, making them safe to insert at any position.

:::caution[Binary never goes on the bus]
Not "usually", and not "only when recording is off" — always. Handling binary is
what the opt-in recording route is *for*, and that route never publishes, so
enabling recording does not start putting media on the bus.

This is about **bytes, not content**. Message text, an eval's structured value,
a judge's reasoning all travel on the bus — a live consumer that received a
score with no idea what was measured would be useless. What they do not carry is
audio samples or image data: for media you get the MIME type, dimensions, size
and a URL reference, and the payload stays behind the recording route.

If you are adding an event, read
[Adding a new event](/architecture/runtime-events/#adding-a-new-event) first —
it covers this and the three other decisions that have been got wrong silently.
:::

:::note[Two routes, different guarantees]
Events reach consumers two ways, and which route a type takes is not visible
from its name:

| Route | Delivery | Reaches |
|-------|----------|---------|
| `Emitter` → `EventBus` | async, worker-pooled, **lossy** | bus subscribers |
| `RecordingStage` → `EventStore.Append` | synchronous, **lossless**, opt-in | the store only |

`message.created` takes **both** routes, and both build their payload with
`events.NewMessageCreatedData`, so they carry the same data with exactly one
deliberate difference:

| | Binary content parts | Needs |
|---|---|---|
| Bus (`MessageBroadcastStage`) | stripped to metadata | an event bus |
| Store (`RecordingStage`) | retained in full | `WithRecording()` + an `EventStore` |

Two things a consumer needs to know:

- **The bus is lossy.** A live view can miss a message under burst. The state
  store remains the source of truth for a transcript.
- **Arrival order is not publish order.** The bus dispatches through a worker
  pool. Read `MessageCreatedData.Index`, which is transcript-absolute, rather
  than relying on the order events turn up in.

The remaining trap is not which route a *type* takes but which route a *field*
lands on. That shipped once, in v1.5.12: an accumulated reasoning trace was put
on `message.created` when only the recording route existed, and reached nobody.
Bus-delivered reasoning arrives as `reasoning.completed`.
:::

To subscribe:

```go
bus := events.NewEventBus()
bus.Subscribe(events.EventMessageCreated, func(e *events.Event) {
    if d, ok := e.Data.(*events.MessageCreatedData); ok {
        fmt.Printf("[%d] %s: %s\n", d.Index, d.Role, d.Content)
    }
})

conv, _ := sdk.Open("./app.pack.json", "assistant", sdk.WithEventBus(bus))
```

To enable RecordingStages via the SDK, use `WithRecording()`:

```go
conv, _ := sdk.Open("./app.pack.json", "assistant",
    sdk.WithRecording(nil), // defaults: audio=true, video=false, images=true
)
```

This inserts an input RecordingStage (after template assembly) and an output RecordingStage (before state store save). For manual pipeline construction:

```go
// NewRecordingStage takes an EventStore, not an EventBus — recording bypasses
// the bus entirely.
pipeline := stage.NewPipelineBuilder().
    Chain(
        stage.NewRecordingStage(eventStore, stage.RecordingStageConfig{Position: "input"}),
        stage.NewProviderStage(provider, tools, policy, config),
        stage.NewRecordingStage(eventStore, stage.RecordingStageConfig{Position: "output"}),
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

This pattern enables evals without explicit SDK middleware — events from RecordingStage or any other publisher are automatically evaluated. See [Arena Eval Framework](https://promptarena.altairalabs.ai/arena/explanation/eval-framework/) for details.

### Content and redaction

Events carry customer data — tool arguments the model composed, tool results,
message text, and the output of any eval that judged them. Two controls, doing
different jobs:

| Option | Scope | Default |
|--------|-------|---------|
| `WithTelemetryContentCapture(bool)` | OTel spans only | **off** |
| `WithEventRedactor(policy)` | every bus subscriber | none |

```go
sdk.WithEventRedactor(func(field, value string) string {
    if field == events.FieldToolCallArgs {
        return scrub(value)
    }
    return value
}),
```

The redactor is handed a field name, so a policy can be as coarse or as narrow
as it needs: `FieldMessageContent`, `FieldToolCallArgs`, `FieldToolResult`,
`FieldContentPart`, and — for eval events — `FieldEvalValue`,
`FieldEvalExplanation`, `FieldEvalDetails` and `FieldEvalEvidence`. An eval's
output quotes what it judged (a judge restates the answer, a violation's
evidence *is* the offending span), so it is redacted like any other content.
Nested strings inside a value or a details map are rewritten too, which is where
the quoted text usually lives.

Measurements are left alone: score, metric value, pass/fail, kind and timings
are facts *about* content rather than content, and scrubbing them would leave an
audit unable to tell a blocked guardrail from a passing one.

`Redacting` wraps each subscriber and hands it its own **copy**, so redacting
for the tracer does not strip a store that should keep the original. Redaction
is applied per consumer rather than at emit time deliberately: consumers have
different entitlements — recording is meant to hold content, a trace exported to
a third-party APM is not.

`WithRecording()` is unaffected by both, since `RecordingStage` never touches the
bus.

See the runnable
[telemetry-redaction example](https://github.com/AltairaLabs/PromptKit/tree/main/sdk/examples/telemetry-redaction).

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
    "github.com/AltairaLabs/PromptKit/sdk/v2/hooks"
    "github.com/AltairaLabs/PromptKit/runtime/v2/events"
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

- [How-To: Monitor Events](/sdk/how-to/observability/monitor-events/)
- [How-To: Export Traces with OTLP](/runtime/how-to/observability/export-traces-otlp) — send session traces to OpenTelemetry backends
- [Telemetry Reference](/runtime/reference/telemetry) — OTLP exporter API, span attributes, and semantic conventions
- [Arena Eval Framework](https://promptarena.altairalabs.ai/arena/explanation/eval-framework/)
- [Session Recording](https://promptarena.altairalabs.ai/arena/explanation/session-recording/)
- [Tutorial 6: Observability](/sdk/tutorials/06-media-storage/)
