---
title: Export traces with OTLP
description: Send PromptKit session traces to any OpenTelemetry-compatible backend.
sidebar:
  order: 9
verified:
  commit: 8489bf3d95c7498f9de0f024769db9399c24e884
  sources:
    - runtime/a2a/client.go
    - runtime/events/bus.go
    - runtime/events/redact.go
    - runtime/telemetry/content_capture.go
    - runtime/telemetry/listener.go
    - runtime/telemetry/provider.go
    - sdk/conversation.go
    - sdk/options.go
    - sdk/sdk.go
    - server/a2a/handler_send.go
    - server/a2a/server.go
---
Send PromptKit session traces to an OpenTelemetry-compatible backend over OTLP/HTTP.

## Prerequisites

- A collector or backend that accepts OTLP/HTTP, such as [Jaeger](https://www.jaegertracing.io/), [Grafana Tempo](https://grafana.com/oss/tempo/), [Honeycomb](https://www.honeycomb.io/) or the [OpenTelemetry Collector](https://opentelemetry.io/docs/collector/).
- An application that opens conversations with the SDK.

## 1. Create a tracer provider

`telemetry.NewTracerProvider` builds a provider that exports spans over OTLP/HTTP to the endpoint you give it:

```go
import "github.com/AltairaLabs/PromptKit/runtime/v2/telemetry"

tp, err := telemetry.NewTracerProvider(ctx,
    "http://localhost:4318/v1/traces",
    "my-chat-app",
)
if err != nil {
    log.Fatal(err)
}
defer tp.Shutdown(ctx)
```

To set headers, a resource or a sampler, build the provider with the OpenTelemetry SDK instead:

```go
exporter, err := otlptracehttp.New(ctx,
    otlptracehttp.WithEndpointURL("http://localhost:4318/v1/traces"),
)
if err != nil {
    log.Fatal(err)
}

tp := sdktrace.NewTracerProvider(
    sdktrace.WithBatcher(exporter),
    sdktrace.WithResource(resource.NewSchemaless(
        semconv.ServiceName("my-chat-app"),
    )),
)
defer tp.Shutdown(ctx)
```

### Authenticate with a hosted backend

Pass the API key as an exporter header:

```go
// Honeycomb
exporter, err := otlptracehttp.New(ctx,
    otlptracehttp.WithEndpointURL("https://api.honeycomb.io/v1/traces"),
    otlptracehttp.WithHeaders(map[string]string{
        "x-honeycomb-team": os.Getenv("HONEYCOMB_API_KEY"),
    }),
)
```

```go
// Grafana Cloud
exporter, err := otlptracehttp.New(ctx,
    otlptracehttp.WithEndpointURL("https://otlp-gateway-prod-us-east-0.grafana.net/otlp/v1/traces"),
    otlptracehttp.WithHeaders(map[string]string{
        "Authorization": "Basic " + base64.StdEncoding.EncodeToString(
            []byte(os.Getenv("GRAFANA_INSTANCE_ID")+":"+os.Getenv("GRAFANA_API_KEY")),
        ),
    }),
)
```

## 2. Open a conversation with the provider

`WithTracerProvider` subscribes an `OTelEventListener` to the conversation's event bus. Without it, the SDK creates no spans.

```go
conv, err := sdk.Open("./app.pack.json", "chat",
    sdk.WithTracerProvider(tp),
)
if err != nil {
    log.Fatal(err)
}
defer conv.Close()

resp, err := conv.Send(ctx, "What's the weather in London?")
if err != nil {
    log.Fatal(err)
}
log.Println(resp.Text())
```

To export Prometheus metrics from the same conversation, add `sdk.WithMetrics(collector, nil)` to the options. See [Prometheus metrics](/runtime/how-to/observability/prometheus-metrics/).

## 3. Link traces across services

`Send` and `Stream` parent the session span under the span in the `ctx` you pass. To join a caller's trace, configure the propagator once at startup:

```go
telemetry.SetupPropagation()
```

This sets the global propagator to W3C Trace Context, W3C Baggage and AWS X-Ray.

- The A2A server wraps its handler in `otelhttp`, which extracts inbound trace context, so conversation spans appear under the caller's trace.
- The A2A client injects trace context into outbound requests with the global propagator.

## 4. Capture content (optional)

Spans carry structure, timing, token usage, model and tool names. They do not carry content unless you opt in. These attributes are off by default:

| Attribute | Carries |
|-----------|---------|
| `gen_ai.tool.call.arguments` | arguments the model passed to a tool |
| `gen_ai.tool_calls` | tool-call payloads on a message |
| `gen_ai.tool_result` | tool return values |
| `gen_ai.message.content` | raw message content |

Tool arguments can contain identifiers, free text and delegated credentials. Enable capture only where the backend is an appropriate place for that data:

```go
conv, err := sdk.Open("./app.pack.json", "chat",
    sdk.WithTracerProvider(tp),
    sdk.WithTelemetryContentCapture(true),
)
```

### Scrub captured content

`WithEventRedactor` runs a policy for every subscriber of the conversation's bus: the tracer, a configured event store and the metrics collector. Each subscriber receives its own redacted copy.

```go
sdk.WithEventRedactor(func(field, value string) string {
    if field == events.FieldToolCallArgs {
        return tokenPattern.ReplaceAllString(value, "[REDACTED]")
    }
    return value
}),
```

`WithTelemetryContentCapture` only gates spans: an event store you configure still receives raw events unless you set a redactor. Neither setting applies to `WithRecording`. `RecordingStage` writes to its store without a bus hop, so recordings keep full fidelity. The [telemetry-redaction example](https://github.com/AltairaLabs/PromptKit/tree/main/sdk/examples/telemetry-redaction) shows each configuration.

## 5. Route through an OpenTelemetry Collector (optional)

Point the exporter at the Collector's OTLP/HTTP receiver (port 4318 by default) and let it fan out:

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

## 6. Wire the listener yourself (optional)

If you manage the event bus, subscribe the listener directly. Pass your own parent context to `StartSession` to nest the session under an existing trace:

```go
tracer := telemetry.Tracer(tp)
listener := telemetry.NewOTelEventListener(tracer, telemetry.WithContentCapture(false))
defer listener.Close()

listener.StartSession(parentCtx, sessionID)

bus := events.NewEventBus()
bus.SubscribeAll(listener.OnEvent)

// ... run your conversation with this bus ...

listener.EndSession(sessionID)
```

To redact, subscribe `events.Redacting(listener.OnEvent, policy)` instead of `listener.OnEvent`.

## Verify

Send a message, then search your tracing UI (Jaeger listens on `http://localhost:16686`) for the service name. The trace has one root span per session and these children:

| Runtime event | Span | `gen_ai.operation.name` | Key attributes |
|---------------|------|-------------------------|----------------|
| Session start and end | `promptkit invoke_agent` (server) | `invoke_agent` | `gen_ai.conversation.id`, `gen_ai.agent.name`, `gen_ai.agent.id` |
| `pipeline.*` | `promptkit.pipeline` | none | `promptkit.pipeline.cost`, `gen_ai.usage.input_tokens`, `gen_ai.usage.output_tokens` |
| `provider.call.*` | `{provider} chat` (client) | `chat` | `gen_ai.system`, `gen_ai.request.model`, `gen_ai.usage.*`, `gen_ai.response.finish_reason`, `promptkit.provider.cost` |
| `tool.call.*` | `execute_tool` | `execute_tool` | `gen_ai.tool.name`, `gen_ai.tool.call.id`, `gen_ai.tool.type` |
| `message.created` | span event `gen_ai.<role>.message` on the provider span | none | content attributes, only with capture on |
| `middleware.*` | `promptkit.middleware.{name}` | none | `promptkit.middleware.name`, `promptkit.middleware.index` |
| `validation.*` | `promptkit.eval.{name}` | none | `gen_ai.evaluation.name`, `gen_ai.evaluation.score`, `promptkit.guardrail` |
| `eval.*` | `promptkit.eval.{evalID}` (instant) | none | `gen_ai.evaluation.name`, `gen_ai.evaluation.score`, `gen_ai.evaluation.explanation` |
| `workflow.transitioned` | `promptkit.workflow.transition` (instant) | none | `promptkit.workflow.from_state`, `promptkit.workflow.to_state` |
| `workflow.completed` | `promptkit.workflow.completed` (instant) | none | `promptkit.workflow.final_state`, `promptkit.workflow.transition_count` |
| `composition.*` | `promptkit.composition`, `promptkit.composition.step` | none | `promptkit.composition.name`, `promptkit.composition.step_id` |

`gen_ai.tool.call.arguments` also appears on tool spans when capture is on. Attribute names follow the [OpenTelemetry GenAI conventions](https://opentelemetry.io/docs/specs/semconv/gen-ai/). PromptKit-specific attributes use the `promptkit.*` prefix.

## Troubleshoot

- No spans arrive: the provider batches exports, so call `tp.Shutdown(ctx)` before the process exits.
- Tool arguments or message content are missing: content capture is off. Enable it in step 4.

## Related

- [Telemetry reference](/runtime/reference/telemetry/)
- [Prometheus metrics](/runtime/how-to/observability/prometheus-metrics/)
- [SDK observability](/sdk/explanation/observability/)
- [Monitor events](/sdk/how-to/observability/monitor-events/)
