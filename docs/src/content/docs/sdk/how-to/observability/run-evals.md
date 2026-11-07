---
title: Run Evals
description: Score a conversation snapshot with sdk.Evaluate(), supply judges, and send the results to metrics, traces and events
sidebar:
  order: 7
verified:
  commit: ffbed7c4b0fff266953352be5e73d4e308a97acf
  sources:
    - runtime/evals/handlers/audio_emotion.go
    - runtime/evals/handlers/exec_handler.go
    - runtime/evals/handlers/groups.go
    - runtime/evals/handlers/judge_required.go
    - runtime/evals/handlers/llm_judge.go
    - runtime/evals/handlers/rag_helpers.go
    - runtime/evals/hooks.go
    - runtime/evals/metrics.go
    - runtime/evals/runner.go
    - runtime/evals/trigger.go
    - runtime/evals/types.go
    - runtime/evals/wrappers.go
    - runtime/events/types.go
    - runtime/hooks/guardrails/factory.go
    - runtime/metrics/collector.go
    - runtime/metrics/eval_dimensions.go
    - runtime/prompt/schema/promptpack.schema.json
    - runtime/telemetry/listener.go
    - sdk/binding.go
    - sdk/eval_middleware.go
    - sdk/evaluate.go
    - sdk/evaluate_binding.go
    - sdk/options.go
    - sdk/provider_keys.go
    - sdk/provider_refs.go
---
Score a conversation snapshot against the evals in a pack with `sdk.Evaluate()`. Messages go in and results come out; no live agent connection is needed.

## Prerequisites

