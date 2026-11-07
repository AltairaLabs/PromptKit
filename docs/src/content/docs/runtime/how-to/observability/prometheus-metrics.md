---
title: Expose Prometheus Metrics
description: Expose PromptKit pipeline, provider, tool and audio metrics to Prometheus and chart them in Grafana
verified:
  commit: 8489bf3d95c7498f9de0f024769db9399c24e884
  sources:
    - runtime/events/types.go
    - runtime/metrics/collector.go
    - runtime/metrics/eventbus_health.go
    - runtime/metrics/grafana/pipeline-dashboard.json
    - runtime/pipeline/stage/stages_pacing.go
    - runtime/providers/stream_metrics.go
    - sdk/evaluate.go
    - sdk/options.go
    - sdk/sdk.go
---

Expose pipeline, provider and cost metrics from your application to Prometheus.

## Prerequisites

- An application that opens conversations with `sdk.Open`
- `github.com/prometheus/client_golang`
- A Prometheus server that can reach your application

## 1. Create a collector and expose it

Create one collector per process, attach it to each conversation with `sdk.WithMetrics`, and serve the registry over HTTP:

```go
reg := prometheus.NewRegistry()
collector := metrics.NewCollector(metrics.CollectorOpts{
    Registerer:  reg,
    Namespace:   "myapp",
    ConstLabels: prometheus.Labels{"env": "prod"},
})

conv, err := sdk.Open("./app.pack.json", "chat", sdk.WithMetrics(collector, nil))
if err != nil {
    log.Fatal(err)
}
defer conv.Close()

http.Handle("/metrics", promhttp.HandlerFor(reg, promhttp.HandlerOpts{}))
log.Fatal(http.ListenAndServe(":9090", nil))
```

`Namespace` prefixes every metric name and defaults to `promptkit`. `Registerer` defaults to `prometheus.DefaultRegisterer`. In the rest of this page, `{ns}` stands for the namespace.

## 2. Label metrics per tenant

To share one endpoint between tenants or prompts, declare the label names on the collector and supply their values for each conversation:

```go
collector := metrics.NewCollector(metrics.CollectorOpts{
    Registerer:     reg,
    Namespace:      "myapp",
    InstanceLabels: []string{"tenant", "prompt_name"},
})

conv, err := sdk.Open(pack, "support", sdk.WithMetrics(collector, map[string]string{
    "tenant": "acme", "prompt_name": "support",
}))
```

The keys of the map must match `InstanceLabels`. Their order does not matter.

## 3. Watch event-bus drops (voice pipelines)

`NewCollector` registers the audio health counters. Register the event-bus counter yourself, against the bus your conversations publish on:

```go
reg.MustRegister(metrics.NewEventBusHealthCollector(bus, "myapp", nil))
```

The audio counters and the event-bus counter are updated at their source, so they stay accurate when the bus drops events under load. The pipeline, provider, tool and validation metrics are derived from events and under-report while the bus drops. A rising `{ns}_eventbus_events_dropped_total` tells you to distrust them.

## 4. Record eval metrics without a pipeline

A worker that only calls `sdk.Evaluate` needs the eval metrics, not the pipeline metrics. Use `NewEvalOnlyCollector` and pass it in `EvaluateOpts`:

```go
collector := metrics.NewEvalOnlyCollector(metrics.CollectorOpts{
    Registerer:     reg,
    Namespace:      "myapp",
    InstanceLabels: []string{"tenant"},
})

results, err := sdk.Evaluate(ctx, sdk.EvaluateOpts{
    PackPath:              "./app.pack.json",
    Messages:              messages,
    MetricsCollector:      collector,
    MetricsInstanceLabels: map[string]string{"tenant": "acme"},
})
```

