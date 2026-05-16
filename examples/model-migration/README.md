# Model Migration Regression Demo

Run the same scenarios against multiple model versions; gate on a common assertion bar. If any model fails an assertion the others pass, the migration introduced a regression.

## What it tests

Two scenarios (`billing-inquiry`, `tech-inquiry`) registered with two mock providers simulating an upgrade (`gpt-4o-2024-08` → `gpt-4o-mini-2024-12`). Each scenario classifies a support query into a single category word; assertions check the right category appears AND the response is short (no explanation).

Both providers pass in the default config. The how-to walks through what a regression would look like (e.g., a newer model that adds explanatory text despite the one-word instruction) and how to catch it in CI.

## Running

```bash
cd examples/model-migration
../../bin/promptarena run --ci --formats html,json
open out/report.html
```

The HTML report groups runs by provider. Expand each scenario to see the per-model output. A regression shows as a red cell on one model and a green cell on the others.

Live dev loop:

```bash
../../bin/promptarena serve
../../bin/promptarena run --tui
```

## Adding real models

Swap the mock providers for real ones to gate a real migration:

```yaml
providers:
  - file: providers/openai-gpt4o.provider.yaml
  - file: providers/openai-gpt4o-mini.provider.yaml
  - file: providers/anthropic-claude-haiku.provider.yaml
```

Same scenarios, same assertions. The report shows which model passes which test; CI exits non-zero if any cell fails.

## File layout

```
model-migration/
├── README.md
├── config.arena.yaml
├── mock-responses-v1.yaml         # gpt-4o-2024-08 baseline
├── mock-responses-v2.yaml         # gpt-4o-mini-2024-12 candidate
├── prompts/support-classifier.yaml
├── providers/
│   ├── model-v1.provider.yaml
│   └── model-v2.provider.yaml
└── scenarios/
    ├── billing-inquiry.scenario.yaml
    └── tech-inquiry.scenario.yaml
```
