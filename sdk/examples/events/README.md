# Runtime Events Example

This example demonstrates how to use the PromptKit runtime event system to observe and monitor LLM pipeline execution.

## Overview

The event system provides real-time visibility into:
- Pipeline lifecycle (started, completed, failed)
- Middleware execution (context building, validation, provider calls)
- Provider operations (API calls, token usage, costs)
- Tool executions
- State management operations

## Running the Example

```bash
cd sdk/examples/events
go run main.go
```

## Expected Output

```
[2025-12-01T12:00:00Z] pipeline.started
[2025-12-01T12:00:00Z] middleware.started
[2025-12-01T12:00:00Z] middleware.completed
[2025-12-01T12:00:01Z] provider.call.started
[2025-12-01T12:00:01Z] provider.call.completed
[2025-12-01T12:00:01Z] pipeline.completed
```

## Key Concepts

### Event Bus

The event bus is a pub/sub system that distributes events to registered listeners:

```go
eventBus := events.NewEventBus()
```

### Event Listeners

Add listeners to conversations to receive events:

```go
conversation.AddEventListener(func(e *events.Event) {
    fmt.Printf("[%s] %s\n", e.Timestamp.Format(time.RFC3339), e.Type)
})
```

### Event Types

Common event types include:
- `pipeline.started` - Pipeline execution begins
- `pipeline.completed` - Pipeline execution succeeds
- `pipeline.failed` - Pipeline execution fails
- `middleware.started` - Middleware begins processing
- `middleware.completed` - Middleware finishes successfully
- `middleware.failed` - Middleware encounters an error
- `provider.call.started` - LLM API call begins
- `provider.call.completed` - LLM API call succeeds
- `provider.call.failed` - LLM API call fails
- `conversation.started` - New conversation started
- `message.created` - Message added to conversation
- `message.updated` - Message metadata updated (cost, latency)
- `stream.interrupted` - Stream was interrupted

## Use Cases

### Logging

Monitor all pipeline activity:

```go
conversation.AddEventListener(func(e *events.Event) {
    log.Printf("[%s] %s: %+v", e.Type, e.Timestamp, e.Data)
})
```

### Metrics Collection

Track performance and costs:

```go
conversation.AddEventListener(func(e *events.Event) {
    if e.Type == events.EventProviderCallCompleted {
        data := e.Data.(events.ProviderCallCompletedData)
        metrics.RecordLatency(data.Duration)
        metrics.RecordCost(data.Cost)
        metrics.RecordTokens(data.InputTokens + data.OutputTokens)
    }
})
```

### Debugging

Trace execution flow:

```go
conversation.AddEventListener(func(e *events.Event) {
    if strings.Contains(string(e.Type), "failed") {
        debugLog.Printf("FAILURE: %s - %+v", e.Type, e.Data)
    }
})
```

## Related Documentation

- [Event System Architecture](../../../docs/src/content/architecture/runtime-events.md)
- [SDK How-To: Monitor Events](../../../docs/src/content/sdk/how-to/monitor-events.md)
- [SDK Explanation: Observability](../../../docs/src/content/sdk/explanation/observability.md)