Pack-defined eval metrics are named `{ns}_eval_<metric>`. A metric that already starts with `{ns}_eval_` keeps its name. Query them all with `{__name__=~"myapp_eval_.*"}`. See the [eval framework](https://promptarena.altairalabs.ai/arena/explanation/eval-framework/#metrics--prometheus) for metric types.

An eval metric of type `histogram` gets buckets at 0, 0.1, 0.2 and so on up to 1.0, the range eval scores live in. A metric that declares both `range.min` and `range.max` gets ten equal-width buckets across that range instead. To change the buckets for every histogram that declares no range, set `EvalHistogramBuckets`:

```go
collector := metrics.NewEvalOnlyCollector(metrics.CollectorOpts{
    Registerer:           reg,
    Namespace:            "myapp",
    EvalHistogramBuckets: []float64{0.25, 0.5, 0.75, 0.9, 1},
})
```

## 5. Configure the scrape

Add the endpoint to `prometheus.yml`:

```yaml
scrape_configs:
  - job_name: 'promptkit'
    static_configs:
      - targets: ['localhost:9090']
    scrape_interval: 15s
```

## 6. Import the Grafana dashboard

1. In Grafana, open Dashboards, then Import.
2. Upload `runtime/metrics/grafana/pipeline-dashboard.json`.
3. Select your Prometheus data source and click Import.

The dashboard charts pipeline throughput, errors and duration, provider latency, tokens and cost, and tool and validation results. Its queries use the default `promptkit` namespace.

## 7. Add alerts

```yaml
groups:
  - name: promptkit
    rules:
      - alert: HighPipelineErrorRate
        expr: |
          sum(rate(promptkit_pipeline_duration_seconds_count{status="error"}[5m]))
          /
          sum(rate(promptkit_pipeline_duration_seconds_count[5m]))
          > 0.1
        for: 5m
        labels:
          severity: warning
        annotations:
          summary: "High pipeline error rate"

      - alert: HighProviderLatency
        expr: |
          histogram_quantile(0.95,
            sum(rate(promptkit_provider_request_duration_seconds_bucket[5m]))
            by (provider, model, le)
          ) > 30
        for: 5m
        labels:
          severity: warning
        annotations:
          summary: "p95 provider latency exceeds 30 seconds"

      - alert: HighTokenConsumption
        expr: |
          (
            sum(increase(promptkit_provider_input_tokens_total[1h]))
            + sum(increase(promptkit_provider_output_tokens_total[1h]))
          ) > 1000000
        for: 1m
        labels:
          severity: info
        annotations:
          summary: "Over 1M tokens consumed in the last hour"
```

## Verify

Fetch the endpoint after a conversation sends a message:

```bash
curl -s localhost:9090/metrics | grep myapp_provider_requests_total
```

The output has a `myapp_provider_requests_total` series per provider, model, source and status.

## Metrics

All names carry the `{ns}_` prefix. Const labels and instance labels appear on every series.

| Metric | Type | Labels | Description |
|--------|------|--------|-------------|
| `pipeline_duration_seconds` | Histogram | `status` (`success`, `error`) | Duration of each pipeline execution |
| `provider_request_duration_seconds` | Histogram | `provider`, `model`, `source` | Duration of each provider API call |
| `provider_requests_total` | Counter | `provider`, `model`, `source`, `status` (`success`, `error`) | Provider API calls |
| `provider_input_tokens_total`, `provider_output_tokens_total`, `provider_cached_tokens_total` | Counter | `provider`, `model`, `source` | Input, output and cached tokens in provider calls |
| `provider_cost_total` | Counter, USD | `provider`, `model`, `source` | Cost of provider calls |
| `tool_call_duration_seconds` | Histogram | `tool` | Duration of each tool call |
| `tool_calls_total` | Counter | `tool`, `status` (`error` when the call failed, otherwise `success`) | Tool calls |
| `validation_duration_seconds` | Histogram | `validator`, `validator_type` | Duration of each validation check |
| `validations_total` | Counter | `validator`, `validator_type`, `status` (`passed`, `failed`) | Validation checks |
| `audio_frame_underruns_total` | Counter | `direction` | Pulls from the audio buffer that were short-filled with silence because it was starved |
| `audio_frame_underrun_samples_total` | Counter | `direction` | Silence samples substituted on underrun |
| `audio_frame_drops_total` | Counter | `direction`, `reason` (`overflow`) | Audio samples dropped because the producer exceeded buffer capacity |
| `audio_pacing_behind_deadline_total` | Counter | `direction` | Times audio pacing was already past a chunk's playback deadline when it forwarded it |
| `eventbus_events_dropped_total` | Counter | none | Events dropped because the bus buffer or a listener queue was full, counted once per listener |

`source` is `agent` for pipeline calls, `judge` for LLM judge evals and `selfplay` for self-play generation. `direction` is `input` or `output`. The collector also records `tts_`, `stt_`, `embedding_`, `image_gen_` and `inference_` families, each with `request_duration_seconds`, `requests_total` and `cost_total` series and the same `provider`, `model` and `source` labels.

## Related

- [Metrics reference](/runtime/reference/metrics/): the `metrics` package API
- [Monitor events](/sdk/how-to/observability/monitor-events/)
- [Run evals](/sdk/how-to/observability/run-evals/)
