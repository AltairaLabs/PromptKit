---
title: Export Traces with OTLP
sidebar:
  order: 9
---
Send PromptKit session traces to any OpenTelemetry-compatible backend.

## Prerequisites

- A running OTLP-compatible collector or backend (e.g., [Jaeger](https://www.jaegertracing.io/), [Grafana Tempo](https://grafana.com/oss/tempo/), [Honeycomb](https://www.honeycomb.io/), or the [OpenTelemetry Collector](https://opentelemetry.io/docs/collector/))
- PromptKit application using the SDK or runtime EventBus

## Basic Setup

### 1. Record events with the EventBus

PromptKit's event system publishes lifecycle events as your pipeline runs. To capture them for OTLP export, attach a `FileEventStore` or accumulate events in memory:

```go
package main

import (
    "context"
    "log"

    "github.com/AltairaLabs/PromptKit/runtime/events"
    "github.com/AltairaLabs/PromptKit/runtime/telemetry"
    "github.com/AltairaLabs/PromptKit/sdk"
)

func main() {
    // Open a conversation (events are published to the internal EventBus)
    conv, err := sdk.Open("./app.pack.json", "chat")
    if err != nil {
        log.Fatal(err)
    }
    defer conv.Close()

    // Collect events for export
    var sessionEvents []events.Event
    conv.EventBus().SubscribeAll(func(e *events.Event) {
        sessionEvents = append(sessionEvents, *e)
    })

    // Run your conversation
    ctx := context.Background()
    resp, _ := conv.Send(ctx, "What's the weather in London?")
    log.Println(resp.Text())

    // Convert and export
    converter := telemetry.NewEventConverter(nil)
    spans, _ := converter.ConvertSession(conv.SessionID(), sessionEvents)

    exporter := telemetry.NewOTLPExporter("http://localhost:4318/v1/traces")
    defer exporter.Shutdown(ctx)

    if err := exporter.Export(ctx, spans); err != nil {
        log.Printf("OTLP export failed: %v", err)
    }
}
```

### 2. View in your backend

Open your tracing UI (e.g., Jaeger at `http://localhost:16686`) and search for traces from `promptkit`. You'll see the full session trace with pipeline, provider, tool, and message spans.

## Tag Traces with Pack ID

Use `ResourceWithPackID` to identify which pack generated the trace:

```go
resource := telemetry.ResourceWithPackID("customer-support-v2")
converter := telemetry.NewEventConverter(resource)
```

Or use `WithResource` on the exporter to set it globally:

```go
exporter := telemetry.NewOTLPExporter(
    "http://localhost:4318/v1/traces",
    telemetry.WithResource(telemetry.ResourceWithPackID("customer-support-v2")),
)
```

This adds a `pack.id` attribute to the [OTLP resource](https://opentelemetry.io/docs/specs/semconv/resource/), making it easy to filter traces by pack in your backend.

## Authenticate with Your Backend

Most hosted backends require an API key or bearer token. Pass custom headers:

```go
exporter := telemetry.NewOTLPExporter(
    "https://api.honeycomb.io/v1/traces",
    telemetry.WithHeaders(map[string]string{
        "x-honeycomb-team": os.Getenv("HONEYCOMB_API_KEY"),
    }),
)
```

For Grafana Cloud:

```go
exporter := telemetry.NewOTLPExporter(
    "https://otlp-gateway-prod-us-east-0.grafana.net/otlp/v1/traces",
    telemetry.WithHeaders(map[string]string{
        "Authorization": "Basic " + base64.StdEncoding.EncodeToString(
            []byte(os.Getenv("GRAFANA_INSTANCE_ID")+":"+os.Getenv("GRAFANA_API_KEY")),
        ),
    }),
)
```

## Propagate Trace Context from Inbound Requests

If your PromptKit application is called from another service that sends [W3C `traceparent`](https://www.w3.org/TR/trace-context/#traceparent-header) headers, you can link the PromptKit session trace to the caller's trace:

### Using TraceMiddleware (recommended)

```go
import (
    "net/http"
    "github.com/AltairaLabs/PromptKit/runtime/telemetry"
)

// Wrap your HTTP handler
mux := http.NewServeMux()
mux.HandleFunc("/chat", handleChat)
http.ListenAndServe(":8080", telemetry.TraceMiddleware(mux))

func handleChat(w http.ResponseWriter, r *http.Request) {
    // Extract trace context from the Go context
    traceCtx := telemetry.TraceContextFromContext(r.Context())

    // ... run your conversation and collect events ...

    // Convert with parent trace
    converter := telemetry.NewEventConverter(nil)
    spans, _ := converter.ConvertSessionWithParent(sessionID, sessionEvents, &traceCtx)

    exporter.Export(r.Context(), spans)
}
```

### Manual extraction

```go
traceCtx := telemetry.ExtractTraceContext(r)
spans, _ := converter.ConvertSessionWithParent(sessionID, sessionEvents, &traceCtx)
```

When a valid `traceparent` is provided, the session root span inherits the caller's trace ID and sets `ParentSpanID` to the caller's span ID. This makes the PromptKit session appear as a child in the caller's distributed trace.

## What Gets Exported

The converter handles these event types:

| Runtime Event | OTLP Representation | Key Attributes |
|---------------|---------------------|----------------|
| `pipeline.started/completed/failed` | `pipeline` span | `pipeline.duration_ms`, `pipeline.total_cost`, token counts |
| `provider.call.started/completed/failed` | `provider.<name>` span | `gen_ai.system`, `gen_ai.usage.*`, `gen_ai.response.finish_reason` |
| `message.created` | Span event on provider span | `gen_ai.message.content`, `gen_ai.tool_calls` |
| `tool.call.started/completed/failed` | `tool.<name>` span | `tool.args` (JSON), `tool.duration_ms`, `tool.status` |
| `middleware.started/completed/failed` | `middleware.<name>` span | `middleware.duration_ms` |
| `workflow.transitioned` | `workflow.transition` instant span | `workflow.from_state`, `workflow.to_state`, `workflow.event` |
| `workflow.completed` | `workflow.completed` instant span | `workflow.final_state`, `workflow.transition_count` |

### Semantic conventions

Provider spans and message events follow the [OpenTelemetry GenAI Semantic Conventions](https://opentelemetry.io/docs/specs/semconv/gen-ai/):

- **Provider spans** use `gen_ai.system`, `gen_ai.operation`, `gen_ai.usage.input_tokens`, `gen_ai.usage.output_tokens`, and `gen_ai.response.finish_reason`
- **Message events** are named `gen_ai.<role>.message` (e.g., `gen_ai.user.message`, `gen_ai.assistant.message`) following the [GenAI span events spec](https://opentelemetry.io/docs/specs/semconv/gen-ai/gen-ai-spans/)
- **Tool call arguments** are serialised as JSON in the `tool.args` attribute

## Export to the OpenTelemetry Collector

If you run an [OpenTelemetry Collector](https://opentelemetry.io/docs/collector/), point the exporter at its OTLP HTTP receiver (default port 4318):

```go
exporter := telemetry.NewOTLPExporter("http://localhost:4318/v1/traces")
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
    "github.com/AltairaLabs/PromptKit/runtime/events"
    "github.com/AltairaLabs/PromptKit/runtime/metrics/prometheus"
    "github.com/AltairaLabs/PromptKit/runtime/telemetry"
)

// Metrics (real-time aggregation)
metricsListener := prometheus.NewMetricsListener()
eventBus.SubscribeAll(metricsListener.Listener())

// Traces (per-request detail)
var sessionEvents []events.Event
eventBus.SubscribeAll(func(e *events.Event) {
    sessionEvents = append(sessionEvents, *e)
})
```

- **Prometheus** gives you dashboards, alerts, and aggregate metrics (p99 latency, error rates, token costs)
- **OTLP traces** give you per-session drill-down (which tool was slow, what the LLM said, workflow path)

## See Also

- [Telemetry Reference](../reference/telemetry) — full API reference and attribute tables
- [Prometheus Metrics](prometheus-metrics) — Prometheus-based monitoring
- [SDK Observability](../../sdk/explanation/observability) — event system architecture
- [Monitor Events](../../sdk/how-to/monitor-events) — SDK event hooks
