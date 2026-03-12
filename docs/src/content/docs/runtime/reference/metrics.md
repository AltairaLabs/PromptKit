---
title: Metrics Reference
sidebar:
  order: 9
---

Complete reference for all Prometheus metrics emitted by PromptKit's unified `metrics.Collector`.

## Overview

PromptKit emits two categories of metrics through a single `metrics.Collector`:

- **Pipeline metrics** — operational metrics recorded automatically from EventBus events (provider calls, tool calls, pipeline duration, validation checks)
- **Eval metrics** — quality metrics recorded from pack-defined `EvalDef.Metric` definitions

All metrics share a common label structure:

```
{namespace}_{metric_name}{const_labels, instance_labels, event_labels}
```

Where:
- **Namespace** — configurable prefix (default: `promptkit`)
- **Const labels** — process-level labels baked into the metric descriptor (`env`, `region`)
- **Instance labels** — per-conversation labels bound via `Bind()` (`tenant`, `prompt_name`)
- **Event labels** — per-observation labels specific to each metric (listed in the tables below)

## Pipeline Metrics

These are registered at Collector creation time and recorded automatically when `MetricContext.OnEvent` is wired to the EventBus. Disable with `DisablePipelineMetrics: true` or use `NewEvalOnlyCollector()`.

### Pipeline Duration

| Metric | Type | Event Labels | Description |
|--------|------|--------------|-------------|
| `{ns}_pipeline_duration_seconds` | Histogram | `status` | Total pipeline execution duration |

`status` values: `success`, `error`

**Buckets:** 0.1, 0.25, 0.5, 1, 2.5, 5, 10, 30, 60, 120 seconds

**Source events:** `pipeline.completed`, `pipeline.failed`

### Provider Metrics

| Metric | Type | Event Labels | Description |
|--------|------|--------------|-------------|
| `{ns}_provider_request_duration_seconds` | Histogram | `provider`, `model` | LLM API call duration |
| `{ns}_provider_requests_total` | Counter | `provider`, `model`, `status` | Total provider API calls |
| `{ns}_provider_input_tokens_total` | Counter | `provider`, `model` | Input tokens sent to provider |
| `{ns}_provider_output_tokens_total` | Counter | `provider`, `model` | Output tokens received from provider |
| `{ns}_provider_cached_tokens_total` | Counter | `provider`, `model` | Cached tokens in provider calls |
| `{ns}_provider_cost_total` | Counter | `provider`, `model` | Total cost in USD |

`status` values: `success`, `error`

**Duration buckets:** 0.1, 0.25, 0.5, 1, 2.5, 5, 10, 30, 60 seconds

**Source events:** `provider.call.completed`, `provider.call.failed`

**Notes:**
- Token metrics are only incremented when the count is > 0
- Cost is only incremented when > 0
- Duration is recorded on both success and failure

### Tool Call Metrics

| Metric | Type | Event Labels | Description |
|--------|------|--------------|-------------|
| `{ns}_tool_call_duration_seconds` | Histogram | `tool` | Tool call execution duration |
| `{ns}_tool_calls_total` | Counter | `tool`, `status` | Total tool call count |

`status` values: `success`, `error`

**Buckets:** 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10 seconds

**Source events:** `tool.call.completed`, `tool.call.failed`

### Validation Metrics

| Metric | Type | Event Labels | Description |
|--------|------|--------------|-------------|
| `{ns}_validation_duration_seconds` | Histogram | `validator`, `validator_type` | Validation check duration |
| `{ns}_validations_total` | Counter | `validator`, `validator_type`, `status` | Validation results |

`status` values: `passed`, `failed`

**Buckets:** 0.001, 0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1 seconds

**Source events:** `validation.passed`, `validation.failed`

## Eval Metrics

Eval metrics are defined per-eval in the pack file's `metric` field. They are registered dynamically on first observation (with double-checked locking for thread safety). Disable with `DisableEvalMetrics: true`.

### MetricDef Structure

```json
{
  "id": "response_quality",
  "type": "llm_judge",
  "trigger": "every_turn",
  "metric": {
    "name": "response_quality_score",
    "type": "gauge",
    "labels": {
      "eval_type": "llm_judge",
      "category": "quality"
    }
  }
}
```

| Field | Required | Description |
|-------|----------|-------------|
| `name` | Yes | Prometheus metric name (auto-prefixed with namespace if not already) |
| `type` | Yes | One of `gauge`, `counter`, `histogram`, `boolean` |
| `range` | No | Value range hint (`min`, `max`) — used for documentation, not enforced |
| `labels` | No | Static labels added to this metric (pack-author defined) |

### Eval Metric Types

| Type | Prometheus Type | Behavior | Typical Use |
|------|----------------|----------|-------------|
| `gauge` | Gauge | Set to the eval's score value | Relevance scores, quality ratings |
| `counter` | Counter | Increment by 1 on each eval execution | Execution counts |
| `histogram` | Histogram | Observe the score value | Score distributions |
| `boolean` | Gauge | Set to 1.0 (pass) or 0.0 (fail) | Binary checks (JSON valid, contains keyword) |

