---
title: Eval Framework
---

Understanding PromptKit's automated evaluation system for LLM outputs.

## Overview

Evals are automated quality checks that run against LLM outputs. They answer questions like "Did the assistant stay on topic?", "Was the JSON valid?", or "Did it call the right tools?". Evals are defined in pack files and execute automatically during conversations or against recorded sessions.

Evals use the same [check types](/reference/checks/) as assertions and guardrails. The difference is *when* and *where* they run: evals can fire in production on every turn, on a sampled subset, or at session close, whereas assertions only run during Arena tests and guardrails run inline before the response is delivered.

```
Pack File (evals) ──► EvalRunner ──► ResultWriter ──► Metrics / Metadata
```

:::note
For the complete list of check types available as evals, see the [Checks Reference](/reference/checks/).
:::

## Pack Evals vs Scenario Assertions

PromptKit offers two complementary evaluation mechanisms that share the same underlying [check types](/reference/checks/):

| | Pack Evals | Scenario Assertions |
|---|---|---|
| **Defined in** | Pack file (`evals` array) | Arena scenario YAML |
| **Scope** | Any conversation using the pack | Specific test scenarios |
| **When** | Production + testing | Testing only |
| **Check types** | Any check from the [unified catalog](/reference/checks/) | Any check from the [unified catalog](/reference/checks/) |
| **Trigger** | Configurable (every turn, sampling, session close) | Every turn / conversation end |

**Pack evals** travel with your pack — they run in production, in Arena tests, and anywhere the pack is used. Think of them as built-in quality monitors.

**Scenario assertions** are Arena-specific test expectations. They validate specific conversation flows defined in your test scenarios.

Both can coexist: pack evals provide baseline quality monitoring while scenario assertions verify specific behaviors. See [Unified Check Model](/concepts/validation/) for how evals, assertions, and guardrails relate.

## Eval Definition Structure

Each eval is an `EvalDef` object in the pack's `evals` array. The structure combines a check type with trigger, sampling, threshold, and metric configuration:

```json
{
  "id": "quality_check",
  "type": "contains",
  "trigger": "every_turn",
  "params": { "patterns": ["thank you"] },
  "threshold": { "min_score": 0.8 },
  "enabled": true,
  "sample_percentage": 10,
  "metric": {
    "name": "response_quality",
    "type": "gauge",
    "labels": { "category": "tone" }
  }
}
```

