---
title: Run Evals
sidebar:
  order: 7
---
Run quality checks against conversation snapshots with `sdk.Evaluate()`.

## Basic Usage

```go
import (
    "context"
    "fmt"

    "github.com/AltairaLabs/PromptKit/sdk"
)

results, err := sdk.Evaluate(ctx, sdk.EvaluateOpts{
    PackPath:  "./app.pack.json",
    PromptName: "assistant",
    Messages:  messages, // []types.Message
    SessionID: "session-123",
    TurnIndex: 0,
})
if err != nil {
    log.Fatal(err)
}

for _, r := range results {
    fmt.Printf("%s: score=%v explanation=%s\n", r.EvalID, r.Score, r.Explanation)
}
```

No live provider connection is needed — just messages in, results out.

## Eval Sources

Provide eval definitions from one of three sources (checked in order):

### From a pack file

```go
results, _ := sdk.Evaluate(ctx, sdk.EvaluateOpts{
    PackPath:   "./app.pack.json",
    PromptName: "assistant",  // merge prompt-level evals with pack-level
    Messages:   messages,
})
```

### From JSON bytes

```go
results, _ := sdk.Evaluate(ctx, sdk.EvaluateOpts{
    PackData:   packJSON,  // []byte
    PromptName: "assistant",
    Messages:   messages,
})
```

### From explicit definitions

```go
import "github.com/AltairaLabs/PromptKit/runtime/evals"

results, _ := sdk.Evaluate(ctx, sdk.EvaluateOpts{
    EvalDefs: []evals.EvalDef{
        {
            ID:      "no_profanity",
            Type:    "content_excludes",
            Trigger: evals.TriggerEveryTurn,
            Params:  map[string]any{"patterns": []string{"damn", "hell"}},
        },
        {
            ID:      "valid_json",
            Type:    "json_valid",
            Trigger: evals.TriggerEveryTurn,
        },
    },
    Messages: messages,
})
```

## Triggers

Control when evals fire:

| Trigger | Constant | Description |
|---------|----------|-------------|
| `every_turn` | `evals.TriggerEveryTurn` | After each assistant response (default) |
| `on_session_complete` | `evals.TriggerOnSessionComplete` | When session ends |
| `on_conversation_complete` | `evals.TriggerOnConversationComplete` | When conversation ends |
| `sample_turns` | `evals.TriggerSampleTurns` | Hash-based turn sampling |
| `sample_sessions` | `evals.TriggerSampleSessions` | Hash-based session sampling |
| `on_workflow_step` | `evals.TriggerOnWorkflowStep` | After workflow transition |

```go
results, _ := sdk.Evaluate(ctx, sdk.EvaluateOpts{
    PackPath: "./app.pack.json",
    Messages: messages,
    Trigger:  evals.TriggerOnSessionComplete,
})
```

## LLM Judge Support

For `llm_judge` and `llm_judge_session` evals, provide a judge provider:

```go
results, _ := sdk.Evaluate(ctx, sdk.EvaluateOpts{
    PackPath:      "./app.pack.json",
    Messages:      messages,
    JudgeProvider: judgeProvider, // pre-built provider instance
})
```

## Eval Groups

Evals are automatically classified into well-known groups based on their handler type. When no explicit groups are configured, each eval belongs to `default` plus a classification group:

| Group | Constant | Description |
|-------|----------|-------------|
| `default` | `evals.DefaultEvalGroup` | All evals with no explicit groups |
| `fast-running` | `evals.GroupFastRunning` | Deterministic checks (string matching, regex, JSON validation) |
| `long-running` | `evals.GroupLongRunning` | LLM calls, embeddings, network requests |
| `external` | `evals.GroupExternal` | External systems (REST APIs, A2A agents, exec subprocesses) |

Filter which groups to run with `EvalGroups`:

```go
// Only run fast, deterministic evals
results, _ := sdk.Evaluate(ctx, sdk.EvaluateOpts{
    PackPath:   "./app.pack.json",
    Messages:   messages,
    EvalGroups: []string{evals.GroupFastRunning},
})
```

Override automatic classification by setting explicit groups on an eval definition:

```json
{
  "id": "custom_check",
  "type": "llm_judge",
  "trigger": "every_turn",
  "groups": ["safety", "compliance"],
  "params": { "criteria": "..." }
}
```

When explicit groups are set, they fully replace the automatic classification.

## Metrics

Record eval results as Prometheus metrics by passing a `MetricsCollector` — the SDK calls `Bind()` internally, matching the `WithMetrics()` pattern from the conversation API:

