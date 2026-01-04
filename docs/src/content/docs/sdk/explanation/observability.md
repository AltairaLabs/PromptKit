---
title: Observability
sidebar:
  order: 2
---
Understanding the event system in SDK.

## Overview

SDK uses an event-based observability system through the `hooks` package. Events are emitted at key points during execution, allowing you to monitor, debug, and audit your applications.

## Event Types

```go
const (
    EventSend       = "send"        // Message sent
    EventResponse   = "response"    // Response received
    EventToolCall   = "tool_call"   // Tool invoked
    EventToolResult = "tool_result" // Tool returned
    EventError      = "error"       // Error occurred
    EventStream     = "stream"      // Stream chunk
)
```

## Event Flow

```
conv.Send(ctx, "Hello")
        │
        ▼
   EventSend ─────────► Subscriber
        │
        ▼
   Provider Call
        │
        ▼
   EventResponse ─────► Subscriber
        │
        │ (if tool call)
        ├────────────────┐
        │                ▼
        │          EventToolCall ──► Subscriber
        │                │
        │          Handler executes
        │                │
        │          EventToolResult ─► Subscriber
        │                │
        └────────────────┘
        │
        ▼
   Return Response
```

## Subscribing to Events

```go
import "github.com/AltairaLabs/PromptKit/sdk/hooks"

conv.Subscribe(hooks.EventSend, func(e hooks.Event) {
    log.Printf("Sent: %v", e.Data["message"])
})
```

## Event Structure

```go
type Event struct {
    Type      string         // Event type (EventSend, etc.)
    Timestamp time.Time      // When the event occurred
    Data      map[string]any // Event-specific data
}
```

## Event Data

### EventSend

```go
e.Data["message"]  // The message sent
```

### EventResponse

```go
e.Data["text"]     // Response text
e.Data["tokens"]   // Token count (if available)
```

### EventToolCall

```go
e.Data["tool"]     // Tool name
e.Data["args"]     // Tool arguments
```

### EventToolResult

```go
e.Data["tool"]     // Tool name
e.Data["result"]   // Tool result
```

### EventError

```go
e.Data["error"]    // The error
```

## Use Cases

### Logging

```go
func attachLogger(conv *sdk.Conversation) {
    events := []string{
        hooks.EventSend,
        hooks.EventResponse,
        hooks.EventToolCall,
        hooks.EventError,
    }
    
    for _, event := range events {
        name := event
        conv.Subscribe(name, func(e hooks.Event) {
            log.Printf("[%s] %s: %v",
                e.Timestamp.Format("15:04:05"),
                name,
                e.Data,
            )
        })
    }
}
```

### Metrics

```go
type Metrics struct {
    Messages  int64
    ToolCalls int64
    Errors    int64
    mu        sync.Mutex
}

func (m *Metrics) Attach(conv *sdk.Conversation) {
    conv.Subscribe(hooks.EventSend, func(e hooks.Event) {
        m.mu.Lock()
        m.Messages++
        m.mu.Unlock()
    })
    
    conv.Subscribe(hooks.EventToolCall, func(e hooks.Event) {
        m.mu.Lock()
        m.ToolCalls++
        m.mu.Unlock()
    })
    
    conv.Subscribe(hooks.EventError, func(e hooks.Event) {
        m.mu.Lock()
        m.Errors++
        m.mu.Unlock()
    })
}
```

### Debugging

```go
func enableDebug(conv *sdk.Conversation) {
    conv.Subscribe(hooks.EventSend, func(e hooks.Event) {
        fmt.Printf("📤 SEND: %v\n", e.Data)
    })
    
    conv.Subscribe(hooks.EventResponse, func(e hooks.Event) {
        fmt.Printf("📥 RESPONSE\n")
    })
    
    conv.Subscribe(hooks.EventToolCall, func(e hooks.Event) {
        fmt.Printf("🔧 TOOL: %s(%v)\n", e.Data["tool"], e.Data["args"])
    })
    
    conv.Subscribe(hooks.EventError, func(e hooks.Event) {
        fmt.Printf("❌ ERROR: %v\n", e.Data["error"])
    })
}
```

## Thread Safety

Event handlers are called synchronously on the goroutine that triggered the event. Use appropriate synchronization if handlers access shared state.

## See Also

- [How-To: Monitor Events](../how-to/monitor-events)
- [Tutorial 6: Observability](../tutorials/06-media-storage)
