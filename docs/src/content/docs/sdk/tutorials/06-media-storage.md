---
title: 'Tutorial 6: Observability'
sidebar:
  order: 6
---
Learn how to monitor and observe your SDK applications with the hooks package.

## What You'll Learn

- Listen for SDK events with hooks
- Monitor tool calls and responses
- Track conversation flow
- Debug issues

## Why Observability?

Observability helps you:

- **Debug** - Understand what's happening in your application
- **Monitor** - Track performance and usage
- **Audit** - Log all actions for compliance
- **Optimize** - Find bottlenecks and improve

## Prerequisites

Complete [Tutorial 1](01-first-conversation) and understand basic SDK usage.

## Basic Event Subscription

Use the `hooks` package to listen for events:

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
    conv, err := sdk.Open("./hello.pack.json", "chat")
    if err != nil {
        log.Fatal(err)
    }
    defer conv.Close()

    // Subscribe to provider call events
    hooks.On(conv, events.EventProviderCallStarted, func(e *events.Event) {
        fmt.Printf("[PROVIDER] Call started\n")
    })

    hooks.On(conv, events.EventProviderCallCompleted, func(e *events.Event) {
        fmt.Printf("[PROVIDER] Call completed\n")
    })

    ctx := context.Background()
    resp, _ := conv.Send(ctx, "Hello!")
    fmt.Println(resp.Text())
}
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

## Monitoring Tool Calls

Track tool execution:

```go
hooks.OnToolCall(conv, func(name string, args map[string]any) {
    fmt.Printf("[TOOL] Calling %s with %v\n", name, args)
})
```

## Logging All Events

Create a comprehensive logger:

```go
func logAllEvents(conv *sdk.Conversation) {
    hooks.OnEvent(conv, func(e *events.Event) {
        log.Printf("[%s] %s",
            e.Timestamp.Format("15:04:05"),
            e.Type,
        )
    })
}

// Usage
conv, _ := sdk.Open("./pack.json", "chat")
logAllEvents(conv)
```

## Error Monitoring

Track pipeline and provider failures:

```go
hooks.On(conv, events.EventProviderCallFailed, func(e *events.Event) {
    log.Printf("[ERROR] Provider call failed")
})

hooks.On(conv, events.EventPipelineFailed, func(e *events.Event) {
    log.Printf("[ERROR] Pipeline failed")
})
```

## Metrics Collection

Collect metrics for monitoring systems:

```go
type Metrics struct {
    ToolCalls     int64
    Errors        int64
    mu            sync.Mutex
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

Enable verbose debugging:

```go
func enableDebug(conv *sdk.Conversation) {
    hooks.OnEvent(conv, func(e *events.Event) {
        log.Printf("[DEBUG] %s: %s", e.Timestamp.Format("15:04:05"), e.Type)
    })
}
```

## What You've Learned

✅ Subscribe to events with `hooks.On()`, `hooks.OnEvent()`, `hooks.OnToolCall()`
✅ Monitor tool calls and provider calls
✅ Track errors with event types
✅ Build metrics collection
✅ Enable debug logging  

## Next Steps

- **[How-To: Monitor Events](../how-to/monitor-events)** - Advanced monitoring
- **[Explanation: Observability](../explanation/observability)** - Architecture deep-dive

## Complete Example

See observability patterns in `sdk/examples/`.
