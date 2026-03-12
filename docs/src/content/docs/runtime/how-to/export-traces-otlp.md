---
title: Export Traces with OTLP
sidebar:
  order: 9
---
Send PromptKit session traces to any OpenTelemetry-compatible backend.

## Prerequisites

- A running OTLP-compatible collector or backend (e.g., [Jaeger](https://www.jaegertracing.io/), [Grafana Tempo](https://grafana.com/oss/tempo/), [Honeycomb](https://www.honeycomb.io/), or the [OpenTelemetry Collector](https://opentelemetry.io/docs/collector/))
- PromptKit application using the SDK

## Quick Start with the SDK

The simplest way to export traces is via the `WithTracerProvider` SDK option. The SDK automatically wires an `OTelEventListener` into the EventBus — no manual setup needed.

```go
package main

import (
    "context"
    "log"

    "go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
    "go.opentelemetry.io/otel/sdk/resource"
    sdktrace "go.opentelemetry.io/otel/sdk/trace"
    semconv "go.opentelemetry.io/otel/semconv/v1.26.0"

    "github.com/AltairaLabs/PromptKit/sdk"
)

func main() {
    ctx := context.Background()

    // Set up an OTLP exporter pointing at your backend.
    exporter, err := otlptracehttp.New(ctx,
        otlptracehttp.WithEndpointURL("http://localhost:4318/v1/traces"),
    )
    if err != nil {
        log.Fatal(err)
    }

    // Create a TracerProvider with the exporter.
    tp := sdktrace.NewTracerProvider(
        sdktrace.WithBatcher(exporter),
        sdktrace.WithResource(resource.NewSchemaless(
            semconv.ServiceName("my-chat-app"),
        )),
    )
    defer tp.Shutdown(ctx)

    // Open a conversation with tracing enabled.
    conv, err := sdk.Open("./app.pack.json", "chat",
        sdk.WithTracerProvider(tp),
    )
    if err != nil {
        log.Fatal(err)
    }
    defer conv.Close()

    resp, _ := conv.Send(ctx, "What's the weather in London?")
    log.Println(resp.Text())
}
```

Open your tracing UI (e.g., Jaeger at `http://localhost:16686`) and search for traces. You'll see provider, tool, and message spans with full attribute detail.

### Using the built-in helper

If you don't need custom exporter configuration, `telemetry.NewTracerProvider` creates a ready-to-use provider:

```go
import "github.com/AltairaLabs/PromptKit/runtime/telemetry"

tp, err := telemetry.NewTracerProvider(ctx,
    "http://localhost:4318/v1/traces",
    "my-chat-app",
)
if err != nil {
    log.Fatal(err)
}
defer tp.Shutdown(ctx)

conv, _ := sdk.Open("./app.pack.json", "chat",
    sdk.WithTracerProvider(tp),
)
```

## Manual Listener Setup

For advanced use cases (e.g., injecting a parent trace context), wire the listener manually:

```go
import (
    "github.com/AltairaLabs/PromptKit/runtime/events"
    "github.com/AltairaLabs/PromptKit/runtime/telemetry"
)

tracer := telemetry.Tracer(tp)
listener := telemetry.NewOTelEventListener(tracer)

// Create a root session span, optionally parented under an inbound trace.
listener.StartSession(parentCtx, sessionID)

// Wire into the EventBus.
bus := events.NewEventBus()
bus.SubscribeAll(listener.OnEvent)

// ... run your conversation with this bus ...

listener.EndSession(sessionID)
```

## Propagate Trace Context Across Services

If your PromptKit application is called from another service (e.g., via A2A), you can link the PromptKit session trace to the caller's trace using standard OTel propagation.

### Setup propagation (once at startup)

```go
import "github.com/AltairaLabs/PromptKit/runtime/telemetry"

telemetry.SetupPropagation()
```

This configures the global propagator for W3C Trace Context, W3C Baggage, and AWS X-Ray headers.

### Server side (extract inbound context)

The A2A server uses `otelhttp` middleware, which automatically extracts trace context from inbound HTTP requests. When `WithTracerProvider` is configured, spans from the conversation appear as children of the caller's trace.

### Client side (inject outbound context)

The A2A client automatically injects trace context into outbound HTTP requests using the global propagator:

```go
import "go.opentelemetry.io/otel"

// The propagator injects traceparent/tracestate/X-Amzn-Trace-Id headers.
otel.GetTextMapPropagator().Inject(ctx, propagation.HeaderCarrier(req.Header))
```

## Authenticate with Your Backend

Most hosted backends require an API key. Configure authentication on the OTLP exporter:

### Honeycomb

```go
exporter, _ := otlptracehttp.New(ctx,
    otlptracehttp.WithEndpointURL("https://api.honeycomb.io/v1/traces"),
    otlptracehttp.WithHeaders(map[string]string{
        "x-honeycomb-team": os.Getenv("HONEYCOMB_API_KEY"),
    }),
)
```

### Grafana Cloud

```go
exporter, _ := otlptracehttp.New(ctx,
    otlptracehttp.WithEndpointURL("https://otlp-gateway-prod-us-east-0.grafana.net/otlp/v1/traces"),
    otlptracehttp.WithHeaders(map[string]string{
        "Authorization": "Basic " + base64.StdEncoding.EncodeToString(
            []byte(os.Getenv("GRAFANA_INSTANCE_ID")+":"+os.Getenv("GRAFANA_API_KEY")),
        ),
    }),
)
```

## What Gets Exported

The listener converts runtime events into typed OTel spans following the [GenAI Semantic Conventions](https://opentelemetry.io/docs/specs/semconv/gen-ai/). Each span carries a `gen_ai.operation.name` attribute that identifies its semantic type.

| Runtime Event | OTel Span | `gen_ai.operation.name` | Key Attributes |
|---------------|-----------|------------------------|----------------|
| Session start/end | `promptkit invoke_agent` (Server) | `invoke_agent` | `gen_ai.conversation.id`, `gen_ai.agent.name`, `gen_ai.agent.id` |
| `provider.call.*` | `{system} chat` (Client) | `chat` | `gen_ai.system`, `gen_ai.request.model`, `gen_ai.usage.*`, `promptkit.provider.cost` |
| `pipeline.*` | `promptkit.pipeline` (Internal) | — | `promptkit.pipeline.cost`, token counts |
| `message.created` | Span event on provider span | — | `gen_ai.message.content`, `gen_ai.tool_calls` |
| `tool.call.*` | `execute_tool` (Internal) | `execute_tool` | `gen_ai.tool.name`, `gen_ai.tool.call.id`, `gen_ai.tool.call.arguments`, `gen_ai.tool.type` |
| `middleware.*` | `promptkit.middleware.{name}` (Internal) | — | `promptkit.middleware.name`, `promptkit.middleware.index` |
| `validation.*` | `promptkit.eval.{name}` (Internal) | — | `gen_ai.evaluation.name`, `gen_ai.evaluation.score`, `promptkit.guardrail` |
| `eval.*` | `promptkit.eval.{evalID}` (Internal, instant) | — | `gen_ai.evaluation.name`, `gen_ai.evaluation.score`, `gen_ai.evaluation.explanation` |
| `workflow.transitioned` | `promptkit.workflow.transition` (instant) | — | `promptkit.workflow.from_state`, `promptkit.workflow.to_state` |
| `workflow.completed` | `promptkit.workflow.completed` (instant) | — | `promptkit.workflow.final_state`, `promptkit.workflow.transition_count` |

### Semantic conventions

Spans follow the [OpenTelemetry GenAI Semantic Conventions](https://opentelemetry.io/docs/specs/semconv/gen-ai/):

- **Session spans** use `invoke_agent` from the [GenAI Agent Spans](https://opentelemetry.io/docs/specs/semconv/gen-ai/gen-ai-agent-spans/) spec
- **Provider spans** use `chat` from the [GenAI Client Spans](https://opentelemetry.io/docs/specs/semconv/gen-ai/gen-ai-spans/) spec with `gen_ai.system`, `gen_ai.request.model`, `gen_ai.usage.*`, and `gen_ai.response.finish_reason`
- **Tool spans** use `execute_tool` from the [GenAI Agent Spans](https://opentelemetry.io/docs/specs/semconv/gen-ai/gen-ai-agent-spans/) spec with `gen_ai.tool.name`, `gen_ai.tool.call.id`, and `gen_ai.tool.call.arguments`
- **Eval and guardrail spans** use [GenAI Evaluation Attributes](https://opentelemetry.io/docs/specs/semconv/registry/attributes/gen-ai/) (`gen_ai.evaluation.name`, `gen_ai.evaluation.score`, `gen_ai.evaluation.explanation`). Guardrails are distinguished by `promptkit.guardrail = true`
- **Message events** are named `gen_ai.<role>.message` following the [GenAI span events spec](https://opentelemetry.io/docs/specs/semconv/gen-ai/gen-ai-spans/)
- **PromptKit-specific attributes** are namespaced under `promptkit.*` (e.g., `promptkit.provider.cost`, `promptkit.workflow.from_state`)

## Export to the OpenTelemetry Collector

If you run an [OpenTelemetry Collector](https://opentelemetry.io/docs/collector/), point the exporter at its OTLP HTTP receiver (default port 4318):

```go
exporter, _ := otlptracehttp.New(ctx,
    otlptracehttp.WithEndpointURL("http://localhost:4318/v1/traces"),
)
```

The Collector can then fan out to multiple backends. Example `otel-collector-config.yaml`:

```yaml
receivers:
  otlp:
    protocols:
      http:
        endpoint: "0.0.0.0:4318"

exporters:
  otlp/jaeger:
    endpoint: "jaeger:4317"
    tls:
      insecure: true
  otlp/tempo:
    endpoint: "tempo:4317"
    tls:
      insecure: true

service:
  pipelines:
    traces:
      receivers: [otlp]
      exporters: [otlp/jaeger, otlp/tempo]
```

## Combine with Prometheus Metrics

OTLP traces and Prometheus metrics are complementary. Use both for full observability:

```go
import (
    "github.com/prometheus/client_golang/prometheus"
    "github.com/AltairaLabs/PromptKit/runtime/metrics"
    "github.com/AltairaLabs/PromptKit/sdk"
)

reg := prometheus.NewRegistry()
collector := metrics.NewCollector(metrics.CollectorOpts{
    Registerer: reg,
    Namespace:  "myapp",
})

// Both traces and metrics in one call
conv, _ := sdk.Open("./app.pack.json", "chat",
    sdk.WithTracerProvider(tp),
    sdk.WithMetrics(collector, nil),
)
```

- **Prometheus** gives you dashboards, alerts, and aggregate metrics (p99 latency, error rates, token costs)
- **OTLP traces** give you per-session drill-down (which tool was slow, what the LLM said, workflow path)

## See Also

- [Telemetry Reference](../reference/telemetry) — full API reference and attribute tables
- [Prometheus Metrics](prometheus-metrics) — Prometheus-based monitoring
- [SDK Observability](../../sdk/explanation/observability) — event system architecture
- [Monitor Events](../../sdk/how-to/monitor-events) — SDK event hooks
