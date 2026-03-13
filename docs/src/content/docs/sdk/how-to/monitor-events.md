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

// Evals
EventEvalCompleted EventType = "eval.completed"  // eval finished (any score)
EventEvalFailed    EventType = "eval.failed"      // eval errored (not low score)

// Stream control
EventStreamInterrupted EventType = "stream.interrupted"
```

## Monitor Tool Calls

```go
hooks.OnToolCall(conv, func(name string, args map[string]any) {
    fmt.Printf("Tool called: %s(%v)\n", name, args)
})
```

## Monitor Guardrail Violations

```go
hooks.On(conv, events.EventValidationFailed, func(e *events.Event) {
    data := e.Data.(*events.ValidationEventData)
    log.Printf("Guardrail %s triggered: score=%.2f enforced=%v monitor=%v",
        data.ValidatorName, data.Score, data.Enforced, data.MonitorOnly)
})
```

The `ValidationEventData` includes:

| Field | Description |
|-------|-------------|
| `ValidatorName` | Validator type (e.g., `banned_words`, `max_length`) |
| `Score` | Evaluation score (0.0–1.0, lower means more violation) |
| `Enforced` | `true` if content was modified (truncated/replaced) |
| `MonitorOnly` | `true` if the guardrail evaluated without enforcing |
| `Violations` | Violation details (reason strings) |
| `Duration` | How long the evaluation took |

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

## Prometheus Metrics

`WithMetrics()` enables automatic Prometheus metrics for both pipeline operations and eval results. It follows the same pattern as `WithTracerProvider()` — pass a collector, and the SDK handles the rest.

### Basic Setup

```go
import (
    "net/http"

    "github.com/prometheus/client_golang/prometheus"
    "github.com/prometheus/client_golang/prometheus/promhttp"

    "github.com/AltairaLabs/PromptKit/runtime/metrics"
    "github.com/AltairaLabs/PromptKit/sdk"
)

// 1. Create collector once per process
reg := prometheus.NewRegistry()
collector := metrics.NewCollector(metrics.CollectorOpts{
    Registerer:  reg,
    Namespace:   "myapp",
    ConstLabels: prometheus.Labels{"env": "prod"},
})

// 2. Attach to conversations
conv, _ := sdk.Open("./app.pack.json", "chat",
    sdk.WithMetrics(collector, nil),
)
defer conv.Close()

// 3. Expose via your own HTTP server
http.Handle("/metrics", promhttp.HandlerFor(collector.Registry(), promhttp.HandlerOpts{}))
```

### Multi-Tenant Setup

When multiple conversations share one Prometheus endpoint, use instance labels to distinguish them:

```go
collector := metrics.NewCollector(metrics.CollectorOpts{
    Registerer:     reg,
    Namespace:      "myapp",
    ConstLabels:    prometheus.Labels{"env": "prod"},
    InstanceLabels: []string{"tenant", "prompt_name"},
})

conv1, _ := sdk.Open(pack, "support", sdk.WithMetrics(collector, map[string]string{
    "tenant": "acme", "prompt_name": "support",
}))
conv2, _ := sdk.Open(pack, "sales", sdk.WithMetrics(collector, map[string]string{
    "tenant": "globex", "prompt_name": "sales",
}))
```

### Pipeline Metrics

These are recorded automatically from EventBus events:

| Metric | Type | Labels |
|--------|------|--------|
| `{ns}_pipeline_duration_seconds` | histogram | status |
| `{ns}_provider_request_duration_seconds` | histogram | provider, model |
| `{ns}_provider_requests_total` | counter | provider, model, status |
| `{ns}_provider_input_tokens_total` | counter | provider, model |
| `{ns}_provider_output_tokens_total` | counter | provider, model |
| `{ns}_provider_cached_tokens_total` | counter | provider, model |
| `{ns}_provider_cost_total` | counter | provider, model |
| `{ns}_tool_call_duration_seconds` | histogram | tool |
| `{ns}_tool_calls_total` | counter | tool, status |
| `{ns}_validation_duration_seconds` | histogram | validator, validator_type |
| `{ns}_validations_total` | counter | validator, validator_type, status |

### Eval Metrics

Pack-defined eval metrics (from `EvalDef.Metric`) are also recorded through the same collector under the `{ns}_eval_` sub-namespace. For example, the metric below becomes `myapp_eval_response_relevance_score`. No extra wiring needed — `WithMetrics()` handles both pipeline and eval metrics.

```json
{
  "evals": [
    {
      "id": "response_relevance",
      "type": "llm_judge",
      "trigger": "every_turn",
      "metric": {
        "name": "response_relevance_score",
        "type": "gauge",
        "labels": {
          "eval_type": "llm_judge",
          "category": "quality"
        }
      },
      "params": {
        "criteria": "Is the response relevant to the user's question?"
      }
    }
  ]
}
```

#### Eval Metric Types

| Type | Behavior |
|------|----------|
| `gauge` | Set to the eval's score value |
| `counter` | Increment on each eval execution |
| `histogram` | Observe score with configurable buckets |
| `boolean` | Record 1.0 if score ≥ 1.0, 0.0 otherwise |

### CollectorOpts Reference

| Field | Type | Description |
|-------|------|-------------|
| `Registerer` | `prometheus.Registerer` | Registry to register into (default: `DefaultRegisterer`) |
| `Namespace` | `string` | Metric name prefix (default: `"promptkit"`) |
| `ConstLabels` | `prometheus.Labels` | Process-level constant labels (env, region) |
| `InstanceLabels` | `[]string` | Label names that vary per conversation (tenant, prompt_name). Sorted internally — `Bind()` label order doesn't matter. |
| `DisablePipelineMetrics` | `bool` | Disable operational metrics (use for eval-only consumers, or use `NewEvalOnlyCollector`) |
| `DisableEvalMetrics` | `bool` | Disable eval result metrics |

### Custom Event Counters

For ad-hoc counters not covered by the built-in metrics, use hooks:

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

- [Metrics Reference](/runtime/reference/metrics/) — Complete catalog of all emitted metrics
- [Tutorial 6: Observability](../tutorials/06-media-storage)
- [Explanation: Observability](../explanation/observability)
- [Arena Eval Framework](/arena/explanation/eval-framework/)
