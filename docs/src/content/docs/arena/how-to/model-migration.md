---
title: Gate model migrations on a regression suite
description: Run the same scenarios against the old and new model side by side. CI fails if the new model breaks an assertion the old one passed.
---

This how-to walks through `examples/model-migration/` — a small regression suite that runs the same scenarios against two mock providers simulating a model upgrade. The pattern works for any "did this swap break anything" question: GPT-4o → 4o-mini, Claude 3.5 → 4, OpenAI → Anthropic.

## What it proves

Migration testing has a sneaky failure mode: most prompts still work after a model swap, so you trust the change. The 5% that broke produce subtly wrong outputs that pass visual review and only get caught in production.

PromptArena makes migration a real gate:

- One scenario set, multiple providers registered, one report.
- Identical assertions per scenario; each must pass on every registered model.
- If any model fails an assertion the others passed, the report flags it and `run --ci` exits non-zero.
- Pin the suite in CI before the migration PR can merge.

## Run it

```bash
cd examples/model-migration
promptarena serve
```

The web UI groups runs by provider; expand a scenario to see each model's output and assertion results.

Headless / CI:

```bash
promptarena run --ci --formats html,json
open out/report.html
```

Keyless: both providers are mock. The report shows side-by-side results.

## What a regression looks like

The default config has both mock providers passing all assertions. To see how a regression surfaces, edit `mock-responses-v2.yaml` to make the new model break the one-word format:

```yaml
# Was: "technical"
# Now: model adds explanation despite the prompt's instruction
tech-inquiry:
  turns:
    1: "This sounds like a technical issue with the application."
```

Re-run: the `max_length` assertion fires on v2 but not v1 — the regression is caught:

```
v1 / billing-inquiry: ✓ content_includes(billing) ✓ max_length(<30)
v1 / tech-inquiry:    ✓ content_includes(technical) ✓ max_length(<30)
v2 / billing-inquiry: ✓ content_includes(billing) ✓ max_length(<30)
v2 / tech-inquiry:    ✓ content_includes(technical) ✗ max_length(60 > 30)
Error: execution failed: 1 runs had errors
```

The CI snippet below uses `set -e` so the migration PR fails until the prompt is reworked or the new model swapped out.

## Adding real models

Swap the mock providers in `config.arena.yaml`:

```yaml
providers:
  - file: providers/openai-gpt4o.provider.yaml         # incumbent
  - file: providers/openai-gpt4o-mini.provider.yaml    # candidate
  - file: providers/anthropic-claude-haiku.provider.yaml  # alternate candidate
```

Same scenarios, same assertions. The report fans out across every provider; CI gates on all of them passing.

## CI gate

```yaml
# .github/workflows/model-migration.yml
name: Model migration regression

on:
  pull_request:
    paths:
      - 'examples/model-migration/**'

jobs:
  test:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      - uses: actions/setup-go@v5
        with:
          go-version: '1.26'
      - run: make build-arena
      - name: Run migration regression suite
        working-directory: examples/model-migration
        run: ../../bin/promptarena run --ci --formats html,json
      - name: Upload report
        if: always()
        uses: actions/upload-artifact@v4
        with:
          name: model-migration-report
          path: examples/model-migration/out/
```

Uploading the report as an artifact lets reviewers eyeball the per-model output on the PR — useful when the question is "did this prompt regress on the new model?" rather than just "did anything fail?"

## Extending it

- **Add a third model**: drop in `providers/anthropic-claude-haiku.provider.yaml`, register it, run. The fan-out scales automatically.
- **Behavior-equivalent assertions**: `outcome_equivalent` lets you assert that the agent's tool-call pattern (or workflow state, or content hash) matches an expected outcome — useful for migrating between models without the prompt changing.
- **Per-model thresholds**: if a new model has known stricter / looser behaviour on certain assertions, use `when:` clauses to scope thresholds per provider (see [the bake-off how-to](/arena/how-to/voice-bake-off/) for the pattern).
