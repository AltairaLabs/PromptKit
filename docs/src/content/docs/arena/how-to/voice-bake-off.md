---
title: Run the same scenario across multiple providers
description: Side-by-side provider comparison without leaving the scenario file. One scenario, N providers, one report — pick the winner on real metrics.
---

This how-to walks through `examples/voice-bake-off/` — the same three-turn support call registered twice (`mock-fast`, `mock-slow`), with per-turn `latency_budget` and `max_length` assertions on each. Arena fans the scenario out automatically; the HTML report shows the providers side by side.

## What it proves

Provider selection for voice is hard because the failure modes are model-specific: one provider is faster but rambles; another is slower but tighter; a third is great at tool calls but slow on first-audio. Static comparison charts go stale; running the same scenario across providers is the only way to evaluate them on *your* prompt, *your* tools, *your* assertions.

PromptArena makes that a one-line change. Register multiple providers in `config.arena.yaml`; every scenario fans out across all of them. Assertions run per-provider, the report shows them on one axis, and you can tune thresholds per provider with `when:` clauses.

## Run it

```bash
cd examples/voice-bake-off
promptarena serve
```

The web UI loads the scenario and shows runs grouped by provider. Click each provider to see its responses and metrics in the timeline view.

Headless:

```bash
promptarena run --ci --formats html,json
open out/report.html
```

The HTML report's run table groups by provider. Expand each turn to see the response and the per-turn `latency_ms` / response length from each provider — the side-by-side comparison.

## The fan-out

In `config.arena.yaml`:

```yaml
providers:
  - file: providers/mock-fast.provider.yaml
  - file: providers/mock-slow.provider.yaml
```

That's it. The scenario doesn't mention providers; Arena pairs every scenario with every registered provider and runs each combination. To add a third provider, drop in a new provider YAML file and add one more line.

## Per-provider thresholds

If a provider has tighter or looser real-world limits, narrow the assertion with `when:`:

```yaml
- type: latency_budget
  params:
    max_ms: 800
  when:
    provider: openai-realtime
- type: latency_budget
  params:
    max_ms: 1500
  when:
    provider: gemini-live
```

Useful for migration testing ("we're moving from Gemini Live to OpenAI Realtime — does the new provider stay inside the same budget on our actual prompts?") and for cross-provider bake-offs where each has a different baseline.

## Adding real voice providers

The default config uses text mock providers so the demo runs keyless. To run across real voice providers:

1. Add provider configs — e.g. `providers/openai-realtime.provider.yaml`:

   ```yaml
   apiVersion: promptkit.altairalabs.ai/v1alpha1
   kind: Provider
   metadata:
     name: openai-realtime
   spec:
     id: openai-realtime
     type: openai
     model: gpt-4o-realtime-preview
     audio_modality: true
   ```

2. Register them in `config.arena.yaml` under `providers:`.
3. Add a `duplex:` block to the scenario (see `examples/voice-refund-demo/scenarios/*.yaml`).
4. Run with provider keys.

## CI gate

```yaml
# .github/workflows/voice-bake-off.yml
name: Voice bake-off

on:
  pull_request:
    paths:
      - 'examples/voice-bake-off/**'

jobs:
  test:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      - uses: actions/setup-go@v5
        with:
          go-version: '1.26'
      - run: make build-arena
      - name: Run bake-off scenarios
        working-directory: examples/voice-bake-off
        run: ../../bin/promptarena run --ci --formats html,json
      - name: Upload report
        if: always()
        uses: actions/upload-artifact@v4
        with:
          name: bake-off-report
          path: examples/voice-bake-off/out/
```

Uploading the report as an artifact lets reviewers eyeball the per-provider responses on the PR — useful when "did the migration regress on prompt X?" is the question.

## Why this matters

Most eval frameworks treat "compare providers" as a separate workflow with its own UI. PromptArena treats it as a property of the scenario set: one scenario, N providers, one report. Adding a provider is one YAML line. The fan-out shape is the same whether you have two mock providers (this demo) or three real duplex providers (the live version). The scenarios don't change as you move from synthetic to real to migration testing — only the registered providers list does.
