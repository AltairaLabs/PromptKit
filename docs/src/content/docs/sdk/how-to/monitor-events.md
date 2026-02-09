---
title: Monitor Events
sidebar:
  order: 5
---
Learn how to observe SDK operations with the `hooks` package.

## Basic Subscription

```go
import (
    "github.com/AltairaLabs/PromptKit/sdk/hooks"
    "github.com/AltairaLabs/PromptKit/runtime/events"
)

hooks.On(conv, events.EventProviderCallStarted, func(e *events.Event) {
    fmt.Printf("Provider call started\n")
})
```

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

## Monitor Tool Calls

```go
hooks.OnToolCall(conv, func(name string, args map[string]any) {
    fmt.Printf("Tool called: %s(%v)\n", name, args)
})
```

## Monitor Provider Calls

```go
hooks.OnProviderCall(conv, func(e *events.Event) {
    log.Printf("Provider call: %v", e)
})
```

## Log All Events

```go
func attachLogger(conv *sdk.Conversation) {
    hooks.OnEvent(conv, func(e *events.Event) {
        log.Printf("[%s] %s", e.Timestamp.Format("15:04:05"), e.Type)
    })
}
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

## Metrics Collection

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

## Debug Mode

```go
func enableDebug(conv *sdk.Conversation) {
    hooks.OnEvent(conv, func(e *events.Event) {
        log.Printf("[DEBUG] %s: %s", e.Timestamp.Format("15:04:05"), e.Type)
    })
}
```

## Complete Example

```go
package main

import (
    "context"
    "fmt"
    "log"

    "github.com/AltairaLabs/PromptKit/sdk"
    "github.com/AltairaLabs/PromptKit/sdk/hooks"
    "github.com/AltairaLabs/PromptKit/runtime/events"
)

func main() {
    conv, _ := sdk.Open("./app.pack.json", "chat")
    defer conv.Close()

    // Monitor all activity
    hooks.OnEvent(conv, func(e *events.Event) {
        log.Printf("[%s] %s", e.Timestamp.Format("15:04:05"), e.Type)
    })

    // Monitor tool calls specifically
    hooks.OnToolCall(conv, func(name string, args map[string]any) {
        log.Printf("Tool called: %s", name)
    })

    // Use normally
    ctx := context.Background()
    resp, _ := conv.Send(ctx, "Hello!")
    fmt.Println(resp.Text())
}
```

## See Also

- [Tutorial 6: Observability](../tutorials/06-media-storage)
- [Explanation: Observability](../explanation/observability)
