---
title: Monitor Pipeline Events
docType: how-to
order: 12
---

# Monitor Pipeline Events

Learn how to use the runtime event system to observe and monitor LLM pipeline execution in real-time.

## Prerequisites

- Basic understanding of PromptKit SDK
- Familiarity with event-driven programming

## Setup Event Bus

Create an event bus when initializing the conversation manager:

```go
import (
    "github.com/AltairaLabs/PromptKit/runtime/events"
    "github.com/AltairaLabs/PromptKit/sdk"
)

// Create event bus
eventBus := events.NewEventBus()

// Create manager with event bus
manager, err := sdk.NewConversationManager(
    sdk.WithProvider(provider),
    sdk.WithEventBus(eventBus),
)
```

## Add Event Listeners

### Listen to All Events

Add a listener to receive all pipeline events:

```go
conversation.AddEventListener(func(e *events.Event) {
    fmt.Printf("[%s] %s\n", e.Timestamp.Format(time.RFC3339), e.Type)
})
```

### Filter by Event Type

Subscribe to specific event types at the bus level:

```go
eventBus.Subscribe(events.EventProviderCallCompleted, func(e events.Event) {
    data := e.Data.(events.ProviderCallCompletedData)
    fmt.Printf("Provider call: %s | Duration: %s | Cost: $%.4f\n",
        data.Provider, data.Duration, data.Cost)
})
```

### Multiple Listeners

Add multiple listeners for different purposes:

```go
// Logging
conversation.AddEventListener(func(e *events.Event) {
    log.Printf("[%s] %s: %+v", e.Type, e.Timestamp, e.Data)
})

// Metrics
conversation.AddEventListener(func(e *events.Event) {
    metrics.RecordEvent(string(e.Type))
})

// Debugging
conversation.AddEventListener(func(e *events.Event) {
    if strings.Contains(string(e.Type), "failed") {
        debugLog.Printf("FAILURE: %s - %+v", e.Type, e.Data)
    }
})
```

## Common Use Cases

### Track API Costs

Monitor LLM API costs in real-time:

```go
var totalCost float64
var mu sync.Mutex

eventBus.Subscribe(events.EventProviderCallCompleted, func(e events.Event) {
    data := e.Data.(events.ProviderCallCompletedData)
    
    mu.Lock()
    totalCost += data.Cost
    mu.Unlock()
    
    fmt.Printf("Call: $%.4f | Total: $%.4f | Tokens: %d in + %d out\n",
        data.Cost, totalCost, data.InputTokens, data.OutputTokens)
})
```

### Measure Performance

Track middleware execution times:

```go
type MiddlewareMetrics struct {
    TotalDuration time.Duration
    CallCount     int
    AvgDuration   time.Duration
}

metrics := make(map[string]*MiddlewareMetrics)
var mu sync.Mutex

eventBus.Subscribe(events.EventMiddlewareCompleted, func(e events.Event) {
    data := e.Data.(events.MiddlewareCompletedData)
    
    mu.Lock()
    defer mu.Unlock()
    
    if _, ok := metrics[data.Name]; !ok {
        metrics[data.Name] = &MiddlewareMetrics{}
    }
    
    m := metrics[data.Name]
    m.TotalDuration += data.Duration
    m.CallCount++
    m.AvgDuration = m.TotalDuration / time.Duration(m.CallCount)
    
    fmt.Printf("%s: %s (avg: %s)\n", data.Name, data.Duration, m.AvgDuration)
})
```

### Debug Execution Flow

Capture full execution traces:

```go
type ExecutionTrace struct {
    ConversationID string
    Events         []events.Event
}

trace := &ExecutionTrace{ConversationID: conversationID}

conversation.AddEventListener(func(e *events.Event) {
    trace.Events = append(trace.Events, *e)
})

// On error, dump trace
if err != nil {
    fmt.Println("Execution Trace:")
    for _, e := range trace.Events {
        fmt.Printf("  [%s] %s\n", e.Timestamp.Format("15:04:05.000"), e.Type)
        
        // Pretty print event data
        if e.Data != nil {
            json, _ := json.MarshalIndent(e.Data, "    ", "  ")
            fmt.Printf("    %s\n", json)
        }
    }
}
```

### Monitor Tool Executions

Track tool calls and their results:

```go
eventBus.Subscribe(events.EventToolCallStarted, func(e events.Event) {
    data := e.Data.(events.ToolCallStartedData)
    fmt.Printf("Tool starting: %s (call_id: %s)\n", data.ToolName, data.CallID)
})

eventBus.Subscribe(events.EventToolCallCompleted, func(e events.Event) {
    data := e.Data.(events.ToolCallCompletedData)
    fmt.Printf("Tool completed: %s in %s (status: %s)\n",
        data.ToolName, data.Duration, data.Status)
})

eventBus.Subscribe(events.EventToolCallFailed, func(e events.Event) {
    data := e.Data.(events.ToolCallFailedData)
    fmt.Printf("Tool failed: %s - %v\n", data.ToolName, data.Error)
})
```

### Track Validation Failures

Monitor validation events:

