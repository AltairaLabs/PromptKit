---
title: SDK Architecture
sidebar:
  order: 1
---
Understanding the pack-first design and component relationships.

## Design Principles

1. **Pack-First** - Pack file is the single source of truth
2. **Minimal Boilerplate** - 5 lines to hello world
3. **Type Safety** - Leverage Go's type system
4. **Thread Safe** - All operations are concurrent-safe
5. **Extensible** - Hooks and custom handlers

## Architecture Overview

```d2
direction: down

Application: {
  label: "Application\n\nconv, _ := sdk.Open(\"pack.json\", \"chat\")\nresp, _ := conv.Send(ctx, \"Hello\")"
}

SDK Layer: {
  PackLoader: {
    label: "PackLoader\n- Load pack\n- Validate\n- Cache"
  }
  Conversation: {
    label: "Conversation\n- Variables\n- Tools\n- Messages"
  }
  Hooks: {
    label: "Hooks\n- Events\n- Subscribe"
  }
  Evals: {
    label: "Evals\n- Dispatchers\n- Metrics\n- Middleware"
  }
}

Runtime Layer: {
  Providers: {
    label: "Providers\n- OpenAI\n- Anthropic\n- Google"
  }
  Pipeline: {
    label: "Pipeline\n- Execute\n- Stream\n- Context"
  }
  Tools: {
    label: "Tools\n- Registry\n- Handlers\n- HTTP"
  }
  EventBus: {
    label: "EventBus\n- Publish\n- Subscribe\n- EventStore"
  }
}

Application -> SDK Layer -> Runtime Layer
```

## Core Components

### PackLoader

Loads and validates pack files:

```go
// Internal - used by sdk.Open()
loader := packloader.New()
pack, err := loader.Load("./app.pack.json")
```

**Responsibilities:**
- Parse JSON pack files
- Validate schema
- Cache loaded packs
- Resolve prompt references

### Conversation

The `Conversation` type is the main interaction point. Its internal fields are unexported;
the public API includes `Send()`, `Stream()`, `SetVar()`, `GetVar()`, `OnTool()`, and more.

**Responsibilities:**
- Manage variables
- Register tool handlers
- Execute messages
- Maintain history
- Emit events

### Hooks (EventBus)

Observability through events:

```go
type Event struct {
    Type           EventType  // e.g. events.EventProviderCallCompleted
    Timestamp      time.Time
    RunID          string
    SessionID      string
    ConversationID string
    Data           EventData  // Type-specific payload (e.g. *ProviderCallCompletedData)
}
```

**Event Flow:**
1. `conv.Send()` emits `EventPipelineStarted`
2. Provider call emits `EventProviderCallStarted` / `EventProviderCallCompleted`
3. Tool calls emit `EventToolCallStarted` / `EventToolCallCompleted`
4. Pipeline completion emits `EventPipelineCompleted`
5. Failures emit `EventProviderCallFailed` / `EventPipelineFailed`

The EventBus supports pluggable persistence via `EventStore` and fan-out to multiple listeners. See [Observability](observability) for the full event architecture.

### Evals

Automated quality checks on LLM outputs, defined in pack files and executed via dispatchers:

- **EvalDispatcher** routes eval requests — `InProcDispatcher` runs synchronously, `EventDispatcher` publishes to an event bus for async workers, `NoOpDispatcher` defers to `EventBusEvalListener`
- **EvalRunner** executes eval handlers (deterministic checks like `contains`, `regex`, `json_valid`, `tools_called`, or LLM judge evaluations) with timeout and panic recovery
- **ResultWriters** record outcomes — `MetricResultWriter` feeds a `MetricCollector` for Prometheus metrics, `MetadataResultWriter` attaches results to message metadata

**Trigger patterns:**
- `every_turn` — after each assistant response
- `on_session_complete` — when a session closes
- `sample_turns` / `sample_sessions` — deterministic hash-based sampling

The SDK eval middleware hooks into `Send()` (turn evals, async) and `Close()` (session evals, sync). Arena uses `PackEvalHook` to run evals against live or recorded conversations.

## Request Flow

```d2
direction: down

send: conv.Send(ctx, "Hello")
emit_start: Emit EventPipelineStarted
build: Build Context {
  label: "Build Context\n- System prompt\n- Variables\n- History\n- Tool defs"
}
provider: Provider Call (OpenAI, etc.)
process: Process Response {
  label: "Process Response\n- Tool calls?\n- Update history"
}
emit_complete: Emit EventPipelineCompleted
return: Return Response

send -> emit_start -> build -> provider -> process -> emit_complete -> return
```

## Tool Execution

```d2
direction: down

response: LLM Response with tool_call
lookup: Lookup Handler {
  label: "Lookup Handler\nconv.handlers[name]"
}

on_tool: OnTool
on_tool_async: OnToolAsync
auto_ok: Auto-OK
pending: Pending
execute: Execute Handler
wait: Wait for Resolve/Reject
emit: Emit EventToolCallCompleted
continue: Continue conversation

response -> lookup
lookup -> on_tool
lookup -> on_tool_async
on_tool -> execute
on_tool_async -> auto_ok
on_tool_async -> pending
auto_ok -> execute
execute -> emit
pending -> wait
wait -> continue
emit -> continue
```

## State Management

### Variable Storage

Variables are stored internally in the session layer with thread-safe access:
- `SetVar(name, value string)` acquires write lock
- `GetVar(name string) (string, bool)` acquires read lock
- Concurrent access is safe

### Message History

Messages are managed through the session and pipeline layers. Use the public API:

```go
// Get all messages
messages := conv.Messages(ctx)

// Clear history
_ = conv.Clear()
```

## Thread Safety

All Conversation methods are thread-safe:

```go
// Safe concurrent access
var wg sync.WaitGroup

for i := 0; i < 10; i++ {
    wg.Add(1)
    go func(n int) {
        defer wg.Done()
        conv.SetVar(fmt.Sprintf("key_%d", n), fmt.Sprintf("%d", n))
    }(i)
}

wg.Wait()
```

## See Also

- [Observability](observability)
- [API Reference](../reference/)
