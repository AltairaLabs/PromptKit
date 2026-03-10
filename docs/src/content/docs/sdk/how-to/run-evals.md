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
    fmt.Printf("%s: passed=%v score=%v\n", r.EvalID, r.Passed, r.Score)
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
| `Trigger` | `evals.EvalTrigger` | Trigger filter (default: `every_turn`) |
| `JudgeProvider` | `any` | Pre-built LLM judge provider |
| `JudgeTargets` | `map[string]any` | Provider specs for LLM judge evals |
| `TracerProvider` | `trace.TracerProvider` | OpenTelemetry tracing |
| `EventBus` | `*events.EventBus` | Event emission |
| `Logger` | `*slog.Logger` | Structured logging |
| `Registry` | `*evals.EvalTypeRegistry` | Custom handler registry |
| `Timeout` | `time.Duration` | Per-eval timeout (default: 30s) |
| `SkipSchemaValidation` | `bool` | Skip JSON schema validation |

## EvalResult

Each result contains:

```go
type EvalResult struct {
    EvalID      string          // Eval identifier
    Type        string          // Handler type
    Passed      bool            // Pass/fail
    Score       *float64        // Optional score (0.0-1.0)
    Explanation string          // Human-readable explanation
    DurationMs  int64           // Execution time
    Error       string          // Error message if failed
    Violations  []EvalViolation // Detailed violations
    Skipped     bool            // Was eval skipped?
    SkipReason  string          // Why skipped
}
```

## See Also

- [Eval Framework](/arena/explanation/eval-framework/) — How evals work
- [Assertions Reference](/arena/reference/assertions/) — All assertion/eval types
- [Monitor Events](monitor-events) — Event-based observability