```go
eventBus.Subscribe(events.EventValidationFailed, func(e events.Event) {
    data := e.Data.(events.ValidationFailedData)
    fmt.Printf("Validation failed: %s (%s)\n", data.ValidatorName, data.ValidatorType)
    fmt.Println("Violations:")
    for _, violation := range data.Violations {
        fmt.Printf("  - %s\n", violation)
    }
})
```

## Integration with External Systems

### Send to Datadog

```go
import "github.com/DataDog/datadog-go/v5/statsd"

ddClient, _ := statsd.New("127.0.0.1:8125")

eventBus.SubscribeAll(func(e events.Event) {
    tags := []string{
        fmt.Sprintf("event_type:%s", e.Type),
        fmt.Sprintf("conversation_id:%s", e.ConversationID),
    }
    
    ddClient.Incr("promptkit.events", tags, 1)
    
    // Send specific metrics
    if e.Type == events.EventProviderCallCompleted {
        data := e.Data.(events.ProviderCallCompletedData)
        ddClient.Timing("promptkit.provider.duration", data.Duration, tags, 1)
        ddClient.Gauge("promptkit.provider.cost", data.Cost, tags, 1)
        ddClient.Count("promptkit.provider.tokens", int64(data.InputTokens+data.OutputTokens), tags, 1)
    }
})
```

### Send to Prometheus

```go
import "github.com/prometheus/client_golang/prometheus"

var (
    eventCounter = prometheus.NewCounterVec(
        prometheus.CounterOpts{
            Name: "promptkit_events_total",
            Help: "Total number of events by type",
        },
        []string{"event_type"},
    )
    
    providerDuration = prometheus.NewHistogramVec(
        prometheus.HistogramOpts{
            Name: "promptkit_provider_duration_seconds",
            Help: "Provider call duration",
        },
        []string{"provider", "model"},
    )
)

eventBus.SubscribeAll(func(e events.Event) {
    eventCounter.WithLabelValues(string(e.Type)).Inc()
})

eventBus.Subscribe(events.EventProviderCallCompleted, func(e events.Event) {
    data := e.Data.(events.ProviderCallCompletedData)
    providerDuration.WithLabelValues(data.Provider, data.Model).Observe(data.Duration.Seconds())
})
```

### Log to Structured Logger

```go
import "github.com/sirupsen/logrus"

log := logrus.New()
log.SetFormatter(&logrus.JSONFormatter{})

eventBus.SubscribeAll(func(e events.Event) {
    fields := logrus.Fields{
        "event_type":      e.Type,
        "timestamp":       e.Timestamp,
        "conversation_id": e.ConversationID,
        "session_id":      e.SessionID,
    }
    
    // Add event-specific fields
    if e.Data != nil {
        fields["data"] = e.Data
    }
    
    log.WithFields(fields).Info("Pipeline event")
})
```

## Best Practices

### Don't Block in Listeners

Event listeners run asynchronously but should be fast:

```go
// ❌ Bad: Blocking operation
conversation.AddEventListener(func(e *events.Event) {
    http.Post("https://slow-api.com/events", ...) // Blocks!
})

// ✅ Good: Buffer and process async
eventChan := make(chan events.Event, 100)

conversation.AddEventListener(func(e *events.Event) {
    select {
    case eventChan <- *e:
    default:
        // Drop if buffer full
    }
})

// Process in background
go func() {
    for e := range eventChan {
        http.Post("https://slow-api.com/events", ...)
    }
}()
```

### Handle Errors Gracefully

Listeners should not panic:

```go
conversation.AddEventListener(func(e *events.Event) {
    defer func() {
        if r := recover(); r != nil {
            log.Printf("Event listener panic: %v", r)
        }
    }()
    
    // Your event handling code
})
```

### Use Type Assertions Carefully

Check event data types:

```go
eventBus.Subscribe(events.EventProviderCallCompleted, func(e events.Event) {
    data, ok := e.Data.(events.ProviderCallCompletedData)
    if !ok {
        log.Printf("Unexpected event data type: %T", e.Data)
        return
    }
    
    // Use data safely
    fmt.Printf("Cost: $%.4f\n", data.Cost)
})
```

## Testing with Events

Use events to verify behavior in tests:

```go
func TestConversationEmitsEvents(t *testing.T) {
    eventBus := events.NewEventBus()
    var capturedEvents []events.Event
    
    eventBus.SubscribeAll(func(e events.Event) {
        capturedEvents = append(capturedEvents, e)
    })
    
    manager, _ := sdk.NewConversationManager(
        sdk.WithProvider(mockProvider),
        sdk.WithEventBus(eventBus),
    )
    
    conv, _ := manager.CreateConversation(ctx, pack, config)
    _, err := conv.Send(ctx, "Hello")
    
    require.NoError(t, err)
    
    // Verify events
    assert.Contains(t, capturedEvents, events.EventPipelineStarted)
    assert.Contains(t, capturedEvents, events.EventProviderCallCompleted)
    assert.Contains(t, capturedEvents, events.EventPipelineCompleted)
}
```

## See Also

- [Event System Architecture](../../architecture/runtime-events)
- [SDK Example: Events](../../sdk/examples/events/)
- [Pipeline Architecture](../../architecture/runtime-pipeline)
