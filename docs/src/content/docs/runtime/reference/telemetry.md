---
title: Telemetry
sidebar:
  order: 8
---
OpenTelemetry-compatible trace export for PromptKit sessions.

## Overview

The `runtime/telemetry` package converts PromptKit's EventBus events into [OpenTelemetry](https://opentelemetry.io/) spans and exports them over [OTLP/HTTP](https://opentelemetry.io/docs/specs/otlp/#otlphttp) to any compatible backend (Jaeger, Grafana Tempo, Datadog, Honeycomb, AWS X-Ray, etc.).

The exporter follows the [OpenTelemetry GenAI Semantic Conventions](https://opentelemetry.io/docs/specs/semconv/gen-ai/) for LLM-specific attributes and the [W3C Trace Context](https://www.w3.org/TR/trace-context/) specification for distributed trace propagation.

```go
import "github.com/AltairaLabs/PromptKit/runtime/telemetry"
```

## Trace Structure

Each session produces a single trace. The root span represents the session, with child spans for pipeline execution, provider calls, middleware, tool calls, and workflow transitions.

```
session (root, SpanKindServer)
├── pipeline (SpanKindInternal)
│   ├── middleware.auth (SpanKindInternal)
│   ├── provider.openai (SpanKindClient)
│   │   ├── [event] gen_ai.user.message
│   │   └── [event] gen_ai.assistant.message
│   ├── tool.search (SpanKindInternal)
│   └── provider.openai (SpanKindClient)
│       └── [event] gen_ai.assistant.message
├── workflow.transition (SpanKindInternal, instant)
├── workflow.transition (SpanKindInternal, instant)
└── workflow.completed (SpanKindInternal, instant)
```

## EventConverter

`EventConverter` transforms a slice of `events.Event` into OpenTelemetry `Span` objects.

### Constructor

```go
func NewEventConverter(resource *Resource) *EventConverter
```

If `resource` is nil, `DefaultResource()` is used.

### ConvertSession

```go
func (c *EventConverter) ConvertSession(
    sessionID string, sessionEvents []events.Event,
) ([]*Span, error)
```

Generates a trace ID deterministically from the session ID. The root span has no parent.

### ConvertSessionWithParent

```go
func (c *EventConverter) ConvertSessionWithParent(
    sessionID string, sessionEvents []events.Event, traceCtx *TraceContext,
) ([]*Span, error)
```

Uses the provided [W3C `traceparent`](https://www.w3.org/TR/trace-context/#traceparent-header) header as the parent trace. The root session span's `TraceID` is taken from the inbound header and its `ParentSpanID` is set to the caller's span ID. Falls back to `ConvertSession` if `traceCtx` is nil or the `Traceparent` is empty/invalid.

## Handled Event Types

### Pipeline events

| Event | Span |
|-------|------|
| `pipeline.started` / `pipeline.completed` / `pipeline.failed` | `pipeline` span (SpanKindInternal) |

**Attributes:** `run.id`, `pipeline.duration_ms`, `pipeline.total_cost`, `pipeline.input_tokens`, `pipeline.output_tokens`

### Provider events

| Event | Span |
|-------|------|
| `provider.call.started` / `provider.call.completed` / `provider.call.failed` | `provider.<name>` span (SpanKindClient) |

**Attributes** follow the [OpenTelemetry GenAI Semantic Conventions](https://opentelemetry.io/docs/specs/semconv/gen-ai/):

| Attribute | Source | Spec reference |
|-----------|--------|---------------|
| `gen_ai.system` | Provider name | [gen_ai.system](https://opentelemetry.io/docs/specs/semconv/attributes-registry/gen-ai/) |
| `gen_ai.operation` | `"chat"` | [gen_ai.operation.name](https://opentelemetry.io/docs/specs/semconv/attributes-registry/gen-ai/) |
| `gen_ai.usage.input_tokens` | Input token count | [gen_ai.usage.input_tokens](https://opentelemetry.io/docs/specs/semconv/gen-ai/gen-ai-metrics/) |
| `gen_ai.usage.output_tokens` | Output token count | [gen_ai.usage.output_tokens](https://opentelemetry.io/docs/specs/semconv/gen-ai/gen-ai-metrics/) |
| `gen_ai.response.finish_reason` | Finish reason | [gen_ai.response.finish_reasons](https://opentelemetry.io/docs/specs/semconv/attributes-registry/gen-ai/) |
| `provider.name` | Provider identifier | PromptKit-specific |
| `provider.model` | Model name | PromptKit-specific |
| `provider.duration_ms` | Call duration | PromptKit-specific |
| `provider.cost` | Estimated cost (USD) | PromptKit-specific |

### Message events

| Event | Behaviour |
|-------|-----------|
| `message.created` | Appended as a [SpanEvent](https://opentelemetry.io/docs/concepts/signals/traces/#span-events) on the active provider span |

Messages are not separate spans. They are attached as **span events** on the currently active `provider.<name>` span, following the [GenAI Events conventions](https://opentelemetry.io/docs/specs/semconv/gen-ai/gen-ai-spans/). If no provider span is active, the event is attached to the root session span.

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
| `tool.call.started` / `tool.call.completed` / `tool.call.failed` | `tool.<name>` span (SpanKindInternal) |

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
| `middleware.started` / `middleware.completed` / `middleware.failed` | `middleware.<name>` span (SpanKindInternal) |

**Attributes:** `middleware.name`, `middleware.index`, `middleware.duration_ms`

### Workflow events

| Event | Span |
|-------|------|
| `workflow.transitioned` | `workflow.transition` instant span (SpanKindInternal) |
| `workflow.completed` | `workflow.completed` instant span (SpanKindInternal) |

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

## Resource

The `Resource` identifies the entity producing telemetry, following the [OpenTelemetry Resource Semantic Conventions](https://opentelemetry.io/docs/specs/semconv/resource/).

### DefaultResource

```go
func DefaultResource() *Resource
```

Returns a resource with:
- `service.name` = `"promptkit"`
- `service.version` = `"1.0.0"`
- `telemetry.sdk` = `"promptkit-telemetry"`

### ResourceWithPackID

```go
func ResourceWithPackID(packID string) *Resource
```

Returns `DefaultResource()` plus a `pack.id` attribute. Use this to tag all spans from a specific pack:

```go
resource := telemetry.ResourceWithPackID("customer-support-v2")
converter := telemetry.NewEventConverter(resource)
```

## OTLPExporter

`OTLPExporter` sends spans to an [OTLP/HTTP](https://opentelemetry.io/docs/specs/otlp/#otlphttp) endpoint as JSON.

```go
func NewOTLPExporter(endpoint string, opts ...OTLPExporterOption) *OTLPExporter
```

### Options

| Option | Description | Default |
|--------|-------------|---------|
| `WithHeaders(map[string]string)` | Custom HTTP headers (auth tokens, API keys) | none |
| `WithResource(*Resource)` | Resource for exported spans | `DefaultResource()` |
| `WithBatchSize(int)` | Max spans per export batch | 100 |
| `WithHTTPClient(HTTPClient)` | Custom HTTP client (useful for testing) | `http.Client{Timeout: 30s}` |

### Methods

```go
func (e *OTLPExporter) Export(ctx context.Context, spans []*Span) error
func (e *OTLPExporter) Shutdown(ctx context.Context) error
```

`Export` sends spans immediately. `Shutdown` flushes any pending spans before returning.

## TraceContext

`TraceContext` holds distributed trace headers extracted from inbound requests, supporting [W3C Trace Context](https://www.w3.org/TR/trace-context/) and [AWS X-Ray](https://docs.aws.amazon.com/xray/latest/devguide/xray-concepts.html#xray-concepts-tracingheader).

```go
type TraceContext struct {
    Traceparent string // W3C traceparent header
    Tracestate  string // W3C tracestate header
    XRayTraceID string // AWS X-Ray X-Amzn-Trace-Id header
}
```

### Extracting from HTTP requests

```go
func ExtractTraceContext(r *http.Request) TraceContext
```

Reads `traceparent`, `tracestate`, and `X-Amzn-Trace-Id` headers. Invalid `traceparent` values are silently discarded.

### Middleware

```go
func TraceMiddleware(next http.Handler) http.Handler
```

HTTP middleware that extracts trace context from inbound requests and stores it in the Go context for downstream propagation.

### Injecting into outbound requests

```go
func InjectTraceHeaders(ctx context.Context, req *http.Request)
```

Writes trace headers from the context onto an outbound HTTP request.

## OTLP Wire Format

The exporter serialises spans as [OTLP/HTTP JSON](https://opentelemetry.io/docs/specs/otlp/#otlphttp-request). The payload structure matches the [OpenTelemetry protobuf schema](https://opentelemetry.io/docs/specs/otlp/#otlphttp-request):

```json
{
  "resourceSpans": [{
    "resource": {
      "attributes": [
        {"key": "service.name", "value": {"stringValue": "promptkit"}},
        {"key": "pack.id", "value": {"stringValue": "my-pack"}}
      ]
    },
    "scopeSpans": [{
      "scope": {"name": "promptkit-telemetry", "version": "1.0.0"},
      "spans": [...]
    }]
  }]
}
```

Timestamps are serialised as Unix nanoseconds. Attribute values use the OTLP typed value encoding (`stringValue`, `intValue`, `doubleValue`, `boolValue`).

## Relevant Specifications

- [OpenTelemetry Specification](https://opentelemetry.io/docs/specs/otel/) — core concepts (traces, spans, resources)
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
