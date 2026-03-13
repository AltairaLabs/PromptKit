---
title: Prometheus Metrics
description: Monitor PromptKit pipelines with Prometheus and Grafana
---

PromptKit provides built-in Prometheus metrics for monitoring pipeline performance, LLM provider usage, and costs in production environments.

## Quick Start

```go
package main

import (
    "net/http"

    "github.com/prometheus/client_golang/prometheus"
    "github.com/prometheus/client_golang/prometheus/promhttp"

    "github.com/AltairaLabs/PromptKit/runtime/metrics"
    "github.com/AltairaLabs/PromptKit/sdk"
)

func main() {
    // 1. Create a collector — registers pipeline metrics once per process
    reg := prometheus.NewRegistry()
    collector := metrics.NewCollector(metrics.CollectorOpts{
        Registerer:  reg,
        Namespace:   "myapp",
        ConstLabels: prometheus.Labels{"env": "prod"},
    })

    // 2. Attach to conversations via sdk.WithMetrics()
    conv, _ := sdk.Open("./app.pack.json", "chat",
        sdk.WithMetrics(collector, nil),
    )
    defer conv.Close()

    // 3. Expose via your own HTTP server
    http.Handle("/metrics", promhttp.HandlerFor(reg, promhttp.HandlerOpts{}))
    http.ListenAndServe(":9090", nil)
}
```

## Available Metrics

### Pipeline Metrics

| Metric | Type | Labels | Description |
|--------|------|--------|-------------|
| `{ns}_pipeline_duration_seconds` | Histogram | `status` | Total pipeline execution duration |
| `{ns}_provider_request_duration_seconds` | Histogram | `provider`, `model` | LLM API call duration |
| `{ns}_provider_requests_total` | Counter | `provider`, `model`, `status` | Total provider API calls |
| `{ns}_provider_input_tokens_total` | Counter | `provider`, `model` | Input tokens sent to provider |
| `{ns}_provider_output_tokens_total` | Counter | `provider`, `model` | Output tokens received from provider |
| `{ns}_provider_cached_tokens_total` | Counter | `provider`, `model` | Cached tokens in provider calls |
| `{ns}_provider_cost_total` | Counter | `provider`, `model` | Total cost in USD |
| `{ns}_tool_call_duration_seconds` | Histogram | `tool` | Tool call execution duration |
| `{ns}_tool_calls_total` | Counter | `tool`, `status` | Total tool call count |
| `{ns}_validation_duration_seconds` | Histogram | `validator`, `validator_type` | Validation check duration |
| `{ns}_validations_total` | Counter | `validator`, `validator_type`, `status` | Validation results (passed/failed) |

Where `{ns}` is the configured namespace (default: `promptkit`).

### Eval Metrics

Pack-defined eval metrics (from `EvalDef.Metric`) are also recorded through the same collector under the `{ns}_eval_` sub-namespace. For example, a metric named `response_quality_score` with namespace `myapp` becomes `myapp_eval_response_quality_score`. This separates eval metrics from pipeline metrics, making it easy to query all evals with a pattern like `myapp_eval_.*`. See [Eval Framework](/arena/explanation/eval-framework/#metrics--prometheus) for metric types and label configuration.

## Standalone Eval Metrics

For eval-only consumers (e.g. workers using `sdk.Evaluate()` without a live pipeline), use `NewEvalOnlyCollector` and pass it via `MetricsCollector` on `EvaluateOpts`:

```go
reg := prometheus.NewRegistry()
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

`NewEvalOnlyCollector` is equivalent to `NewCollector` with `DisablePipelineMetrics: true` — it skips registration of provider, tool, pipeline, and validation metrics.

## Multi-Tenant Setup

When multiple conversations share one Prometheus endpoint, use instance labels to distinguish them:

```go
collector := metrics.NewCollector(metrics.CollectorOpts{
    Registerer:     reg,
    Namespace:      "myapp",
    InstanceLabels: []string{"tenant", "prompt_name"},
})

conv1, _ := sdk.Open(pack, "support", sdk.WithMetrics(collector, map[string]string{
    "tenant": "acme", "prompt_name": "support",
}))
conv2, _ := sdk.Open(pack, "sales", sdk.WithMetrics(collector, map[string]string{
    "tenant": "globex", "prompt_name": "sales",
}))
```

## CollectorOpts Reference

| Field | Type | Description |
|-------|------|-------------|
| `Registerer` | `prometheus.Registerer` | Registry to register into (default: `DefaultRegisterer`) |
| `Namespace` | `string` | Metric name prefix (default: `"promptkit"`) |
| `ConstLabels` | `prometheus.Labels` | Process-level constant labels (env, region) |
| `InstanceLabels` | `[]string` | Label names that vary per conversation (tenant, prompt_name). Sorted internally — `Bind()` label order doesn't matter. |
| `DisablePipelineMetrics` | `bool` | Disable operational metrics (use for eval-only consumers, or use `NewEvalOnlyCollector`) |
| `DisableEvalMetrics` | `bool` | Disable eval result metrics |

## Grafana Dashboard

PromptKit includes a pre-built Grafana dashboard at `runtime/metrics/grafana/pipeline-dashboard.json`.

### Import Dashboard

1. Open Grafana and navigate to **Dashboards > Import**
2. Upload the `pipeline-dashboard.json` file or paste its contents
3. Select your Prometheus data source
4. Click **Import**

### Dashboard Panels

The dashboard includes:

- **Pipeline Overview**: Completion rate, error rate, p95 duration, total cost, total tokens
- **Provider Metrics**: API latency percentiles, request rate, token consumption, cost breakdown
- **Tool & Validation**: Tool call duration, validation pass/fail rates

## Prometheus Scrape Configuration

Add to your `prometheus.yml`:

```yaml
scrape_configs:
  - job_name: 'promptkit'
    static_configs:
      - targets: ['localhost:9090']
    scrape_interval: 15s
```

## Example Alerts

Create alerts in Prometheus Alertmanager:

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
          description: "More than 10% of pipelines are failing"

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
          summary: "High provider latency"
          description: "p95 provider latency exceeds 30 seconds"

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
          summary: "High token consumption"
          description: "Over 1M tokens consumed in the last hour"
```

## See Also

- [Metrics Reference](/runtime/reference/metrics/) — Complete catalog of all emitted metrics, label architecture, and API reference
- [Monitor Events](/sdk/how-to/monitor-events/) — Event-based observability and metrics setup
- [Eval Framework](/arena/explanation/eval-framework/) — Eval architecture and metric types
- [Run Evals](/sdk/how-to/run-evals/) — Programmatic eval execution via SDK
