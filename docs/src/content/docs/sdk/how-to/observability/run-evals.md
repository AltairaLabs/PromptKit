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

    "github.com/AltairaLabs/PromptKit/sdk/v2"
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

No live provider connection is needed: messages in, results out.

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
import "github.com/AltairaLabs/PromptKit/runtime/v2/evals"

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

Judge-backed checks (`llm_judge`, `llm_judge_session`, `bias`, `toxicity`,
`pii_leakage`, `role_violation` and the RAG primitives) need an LLM to grade
with. They get it the same way every other ancillary provider is supplied: the
pack names what it needs, the host decides what answers.

The name is the pack author's own and means nothing outside the pack. You stay
free to change the model behind it whenever you like, which is the point.

Declare it, and point the check at it:

```yaml
requires:
  providers:
    - key: grader                 # any name this pack likes
      role: llm
      description: grades the toxicity and PII checks
      required: true

prompts:
  chat:
    validators:
      - type: toxicity
        params:
          provider: grader        # the name declared above
```

Bind that name when you open the conversation:

```go
conv, _ := sdk.Open("./app.pack.json", "chat",
    sdk.WithProvider(agent),
    sdk.WithLLMProvider(sdk.ProviderSpec{
        ID: "grader", Type: "openai", Model: "gpt-4.1-mini",
    }),
)
```

Nothing falls back to the conversation's own provider. Which model grades the
output is your decision, and self-grading on the agent model (with the agent's
bill) is a decision rather than a default.

Everything that can go wrong is caught at `Open()`, and the error says whose
problem it is:

| What is wrong | Whose | What you see |
|---|---|---|
| The check names a provider the pack never declares | pack | names the check, the name, and what the pack does declare |
| The pack declares it, you bound nothing | host | names the key the pack asked for |
| You bound something that cannot do the job | host | names the key and says what it needs instead — "bound to an inference provider, and this check needs one that runs completions" |
| A judge-backed check names nothing at all | pack | tells you to add `params.provider` |

For a host driving a pack whose checks name nothing, `sdk.WithJudgeProvider`
supplies a default judge. A check that names a provider always wins over it.

For the offline path, pass the judge in the options:

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
    "github.com/AltairaLabs/PromptKit/runtime/v2/metrics"
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

### Eval dimensions

An eval's named sub-scores (`EvalResult.Metrics`) are not exported by default. Set `EvalDimensionLimit` on the collector to export them:

```go
collector := metrics.NewEvalOnlyCollector(metrics.CollectorOpts{
    Registerer:         reg,
    EvalDimensionLimit: 16,
})
```

Each dimension becomes a gauge in a family derived from the eval's metric, leaving the eval's own series unchanged:

```
promptkit_eval_rubric_score 0.8
promptkit_eval_rubric_score_dimension{dimension="clarity"} 0.9
promptkit_eval_rubric_score_dimension{dimension="accuracy"} 0.7
```

The names come from handler output, and a judge model can invent a new one on any call, so the export is bounded:

- A name must be snake_case (`^[a-z_][a-z0-9_]{0,63}$`). Others are not exported, and nothing is case-folded: `Clarity` is dropped, not merged into `clarity`.
- Each eval metric exports at most `EvalDimensionLimit` distinct dimensions for the life of the process, admitted in sorted name order. A series, once created, is never removed, so this cap is the bound on cardinality.
- Dropped dimensions are counted in `promptkit_eval_dimensions_dropped_total{metric, reason}`, with `reason` either `invalid_name` or `over_limit`.
- A result with `Error` set exports no dimensions.
- Collectors sharing a registry share the drop counter. A derived family that cannot be registered (an eval metric already named `<metric>_dimension`, or a metric label already called `dimension`) is skipped with a warning; the eval's own metric still records.

Keep `Metrics` to a handful of summary scores. A per-test or per-finding breakdown belongs in `Details`.