- A pack with `evals`, or a list of `evals.EvalDef` values.
- The conversation to score as `[]types.Message`.
- For judge-backed evals, a judge provider (see [Supply a judge](#supply-a-judge)).

## Run the evals

1. Pass the pack and the messages:

   ```go
   import (
       "context"
       "fmt"
       "log"

       "github.com/AltairaLabs/PromptKit/sdk/v2"
   )

   results, err := sdk.Evaluate(ctx, sdk.EvaluateOpts{
       PackPath:   "./app.pack.json",
       PromptName: "assistant", // merge this prompt's evals with the pack-level evals
       Messages:   messages,    // []types.Message
       SessionID:  "session-123",
       TurnIndex:  0,
   })
   if err != nil {
       log.Fatal(err)
   }

   for _, r := range results {
       if r.Score != nil {
           fmt.Printf("%s: score=%.2f %s\n", r.EvalID, *r.Score, r.Explanation)
       }
   }
   ```

   `Evaluate` takes the eval definitions from the first source that is set: `EvalDefs`, then `PackData` (JSON bytes), then `PackPath`. To pass definitions directly:

   ```go
   import "github.com/AltairaLabs/PromptKit/runtime/v2/evals"

   results, err := sdk.Evaluate(ctx, sdk.EvaluateOpts{
       EvalDefs: []evals.EvalDef{
           {
               ID:      "no_profanity",
               Type:    "content_excludes",
               Trigger: evals.TriggerEveryTurn,
               Params:  map[string]any{"patterns": []string{"damn", "hell"}},
           },
       },
       Messages: messages,
   })
   ```

2. Choose which trigger class runs with `Trigger`. The default is `every_turn`.

   | `Trigger` | Runs evals with trigger |
   |---|---|
   | `evals.TriggerEveryTurn` (default) | `every_turn`, `sample_turns` |
   | `evals.TriggerOnSessionComplete` | `on_session_complete`, `sample_sessions` |
   | `evals.TriggerOnConversationComplete` | `on_conversation_complete` |
   | `evals.TriggerOnWorkflowStep` | `on_workflow_step`: after each workflow state transition, against the turn that made it |

   Sampled triggers hash `SessionID`.

3. Filter by group with `EvalGroups`. An eval with no explicit `groups` belongs to `default` plus the classification groups of its type:

   | Group | Constant | Eval types |
   |---|---|---|
   | `fast-running` | `evals.GroupFastRunning` | Deterministic checks: string matching, regex, JSON validation |
   | `long-running` | `evals.GroupLongRunning` | LLM judges, classifiers, embeddings, network requests |
   | `external` | `evals.GroupExternal` | LLM judges, classifiers, REST APIs, A2A agents, exec subprocesses |

   Every judge-backed and classifier-backed built-in check is both `long-running` and `external`. A handler declares its groups by implementing `evals.GroupDeclarer`; a type whose handler declares none is `fast-running`. An `assertion` or `guardrail` wrapper takes the groups of the eval it wraps, and an alias takes the groups of its target.

   ```go
   results, err := sdk.Evaluate(ctx, sdk.EvaluateOpts{
       PackPath:   "./app.pack.json",
       Messages:   messages,
       EvalGroups: []string{evals.GroupFastRunning},
   })
   ```

   Explicit `groups` on an eval definition (`"groups": ["safety", "compliance"]`) replace the classification.

## Supply a judge

Judge-backed evals (`llm_judge`, `llm_judge_session`, `bias`, `toxicity`, `pii_leakage`, `role_violation` and the RAG primitives) need an LLM to grade with. The pack declares a logical name, the eval points at it, and you decide which model answers.

1. Declare the provider in the pack and name it in the eval:

   ```yaml
   requires:
     providers:
       - key: grader                 # any name this pack likes
         role: llm
         description: grades the toxicity and PII checks
         required: true

   prompts:
     chat:
       evals:
         - id: toxicity
           type: toxicity
           trigger: every_turn
           params:
             provider: grader        # the name declared above
   ```

2. Bind the name for `Evaluate` with `JudgeTargets`, keyed by the pack's logical name:

   ```go
   import "github.com/AltairaLabs/PromptKit/runtime/v2/providers"

   results, err := sdk.Evaluate(ctx, sdk.EvaluateOpts{
       PackPath:   "./app.pack.json",
       PromptName: "chat",
       Messages:   messages,
       JudgeTargets: map[string]any{
           "grader": providers.ProviderSpec{ID: "grader", Type: "openai", Model: "gpt-4.1-mini"},
       },
   })
   ```

   To resolve names yourself, set `ProviderBinding` to an `evals.ProviderBinding`. It takes precedence over `JudgeTargets`.

3. For a live conversation, bind the name with `WithNamedProvider`. It registers the grader without replacing the conversation's agent:

   ```go
   conv, err := sdk.Open("./app.pack.json", "chat",
       sdk.WithProvider(agent),
       sdk.WithNamedProvider(sdk.ProviderSpec{
           ID: "grader", Type: "openai", Model: "gpt-4.1-mini",
       }),
   )
   ```

Nothing falls back to the conversation's own provider. A host that drives packs whose checks name no provider can set a default judge: `sdk.WithJudgeProvider` for a conversation, `EvaluateOpts.JudgeProvider` for `Evaluate`. A check that names a provider ignores the default.

For checks in a prompt's `evals` or `validators`, and in the pack-level `evals` that run on its turns, `Open()` fails before the first turn when the name is undeclared in `requires`, declared but unbound, or bound to an inference provider. A judge-backed guardrail that names no provider, with no default judge, also fails `Open()` and the error says to add `params.provider`.

When a judge-backed check still cannot run on a turn (no judge reached it, the judge call failed, or a RAG check had no retrieved context to grade), its result carries `Error` and is emitted as `eval.failed`, not `eval.completed`. Its score stays 0, so thresholds keep failing closed, but it does not read as a measured zero.

## Record metrics

Pass a `MetricsCollector` to record results as Prometheus metrics. `Evaluate` binds it for you:

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

results, err := sdk.Evaluate(ctx, sdk.EvaluateOpts{
    PackPath:              "./app.pack.json",
    Messages:              messages,
    MetricsCollector:      collector,
    MetricsInstanceLabels: map[string]string{"tenant": "acme"},
})
```

`NewEvalOnlyCollector` skips pipeline metrics. Use `metrics.NewCollector()` when the process also runs conversations. An eval without a `metric` definition is recorded as a gauge named after the eval ID.

An eval metric of type `histogram` uses the buckets `0, 0.1, 0.2, ... 1.0` by default, which span the range eval scores live in. Set `EvalHistogramBuckets` on `metrics.CollectorOpts` to replace them; the collector sorts the values and drops duplicates. A metric that declares both `range.min` and `range.max` (with `max` above `min`) gets ten equal-width buckets across that range instead, so a metric on another scale needs no collector setting.

### Eval dimensions

An eval's named sub-scores (`EvalResult.Metrics`) are not exported by default. Set `EvalDimensionLimit` on the collector to export them:

```go
collector := metrics.NewEvalOnlyCollector(metrics.CollectorOpts{
    Registerer:         reg,
    EvalDimensionLimit: 16,
})
```

Each dimension becomes a gauge in a family derived from the eval's metric. The eval's own series is unchanged:

```text
promptkit_eval_rubric_score 0.8
promptkit_eval_rubric_score_dimension{dimension="clarity"} 0.9
promptkit_eval_rubric_score_dimension{dimension="accuracy"} 0.7
```

Dimension names come from handler output, so the export is bounded:

- A name must match `^[a-z_][a-z0-9_]{0,63}$`. Other names are dropped, not case-folded.
- Each eval metric exports at most `EvalDimensionLimit` distinct dimensions for the life of the process, admitted in sorted name order. A series is never removed.
- Dropped dimensions are counted in `<namespace>_eval_dimensions_dropped_total{metric, reason}`, where `reason` is `invalid_name` or `over_limit`.
- A result with `Error` set exports no dimensions. A derived family that cannot register is skipped with a warning.

## Send results to traces, events and hooks

Set `TracerProvider` to emit one span per result, named `promptkit.eval.{evalID}`. A span carries the result's `Metrics` as `promptkit.eval.metric.<name>` attributes, at most 32 in name order, and sets `promptkit.eval.metrics_truncated` when more were dropped.

Set `EventBus` to receive `eval.completed` and `eval.failed` events:

```go
import "github.com/AltairaLabs/PromptKit/runtime/v2/events"

bus := events.NewEventBus()
defer bus.Close()

bus.Subscribe(events.EventEvalCompleted, func(e *events.Event) {
    if d, ok := e.Data.(*events.EvalEventData); ok {
        log.Printf("eval %s scored", d.EvalID)
    }
})

results, err := sdk.Evaluate(ctx, sdk.EvaluateOpts{
    PackPath: "./app.pack.json",
    Messages: messages,
    EventBus: bus,
})
```

To observe or rewrite each result as the runner produces it, implement `evals.EvalHook` (`Name()` and `OnEvalResult(ctx, def, evalCtx, result)`) and register it on a conversation:

```go
conv, err := sdk.Open("./app.pack.json", "chat",
    sdk.WithEvalHook(hook),
)
```

A hook may mutate the result, for example to redact `Explanation`. It cannot gate execution. Hooks run in registration order, each with panic recovery. See [`EvalHook`](/runtime/reference/evals/#EvalHook) and [`WithEvalHook`](/sdk/reference/conversation-manager/#WithEvalHook).

## Verify

Check each result:

- `Score` holds the eval's measurement. `Passed` is nil for a plain eval and set only under `type: assertion` or `type: guardrail`.
- `Error` is set when no measurement was taken. An assertion fails any result with `Error` set.
- `Skipped` and `SkipReason` explain an eval that did not run.

Run `ValidateEvalTypes` at startup or in CI to find evals whose type has no registered handler:

```go
missing, err := sdk.ValidateEvalTypes(sdk.ValidateEvalTypesOpts{
    PackPath:          "./app.pack.json",
    RuntimeConfigPath: "./runtime-config.yaml", // registers exec handlers
})
if err != nil {
    log.Fatal(err)
}
for _, def := range missing {
    log.Printf("missing handler for eval %q (type: %s)", def.ID, def.Type)
}
```

## Troubleshoot

- No results and no error: `Trigger`, `EvalGroups`, `enabled: false` or sampling filtered out every definition.
- A judge name does not resolve: the result has score 0, and `Error` and `Explanation` both name the cause. This holds for `llm_judge`, the safety judges (`toxicity`, `bias` and the others) and the RAG judges.
- An eval stops with a timeout: `Timeout` defaults to 30 seconds per eval; raise it on `EvaluateOpts`.

## Related

- [`EvaluateOpts`](/sdk/reference/conversation-manager/#EvaluateOpts): every option
- [`EvalResult`](/runtime/reference/evals/#EvalResult): every result field
- [Checks reference](/reference/checks/): check types and parameters
- [Unified check model](/concepts/validation/): how evals, assertions and guardrails relate
- [Monitor events](/sdk/how-to/observability/monitor-events/)
- [Prometheus metrics](/runtime/how-to/observability/prometheus-metrics/)
- [Exec hooks](/sdk/how-to/hooks/exec-hooks/)
