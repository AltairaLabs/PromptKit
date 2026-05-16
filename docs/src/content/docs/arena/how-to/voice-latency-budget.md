---
title: Assert per-turn latency budgets
description: Gate scenarios on real provider latency. Arena bridges the assistant message's LatencyMs into eval context, so the latency_budget assertion just works.
---

This how-to walks through `examples/voice-latency-budget/` — a small scenario that asserts every turn's provider latency against an explicit `max_ms` budget. Useful for gating deploys on regressions, comparing providers on the same scenario, and proving an agent stays inside a real-time budget.

## What it proves

LLM-driven systems silently slow down: a small prompt change adds a hidden retrieval step, a provider quietly degrades, a tool call gets retried, an agent loops one extra round. Pure single-turn eval misses this — the response looks right, it's just slow. PromptArena makes latency a first-class signal:

- The provider stage records `LatencyMs` on every assistant message (LLM round-trip including any in-turn tool-call rounds).
- Arena bridges `LatencyMs` into the eval context metadata as `latency_ms` so the standard `latency_budget` assertion reads it without any custom plumbing.
- Scenarios gate each turn: `max_ms: 1000` fails any reply slower than a second.

## The assertion shape

```yaml
turns:
  - role: user
    content: "Hi, I need help with my account."
    assertions:
      - type: latency_budget
        params:
          max_ms: 1000
        message: "Turn must respond within 1000ms"
```

`latency_budget` returns a score normalised to the budget: `min(1.0, max_ms / latency_ms)`. A reply within budget scores 1.0; a reply at 2× the budget scores 0.5. The HTML report shows the exact `latency_ms` vs `budget_ms` per turn.

## Run it

```bash
cd examples/voice-latency-budget
promptarena serve
```

`serve` loads the scenario into the web UI; the timeline view shows the latency assertion alongside the conversation. Headless:

```bash
promptarena run --ci --formats html,json
open out/report.html
```

The default config runs against a text mock provider — sub-millisecond responses, so the budget passes trivially. The interesting signal comes from real providers.

## Comparing providers

Add multiple provider files and re-register them in `config.arena.yaml`. Arena fans out the scenario across every registered provider; the HTML report shows the per-provider `latency_ms` distribution side by side. Use this for migration testing ("does Claude Haiku stay inside our 800ms budget on this prompt?") or for cross-provider bake-offs.

## What's measured today

`latency_budget` checks the **total provider-call duration per turn**: LLM round-trip time, including any tool-call rounds that happen within that turn. It's a coarse "is this turn fast enough" signal — well-suited to gating regressions.

What richer voice testing wants — and what's coming next — is per-metric capture:

- **TTFB** — time to first token / first audio frame
- **First-audio** — time from user-input-end to first audio-out (duplex providers)
- **End-of-turn delta** — silence-detection latency between generation-complete and turn-complete

Provider stages capture some of these timings internally; they just don't yet flow into the eval context as named metadata keys. When they do, `latency_budget` will accept per-metric thresholds (`max_ttfb_ms`, `max_first_audio_ms`, …); the existing single-metric usage continues to work.

## CI gate

The mock-provider path runs keyless, so the demo fits a fork-safe CI job:

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

For real-provider runs, gate on the same step with provider keys via `secrets:`, and bump `max_ms` to a value that reflects your production budget.

## Extending it

- **Tighter budgets per turn**: vary `max_ms` per turn — the greeting might be fast, the tool-using turn slower.
- **Mix with content assertions**: combine `latency_budget` with `content_includes` / `llm_judge` to test both correctness and speed.
- **Per-provider thresholds**: when running across providers, the assertion config can include `when:` clauses to apply different budgets per provider.
