---
title: Telemetry
sidebar:
  order: 8
---
OpenTelemetry-compatible tracing for PromptKit sessions, following the [OpenTelemetry GenAI Semantic Conventions](https://opentelemetry.io/docs/specs/semconv/gen-ai/).

## Overview

The `runtime/telemetry` package integrates PromptKit with the [OpenTelemetry Go SDK](https://opentelemetry.io/docs/languages/go/). It provides:

- A **real-time event listener** that converts EventBus events into OTel spans as they occur
- **Typed spans** using `gen_ai.operation.name` as a discriminator, per the [GenAI Semantic Conventions](https://opentelemetry.io/docs/specs/semconv/gen-ai/gen-ai-spans/)
- **TracerProvider** helpers for standalone OTLP export
- **Propagation** setup for W3C Trace Context, W3C Baggage, and AWS X-Ray headers

Because it uses the standard OTel SDK, spans are exported through any configured `SpanExporter` — OTLP/HTTP, OTLP/gRPC, Jaeger, Zipkin, or custom exporters.

```go
import "github.com/AltairaLabs/PromptKit/runtime/telemetry"
```

## Trace Structure

Each session produces a single trace. Span names follow the GenAI SIG convention `{gen_ai.system} {gen_ai.operation.name}` where applicable.

```
promptkit invoke_agent (root, SpanKindServer)
├── openai chat (SpanKindClient)
│   ├── [event] gen_ai.user.message
│   └── [event] gen_ai.assistant.message
├── execute_tool (SpanKindInternal)
├── openai chat (SpanKindClient)
│   └── [event] gen_ai.assistant.message
├── promptkit.middleware.auth (SpanKindInternal)
├── promptkit.eval.banned_words (SpanKindInternal)
├── promptkit.eval.response-quality (SpanKindInternal, instant)
├── promptkit.workflow.transition (SpanKindInternal, instant)
├── promptkit.workflow.transition (SpanKindInternal, instant)
└── promptkit.workflow.completed (SpanKindInternal, instant)
```

## Typed Operations

Every span carries a `gen_ai.operation.name` attribute (where applicable) that identifies its semantic type. This follows the [GenAI Agent Spans](https://opentelemetry.io/docs/specs/semconv/gen-ai/gen-ai-agent-spans/) convention.

| Span Name | `gen_ai.operation.name` | Span Kind | Description |
|-----------|------------------------|-----------|-------------|
| `promptkit invoke_agent` | `invoke_agent` | Server | Root session span |
| `{system} chat` | `chat` | Client | LLM provider call |
| `execute_tool` | `execute_tool` | Internal | Tool execution |
| `promptkit.pipeline` | — | Internal | Pipeline execution |
| `promptkit.middleware.{name}` | — | Internal | Middleware execution |
| `promptkit.eval.{name}` | — | Internal | Guardrail or eval execution |
| `promptkit.workflow.transition` | — | Internal | Workflow state transition (instant) |
| `promptkit.workflow.completed` | — | Internal | Workflow terminal state (instant) |

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

`StartSession` creates a root `promptkit invoke_agent` span, optionally parented under the span in `parentCtx`. All subsequent spans for this session are children of this root. `EndSession` ends the root span.

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

### Session span (`invoke_agent`)

The root span for each conversation session.

**Span name:** `promptkit invoke_agent`

**Attributes** follow the [GenAI Agent Spans](https://opentelemetry.io/docs/specs/semconv/gen-ai/gen-ai-agent-spans/) convention:

| Attribute | Source | Spec reference |
|-----------|--------|---------------|
| `gen_ai.operation.name` | `"invoke_agent"` | [gen_ai.operation.name](https://opentelemetry.io/docs/specs/semconv/attributes-registry/gen-ai/) |
| `gen_ai.system` | `"promptkit"` | [gen_ai.system](https://opentelemetry.io/docs/specs/semconv/attributes-registry/gen-ai/) |
| `gen_ai.conversation.id` | Session ID | [gen_ai.conversation.id](https://opentelemetry.io/docs/specs/semconv/attributes-registry/gen-ai/) |
| `gen_ai.agent.name` | Pack name (when available) | [gen_ai.agent.name](https://opentelemetry.io/docs/specs/semconv/attributes-registry/gen-ai/) |
| `gen_ai.agent.id` | Pack ID (when available) | [gen_ai.agent.id](https://opentelemetry.io/docs/specs/semconv/attributes-registry/gen-ai/) |

### Provider events (`chat`)

| Event | Span |
|-------|------|
| `provider.call.started` / `provider.call.completed` / `provider.call.failed` | `{system} chat` span (SpanKindClient) |

**Span name:** `{provider} chat` (e.g., `openai chat`, `anthropic chat`)

**Attributes** follow the [GenAI Client Spans](https://opentelemetry.io/docs/specs/semconv/gen-ai/gen-ai-spans/) convention:

| Attribute | Source | Spec reference |
|-----------|--------|---------------|
| `gen_ai.operation.name` | `"chat"` | [gen_ai.operation.name](https://opentelemetry.io/docs/specs/semconv/attributes-registry/gen-ai/) |
| `gen_ai.system` | Provider name | [gen_ai.system](https://opentelemetry.io/docs/specs/semconv/attributes-registry/gen-ai/) |
| `gen_ai.request.model` | Model name | [gen_ai.request.model](https://opentelemetry.io/docs/specs/semconv/attributes-registry/gen-ai/) |
| `gen_ai.usage.input_tokens` | Input token count | [gen_ai.usage.input_tokens](https://opentelemetry.io/docs/specs/semconv/gen-ai/gen-ai-metrics/) |
| `gen_ai.usage.output_tokens` | Output token count | [gen_ai.usage.output_tokens](https://opentelemetry.io/docs/specs/semconv/gen-ai/gen-ai-metrics/) |
| `gen_ai.response.finish_reason` | Finish reason | [gen_ai.response.finish_reasons](https://opentelemetry.io/docs/specs/semconv/attributes-registry/gen-ai/) |
| `promptkit.message.count` | Number of messages | PromptKit-specific |
| `promptkit.tool.count` | Number of tools | PromptKit-specific |
| `promptkit.provider.cost` | Estimated cost (USD) | PromptKit-specific |

### Pipeline events

| Event | Span |
|-------|------|
| `pipeline.started` / `pipeline.completed` / `pipeline.failed` | `promptkit.pipeline` span (SpanKindInternal) |

**Attributes:** `promptkit.run.id`, `promptkit.pipeline.cost`, `gen_ai.usage.input_tokens`, `gen_ai.usage.output_tokens`

### Message events

| Event | Behaviour |
|-------|-----------|
| `message.created` | Appended as a [SpanEvent](https://opentelemetry.io/docs/concepts/signals/traces/#span-events) on the active provider span |

Messages are not separate spans. They are attached as **span events** on the currently active `{system} chat` span, following the [GenAI Events conventions](https://opentelemetry.io/docs/specs/semconv/gen-ai/gen-ai-spans/). If no provider span is active, the event is attached to the root session span.

**Event name:** `gen_ai.<role>.message` (e.g., `gen_ai.user.message`, `gen_ai.assistant.message`)

**Event attributes:**

| Attribute | Type | Description |
|-----------|------|-------------|
| `gen_ai.message.content` | string | Text content of the message |
| `gen_ai.tool_calls` | string (JSON) | Tool calls requested by assistant (present only when non-empty) |
| `gen_ai.tool_result` | string (JSON) | Tool result for tool-role messages (present only when non-nil) |

### Tool events (`execute_tool`)

| Event | Span |
|-------|------|
| `tool.call.started` / `tool.call.completed` / `tool.call.failed` | `execute_tool` span (SpanKindInternal) |

**Attributes** follow the [GenAI Agent Spans](https://opentelemetry.io/docs/specs/semconv/gen-ai/gen-ai-agent-spans/) convention:

| Attribute | Type | Spec reference |
|-----------|------|---------------|
| `gen_ai.operation.name` | `"execute_tool"` | [gen_ai.operation.name](https://opentelemetry.io/docs/specs/semconv/attributes-registry/gen-ai/) |
| `gen_ai.tool.name` | string | [gen_ai.tool.name](https://opentelemetry.io/docs/specs/semconv/attributes-registry/gen-ai/) |
| `gen_ai.tool.call.id` | string | [gen_ai.tool.call.id](https://opentelemetry.io/docs/specs/semconv/attributes-registry/gen-ai/) |
| `gen_ai.tool.call.arguments` | string (JSON) | [gen_ai.tool.call.arguments](https://opentelemetry.io/docs/specs/semconv/attributes-registry/gen-ai/) (omitted when nil) |
| `gen_ai.tool.type` | string | [gen_ai.tool.type](https://opentelemetry.io/docs/specs/semconv/attributes-registry/gen-ai/) — `"function"` for regular tools, `"extension"` for MCP tools |

Tool execution duration is captured by the span's start/end timestamps. Success or failure is captured by the span status code. MCP tools (prefixed `mcp__`) are automatically detected and tagged with `gen_ai.tool.type = "extension"` per the [MCP conventions](https://opentelemetry.io/docs/specs/semconv/gen-ai/mcp/).

### Middleware events

| Event | Span |
|-------|------|
| `middleware.started` / `middleware.completed` / `middleware.failed` | `promptkit.middleware.{name}` span (SpanKindInternal) |

**Attributes:** `promptkit.middleware.name`, `promptkit.middleware.index`

### Validation events (guardrails)

| Event | Span |
|-------|------|
| `validation.started` / `validation.passed` / `validation.failed` | `promptkit.eval.{name}` span (SpanKindInternal) |

Guardrail validations are traced as evaluation spans using the [GenAI Evaluation Attributes](https://opentelemetry.io/docs/specs/semconv/registry/attributes/gen-ai/). The `promptkit.guardrail` attribute distinguishes guardrails from other evals.

**Attributes:**

| Attribute | Type | Description | Spec reference |
|-----------|------|-------------|----------------|
| `gen_ai.evaluation.name` | string | Validator name (e.g., `banned_words`) | [gen_ai.evaluation.name](https://opentelemetry.io/docs/specs/semconv/registry/attributes/gen-ai/) |
| `gen_ai.evaluation.score` | float64 | `1.0` if passed, `0.0` if failed | [gen_ai.evaluation.score](https://opentelemetry.io/docs/specs/semconv/registry/attributes/gen-ai/) |
| `gen_ai.evaluation.explanation` | string | Error message or joined violations (on failure only) | [gen_ai.evaluation.explanation](https://opentelemetry.io/docs/specs/semconv/registry/attributes/gen-ai/) |
| `promptkit.eval.type` | string | Validator type (e.g., `output`) | PromptKit-specific |
| `promptkit.guardrail` | bool | `true` — distinguishes guardrails from evals | PromptKit-specific |

### Eval events

| Event | Span |
|-------|------|
| `eval.completed` / `eval.failed` | `promptkit.eval.{evalID}` instant span (SpanKindInternal) |

Evals (assertions, LLM judges, content checks) are traced as instant evaluation spans. They share the same [GenAI Evaluation Attributes](https://opentelemetry.io/docs/specs/semconv/registry/attributes/gen-ai/) as guardrails but with `promptkit.guardrail = false`.

**Attributes:**

| Attribute | Type | Description | Spec reference |
|-----------|------|-------------|----------------|
| `gen_ai.evaluation.name` | string | Eval ID | [gen_ai.evaluation.name](https://opentelemetry.io/docs/specs/semconv/registry/attributes/gen-ai/) |
| `gen_ai.evaluation.score` | float64 | Numeric score (omitted when nil) | [gen_ai.evaluation.score](https://opentelemetry.io/docs/specs/semconv/registry/attributes/gen-ai/) |
| `gen_ai.evaluation.explanation` | string | Human-readable explanation (omitted when empty) | [gen_ai.evaluation.explanation](https://opentelemetry.io/docs/specs/semconv/registry/attributes/gen-ai/) |
| `promptkit.eval.type` | string | Handler type (e.g., `llm_judge`, `contains`) | PromptKit-specific |
| `promptkit.guardrail` | bool | `false` — distinguishes evals from guardrails | PromptKit-specific |

Passed evals have span status `Ok`. Failed evals have span status `Error` with the explanation or error message.

### Workflow events

| Event | Span |
|-------|------|
| `workflow.transitioned` | `promptkit.workflow.transition` instant span (SpanKindInternal) |
| `workflow.completed` | `promptkit.workflow.completed` instant span (SpanKindInternal) |

Workflow spans are **instant** — their start and end times are both set to the event timestamp.

**Transition attributes:**

| Attribute | Type | Description |
|-----------|------|-------------|
| `promptkit.workflow.from_state` | string | State before transition |
| `promptkit.workflow.to_state` | string | State after transition |
| `promptkit.workflow.event` | string | Trigger event |
| `promptkit.workflow.prompt_task` | string | Prompt task of the new state |

**Completion attributes:**

| Attribute | Type | Description |
|-----------|------|-------------|
| `promptkit.workflow.final_state` | string | Terminal state reached |
| `promptkit.workflow.transition_count` | int | Total number of transitions |

### Error handling

When a `*.failed` event is received, the corresponding span's status is set to `codes.Error` with the error message.

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
- [GenAI Client Spans](https://opentelemetry.io/docs/specs/semconv/gen-ai/gen-ai-spans/) — span naming for chat completions
- [GenAI Agent Spans](https://opentelemetry.io/docs/specs/semconv/gen-ai/gen-ai-agent-spans/) — span naming for agents and tools
- [GenAI Attributes Registry](https://opentelemetry.io/docs/specs/semconv/registry/attributes/gen-ai/) — full attribute reference
- [Resource Semantic Conventions](https://opentelemetry.io/docs/specs/semconv/resource/) — `service.name`, `service.version`
- [W3C Trace Context](https://www.w3.org/TR/trace-context/) — `traceparent` / `tracestate` headers
- [AWS X-Ray Trace Header](https://docs.aws.amazon.com/xray/latest/devguide/xray-concepts.html#xray-concepts-tracingheader) — `X-Amzn-Trace-Id`

## See Also

- [How-To: Export Traces with OTLP](../how-to/export-traces-otlp) — end-to-end setup guide
- [How-To: Prometheus Metrics](../how-to/prometheus-metrics) — Prometheus-based monitoring
- [Logging Reference](logging) — structured logging
- [SDK Observability](../../sdk/explanation/observability) — event system overview
