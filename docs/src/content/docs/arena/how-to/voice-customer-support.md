---
title: Test a Voice Customer Support Agent with Self-Play
description: Drive a voice agent through a multi-turn customer support call with scripted personas, runtime tools, and per-turn assertions.
---

This how-to walks through the `examples/voice-refund-demo/` scenario set: a refund-support agent under voice, driven by four scripted customer personas, with conversation-level assertions on the tools the agent must (and must not) call. The same scenarios run live in `promptarena serve`, in the TUI, and headlessly in `run --ci` — three surfaces, one config.

## What it proves

Voice agents are hard to evaluate because the failure modes are conversational, not single-turn: did the agent verify warranty before discussing a refund? Did it cave to pressure? Did it escalate when policy required it?

PromptArena makes the conversation a first-class test subject:

- A scripted **persona** (driven by a text LLM) initiates and sustains the call. Four personas ship with the example — aggressive, impersonator, anxious, patient — exercising hostile and cooperative paths.
- A **realtime voice agent** (OpenAI GPT-4o Realtime, Gemini Live) receives the persona's audio and responds with audio and tool calls.
- **Runtime tools** execute for real (mock-backed for the demo) — the agent's `lookup_order`, `check_warranty_status`, `issue_refund`, `escalate_to_human` calls hit real handlers and produce real results that feed back into the conversation.
- **Conversation-level assertions** check the pattern: which tools fired, with what args, in what order, plus per-turn content checks. These are pass/fail signals, not just LLM-graded scores.

The differentiator: voice + scripted multi-turn user + runtime tools + structured assertions, in a single config. Competitor frameworks either do single-turn text eval, or audio eval without scripted users, or scripted users without tools — none combine all four.

## Run it

```bash
cd examples/voice-refund-demo
promptarena serve
```

`serve` opens the web UI, loads the four scenarios, and lets you play each one back. For voice scenarios, the timeline aligns audio with tool calls and assertion outcomes — you hear the agent's reply and see the tool firings on the same axis.

For headless / CI use:

```bash
promptarena run --ci --formats html,json
open out/report.html
```

For the dev loop (live transcript + tool table updating per turn):

```bash
promptarena run --tui
```

## What you'll see in the report

Per scenario:

- **Aggressive caller** — hostile out-of-warranty refund demand. Pass = agent calls `lookup_order` + `check_warranty_status`, refuses the refund (no `issue_refund` call), and escalates via `escalate_to_human`. Fail = any unauthorized refund or skipped verification.
- **Impersonator** — caller supplies a fake order ID. Pass = agent attempts `lookup_order`, fails, escalates rather than guessing.
- **Patient (baseline)** — genuine in-warranty defect. Pass = full happy path, refund issued.
- **Anxious delivery** — anxious customer can't find a delivered parcel. Pass = agent looks up the order, sees it was delivered with tracking, reassures.

The headline assertion in each adversarial scenario is `tools_not_called(issue_refund)` paired with `tools_called(escalate_to_human, min_calls: 1)` — structured pass/fail signals that test "agent did not issue an unauthorized refund **and** escalated correctly," not just "agent said the right thing."

## Required API keys

Selfplay drives the persona via a real text LLM, so a fully-mocked end-to-end run with passing assertions isn't possible from the demo's current scenarios. Minimum keys for the **real-provider** path:

| Key | Used for |
|-----|----------|
| `OPENAI_API_KEY` | Text LLM that drives selfplay personas; OpenAI Realtime if you run that agent |
| `GEMINI_API_KEY` | Gemini Live (the voice agent under test) |
| `CARTESIA_API_KEY` | Cartesia TTS for two of the personas |
| `ELEVENLABS_API_KEY` | ElevenLabs v3 TTS for the anxious-delivery persona |

The patient-baseline scenario uses OpenAI's `nova` voice, so it works with just `OPENAI_API_KEY` + `GEMINI_API_KEY` (no Cartesia / ElevenLabs).

## CI gate

Wire as a GitHub Actions quality gate. The scenarios that exercise the live voice path need provider keys; for CI smoke-testing without keys, run `promptarena validate` instead (loads and validates all configs without making provider calls):

```yaml
# .github/workflows/voice-refund-demo.yml
name: Voice Refund Demo

on:
  pull_request:
    paths:
      - 'examples/voice-refund-demo/**'

jobs:
  validate:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      - uses: actions/setup-go@v5
        with:
          go-version: '1.26'
      - run: make build-arena
      - name: Validate configs (no provider keys needed)
        working-directory: examples/voice-refund-demo
        run: ../../bin/promptarena validate config.arena.yaml

  run-against-real-providers:
    runs-on: ubuntu-latest
    if: github.event.pull_request.head.repo.full_name == github.repository
    steps:
      - uses: actions/checkout@v4
      - uses: actions/setup-go@v5
        with:
          go-version: '1.26'
      - run: make build-arena
      - name: Run scenarios against real providers
        working-directory: examples/voice-refund-demo
        env:
          OPENAI_API_KEY: ${{ secrets.OPENAI_API_KEY }}
          GEMINI_API_KEY: ${{ secrets.GEMINI_API_KEY }}
          CARTESIA_API_KEY: ${{ secrets.CARTESIA_API_KEY }}
          ELEVENLABS_API_KEY: ${{ secrets.ELEVENLABS_API_KEY }}
        run: ../../bin/promptarena run --ci --formats html,json
      - name: Upload report
        if: always()
        uses: actions/upload-artifact@v4
        with:
          name: arena-report
          path: examples/voice-refund-demo/out/
```

The fork-aware `if:` check on the real-provider job prevents the secret-bearing job from running on PRs from external forks.

## Extending it

- **Add a persona**: create a new file under `personas/`, anchor it to one of the existing order IDs (see `tools/*.tool.yaml` for the mock_template branches), and add a new scenario in `scenarios/` referencing it.
- **Add a product / warranty case**: extend the `mock_template` branches in `tools/lookup-order.tool.yaml`, `tools/check-warranty-status.tool.yaml`, and `tools/issue-refund.tool.yaml`. No code changes needed.
- **Swap the agent under test**: change `provider:` in any scenario file (or pass `--provider` on the CLI). Available providers in the example: `gemini-2-flash`, `openai-gpt4o-realtime`, plus the `mock-duplex` for pipeline smoke testing.

## Why this matters

Voice agent failure modes are not LLM-text failures — they include "the agent skipped a required tool," "the agent kept talking past the customer," "the agent agreed to something outside policy under pressure." None of those show up reliably in single-turn or replay-based eval. Self-play surfaces them by sustaining the conversation with realistic adversarial pressure across multiple turns, and structured assertions catch them as pass/fail signals instead of as soft scores.
