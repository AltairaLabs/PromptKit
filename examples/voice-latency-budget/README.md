# Voice Latency Budget Demo

Assert per-turn provider latency against an explicit budget. The scenario fails when an LLM call takes longer than the threshold — the same way you'd gate a deploy on latency regressions.

## What it tests

A three-turn support exchange with a `latency_budget` assertion on every turn:

```yaml
assertions:
  - type: latency_budget
    params:
      max_ms: 1000
```

Arena bridges the assistant message's `LatencyMs` (set by the provider stage) into the eval context metadata, so the assertion reads real per-turn timing without any custom plumbing.

## Running

The default config runs deterministically against an in-process text mock provider — sub-millisecond responses, so the 1000ms budget passes trivially. Real signal arrives when you swap in a real provider:

```bash
cd examples/voice-latency-budget
../../bin/promptarena run --ci --formats html,json
open out/report.html
```

For the live dev loop:

```bash
../../bin/promptarena serve
../../bin/promptarena run --tui
```

## Switching to live providers

Add a real provider config and re-register it in `config.arena.yaml`:

```yaml
providers:
  - file: providers/openai-gpt4o-mini.provider.yaml
  - file: providers/claude-haiku.provider.yaml
  - file: providers/gemini-flash.provider.yaml
```

Then run with provider keys in your environment. The HTML report shows the per-turn `latency_ms` recorded against each provider so you can compare them side by side.

## What `latency_budget` measures today

`latency_budget` reads the **total provider-call duration for the current turn** (LLM round-trip time, including any tool-call rounds within that turn). It's a coarse "is this turn fast enough?" signal — useful for gating deploys against regressions and for cross-provider comparisons.

What it doesn't yet capture (tracked separately):

- **TTFB** — time to first token / first audio frame
- **First-audio** — time from user-input-end to first audio-out for duplex providers
- **End-of-turn delta** — provider-side silence-detection latency between generation-complete and turn-complete

These need richer metric capture in the provider stages. When that lands, the assertion will gain per-metric thresholds; the demo's shape stays the same.

## CI gate

This demo runs keyless against the mock provider, so it fits a fork-safe CI job:

```yaml
# .github/workflows/voice-latency-budget.yml
name: Voice latency budget

on:
  pull_request:
    paths:
      - 'examples/voice-latency-budget/**'

jobs:
  test:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      - uses: actions/setup-go@v5
        with:
          go-version: '1.26'
      - run: make build-arena
      - name: Run latency-budget scenarios
        working-directory: examples/voice-latency-budget
        run: ../../bin/promptarena run --ci --formats json
```

## File layout

```
voice-latency-budget/
├── README.md
├── config.arena.yaml
├── mock-responses.yaml
├── prompts/support-agent.yaml
├── providers/mock-provider.yaml
└── scenarios/turn-budget.scenario.yaml
```
