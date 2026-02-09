---
title: Observability
sidebar:
  order: 2
---
Understanding the event system in SDK.

## Overview

SDK uses an event-based observability system through the `hooks` package (in `sdk/hooks`) and the `events` package (in `runtime/events`). Events are emitted at key points during execution, allowing you to monitor, debug, and audit your applications.

## Event Types

Events are defined as `events.EventType` in the `runtime/events` package:

```go
const (
    EventProviderCallStarted   EventType = "provider.call.started"
    EventProviderCallCompleted EventType = "provider.call.completed"
    EventProviderCallFailed    EventType = "provider.call.failed"
    EventToolCallStarted       EventType = "tool.call.started"
    EventToolCallCompleted     EventType = "tool.call.completed"
    EventToolCallFailed        EventType = "tool.call.failed"
    EventPipelineStarted       EventType = "pipeline.started"
    EventPipelineCompleted     EventType = "pipeline.completed"
    EventPipelineFailed        EventType = "pipeline.failed"
    EventStreamInterrupted     EventType = "stream.interrupted"
)
```

## Event Flow

```
conv.Send(ctx, "Hello")
        │
        ▼
   PipelineStarted ──────────► Listener
        │
        ▼
   ProviderCallStarted ─────► Listener
        │
        ▼
   ProviderCallCompleted ───► Listener
        │
        │ (if tool call)
        ├────────────────┐
        │                ▼
        │     ToolCallStarted ──► Listener
        │                │
        │         Handler executes
        │                │
        │     ToolCallCompleted ─► Listener
        │                │
        └────────────────┘
        │
        ▼
   PipelineCompleted ───────► Listener
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
    SessionID      string
    ConversationID string
    // ... additional event-specific fields
}
```

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
- [Tutorial 6: Observability](../tutorials/06-media-storage)
