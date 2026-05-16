# Text Negotiation Demo

Multi-turn text negotiation: a tenant probes a landlord agent over four turns of rent haggling. The default config runs deterministically against a mock provider (scripted landlord responses) — assertions check that the deal lands at the right price with the right commitment.

## What it tests

A `rental-negotiation` scenario, four turns: tenant opens with an interest question, lowballs at $2300 (below the landlord's $2400 reservation), counter-offers at $2400, then accepts $2450. The mock landlord stays above the reservation price and secures the 12-month lease.

Conversation-level assertions:

- **Outcome** — final deal contains "$2450" (deal lands above reservation).
- **Commitment** — "twelve-month" appears (lease term secured).
- **Hygiene** — no explicit capitulation phrases like "I accept twenty-three hundred."

## Running

```bash
cd examples/text-negotiation
../../bin/promptarena run --ci --formats html,json
open out/report.html
```

Live dev loop:

```bash
../../bin/promptarena serve
../../bin/promptarena run --tui
```

## Switching to real self-play

The default scenario uses **scripted user turns** for the tenant — a faithful four-turn negotiation script. Real self-play uses `role: selfplay-user` (or `role: gemini-user` etc.) with a persona, driven by a real text LLM that sees the conversation and generates new content per turn.

To convert the demo:

1. Add a persona (`personas/savvy-tenant.persona.yaml`) describing the negotiator's strategy.
2. Add a provider for the persona's LLM (real text provider — `providers/openai-gpt4o-mini-text.provider.yaml`).
3. Replace the scripted user turns with one `selfplay-user` block:

```yaml
turns:
  - role: selfplay-user
    persona: savvy-tenant
    turns: 4
```

4. Run with `OPENAI_API_KEY` (or equivalent) in your environment. Each persona-side turn becomes LLM-generated, the landlord (still mock or also a real LLM) responds, and the conversation evolves dynamically.

The assertions stay the same — they check the conversation's outcome, not which path got there.

## File layout

```
text-negotiation/
├── README.md
├── config.arena.yaml
├── mock-responses.yaml
├── prompts/landlord-agent.yaml
├── providers/mock-provider.yaml
└── scenarios/rental-negotiation.scenario.yaml
```
