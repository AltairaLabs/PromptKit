---
title: Telemetry
sidebar:
  order: 8
---
OpenTelemetry-compatible tracing for PromptKit sessions.

## Overview

The `runtime/telemetry` package integrates PromptKit with the [OpenTelemetry Go SDK](https://opentelemetry.io/docs/languages/go/). It provides:

- A **real-time event listener** that converts EventBus events into OTel spans as they occur
- **TracerProvider** helpers for standalone OTLP export
- **Propagation** setup for W3C Trace Context, W3C Baggage, and AWS X-Ray headers

Because it uses the standard OTel SDK, spans are exported through any configured `SpanExporter` — OTLP/HTTP, OTLP/gRPC, Jaeger, Zipkin, or custom exporters.

```go
import "github.com/AltairaLabs/PromptKit/runtime/telemetry"
```

## Trace Structure

Each session produces a single trace. The root span represents the session, with child spans for provider calls, middleware, tool calls, and workflow transitions.

```
promptkit.session (root, SpanKindServer)
├── promptkit.provider.openai (SpanKindClient)
│   ├── [event] gen_ai.user.message
│   └── [event] gen_ai.assistant.message
├── promptkit.tool.search (SpanKindInternal)
├── promptkit.provider.openai (SpanKindClient)
│   └── [event] gen_ai.assistant.message
├── promptkit.middleware.auth (SpanKindInternal)
├── promptkit.workflow.transition (SpanKindInternal, instant)
├── promptkit.workflow.transition (SpanKindInternal, instant)
└── promptkit.workflow.completed (SpanKindInternal, instant)
```

## SDK Integration

The simplest way to enable tracing is via the `WithTracerProvider` SDK option:

```go
import (
    sdktrace "go.opentelemetry.io/otel/sdk/trace"
    "github.com/AltairaLabs/PromptKit/sdk"
)

tp := sdktrace.NewTracerProvider(/* your exporter */)
defer tp.Shutdown(ctx)

conv, _ := sdk.Open("./app.pack.json", "chat",
    sdk.WithTracerProvider(tp),
)
```

When a `TracerProvider` is configured, the SDK automatically wires an `OTelEventListener` into the EventBus. All pipeline events are converted to spans in real time — no manual wiring needed.

## OTelEventListener

`OTelEventListener` converts runtime events into OTel spans in real time. It is safe for concurrent use and tolerates out-of-order event delivery (the EventBus dispatches events asynchronously).

### Constructor

```go
func NewOTelEventListener(tracer trace.Tracer) *OTelEventListener
```

### Session Lifecycle

```go
func (l *OTelEventListener) StartSession(parentCtx context.Context, sessionID string)
func (l *OTelEventListener) EndSession(sessionID string)
```

`StartSession` creates a root `promptkit.session` span, optionally parented under the span in `parentCtx`. All subsequent spans for this session are children of this root. `EndSession` ends the root span.

### OnEvent

```go
func (l *OTelEventListener) OnEvent(evt *events.Event)
```

Handles a single runtime event and creates/completes OTel spans accordingly. Pass this method to `EventBus.SubscribeAll`:

```go
tracer := telemetry.Tracer(tp)
listener := telemetry.NewOTelEventListener(tracer)
listener.StartSession(ctx, sessionID)

bus.SubscribeAll(listener.OnEvent)

// ... run conversation ...

listener.EndSession(sessionID)
```

## Handled Event Types

### Provider events

| Event | Span |
|-------|------|
| `provider.call.started` / `provider.call.completed` / `provider.call.failed` | `promptkit.provider.<name>` span (SpanKindClient) |

**Attributes** follow the [OpenTelemetry GenAI Semantic Conventions](https://opentelemetry.io/docs/specs/semconv/gen-ai/):

| Attribute | Source | Spec reference |
|-----------|--------|---------------|
| `gen_ai.system` | Provider name | [gen_ai.system](https://opentelemetry.io/docs/specs/semconv/attributes-registry/gen-ai/) |
| `gen_ai.request.model` | Model name | [gen_ai.request.model](https://opentelemetry.io/docs/specs/semconv/attributes-registry/gen-ai/) |
| `gen_ai.usage.input_tokens` | Input token count | [gen_ai.usage.input_tokens](https://opentelemetry.io/docs/specs/semconv/gen-ai/gen-ai-metrics/) |
| `gen_ai.usage.output_tokens` | Output token count | [gen_ai.usage.output_tokens](https://opentelemetry.io/docs/specs/semconv/gen-ai/gen-ai-metrics/) |
| `gen_ai.response.finish_reason` | Finish reason | [gen_ai.response.finish_reasons](https://opentelemetry.io/docs/specs/semconv/attributes-registry/gen-ai/) |
| `message.count` | Number of messages | PromptKit-specific |
| `tool.count` | Number of tools | PromptKit-specific |
| `provider.duration_ms` | Call duration | PromptKit-specific |
| `provider.cost` | Estimated cost (USD) | PromptKit-specific |

### Pipeline events

| Event | Span |
|-------|------|
| `pipeline.started` / `pipeline.completed` / `pipeline.failed` | `promptkit.pipeline` span (SpanKindInternal) |

**Attributes:** `run.id`, `pipeline.duration_ms`, `pipeline.total_cost`, `gen_ai.usage.input_tokens`, `gen_ai.usage.output_tokens`

### Message events

| Event | Behaviour |
|-------|-----------|
| `message.created` | Appended as a [SpanEvent](https://opentelemetry.io/docs/concepts/signals/traces/#span-events) on the active provider span |

Messages are not separate spans. They are attached as **span events** on the currently active `promptkit.provider.<name>` span, following the [GenAI Events conventions](https://opentelemetry.io/docs/specs/semconv/gen-ai/gen-ai-spans/). If no provider span is active, the event is attached to the root session span.

**Event name:** `gen_ai.<role>.message` (e.g., `gen_ai.user.message`, `gen_ai.assistant.message`)

**Event attributes:**

| Attribute | Type | Description |
|-----------|------|-------------|
| `gen_ai.message.content` | string | Text content of the message |
| `gen_ai.tool_calls` | string (JSON) | Tool calls requested by assistant (present only when non-empty) |
| `gen_ai.tool_result` | string (JSON) | Tool result for tool-role messages (present only when non-nil) |

### Tool events

| Event | Span |
|-------|------|
| `tool.call.started` / `tool.call.completed` / `tool.call.failed` | `promptkit.tool.<name>` span (SpanKindInternal) |

**Attributes:**

| Attribute | Type | Description |
|-----------|------|-------------|
| `tool.name` | string | Tool name |
| `tool.call_id` | string | Unique call identifier |
| `tool.args` | string (JSON) | Serialised tool arguments (omitted when nil) |
| `tool.duration_ms` | int64 | Execution time |
| `tool.status` | string | Completion status |

### Middleware events

| Event | Span |
|-------|------|
| `middleware.started` / `middleware.completed` / `middleware.failed` | `promptkit.middleware.<name>` span (SpanKindInternal) |

**Attributes:** `middleware.name`, `middleware.index`, `middleware.duration_ms`

### Workflow events

| Event | Span |
|-------|------|
| `workflow.transitioned` | `promptkit.workflow.transition` instant span (SpanKindInternal) |
| `workflow.completed` | `promptkit.workflow.completed` instant span (SpanKindInternal) |

Workflow spans are **instant** — their start and end times are both set to the event timestamp.

**Transition attributes:**

| Attribute | Type | Description |
|-----------|------|-------------|
| `workflow.from_state` | string | State before transition |
| `workflow.to_state` | string | State after transition |
| `workflow.event` | string | Trigger event |
| `workflow.prompt_task` | string | Prompt task of the new state |

**Completion attributes:**

| Attribute | Type | Description |
|-----------|------|-------------|
| `workflow.final_state` | string | Terminal state reached |
| `workflow.transition_count` | int | Total number of transitions |

### Error handling

When a `*.failed` event is received, the corresponding span's status is set to `codes.Error` with the error message. All other attributes from the failure data (e.g., `duration_ms`) are still recorded.

### Out-of-order delivery

The EventBus dispatches each `Publish()` in a separate goroutine, so completion events can arrive before their corresponding start events. The listener handles this transparently by buffering early completions and applying them when the start event arrives.

## Tracer

```go
func Tracer(tp trace.TracerProvider) trace.Tracer
```

Returns a named tracer with instrumentation scope `github.com/AltairaLabs/PromptKit` (version `1.0.0`). If `tp` is nil, the global noop provider is used.

## NewTracerProvider

```go
func NewTracerProvider(ctx context.Context, endpoint, serviceName string) (*sdktrace.TracerProvider, error)
```

Creates a `TracerProvider` that exports spans via OTLP/HTTP to the given endpoint. The caller is responsible for calling `Shutdown` on the returned provider. Use this for standalone applications that don't have their own OTel setup.

```go
tp, err := telemetry.NewTracerProvider(ctx,
    "http://localhost:4318/v1/traces",
    "my-service",
)
if err != nil {
    log.Fatal(err)
}
defer tp.Shutdown(ctx)
```

## SetupPropagation

```go
func SetupPropagation()
```

Configures the global OTel text-map propagator to handle:

- [W3C Trace Context](https://www.w3.org/TR/trace-context/) (`traceparent` / `tracestate`)
- [W3C Baggage](https://www.w3.org/TR/baggage/)
- [AWS X-Ray](https://docs.aws.amazon.com/xray/latest/devguide/xray-concepts.html#xray-concepts-tracingheader) (`X-Amzn-Trace-Id`)

Call this once at application startup if you need distributed trace propagation across HTTP boundaries:

```go
telemetry.SetupPropagation()
```

## Relevant Specifications

- [OpenTelemetry Specification](https://opentelemetry.io/docs/specs/otel/) — core concepts (traces, spans, resources)
- [OpenTelemetry Go SDK](https://opentelemetry.io/docs/languages/go/) — Go instrumentation guide
- [OTLP/HTTP Protocol](https://opentelemetry.io/docs/specs/otlp/#otlphttp) — wire format for trace export
- [GenAI Semantic Conventions](https://opentelemetry.io/docs/specs/semconv/gen-ai/) — attribute naming for LLM workloads
- [GenAI Span Conventions](https://opentelemetry.io/docs/specs/semconv/gen-ai/gen-ai-spans/) — span event naming for chat completions
- [Resource Semantic Conventions](https://opentelemetry.io/docs/specs/semconv/resource/) — `service.name`, `service.version`
- [W3C Trace Context](https://www.w3.org/TR/trace-context/) — `traceparent` / `tracestate` headers
- [AWS X-Ray Trace Header](https://docs.aws.amazon.com/xray/latest/devguide/xray-concepts.html#xray-concepts-tracingheader) — `X-Amzn-Trace-Id`

## See Also

- [How-To: Export Traces with OTLP](../how-to/export-traces-otlp) — end-to-end setup guide
- [How-To: Prometheus Metrics](../how-to/prometheus-metrics) — Prometheus-based monitoring
- [Logging Reference](logging) — structured logging
- [SDK Observability](../../sdk/explanation/observability) — event system overview
