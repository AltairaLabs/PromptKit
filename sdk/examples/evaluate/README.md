# Standalone Evaluate Example

Run evals against a conversation snapshot with `sdk.Evaluate()` — no live provider
or agent connection needed, just messages in, results out.

## What it shows

- Running evals from a PromptPack against a conversation snapshot
- Filtering evals by trigger (`every_turn` vs `on_session_complete`)
- Running evals from inline definitions (no pack file needed)
- Receiving eval results via the `EventBus` for reactive workflows
- Recording eval results as Prometheus metrics via a `MetricsCollector`
- Validating that all eval types are registered before execution
- Extending the eval registry with custom exec handlers via `RuntimeConfig`

## Running

```bash
cd sdk/examples/evaluate
go run .
```
