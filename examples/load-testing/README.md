# Load Testing with PromptArena

Use Arena as a workload generator for load testing deployed LLM services. Self-play produces realistic multi-turn conversations — no assertions, so runs never fail due to response content. Each run is a single-conversation workload unit; drive concurrency externally.

## What's included

| Scenario | Turns | Trials | Purpose |
|----------|-------|--------|---------|
| `simple-qa` | 1 | 5 | Baseline single-turn latency |
| `multi-turn-selfplay` | 5 | 3 | Sustained context handling |
| `deep-conversation` | 10 | 2 | Context window stress |

Self-play (`gemini-user`) drives follow-up turns so every conversation is unique. No assertions means runs complete regardless of response quality — you get timing data without test failures blocking the load.

## Setup

Edit `providers/target.provider.yaml` and set `base_url` to point at your deployed service:

```yaml
spec:
  type: openai
  model: your-model
  base_url: http://your-service:8080/v1
```

The service must expose an OpenAI-compatible `/chat/completions` endpoint.

## Running

### Single instance (baseline)

```bash
promptarena run --config config.arena.yaml --ci --formats json,html
```

### Concurrent load (external orchestration)

Arena is the workload unit — drive concurrency externally:

```bash
# Shell — 20 concurrent instances
for i in $(seq 1 20); do
  promptarena run --config config.arena.yaml --ci --formats json \
    -o "out/run-$i" &
done
wait
```

## Reading results

Each run produces:
- **`out/report.html`** — visual report with per-turn latency
- **`out/*.json`** — machine-parseable results with `latency_ms` per message

### Key metrics

| Metric | Where | What it tells you |
|--------|-------|-------------------|
| `latency_ms` per message | JSON results | Per-turn response time under load |
| Token count per message | JSON results | Verbosity changes under pressure |
| Trial pass rate | Aggregated results | Consistency across repetitions |
| Total cost | Run summary | Cost per conversation under load |

## Quality measurement

These scenarios deliberately omit assertions so load runs complete cleanly. To measure quality *under* load, two options:

1. **Post-hoc evals**: Record the output, then replay through Arena's eval system with LLM judge assertions
2. **Separate quality config**: Create a second arena config that adds `llm_judge` assertions to the same scenarios and run it at lower concurrency alongside the load

## Customising

- **More load**: Increase `trials` in scenario files
- **Longer conversations**: Increase the `turns` count on `gemini-user` entries
- **Different topics**: Change the seed user messages — self-play will follow
- **Different personas**: Add new persona files in `personas/`
