---
title: 'Tutorial 6: Observability'
sidebar:
  order: 6
verified:
  commit: 885cafa787a84bfba66f158fe51cda15a077ec23
  sources:
    - runtime/events/types.go
    - sdk/examples/hello/hello.pack.json
    - sdk/examples/hooks/main_interactive.go
    - sdk/hooks/events.go
    - sdk/session_vars.go
---
In this tutorial we subscribe to SDK events with the `hooks` package, then use them to log activity, report failures and count tool calls.

## Prerequisites

Complete [Tutorial 1](/sdk/tutorials/01-first-conversation/). The examples reuse its `hello.pack.json`.

## Step 1: Subscribe to events

We use the `hooks` package to listen for events:

```go
package main

import (
    "context"
    "fmt"
    "log"

    "github.com/AltairaLabs/PromptKit/sdk/v2"
    "github.com/AltairaLabs/PromptKit/sdk/v2/hooks"
    "github.com/AltairaLabs/PromptKit/runtime/v2/events"
)

func main() {
    conv, err := sdk.Open("./hello.pack.json", "chat")
    if err != nil {
        log.Fatal(err)
    }
    defer conv.Close()
    conv.SetVar("user_name", "World")

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

Run it. The output shows `[PROVIDER] Call started`, then `[PROVIDER] Call completed`, then the model's reply.

## Step 2: Pick event types

The `runtime/events` package defines each event as an `events.EventType`. These are the provider, tool, pipeline and stream events:

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

Each constant is a type we can pass to `hooks.On`.

## Step 3: Monitor tool calls

To track tool execution:

```go
hooks.OnToolCall(conv, func(name string, args map[string]any) {
    fmt.Printf("[TOOL] Calling %s with %v\n", name, args)
})
```

When the model calls a tool, the program prints `[TOOL] Calling <name> with <arguments>`.

## Step 4: Log all events

We create a logger for every event:

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

Each event now writes a log line with its time and type.

## Step 5: Monitor errors

To track pipeline and provider failures:

```go
hooks.On(conv, events.EventProviderCallFailed, func(e *events.Event) {
    log.Printf("[ERROR] Provider call failed")
})

hooks.On(conv, events.EventPipelineFailed, func(e *events.Event) {
    log.Printf("[ERROR] Pipeline failed")
})
```

A failed provider call or pipeline logs an `[ERROR]` line.

## Step 6: Collect metrics

To collect metrics for monitoring systems:

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

After `Attach`, `ToolCalls` counts started tool calls and `Errors` counts failed ones.

## Next steps

- **[How-To: Monitor Events](/sdk/how-to/observability/monitor-events/)** - Advanced monitoring
- **[Explanation: Observability](/sdk/explanation/observability/)** - Architecture deep-dive
