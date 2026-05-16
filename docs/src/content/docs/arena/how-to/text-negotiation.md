---
title: Test agent negotiation with scripted or self-play opponents
description: Two agents over many turns, with conversation-outcome assertions. Same scenario shape works for scripted CI and real LLM-driven self-play.
---

This how-to walks through `examples/text-negotiation/` — a four-turn rental-price negotiation. The default config runs deterministically against a mock landlord; the how-to documents the swap to real LLM-driven self-play.

## What it proves

Single-turn eval misses negotiation entirely: the failure mode is *trajectory*, not response quality. Did the agent cave under pressure? Did it secure the term commitment? Did it stop above its reservation price?

PromptArena tests negotiations as conversations:

- **Both sides** are first-class. The tenant side is either scripted (deterministic CI) or `selfplay-user` with a persona (real LLM-driven). The landlord side is your prompt config.
- **Multi-turn** is the default. Scenarios encode a sequence of user turns; conversation_assertions evaluate the end state.
- **Outcome assertions** catch what matters: did the deal land above the reservation, did the term commitment get secured, did either side make a forbidden capitulation move.

## Run it

```bash
cd examples/text-negotiation
promptarena serve
```

`serve` loads the scenario. The TUI is good for the dev loop:

```bash
promptarena run --tui
```

Headless / CI:

```bash
promptarena run --ci --formats html,json
open out/report.html
```

Keyless: the default config uses a mock landlord with scripted responses.

## The assertion shape

```yaml
conversation_assertions:
  - type: content_includes
    params:
      patterns: ["twenty-four fifty"]
    message: "Final deal must land at the negotiated price"

  - type: content_includes
    params:
      patterns: ["twelve-month"]
    message: "Final deal must include the 12-month commitment"

  - type: content_excludes
    params:
      patterns: ["I accept twenty-three hundred", "twenty-three hundred works"]
    message: "Landlord must not capitulate on the below-reservation offer"
```

All three at conversation level — they check the *end state*, not per-turn behaviour. Negotiations succeed or fail by the deal that lands; per-turn checks tend to over-constrain the agent.

For stricter contracts, layer in `llm_judge_session` with criteria over the full conversation:

```yaml
- type: llm_judge_session
  params:
    criteria: |
      Did the landlord maintain a professional negotiation posture
      across all turns, avoiding capitulation while reaching an
      acceptable deal? Score 1.0 if yes.
    judge: default
    min_score: 0.8
```

(That assertion needs a judge provider — see [the assertion catalog](/reference/checks/) for the standard judge params.)

## Switching to real self-play

The default scenario uses scripted user turns. To run with a real LLM driving the tenant side:

1. Add a persona file:

   ```yaml
   # personas/savvy-tenant.persona.yaml
   apiVersion: promptkit.altairalabs.ai/v1alpha1
   kind: Persona
   metadata:
     name: savvy-tenant
   spec:
     id: savvy-tenant
     description: "Cost-conscious tenant negotiating rent."
     system_template: |
       You are a tenant negotiating monthly rent. Target $2300/month
       with a 12-month lease. Be polite but firm; don't accept above
       $2500. If the landlord won't budge below $2500, walk away.
   ```

2. Add a real text-LLM provider:

   ```yaml
   # providers/openai-gpt4o-mini-text.provider.yaml
   apiVersion: promptkit.altairalabs.ai/v1alpha1
   kind: Provider
   metadata:
     name: openai-gpt4o-mini-text
   spec:
     id: openai-gpt4o-mini-text
     type: openai
     model: gpt-4o-mini
   ```

3. Replace the scripted user turns with a `selfplay-user` block:

   ```yaml
   turns:
     - role: selfplay-user
       persona: savvy-tenant
       turns: 4
   ```

4. Run with `OPENAI_API_KEY` in your environment.

The assertions stay the same. The persona-driver LLM generates new content per turn; the landlord responds; the conversation evolves dynamically. The report shows each persona-driven turn — useful for spotting cases where the persona behaves unexpectedly.

## CI gate

```yaml
# .github/workflows/text-negotiation.yml
name: Text negotiation

on:
  pull_request:
    paths:
      - 'examples/text-negotiation/**'

jobs:
  test:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      - uses: actions/setup-go@v5
        with:
          go-version: '1.26'
      - run: make build-arena
      - name: Run text-negotiation
        working-directory: examples/text-negotiation
        run: ../../bin/promptarena run --ci --formats json
```

Keyless and fork-safe with the default scripted config.

## Extending it

- **Reservation-price assertions**: instead of asserting on specific phrases, parse the final agreed amount and assert numerically. `tool_calls_with_args` works well if the agent ends the negotiation with a `record_agreement(amount: ...)` tool call.
- **Walk-away scenarios**: scripted tenant offers $2000; assert the landlord ends the conversation. `content_includes` with words like "won't be able to make this work" / "not the right fit."
- **Adversarial tenants**: persona variants — aggressive, indecisive, deadline-pressured. Same landlord, different self-play personas, different outcome profiles.