**Histogram buckets:** Prometheus default buckets (0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10)

### Eval Metric Label Ordering

The full label set for an eval metric is: **instance labels** (sorted) + **pack-author labels** (sorted by key).

For example, with `InstanceLabels: ["tenant"]` and `metric.labels: {"category": "quality", "eval_type": "llm_judge"}`:

```
response_quality_score{tenant="acme",category="quality",eval_type="llm_judge"} 0.85
```

### Score Extraction

The score value recorded for `gauge` and `histogram` types is extracted as follows:

1. If `EvalResult.Score` is non-nil, use `*Score`
2. Otherwise, if `EvalResult.Passed` is true, use `1.0`; else `0.0`

## Label Architecture

### Label Levels

Labels come from three sources, applied in order:

| Level | Set At | Scope | Examples |
|-------|--------|-------|----------|
| **Const labels** | `CollectorOpts.ConstLabels` | Process-wide, baked into descriptor | `env`, `region`, `service_name` |
| **Instance labels** | `Bind(map)` / `MetricsInstanceLabels` | Per-conversation or per-invocation | `tenant`, `prompt_name` |
| **Event labels** | Per-observation (automatic) | Per-metric-observation | `provider`, `model`, `status`, `tool` |

### Instance Label Ordering

`InstanceLabels` are sorted alphabetically when the Collector is created. When calling `Bind()`, the map key order doesn't matter — values are looked up by key name, not position.

```go
// These produce identical results:
collector.Bind(map[string]string{"z_tenant": "acme", "a_prompt": "chat"})
collector.Bind(map[string]string{"a_prompt": "chat", "z_tenant": "acme"})
```

## API Quick Reference

### Conversation API (WithMetrics)

```go
reg := prometheus.NewRegistry()
collector := metrics.NewCollector(metrics.CollectorOpts{
    Registerer:     reg,
    Namespace:      "myapp",
    ConstLabels:    prometheus.Labels{"env": "prod"},
    InstanceLabels: []string{"tenant"},
})

conv, _ := sdk.Open("./app.pack.json", "chat",
    sdk.WithMetrics(collector, map[string]string{"tenant": "acme"}),
)
```

Records both pipeline and eval metrics automatically.

### Standalone Eval API (sdk.Evaluate)

```go
collector := metrics.NewEvalOnlyCollector(metrics.CollectorOpts{
    Registerer:     reg,
    Namespace:      "myapp",
    InstanceLabels: []string{"tenant"},
})

results, _ := sdk.Evaluate(ctx, sdk.EvaluateOpts{
    PackPath:              "./app.pack.json",
    Messages:              messages,
    MetricsCollector:      collector,
    MetricsInstanceLabels: map[string]string{"tenant": "acme"},
})
```

### CollectorOpts

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `Registerer` | `prometheus.Registerer` | `DefaultRegisterer` | Registry to register into |
| `Namespace` | `string` | `"promptkit"` | Metric name prefix |
| `ConstLabels` | `prometheus.Labels` | `nil` | Process-level constant labels |
| `InstanceLabels` | `[]string` | `nil` | Label names that vary per conversation (sorted internally) |
| `DisablePipelineMetrics` | `bool` | `false` | Skip pipeline metric registration |
| `DisableEvalMetrics` | `bool` | `false` | Skip eval metric recording |

### Constructors

| Function | Description |
|----------|-------------|
| `NewCollector(opts)` | Full collector — pipeline + eval metrics |
| `NewEvalOnlyCollector(opts)` | Eval metrics only (`DisablePipelineMetrics: true`) |

## Complete Metric Catalog

For quick reference, here is every metric name emitted with the default `promptkit` namespace:

| Metric Name | Type | Category |
|-------------|------|----------|
| `promptkit_pipeline_duration_seconds` | Histogram | Pipeline |
| `promptkit_provider_request_duration_seconds` | Histogram | Provider |
| `promptkit_provider_requests_total` | Counter | Provider |
| `promptkit_provider_input_tokens_total` | Counter | Provider |
| `promptkit_provider_output_tokens_total` | Counter | Provider |
| `promptkit_provider_cached_tokens_total` | Counter | Provider |
| `promptkit_provider_cost_total` | Counter | Provider |
| `promptkit_tool_call_duration_seconds` | Histogram | Tool |
| `promptkit_tool_calls_total` | Counter | Tool |
| `promptkit_validation_duration_seconds` | Histogram | Validation |
| `promptkit_validations_total` | Counter | Validation |
| `{ns}_{eval_metric_name}` | Varies | Eval (pack-defined) |

## See Also

- [Prometheus Metrics How-To](/runtime/how-to/prometheus-metrics/) — Setup guide with Grafana dashboard and alerts
- [Monitor Events](/sdk/how-to/monitor-events/) — EventBus hooks and metrics in the SDK
- [Run Evals](/sdk/how-to/run-evals/) — Standalone eval execution with metrics
- [Eval Framework](/arena/explanation/eval-framework/) — Eval architecture and metric definitions
- [Telemetry Reference](/runtime/reference/telemetry/) — OpenTelemetry tracing (complementary to metrics)
