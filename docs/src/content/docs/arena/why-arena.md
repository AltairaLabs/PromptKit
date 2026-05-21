---
title: Why PromptArena
description: PromptArena is a testing framework for LLM-driven systems — voice, multi-agent, workflow-driven, retrieval-augmented. The product compared to eval-only frameworks like DeepEval, Ragas, Patronus, promptfoo, Braintrust, LangSmith.
sidebar:
  order: 2
---

PromptArena is a **testing framework** for LLM-driven systems. Eval primitives are inputs; assertions over those primitives are the output. The product covers the surface that eval-only frameworks structurally can't:

- **Voice** — duplex audio with self-play personas, runtime tools, and assertions that observe what got spoken and what got blocked.
- **Multi-turn agents** — scripted or self-play opponents over many turns, with conversation-outcome assertions.
- **Workflows** — state machines as first-class test subjects.
- **Multi-agent (A2A)** — supervisor + specialist delegation, asserted per-turn.
- **Guardrails as test signals** — production-side primitives observed via `guardrail_triggered`. Same code enforces in production and fires the test signal.

It also ships the **standard eval-primitive catalog** (faithfulness, hallucination, contextual recall, etc.) so buyers fluent in DeepEval / Ragas vocabulary find what they expect — but those primitives are inputs to the testing framework, not the product surface.

This page is a use-case-organized comparison: what PromptArena does that frameworks focused on single-turn eval can't.

## Test a voice agent end to end

Voice eval has the worst signal-to-noise ratio in the LLM-test world: the only thing single-turn frameworks can measure is the transcript, which misses everything voice-specific — turn-taking, latency, expressiveness, tool calls during audio, guardrail catches before TTS.

PromptArena tests voice as a full conversation:

- A scripted persona (LLM-driven via self-play, or scripted text) drives the user side. TTS makes it indistinguishable from a real call from the agent's perspective.
- The agent under test is a real duplex provider (OpenAI Realtime, Gemini Live) or a mock for CI.
- Per-turn latency budgets, content checks, and guardrail observation run on real signals.

Demos:

- [Voice customer support self-play](/arena/how-to/voice-customer-support/) — four personas drive a refund agent. Asserts the tool-call pattern.
- [Voice IVR with workflow](/arena/how-to/voice-ivr/) — workflow state machine routes the call. Asserts transitions.
- [Voice + tool calls](/arena/how-to/voice-tool-calls/) — agent calls tools mid-conversation under live audio.
- [Voice latency budget](/arena/how-to/voice-latency-budget/) — assert `latency_budget(max_ms: ...)` per turn.
- [Voice red-team](/arena/how-to/voice-red-team/) — adversarial personas probe safety guardrails.
- [Voice + guardrails](/arena/how-to/voice-guardrails/) — PII redaction observed via `guardrail_triggered`.
- [Voice provider bake-off](/arena/how-to/voice-bake-off/) — same scenario, multiple duplex providers, side-by-side report.
- [Expressive voice characterization](/arena/how-to/voice-characterization/) — bracket-tag persona markup lowered into each provider's native dialect.

## Test multi-turn agent conversations with scripted users

Single-turn evaluators miss the failure modes that only show up across multiple turns: agents that cave under pressure, agents that drift off-task, agents that loop. PromptArena's self-play makes the user side a first-class test subject — scripted text in CI, real LLM-driven personas in dev.

Demos:

- [Voice customer support self-play](/arena/how-to/voice-customer-support/) — multi-turn voice conversation with adversarial personas.
- [Voice red-team](/arena/how-to/voice-red-team/) — sustained adversarial pressure.
- [Text negotiation](/arena/how-to/text-negotiation/) — four-turn rental haggling with conversation-outcome assertions.

## Assert workflow state transitions

Workflow state machines aren't an eval concern in any other framework — they're treated as agent internals or skipped entirely. PromptArena ships a workflow primitive in the pack; scenarios assert state transitions and tool-call patterns per state.

Demos:

- [Voice IVR with workflow](/arena/how-to/voice-ivr/) — three-state IVR with the assertion pattern that catches misroutes.

## Observe runtime guardrails as test signals — the three-role model

This is the architectural differentiator. PromptArena keeps three roles distinct:

