---
title: Observability & Events
docType: explanation
order: 6
---

# Observability & Events

Understanding how the event system provides observability into PromptKit pipeline execution.

## Why Events?

Traditional LLM SDKs provide limited visibility into execution. You send a request, get a response, and have no insight into what happened in between. This makes debugging, monitoring, and optimization difficult.

PromptKit's event system solves this by emitting detailed events for every stage of execution:

- **What** happened (event type)
- **When** it happened (timestamp)
- **Where** it happened (middleware, provider)
- **How long** it took (duration)
- **What it cost** (tokens, API costs)
- **Why it failed** (errors, validation violations)

## Events vs. Streaming

It's important to distinguish between two separate concepts:

### Content Streaming

Content streaming forwards LLM response chunks as they arrive:

```go
ch, _ := conv.SendStream(ctx, "Tell me a story")
for event := range ch {
    fmt.Print(event.Chunk.Text)  // Real-time text output
}
```

**Purpose**: Display responses to users in real-time

### Event System

The event system provides execution metadata:

```go
conv.AddEventListener(func(e *events.Event) {
    fmt.Printf("[%s] %s took %s\n", e.Type, middleware, duration)
})
```

**Purpose**: Observe, monitor, debug, and integrate with observability platforms

**Key Difference**: Content streaming is about getting LLM output. Events are about understanding execution flow and performance.

## Event Categories

### Lifecycle Events

Track the overall pipeline lifecycle:

```
pipeline.started → middleware chain → pipeline.completed
```

These events bookend execution and provide summary metrics:
- Total duration
- Total cost
- Token counts
- Message counts

### Middleware Events

Each middleware emits its own lifecycle:

```
middleware.started → processing → middleware.completed
```

This visibility helps identify:
- Which middleware is slow
- Which middleware is failing
- Where in the chain errors occur

### Provider Events

Provider events capture LLM API interactions:

```
provider.call.started → API call → provider.call.completed
```

These events include critical metrics:
- API latency
- Token usage (input, output, cached)
- Estimated cost
- Finish reason
- Tool call counts

### Domain Events

Specialized events for specific operations:

- **Tool Events**: Track tool execution
- **Validation Events**: Monitor constraint violations
- **Context Events**: Track token budget management
- **State Events**: Monitor persistence operations

## Event Flow Architecture

```
┌─────────────────────────────────────────────────┐
│          Application Code                       │
│  conversation.Send(ctx, "Hello")                │
└───────────────────┬─────────────────────────────┘
                    │
┌───────────────────▼─────────────────────────────┐
│          Pipeline Execution                      │
│  ┌──────────────────────────────────────────┐  │
│  │  Middleware 1: Context Builder           │  │
│  │    → EmitMiddlewareStarted              │  │
│  │    → process()                          │  │
│  │    → EmitMiddlewareCompleted            │  │
│  └──────────────────────────────────────────┘  │
│  ┌──────────────────────────────────────────┐  │
│  │  Middleware 2: Provider Call             │  │
│  │    → EmitProviderCallStarted            │  │
│  │    → callAPI()                          │  │
│  │    → EmitProviderCallCompleted          │  │
│  └──────────────────────────────────────────┘  │
└───────────────────┬─────────────────────────────┘
                    │ Events published to bus
┌───────────────────▼─────────────────────────────┐
│              Event Bus (Pub/Sub)                 │
│  ┌─────────────────────────────────────────┐    │
│  │  Thread-safe event distribution         │    │
│  │  Async delivery to all listeners        │    │
│  └─────────────────────────────────────────┘    │
└─────┬─────────────────┬─────────────────┬───────┘
      │                 │                 │
      ▼                 ▼                 ▼
┌───────────┐    ┌──────────┐    ┌──────────────┐
│ Arena TUI │    │ Metrics  │    │ Your Listener│
│ (Monitor) │    │ Collector│    │   (Custom)   │
└───────────┘    └──────────┘    └──────────────┘
```

## Design Decisions

### Asynchronous Delivery

Events are delivered asynchronously so listeners don't block pipeline execution:

```go
func (eb *EventBus) Publish(event Event) {
    // Execute listeners in goroutines
    go func() {
        for _, listener := range listeners {
            listener(event)  // Async
        }
    }()
}
```

**Tradeoff**: Listeners can't prevent pipeline execution, but they also don't slow it down.

### Lightweight Payloads

Events contain metrics and metadata, not full message content:

```go
type ProviderCallCompletedData struct {
    Provider     string        // "openai"
    Model        string        // "gpt-4"
    Duration     time.Duration // 1.2s
    InputTokens  int          // 150
    OutputTokens int          // 200
    Cost         float64      // 0.0042
    // NOT: full request/response payloads
}
```

**Tradeoff**: Can't reconstruct full conversation from events, but events remain memory-efficient.

### Fail-Safe

Listener panics are caught to prevent cascading failures:

```go
func safeInvoke(listener Listener, event Event) {
    defer func() {
        if r := recover(); r != nil {
            // Log but don't crash
        }
    }()
    listener(event)
}
```

**Tradeoff**: Buggy listeners won't crash the application, but failures may go unnoticed if not logged.

### Opt-In

Events are only emitted if an `EventEmitter` is provided:

```go
if ctx.EventEmitter != nil {
    ctx.EventEmitter.EmitMiddlewareStarted(...)
}
```

