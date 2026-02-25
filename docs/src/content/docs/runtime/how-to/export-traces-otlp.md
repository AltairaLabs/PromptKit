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

The listener converts these event types into OTel spans:

| Runtime Event | OTel Span | Key Attributes |
|---------------|-----------|----------------|
| `provider.call.started/completed/failed` | `promptkit.provider.<name>` (Client) | `gen_ai.system`, `gen_ai.request.model`, `gen_ai.usage.*`, `provider.cost` |
| `pipeline.started/completed/failed` | `promptkit.pipeline` (Internal) | `pipeline.duration_ms`, `pipeline.total_cost`, token counts |
| `message.created` | Span event on provider span | `gen_ai.message.content`, `gen_ai.tool_calls` |
| `tool.call.started/completed/failed` | `promptkit.tool.<name>` (Internal) | `tool.args` (JSON), `tool.duration_ms`, `tool.status` |
| `middleware.started/completed/failed` | `promptkit.middleware.<name>` (Internal) | `middleware.duration_ms` |
| `workflow.transitioned` | `promptkit.workflow.transition` (instant) | `workflow.from_state`, `workflow.to_state`, `workflow.event` |
| `workflow.completed` | `promptkit.workflow.completed` (instant) | `workflow.final_state`, `workflow.transition_count` |

### Semantic conventions

Provider spans and message events follow the [OpenTelemetry GenAI Semantic Conventions](https://opentelemetry.io/docs/specs/semconv/gen-ai/):

- **Provider spans** use `gen_ai.system`, `gen_ai.request.model`, `gen_ai.usage.input_tokens`, `gen_ai.usage.output_tokens`, and `gen_ai.response.finish_reason`
- **Message events** are named `gen_ai.<role>.message` (e.g., `gen_ai.user.message`, `gen_ai.assistant.message`) following the [GenAI span events spec](https://opentelemetry.io/docs/specs/semconv/gen-ai/gen-ai-spans/)
- **Tool call arguments** are serialised as JSON in the `tool.args` attribute

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
    "github.com/AltairaLabs/PromptKit/runtime/metrics/prometheus"
    "github.com/AltairaLabs/PromptKit/sdk"
)

// Traces: automatic with WithTracerProvider
conv, _ := sdk.Open("./app.pack.json", "chat",
    sdk.WithTracerProvider(tp),
)

// Metrics: subscribe a Prometheus listener
metricsListener := prometheus.NewMetricsListener()
conv.EventBus().SubscribeAll(metricsListener.Listener())
```

- **Prometheus** gives you dashboards, alerts, and aggregate metrics (p99 latency, error rates, token costs)
- **OTLP traces** give you per-session drill-down (which tool was slow, what the LLM said, workflow path)

## See Also

- [Telemetry Reference](../reference/telemetry) — full API reference and attribute tables
- [Prometheus Metrics](prometheus-metrics) — Prometheus-based monitoring
- [SDK Observability](../../sdk/explanation/observability) — event system architecture
- [Monitor Events](../../sdk/how-to/monitor-events) — SDK event hooks