- **Eval** — primitive scoring function `(content) → result`. Stateless. Lives in `runtime/evals/handlers/`.
- **Guardrail** — eval applied as production enforcement. Wired in the pack's `validators:` block, runs in production AND in tests, mutates content.
- **Assertion** — eval or query applied as a test predicate. Observes guardrail firings via `guardrail_triggered`. Never mutates.

Same code, three roles. One implementation. Production catches in real time AND test observes the catch — from the same primitive.

This is the gap competing frameworks structurally can't fill:

- Guardrail-only systems (OpenAI / Anthropic content filters) catch in production but can't be tested without log scraping.
- Eval-only frameworks (DeepEval, Ragas, Patronus, Galileo) score transcripts post-hoc — by then the agent has already said the bad thing.

Demos:

- [Guardrails as test signals](/arena/how-to/guardrails-as-signals/) — the canonical demonstration: `banned_words` wired as a guardrail, asserted via `guardrail_triggered`.
- [Voice red-team](/arena/how-to/voice-red-team/) — the safety primitives (`pii_leakage`, etc.) applied under voice.
- [Voice + guardrails](/arena/how-to/voice-guardrails/) — focused PII redaction demo emphasising the runtime + test bridge.

## Run a real agent runtime under mock LLMs

Mock-LLM testing usually means mocked everything — tools, state machines, downstream services — which makes it impossible to catch integration bugs. PromptArena runs the **real runtime** (real tools, real workflow state machine, real guardrails) with the LLM mocked. The result: a deterministic test environment where the agent's structural behaviour gets exercised even without provider keys.

Every demo in this catalog uses this pattern. The CI snippets exit deterministically; the agent's tool-call patterns, workflow transitions, and guardrail firings are real.

## Test multi-agent A2A delegation

Multi-agent systems fail in two ways: the supervisor routes to the wrong specialist, or the right specialist produces wrong output. Single-agent eval frameworks miss the supervisor; agent-output eval misses the supervisor's contribution. PromptArena asserts both.

Demos:

- [A2A multi-agent](/arena/how-to/a2a-multi-agent/) — supervisor delegates research and translation to two specialised A2A agents; assertions catch routing and content per turn.

## Compare providers across the same scenario

Migrating models without a regression suite is how you ship bugs to production. PromptArena fans out scenarios across registered providers automatically; CI fails if any provider regresses on a shared assertion bar.

Demos:

- [Voice provider bake-off](/arena/how-to/voice-bake-off/) — one scenario, multiple voice providers, side-by-side report.
- [Model migration regression](/arena/how-to/model-migration/) — same scenarios across two model versions; CI catches behavioural drift.

## Test RAG with the standard primitives

The named RAG primitives every buyer searches for — `faithfulness`, `hallucination`, `contextual_precision`, `contextual_recall`, `contextual_relevancy`, `answer_relevancy` — ship as pure eval handlers and are exercisable as scenario assertions by wrapping each with `type: assertion` and a threshold. PromptArena's framing isn't "we ship the RAG primitives" — it's "we ship the testing framework that consumes them, on a live retrieval agent rather than a fixed transcript."

Demos:

- [RAG agent assertions](/arena/how-to/rag-agent/) — full named primitive suite as scenario assertions, keyless via a mock LLM judge.

## Wire it as a CI quality gate

The product is unfinished until the gate works. `promptarena run --ci` exits non-zero on any assertion failure; the fork-safe split pattern keeps secrets out of fork PRs while still gating internal merges on real-provider runs. Report artifacts give reviewers per-scenario / per-provider visibility into what failed.

Demos:

- [Arena CI quality gate](/arena/how-to/arena-ci-quality-gate/) — fork-safe split, threshold strategies per gate type, branch-protection wiring.
- [Integrate with CI/CD](/arena/how-to/integrate-ci-cd/) — broader CI integration story across GitHub Actions, GitLab CI, Jenkins.

## Where PromptArena does NOT lead

Where the eval-only frameworks already do well — single-turn text scoring on transcripts, score-and-report dashboards over recorded outputs, formal benchmark suites against named datasets — PromptArena is a peer rather than a leader. The product's value is in the assertion surface that the eval-only frameworks structurally don't cover: voice, multi-turn self-play, workflow, multi-agent, runtime guardrails observed in tests.

The standard eval catalog ships so you don't have to leave PromptArena for the commodity primitives — but the catalog isn't the moat. The moat is the testing framework that consumes them.
