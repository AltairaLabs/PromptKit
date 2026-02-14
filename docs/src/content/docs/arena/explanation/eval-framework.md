---
title: Eval Framework
sidebar:
  order: 7
---
Understanding PromptKit's automated evaluation system for LLM outputs.

## Overview

Evals are automated quality checks that run against LLM outputs. They answer questions like "Did the assistant stay on topic?", "Was the JSON valid?", or "Did it call the right tools?". Evals are defined in pack files and execute automatically during conversations or against recorded sessions.

```
Pack File (evals) ──► EvalRunner ──► ResultWriter ──► Metrics / Metadata
```

## Pack Evals vs Scenario Assertions

PromptKit offers two complementary evaluation mechanisms:

| | Pack Evals | Scenario Assertions |
|---|---|---|
| **Defined in** | Pack file (`evals` array) | Arena scenario YAML |
| **Scope** | Any conversation using the pack | Specific test scenarios |
| **When** | Production + testing | Testing only |
| **Types** | Deterministic + LLM judge | Content matching + LLM judge |
| **Trigger** | Configurable (every turn, sampling) | Every turn / conversation end |

**Pack evals** travel with your pack — they run in production, in Arena tests, and anywhere the pack is used. Think of them as built-in quality monitors.

**Scenario assertions** are Arena-specific test expectations. They validate specific conversation flows defined in your test scenarios.

Both can coexist: pack evals provide baseline quality monitoring while scenario assertions verify specific behaviors.

## Eval Types

### Deterministic (Turn-Level)

These run against individual assistant responses:

| Type | Description | Key Params |
|------|-------------|------------|
| `contains` | Check output contains patterns | `patterns` (string array) |
| `regex` | Regex pattern match | `pattern` (string) |
| `json_valid` | Output is valid JSON | — |
| `json_schema` | Output matches JSON schema | `schema` (object) |
| `tools_called` | Specific tools were invoked | `tools` (string array) |
| `tools_not_called` | Specific tools were NOT invoked | `tools` (string array) |
| `tool_args` | Tool arguments match expectations | `tool`, `args` (object) |
| `latency_budget` | Response within time limit | `max_ms` (int) |
| `cosine_similarity` | Semantic similarity to reference | `reference`, `threshold` |

### Deterministic (Session-Level)

These run against the full conversation:

| Type | Description | Key Params |
|------|-------------|------------|
| `contains_any` | Any pattern appears across all turns | `patterns` (string array) |
| `content_excludes` | No forbidden content in any turn | `patterns` (string array) |
| `tools_called_session` | Tools called anywhere in session | `tools` (string array) |
| `tools_not_called_session` | Tools never called in session | `tools` (string array) |
| `tool_args_session` | Tool args match across session | `tool`, `args` (object) |
| `tool_args_excluded_session` | Forbidden tool args absent | `tool`, `args` (object) |

### LLM Judge

Use an LLM to evaluate quality when deterministic checks aren't sufficient:

| Type | Description | Key Params |
|------|-------------|------------|
| `llm_judge` | LLM evaluates a single turn | `criteria` (string) |
| `llm_judge_session` | LLM evaluates full session | `criteria` (string) |

LLM judge evals require a judge provider configured in the eval context metadata.

## Triggers

Each eval specifies when it should fire:

| Trigger | Description | Use Case |
|---------|-------------|----------|
| `every_turn` | After each assistant response | Real-time quality checks |
| `on_session_complete` | When session closes | Summary evaluations |
| `sample_turns` | Percentage of turns (hash-based) | Production sampling |
| `sample_sessions` | Percentage of sessions (hash-based) | Production sampling |

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

The `MetricCollector` records eval results and exports them in Prometheus text format:

```go
collector := evals.NewMetricCollector()
writer := evals.NewMetricResultWriter(collector)

// After evals run, export metrics
collector.WritePrometheus(os.Stdout)
```

Output:

```
# TYPE promptpack_response_relevance_score gauge
promptpack_response_relevance_score 0.85
# TYPE promptpack_json_valid_pass_rate boolean
promptpack_json_valid_pass_rate 1
```

### Metric Types

| Type | Behavior |
|------|----------|
| `gauge` | Set to the eval's score value |
| `counter` | Increment count on each execution |
| `histogram` | Observe value with configurable buckets, track sum/count |
| `boolean` | 1.0 if passed, 0.0 if failed |

Metrics are defined in pack files alongside eval definitions:

```json
{
  "id": "response_quality",
  "type": "llm_judge",
  "trigger": "every_turn",
  "metric": {
    "name": "response_quality_score",
    "type": "histogram",
    "range": { "min": 0, "max": 1 }
  },
  "params": {
    "criteria": "Rate the quality of the response"
  }
}
```

## Pack Eval Resolution

When both pack-level and prompt-level evals are defined, they are **merged**:

1. Prompt evals override pack evals where IDs match
2. Pack-only evals are preserved
3. Prompt-only evals are appended

This allows packs to define baseline evals while individual prompts customize or extend them.

## Example

See the [`eval-test` example](https://github.com/AltairaLabs/PromptKit/tree/main/examples/eval-test) for a working Arena configuration that evaluates saved conversations with both deterministic assertions and LLM judge evals.

## See Also

- [Observability](/sdk/explanation/observability/) — EventBus architecture and event types
- [Session Recording](/arena/explanation/session-recording/) — How recordings feed into evals
- [Monitor Events](/sdk/how-to/monitor-events/) — MetricCollector usage in SDK
- [Testing Philosophy](/arena/explanation/testing-philosophy/) — Why test prompts?
