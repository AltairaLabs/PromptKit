---
title: Prometheus Metrics
description: Monitor PromptKit pipelines with Prometheus and Grafana
---

PromptKit provides built-in Prometheus metrics for monitoring pipeline performance, LLM provider usage, and costs in production environments.

## Quick Start

```go
package main

import (
    "context"
    "log"
    "time"

    "github.com/AltairaLabs/PromptKit/runtime/events"
    "github.com/AltairaLabs/PromptKit/runtime/metrics/prometheus"
)

func main() {
    // Create and start the Prometheus exporter
    exporter := prometheus.NewExporter(":9090")
    go func() {
        if err := exporter.Start(); err != nil {
            log.Printf("Prometheus exporter stopped: %v", err)
        }
    }()
    defer exporter.Shutdown(context.Background())

    // Create event bus and register metrics listener
    eventBus := events.NewEventBus()
    metricsListener := prometheus.NewMetricsListener()
    eventBus.SubscribeAll(metricsListener.Listener())

    // Your pipeline code here...
    // Metrics are now available at http://localhost:9090/metrics
}
```

## Available Metrics

### Pipeline Metrics

| Metric | Type | Labels | Description |
|--------|------|--------|-------------|
| `promptkit_pipelines_active` | Gauge | - | Number of currently active pipelines |
| `promptkit_pipeline_duration_seconds` | Histogram | `status` | Total pipeline execution duration |

### Stage Metrics

| Metric | Type | Labels | Description |
|--------|------|--------|-------------|
| `promptkit_stage_duration_seconds` | Histogram | `stage`, `stage_type` | Per-stage processing duration |
| `promptkit_stage_elements_total` | Counter | `stage`, `status` | Elements processed by stage |

### Provider Metrics

| Metric | Type | Labels | Description |
|--------|------|--------|-------------|
| `promptkit_provider_request_duration_seconds` | Histogram | `provider`, `model` | LLM API call duration |
| `promptkit_provider_requests_total` | Counter | `provider`, `model`, `status` | Total provider API calls |
| `promptkit_provider_tokens_total` | Counter | `provider`, `model`, `type` | Token consumption (input/output/cached) |
| `promptkit_provider_cost_total` | Counter | `provider`, `model` | Total cost in USD |

### Tool Metrics

| Metric | Type | Labels | Description |
|--------|------|--------|-------------|
| `promptkit_tool_call_duration_seconds` | Histogram | `tool` | Tool call execution duration |
| `promptkit_tool_calls_total` | Counter | `tool`, `status` | Total tool call count |

### Validation Metrics

| Metric | Type | Labels | Description |
|--------|------|--------|-------------|
| `promptkit_validation_duration_seconds` | Histogram | `validator`, `validator_type` | Validation check duration |
| `promptkit_validations_total` | Counter | `validator`, `validator_type`, `status` | Validation results (passed/failed) |

## Pipeline Configuration

Enable Prometheus metrics export via pipeline configuration:

```go
import "github.com/AltairaLabs/PromptKit/runtime/pipeline/stage"

config := stage.DefaultPipelineConfig().
    WithMetrics(true).
    WithPrometheusExporter(":9090")
```

## Integration with Existing HTTP Server

If you already have an HTTP server, use the `Handler()` method:

```go
import (
    "net/http"
    "github.com/AltairaLabs/PromptKit/runtime/metrics/prometheus"
)

func main() {
    exporter := prometheus.NewExporter(":9090")

    // Add to your existing mux
    http.Handle("/metrics", exporter.Handler())
    http.ListenAndServe(":8080", nil)
}
```

## Grafana Dashboard

PromptKit includes a pre-built Grafana dashboard at `runtime/metrics/grafana/pipeline-dashboard.json`.

### Import Dashboard

1. Open Grafana and navigate to **Dashboards > Import**
2. Upload the `pipeline-dashboard.json` file or paste its contents
3. Select your Prometheus data source
4. Click **Import**

### Dashboard Panels

The dashboard includes:

- **Pipeline Overview**: Active pipelines, completion rate, error rate, p95 duration, total cost, total tokens
- **Stage Performance**: Per-stage latency heatmap, throughput by status
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
          sum(increase(promptkit_provider_tokens_total[1h])) > 1000000
        for: 1m
        labels:
          severity: info
        annotations:
          summary: "High token consumption"
          description: "Over 1M tokens consumed in the last hour"
```

## Custom Metrics

Register additional collectors with the exporter:

```go
import (
    "github.com/prometheus/client_golang/prometheus"
    pkprometheus "github.com/AltairaLabs/PromptKit/runtime/metrics/prometheus"
)

customCounter := prometheus.NewCounter(prometheus.CounterOpts{
    Name: "my_custom_metric",
    Help: "Custom application metric",
})

exporter := pkprometheus.NewExporter(":9090")
exporter.MustRegister(customCounter)
```

## Recording Functions

For manual metric recording (outside of event-based collection):

```go
import "github.com/AltairaLabs/PromptKit/runtime/metrics/prometheus"

// Record stage metrics
prometheus.RecordStageDuration("my_stage", "transform", 0.5)
prometheus.RecordStageElement("my_stage", "success")

// Record provider metrics
prometheus.RecordProviderRequest("anthropic", "claude-3", "success", 2.5)
prometheus.RecordProviderTokens("anthropic", "claude-3", 100, 50, 0)
prometheus.RecordProviderCost("anthropic", "claude-3", 0.05)

// Record tool metrics
prometheus.RecordToolCall("web_search", "success", 1.2)

// Record validation metrics
prometheus.RecordValidation("schema_validator", "output", "passed", 0.01)
```

## Health Endpoint

The exporter also serves a health endpoint at `/health`:

```bash
curl http://localhost:9090/health
# Returns: ok
```

## Graceful Shutdown

Always shut down the exporter gracefully:

```go
import (
    "context"
    "os"
    "os/signal"
    "syscall"
    "time"
)

ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
defer cancel()

if err := exporter.Shutdown(ctx); err != nil {
    log.Printf("Error shutting down exporter: %v", err)
}
```