Evals must have a `metric` definition in the pack to be recorded. See [Metrics & Prometheus](https://promptarena.altairalabs.ai/arena/explanation/eval-framework/#metrics--prometheus) for metric types and label configuration, and [Monitor Events](/sdk/how-to/observability/monitor-events/#prometheus-metrics) for the full metrics reference.

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

Each result contains, among other fields:

```go
type EvalResult struct {
    EvalID      string             // Eval identifier
    Type        string             // Handler type
    Kind        events.EvalKind    // Role: eval, assertion or guardrail
    Passed      *bool              // Verdict; set only by assertion and guardrail wrappers
    Score       *float64           // The eval's score
    MetricValue *float64           // The number a metric pipeline graphs
    Metrics     map[string]float64 // Named sub-scores from the same eval
    Explanation string             // Human-readable explanation
    DurationMs  int64              // Execution time
    Error       string             // Set when no measurement was taken
    Details     map[string]any     // Handler-specific structured detail
    Violations  []EvalViolation    // Detailed violations
    Skipped     bool               // Was eval skipped?
    SkipReason  string             // Why skipped
}
```

Eval handlers measure; they do not pass or fail. `Passed` is nil for a plain eval and is set only when the eval runs under `type: assertion` or `type: guardrail`. An assertion fails any result with `Error` set.

`Metrics` holds named sub-scores an eval produced alongside its score, such as a judge's per-dimension rubric scores or an exec eval's `metrics`. The names come from the handler's output, not from the pack. They travel on `eval.completed` events and as `promptkit.eval.metric.<name>` span attributes; a span carries at most 32 of them, in name order, and is marked `promptkit.eval.metrics_truncated` when more were dropped. They are exported to Prometheus only when the collector opts in; see [Eval dimensions](#eval-dimensions).

## Observing Results with EvalHook

Register an `EvalHook` to observe (or mutate) each eval result as it's produced. Useful for:

- Pushing results to external metrics/tracing systems.
- Redacting sensitive content from `Explanation` or `Details` before results propagate.
- Fanning out to a subprocess via `ExecEvalHook` (configured in `RuntimeConfig`).

```go
type MetricsHook struct {
    exporter MetricExporter
}

func (h *MetricsHook) Name() string { return "metrics_exporter" }

func (h *MetricsHook) OnEvalResult(
    ctx context.Context,
    def *evals.EvalDef,
    _ *evals.EvalContext,
    result *evals.EvalResult,
) {
    h.exporter.Record(ctx, def.ID, result.Score, result.DurationMs)
}

conv, _ := sdk.Open("./app.pack.json", "chat",
    sdk.WithEvalHook(&MetricsHook{exporter: exp}),
)
```

Eval hooks are observational by contract — they cannot gate execution. Every registered hook runs for every eval result, in registration order, with per-hook panic recovery. See [Hooks Explanation](/sdk/explanation/hooks/) for the full mental model, [`EvalHook`](/runtime/reference/evals/#EvalHook) for the interface details, and [`WithEvalHook`](/sdk/reference/conversation-manager/#WithEvalHook) for the registration API.

## See Also

- [Metrics Reference](/runtime/reference/metrics/) -- Complete catalog of all emitted metrics
- [Checks Reference](/reference/checks/) -- All check types and parameters
- [Unified Check Model](/concepts/validation/) -- How evals, assertions, and guardrails relate
- [Eval Framework](https://promptarena.altairalabs.ai/arena/explanation/eval-framework/) -- Eval architecture, triggers, and metrics
- [Evals Reference](/runtime/reference/evals/#EvalHook) -- the `EvalHook` interface
- [`WithEvalHook`](/sdk/reference/conversation-manager/#WithEvalHook) -- registering one
- [Exec Hooks How-To](/sdk/how-to/hooks/exec-hooks/) -- subprocess-backed eval hooks in any language
- [Monitor Events](/sdk/how-to/observability/monitor-events/) -- Event-based observability
