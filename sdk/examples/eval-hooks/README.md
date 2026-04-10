# SDK Eval Hooks Example

Demonstrates `sdk.WithEvalHook` — an observational hook fired once per
eval result the runner produces. Hooks can observe results, mutate them
in place (for redaction/enrichment), or fan them out to external systems.

This example wires up two hooks:

| Hook | Purpose |
|------|---------|
| `countingHook` | Pure-Go observer that tallies results by eval ID and prints a summary at shutdown. |
| `ExecEvalHook` | Shells out to `log-result.sh` per result, piping the JSON-encoded `EvalResult` on stdin. |

`log-result.sh` is a minimal bash consumer — it prints a one-line summary
to stderr and appends the raw JSON to `eval-results.ndjson` for offline
analysis. Replace it with whatever you want (a Python script, an HTTP
`curl`, a Kafka producer).

## Run it

```bash
go run .
```

The example uses a mock provider, so no API keys are needed. After the
run, inspect `eval-results.ndjson`:

```bash
cat eval-results.ndjson
```

## Panic safety

Misbehaving hooks cannot crash the pipeline. The eval runner recovers
panics per-hook, logs them, and continues — a broken `ExecEvalHook` or a
Go hook with a nil-pointer bug will not take down the conversation.

## Writing your own hook

```go
type MyHook struct{}

func (MyHook) Name() string { return "my-hook" }

func (MyHook) OnEvalResult(
    ctx context.Context,
    def *evals.EvalDef,
    evalCtx *evals.EvalContext,
    result *evals.EvalResult,
) {
    // Observe or mutate result here.
}

conv, _ := sdk.Open(packPath, "chat",
    sdk.WithEvalHook(MyHook{}),
)
```

Hooks receive the `EvalDef` (which eval was this), the `EvalContext`
(messages/session metadata), and the `EvalResult` (mutable). The mutated
result is what subsequent hooks see and what is emitted as the
`eval.completed` / `eval.failed` event.