| Field | Required | Description |
|-------|----------|-------------|
| `id` | Yes | Unique identifier for the eval within the pack |
| `type` | Yes | Check type from the [Checks Reference](/reference/checks/) |
| `trigger` | Yes | When the eval fires (see [Triggers](#triggers)) |
| `params` | Varies | Parameters specific to the check type |
| `threshold` | No | Pass/fail threshold (e.g. `min_score`) |
| `enabled` | No | Whether the eval is active (default: `true`) |
| `sample_percentage` | No | Percentage of turns/sessions to evaluate (for sampling triggers) |
| `groups` | No | Eval groups for filtering (see [Eval Groups](#eval-groups)) |
| `metric` | No | Prometheus metric configuration (see [MetricCollector](#metriccollector--prometheus)) |

## Triggers

Each eval specifies when it should fire:

| Trigger | Description | Use Case |
|---------|-------------|----------|
| `every_turn` | After each assistant response | Real-time quality checks |
| `on_session_complete` | When session closes | Summary evaluations |
| `sample_turns` | Percentage of turns (hash-based) | Production sampling |
| `sample_sessions` | Percentage of sessions (hash-based) | Production sampling |
| `on_conversation_complete` | When multi-session conversation closes | Final evaluation |
| `on_workflow_step` | After a workflow state transition | Workflow validation |

Sampling is **deterministic** — the same session ID and turn index always produce the same sampling decision (FNV-1a hash). This ensures reproducible behavior across runs.

```json
{
  "id": "toxicity_check",
  "type": "contains",
  "trigger": "sample_turns",
  "sample_percentage": 10,
  "params": {
    "patterns": ["harmful", "offensive"]
  }
}
```

## Eval Groups

Evals can belong to one or more groups, enabling selective execution. When no explicit groups are set, evals are automatically classified based on their handler type:

| Group | Value | Assigned To |
|-------|-------|-------------|
| Default | `default` | All evals with no explicit groups |
| Fast-running | `fast-running` | Deterministic checks: `contains`, `regex`, `json_valid`, `tools_called`, workflow checks, etc. |
| Long-running | `long-running` | Compute/network-intensive: `llm_judge`, `cosine_similarity`, `outcome_equivalent`, `a2a_eval`, `rest_eval`, exec handlers |
| External | `external` | External system calls: `llm_judge`, `a2a_eval`, `rest_eval`, exec handlers |

### Automatic classification

Evals with no explicit `groups` field receive `default` plus one or more well-known groups. For example, a `contains` eval gets `["default", "fast-running"]`, while an `llm_judge` eval gets `["default", "long-running", "external"]`.

### Explicit groups

Setting `groups` on an eval definition overrides the automatic classification entirely:

```json
{
  "id": "compliance_check",
  "type": "llm_judge",
  "trigger": "every_turn",
  "groups": ["compliance", "safety"],
  "params": { "criteria": "Check regulatory compliance" }
}
```

This eval will only match when filtering for `compliance` or `safety` — it will no longer match `default`, `long-running`, or `external`.

### Filtering by group

In the SDK, use `EvalGroups` to select which groups to run:

```go
// Only run fast evals in the hot path
results, _ := sdk.Evaluate(ctx, sdk.EvaluateOpts{
    PackPath:   "./app.pack.json",
    Messages:   messages,
    EvalGroups: []string{"fast-running"},
})
```

When `EvalGroups` is nil or empty, all evals run regardless of group.

## Dispatch Patterns

The eval system supports three dispatch patterns for different deployment scenarios:

### Pattern A: InProcDispatcher

Runs evals synchronously in the same process. Used by Arena and simple SDK deployments.

```
Conversation ──► InProcDispatcher ──► EvalRunner ──► Handlers ──► ResultWriter
```

### Pattern B: EventDispatcher

Publishes eval requests to an event bus for async processing by workers. Used in production SDK deployments.

```
Conversation ──► EventDispatcher ──► Event Bus ──► EvalWorker ──► EvalRunner ──► ResultWriter
```

### Pattern C: EventBusEvalListener

Subscribes to EventBus `message.created` events and triggers evals automatically. No explicit middleware needed.

```
RecordingStage ──► EventBus ──► EventBusEvalListener ──► SessionAccumulator ──► Dispatcher ──► Runner
```

The `EventBusEvalListener` uses a `SessionAccumulator` that accumulates messages per session and builds `EvalContext` on demand. Sessions expire after a configurable TTL (default: 30 minutes).

## Eval Executor (Arena)

The `EvalConversationExecutor` evaluates **saved conversations** from recordings:

1. Load recording via adapter registry
2. Build conversation context from recorded messages
3. Apply turn-level assertions to each assistant message
4. Evaluate conversation-level assertions
5. Run pack session evals (if configured)
6. Return aggregated results

This enables offline evaluation of historical conversations without re-running them against a live LLM.

## MetricCollector & Prometheus

The `MetricCollector` records eval results and exports them in Prometheus text format. It supports three label sources that are merged at record time:

1. **Pack-author labels** — declared per-metric in the pack file (e.g. `eval_type`, `category`)
2. **Platform base labels** — injected at collector creation via `WithLabels` (e.g. `env`, `tenant_id`)
3. **Dynamic context labels** — `session_id` and `turn_index` injected automatically by `MetricResultWriter`

```go
// Platform injects deployment-level labels at collector creation
collector := evals.NewMetricCollector(
    evals.WithLabels(map[string]string{
        "env":    "prod",
        "tenant": "acme",
    }),
)
writer := evals.NewMetricResultWriter(collector, pack.Evals)

// After evals run, export metrics
collector.WritePrometheus(os.Stdout)
```

Output:

```
# TYPE promptpack_response_quality gauge
promptpack_response_quality{category="tone",env="prod",eval_type="llm_judge",session_id="abc-123",tenant="acme",turn_index="1"} 0.85
# TYPE promptpack_json_valid gauge
promptpack_json_valid{category="format",env="prod",eval_type="json_valid",session_id="abc-123",tenant="acme",turn_index="1"} 1
```

The same metric name with different label sets produces separate time series, with a single deduplicated `# TYPE` comment line.

### Metric Types

| Type | Behavior |
|------|----------|
| `gauge` | Set to the eval's score value |
| `counter` | Increment count on each execution |
| `histogram` | Observe value with configurable buckets, track sum/count |
| `boolean` | 1.0 if passed, 0.0 if failed |

### Collector Options

| Option | Description |
|--------|-------------|
| `WithNamespace(ns)` | Set metric name prefix (default: `"promptpack"`) |
| `WithBuckets(b)` | Set custom histogram bucket boundaries |
| `WithLabels(m)` | Set base labels merged into every recorded metric. Base labels take precedence over pack-author labels on conflict. |

### Label Sources

**Pack-author labels** are declared in the `metric.labels` field of each eval definition. These describe per-metric dimensions controlled by the pack author:

```json
{
  "id": "response_quality",
  "type": "llm_judge",
  "trigger": "every_turn",
  "metric": {
    "name": "response_quality_score",
    "type": "histogram",
    "range": { "min": 0, "max": 1 },
    "labels": {
      "eval_type": "llm_judge",
      "category": "quality"
    }
  },
  "params": {
    "criteria": "Rate the quality of the response"
  }
}
```

**Platform base labels** are set via `WithLabels()` when creating the collector. These are deployment-level labels (e.g. `env`, `tenant_id`, `region`) that the hosting platform controls. Base labels win on conflict with pack-author labels.

**Dynamic context labels** (`session_id`, `turn_index`) are injected automatically by `MetricResultWriter` from the `EvalResult`. No configuration needed — every metric gets these labels when they are available.

Label names must match Prometheus naming rules (`^[a-zA-Z_][a-zA-Z0-9_]*$`) and must not start with `__` (reserved by Prometheus). Invalid label names are caught during pack validation.

## Pack Eval Resolution

When both pack-level and prompt-level evals are defined, they are **merged**:

1. Prompt evals override pack evals where IDs match
2. Pack-only evals are preserved
3. Prompt-only evals are appended

This allows packs to define baseline evals while individual prompts customize or extend them.

## Example

See the [`eval-test` example](https://github.com/AltairaLabs/PromptKit/tree/main/examples/eval-test) for a working Arena configuration that evaluates saved conversations with both deterministic assertions and LLM judge evals.

## See Also

- [Checks Reference](/reference/checks/) — All check types and parameters
- [Unified Check Model](/concepts/validation/) — How evals, assertions, and guardrails relate
- [Run Evals](/sdk/how-to/run-evals/) — Programmatic eval execution via SDK
- [Assertions Reference](/arena/reference/assertions/) — Test-time checks
- [Observability](/sdk/explanation/observability/) — EventBus architecture
- [Session Recording](/arena/explanation/session-recording/) — How recordings feed into evals
- [Monitor Events](/sdk/how-to/monitor-events/) — MetricCollector usage in SDK