**Tradeoff**: Zero overhead when not used, but requires explicit opt-in.

## Common Patterns

### Aggregation Pattern

Aggregate events for summary metrics:

```go
type Stats struct {
    PipelineCount    int
    SuccessCount     int
    FailureCount     int
    TotalCost        float64
    TotalDuration    time.Duration
}

stats := &Stats{}

bus.Subscribe(events.EventPipelineCompleted, func(e events.Event) {
    data := e.Data.(events.PipelineCompletedData)
    stats.PipelineCount++
    stats.SuccessCount++
    stats.TotalCost += data.TotalCost
    stats.TotalDuration += data.Duration
})

bus.Subscribe(events.EventPipelineFailed, func(e events.Event) {
    stats.PipelineCount++
    stats.FailureCount++
})
```

### Filtering Pattern

Filter events based on conditions:

```go
// Only log expensive calls
bus.Subscribe(events.EventProviderCallCompleted, func(e events.Event) {
    data := e.Data.(events.ProviderCallCompletedData)
    if data.Cost > 0.10 {  // > 10 cents
        log.Warnf("Expensive call: $%.2f (%d tokens)", data.Cost, data.OutputTokens)
    }
})

// Only track slow middleware
bus.Subscribe(events.EventMiddlewareCompleted, func(e events.Event) {
    data := e.Data.(events.MiddlewareCompletedData)
    if data.Duration > 1*time.Second {
        metrics.RecordSlow(data.Name, data.Duration)
    }
})
```

### Correlation Pattern

Correlate events by conversation:

```go
type ConversationTrace map[string][]events.Event

traces := make(ConversationTrace)
var mu sync.Mutex

bus.SubscribeAll(func(e events.Event) {
    mu.Lock()
    defer mu.Unlock()
    
    traces[e.ConversationID] = append(traces[e.ConversationID], e)
})

// Later: analyze specific conversation
func analyzeConversation(id string) {
    mu.Lock()
    defer mu.Unlock()
    
    events := traces[id]
    fmt.Printf("Conversation %s had %d events\n", id, len(events))
    
    for _, e := range events {
        fmt.Printf("  [%s] %s\n", e.Timestamp, e.Type)
    }
}
```

### Alerting Pattern

Trigger alerts based on events:

```go
// Alert on repeated failures
failureCount := make(map[string]int)

bus.Subscribe(events.EventMiddlewareFailed, func(e events.Event) {
    data := e.Data.(events.MiddlewareFailedData)
    
    failureCount[data.Name]++
    
    if failureCount[data.Name] > 5 {
        alert.Send(fmt.Sprintf("Middleware %s failing repeatedly", data.Name))
    }
})

// Alert on budget exhaustion
bus.Subscribe(events.EventTokenBudgetExceeded, func(e events.Event) {
    data := e.Data.(events.TokenBudgetExceededData)
    alert.Send(fmt.Sprintf("Token budget exceeded by %d tokens", data.Excess))
})
```

## Comparison with Other Approaches

### vs. Logging

**Logging**: Unstructured text output
```go
log.Printf("Pipeline started for conversation %s", id)
```

**Events**: Structured, typed data
```go
emitter.EmitPipelineStarted(middlewareCount)
// → Event{Type: "pipeline.started", Data: {MiddlewareCount: 5}}
```

**When to use events**: Real-time monitoring, metrics collection, programmatic analysis  
**When to use logging**: Human-readable debugging, audit trails

### vs. Metrics

**Metrics**: Aggregated counters/gauges
```go
metrics.Incr("pipeline.executions")
```

**Events**: Individual execution details
```go
// Events can be aggregated into metrics
bus.SubscribeAll(func(e events.Event) {
    metrics.Incr(fmt.Sprintf("events.%s", e.Type))
})
```

**When to use events**: Detailed observability, debugging  
**When to use metrics**: Dashboards, alerting, trending

### vs. Tracing

**Tracing (OpenTelemetry)**: Distributed request tracking across services

**Events**: Pipeline-internal observability

**Complementary**: Events can be converted to trace spans:
```go
bus.Subscribe(events.EventMiddlewareStarted, func(e events.Event) {
    span := tracer.StartSpan(fmt.Sprintf("middleware.%s", data.Name))
    // ... store span for later completion
})

bus.Subscribe(events.EventMiddlewareCompleted, func(e events.Event) {
    // ... finish corresponding span
})
```

## Migration from Observer Pattern

Prior to the event system, PromptArena used an observer pattern:

```go
// Old: Observer pattern (deprecated)
type Observer interface {
    OnRunStarted(runID string)
    OnRunCompleted(runID string, result *Result)
    OnRunFailed(runID string, err error)
}

engine.SetObserver(observer)
```

**Problems**:
- Tightly coupled to Arena
- No SDK access
- No pipeline visibility
- Fixed interface (not extensible)

The event system replaced this with:

```go
// New: Event bus (current)
bus := events.NewEventBus()
bus.Subscribe(events.EventPipelineStarted, func(e events.Event) {
    // Handle event
})
```

**Benefits**:
- Decoupled (pub/sub)
- Available in SDK
- Full pipeline visibility
- Extensible (custom events)

## See Also

- [Event System Architecture](../../architecture/runtime-events)
- [How-To: Monitor Events](../how-to/monitor-events)
- [Pipeline Architecture](../../architecture/runtime-pipeline)
- [Example: Event Monitoring](../examples/events/)