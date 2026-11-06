---
title: 'Tutorial 6: Observability'
sidebar:
  order: 6
verified:
  commit: 8489bf3d95c7498f9de0f024769db9399c24e884
  sources:
    - runtime/events/bus.go
    - runtime/events/types.go
    - sdk/conversation.go
    - sdk/hooks/events.go
---
## What we will build

In this tutorial we build a weather assistant that reports what it does while it answers. We subscribe to SDK events with the `hooks` package to print provider calls and tool calls, log every event, and count tool calls and failures.

## Prerequisites

Complete [Tutorial 3: Tools](/sdk/tutorials/03-tool-integration/). We work in the same directory and reuse its `tools.pack.json`.

## Step 1: Subscribe to provider events

`hooks.On` runs a handler for one event type. Replace `main.go` with:

```go
package main

import (
    "context"
    "fmt"
    "log"

    "github.com/AltairaLabs/PromptKit/runtime/v2/events"
    "github.com/AltairaLabs/PromptKit/sdk/v2"
    "github.com/AltairaLabs/PromptKit/sdk/v2/hooks"
)

func main() {
    conv, err := sdk.Open("./tools.pack.json", "assistant")
    if err != nil {
        log.Fatal(err)
    }
    defer conv.Close()

    conv.OnTool("get_weather", func(args map[string]any) (any, error) {
        city, _ := args["city"].(string)
        return map[string]any{"city": city, "temperature": 22.5, "conditions": "Partly cloudy"}, nil
    })

    hooks.On(conv, events.EventProviderCallStarted, func(e *events.Event) {
        fmt.Println("[PROVIDER] Call started")
    })
    hooks.On(conv, events.EventProviderCallCompleted, func(e *events.Event) {
        fmt.Println("[PROVIDER] Call completed")
    })

    resp, err := conv.Send(context.Background(), "What's the weather in London?")
    if err != nil {
        log.Fatal(err)
    }
    fmt.Println(resp.Text())
}
```

Run it:

```bash
go run main.go
```

When the model calls `get_weather`, the turn makes two provider calls (one that asks for the tool, one that answers), so each line prints twice:

```text
[PROVIDER] Call started
[PROVIDER] Call completed
[PROVIDER] Call completed
[PROVIDER] Call started
It's currently 22.5°C and partly cloudy in London.
```

Each handler runs on its own goroutine, so lines from different handlers can appear in any order and can interleave with the reply. The reply's wording varies between runs.

## Step 2: Watch tool calls

`hooks.OnToolCall` passes each tool call's name and arguments. Add it before the `Send` call:

```go
hooks.OnToolCall(conv, func(name string, args map[string]any) {
    fmt.Printf("[TOOL] Calling %s with %v\n", name, args)
})
```

Run the program again. Among the provider lines it now prints:

```text
[TOOL] Calling get_weather with map[city:London]
```

The arguments are the ones the model chose, so they typically match the city in the question.

## Step 3: Log every event

`hooks.OnEvent` runs one handler for every event type. Add it before the `Send` call:

```go
hooks.OnEvent(conv, func(e *events.Event) {
    fmt.Println("[EVENT]", e.Type)
})
```

Run the program. A single handler sees events in the order they were published, so the `[EVENT]` lines trace the turn from `pipeline.started` to `pipeline.completed`. This excerpt shows the tool round:

```text
[EVENT] provider.call.started
[EVENT] provider.call.completed
[EVENT] tool.call.started
[EVENT] tool.call.completed
[EVENT] provider.call.started
[EVENT] provider.call.completed
```

The [runtime events page](/architecture/runtime-events/#event-types) lists every event type.

## Step 4: Count tool calls and failures

Add a `Metrics` type below `main`. It counts started tool calls and any failed tool call, provider call or pipeline:

```go
// Metrics counts tool calls and failures.
type Metrics struct {
    mu        sync.Mutex
    ToolCalls int
    Errors    int
}

func (m *Metrics) Attach(conv *sdk.Conversation) {
    hooks.On(conv, events.EventToolCallStarted, func(e *events.Event) {
        m.mu.Lock()
        m.ToolCalls++
        m.mu.Unlock()
    })
    for _, failed := range []events.EventType{
        events.EventToolCallFailed,
        events.EventProviderCallFailed,
        events.EventPipelineFailed,
    } {
        hooks.On(conv, failed, func(e *events.Event) {
            m.mu.Lock()
            m.Errors++
            m.mu.Unlock()
            fmt.Println("[ERROR]", e.Type)
        })
    }
}
```

Add `"sync"` to the imports. Attach the counter before the `Send` call:

```go
metrics := &Metrics{}
metrics.Attach(conv)
```

Then add these lines at the end of `main`:

```go
// Close waits for the handlers to finish the events they were given.
conv.Close()
fmt.Printf("Tool calls: %d, errors: %d\n", metrics.ToolCalls, metrics.Errors)
```

`Close()` waits for every handler to finish, so the counts are complete when they print. The deferred `Close()` that runs afterwards does nothing. Run the program. When the model calls the tool once, as it typically does here, the last line is:

```text
Tool calls: 1, errors: 0
```

## Next steps

- [Monitor Events](/sdk/how-to/observability/monitor-events/): Prometheus metrics, guardrail events and template debugging
- [Observability](/sdk/explanation/observability/): how events, hooks and telemetry fit together
