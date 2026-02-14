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

Events are defined as `events.EventType` in the `runtime/events` package, grouped by category:

```go
// Pipeline lifecycle
EventPipelineStarted   EventType = "pipeline.started"
EventPipelineCompleted EventType = "pipeline.completed"
EventPipelineFailed    EventType = "pipeline.failed"

// Middleware execution
EventMiddlewareStarted   EventType = "middleware.started"
EventMiddlewareCompleted EventType = "middleware.completed"
EventMiddlewareFailed    EventType = "middleware.failed"

// Stage execution (streaming pipeline)
EventStageStarted   EventType = "stage.started"
EventStageCompleted EventType = "stage.completed"
EventStageFailed    EventType = "stage.failed"

// Provider (LLM) calls
EventProviderCallStarted   EventType = "provider.call.started"
EventProviderCallCompleted EventType = "provider.call.completed"
EventProviderCallFailed    EventType = "provider.call.failed"

// Tool calls
EventToolCallStarted   EventType = "tool.call.started"
EventToolCallCompleted EventType = "tool.call.completed"
EventToolCallFailed    EventType = "tool.call.failed"

// Validation
EventValidationStarted EventType = "validation.started"
EventValidationPassed  EventType = "validation.passed"
EventValidationFailed  EventType = "validation.failed"

// Context & state
EventContextBuilt          EventType = "context.built"
EventTokenBudgetExceeded   EventType = "context.token_budget_exceeded"
EventStateLoaded           EventType = "state.loaded"
EventStateSaved            EventType = "state.saved"

// Messages & conversation
EventMessageCreated      EventType = "message.created"
EventMessageUpdated      EventType = "message.updated"
EventConversationStarted EventType = "conversation.started"

// Multimodal
EventAudioInput         EventType = "audio.input"
EventAudioOutput        EventType = "audio.output"
EventAudioTranscription EventType = "audio.transcription"
EventVideoFrame         EventType = "video.frame"
EventScreenshot         EventType = "screenshot"
EventImageInput         EventType = "image.input"
EventImageOutput        EventType = "image.output"

// Stream control
EventStreamInterrupted EventType = "stream.interrupted"
```

## Monitor Tool Calls

```go
hooks.OnToolCall(conv, func(name string, args map[string]any) {
    fmt.Printf("Tool called: %s(%v)\n", name, args)
})
```

## Monitor Provider Calls

```go
hooks.OnProviderCall(conv, func(model string, inputTokens, outputTokens int, cost float64) {
    log.Printf("Model %s: %d in, %d out, $%.4f", model, inputTokens, outputTokens, cost)
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
    RunID          string
    SessionID      string
    ConversationID string
    Data           EventData  // Type-specific payload
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

## Collect Eval Metrics

The `MetricCollector` records eval results as Prometheus-compatible metrics. Configure it as a `ResultWriter` on conversations:

```go
import "github.com/AltairaLabs/PromptKit/runtime/evals"

// Create a MetricCollector
collector := evals.NewMetricCollector()

// Wire it into conversation options
conv, _ := sdk.Open("./app.pack.json", "chat",
    sdk.WithEvalDispatcher(evals.NewInProcDispatcher(nil)),
    sdk.WithResultWriters(evals.NewMetricResultWriter(collector)),
)
defer conv.Close()

// Use the conversation normally — evals run automatically based on pack config
resp, _ := conv.Send(ctx, "Hello!")

// Export metrics in Prometheus text format
collector.WritePrometheus(os.Stdout)
```

Pack evals define metrics in the pack file:

```json
{
  "evals": [
    {
      "id": "response_relevance",
      "type": "llm_judge",
      "trigger": "every_turn",
      "metric": {
        "name": "response_relevance_score",
        "type": "gauge"
      },
      "params": {
        "criteria": "Is the response relevant to the user's question?"
      }
    }
  ]
}
```

The `MetricCollector` supports four metric types:

| Type | Behavior |
|------|----------|
| `gauge` | Set to the eval's score value |
| `counter` | Increment on each eval execution |
| `histogram` | Observe score with configurable buckets |
| `boolean` | Record 1.0 (pass) or 0.0 (fail) |

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
- [Arena Eval Framework](/arena/explanation/eval-framework/)