```go
import (
    "github.com/prometheus/client_golang/prometheus"
    "github.com/AltairaLabs/PromptKit/runtime/metrics"
)

reg := prometheus.NewRegistry()
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

`NewEvalOnlyCollector` skips pipeline metric registration — use it for standalone eval workers that don't run a live pipeline. For consumers that also need pipeline metrics, use `metrics.NewCollector()` instead.

You can also pass a raw `MetricRecorder` for custom implementations, but `MetricsCollector` is preferred for new code.

Evals must have a `metric` definition in the pack to be recorded. See [Metrics & Prometheus](/arena/explanation/eval-framework/#metrics--prometheus) for metric types and label configuration, and [Monitor Events](/sdk/how-to/monitor-events/#prometheus-metrics) for the full metrics reference.

## Type Validation

Use `ValidateEvalTypes()` as a preflight check to ensure all eval types have registered handlers:

```go
missing, err := sdk.ValidateEvalTypes(sdk.ValidateEvalTypesOpts{
    PackPath:          "./app.pack.json",
    RuntimeConfigPath: "./runtime-config.yaml", // registers exec handlers
})
if len(missing) > 0 {
    for _, def := range missing {
        log.Printf("missing handler for eval %q (type: %s)", def.ID, def.Type)
    }
}
```

This catches configuration errors (typos, missing RuntimeConfig bindings) at startup or in CI before evals are actually executed.

## Observability

### OpenTelemetry Tracing

```go
results, _ := sdk.Evaluate(ctx, sdk.EvaluateOpts{
    PackPath:       "./app.pack.json",
    Messages:       messages,
    TracerProvider: tp, // trace.TracerProvider
})
```

Each eval result emits a span named `promptkit.eval.{evalID}`.

### Event Bus

```go
bus := events.NewEventBus()
defer bus.Close()

bus.Subscribe(events.EventEvalCompleted, func(e *events.Event) {
    log.Printf("Eval passed: %s", e.Data)
})

results, _ := sdk.Evaluate(ctx, sdk.EvaluateOpts{
    PackPath: "./app.pack.json",
    Messages: messages,
    EventBus: bus,
})
```

## EvaluateOpts Reference

| Field | Type | Description |
|-------|------|-------------|
| `PackPath` | `string` | Load pack from filesystem |
| `PackData` | `[]byte` | Parse pack from JSON bytes |
| `EvalDefs` | `[]evals.EvalDef` | Pre-resolved eval definitions |
| `PromptName` | `string` | Select prompt-level evals to merge |
| `Messages` | `[]types.Message` | Conversation history to evaluate |
| `SessionID` | `string` | Session ID for sampling determinism |
| `TurnIndex` | `int` | Current turn index (0-based) |
| `EvalGroups` | `[]string` | Filter evals by group (default: all) |
| `Trigger` | `evals.EvalTrigger` | Trigger filter (default: `every_turn`) |
| `JudgeProvider` | `any` | Pre-built LLM judge provider |
| `JudgeTargets` | `map[string]any` | Provider specs for LLM judge evals |
| `TracerProvider` | `trace.TracerProvider` | OpenTelemetry tracing |
| `EventBus` | `events.Bus` | Event emission |
| `Logger` | `*slog.Logger` | Structured logging |
| `RuntimeConfigPath` | `string` | Load exec eval handlers from RuntimeConfig YAML |
| `MetricsCollector` | `*metrics.Collector` | Unified Prometheus collector (preferred — SDK calls `Bind()` internally) |
| `MetricsInstanceLabels` | `map[string]string` | Per-invocation label values for `MetricsCollector` |
| `MetricRecorder` | `evals.MetricRecorder` | Custom metric recorder (use `MetricsCollector` for new code) |
| `Registry` | `*evals.EvalTypeRegistry` | Custom handler registry |
| `Timeout` | `time.Duration` | Per-eval timeout (default: 30s) |
| `SkipSchemaValidation` | `bool` | Skip JSON schema validation |

## EvalResult

Each result contains:

```go
type EvalResult struct {
    EvalID      string          // Eval identifier
    Type        string          // Handler type
    Score       *float64        // Score (0.0-1.0)
    Explanation string          // Human-readable explanation
    DurationMs  int64           // Execution time
    Error       string          // Error message if eval errored
    Violations  []EvalViolation // Detailed violations
    Skipped     bool            // Was eval skipped?
    SkipReason  string          // Why skipped
    Passed      bool            // Deprecated: set only by assertion/guardrail wrappers
}
```

Eval handlers produce scores only. Use `result.IsPassed()` to derive pass/fail from the score (true when score is nil or ≥ 1.0). The `Passed` field is deprecated for standalone evals — it is only set explicitly by `AssertionEvalHandler` and `GuardrailEvalHandler` wrappers.

## See Also

- [Metrics Reference](/runtime/reference/metrics/) -- Complete catalog of all emitted metrics
- [Checks Reference](/reference/checks/) -- All check types and parameters
- [Unified Check Model](/concepts/validation/) -- How evals, assertions, and guardrails relate
- [Eval Framework](/arena/explanation/eval-framework/) -- Eval architecture, triggers, and metrics
- [Monitor Events](monitor-events) -- Event-based observability
