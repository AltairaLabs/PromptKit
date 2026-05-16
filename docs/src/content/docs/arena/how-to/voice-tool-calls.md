---
title: Test voice agents that call tools mid-conversation
description: Assert that a voice agent calls the right tools, with the right arguments, in the right order — during a duplex audio conversation.
---

This how-to walks through the tool-calling scenarios in `examples/duplex-streaming/`. The differentiator: tool calls happen *during* a real-time voice conversation, and the assertions catch them at conversation level — not in isolation.

## What it proves

Voice agents that call tools have two interacting failure modes:

- **Conversation failure** — the agent talks fine but never reaches for the tool when it should.
- **Tool failure** — the agent calls the right tool but with wrong arguments, or hangs because the result comes back during ongoing audio output.

Pure text eval misses the first because it doesn't sustain the conversation; pure tool-call eval misses the second because it doesn't run under the audio pipeline. PromptArena does both in one scenario:

- A scripted persona drives a sustained voice conversation via self-play + TTS.
- The voice agent under test (OpenAI Realtime, Gemini Live) handles audio and decides when to call tools.
- The tool registry executes real handlers — mock-backed for the demo, real services for production.
- **Conversation-level assertions** check the tool-call pattern: which tools fired, with what args, in what order, with what results.

## Run it

```bash
cd examples/duplex-streaming
promptarena serve
```

`serve` loads all duplex-streaming scenarios. The tool-call scenario is `duplex-tools` — a busy-professional persona that asks about weather, calendar, and reminders, exercising three tools in a single conversation.

For headless / CI use:

```bash
# Real provider (requires GEMINI_API_KEY or OPENAI_API_KEY)
promptarena run --scenario duplex-tools --provider gemini-2-flash --ci

# Mock provider (no keys; smoke-tests the pipeline; see "Mock mode limits" below)
promptarena run --scenario duplex-tools --provider mock-duplex --ci
```

## The assertion shape

`examples/duplex-streaming/scenarios/duplex-tools.scenario.yaml`:

```yaml
turns:
  - role: user
    parts:
      - type: audio
        media:
          file_path: audio/greeting.pcm
          mime_type: audio/L16
    assertions:
      - type: content_matches
        params:
          pattern: "(?i)(hello|hi|help|assist)"

  - role: selfplay-user
    persona: busy-professional
    turns: 4

conversation_assertions:
  - type: tools_called
    params:
      tool_names: [get_weather]
      min_calls: 1
  - type: tools_called
    params:
      tool_names: [get_calendar_events]
      min_calls: 1
  - type: tools_called
    params:
      tool_names: [set_reminder]
      min_calls: 1
```

The headline checks live at the conversation level: each of the three tools fired at least once over the four-turn self-play conversation. Layering in `tool_calls_with_args` or `tool_call_sequence` lets you tighten the contract:

```yaml
- type: tool_calls_with_args
  params:
    tool_name: set_reminder
    expected_args:
      time: "9am"
- type: tool_call_sequence
  params:
    sequence: [get_calendar_events, set_reminder]
```

## CI gate

```yaml
# .github/workflows/voice-tool-calls.yml
name: Voice tool calls

on:
  pull_request:
    paths:
      - 'examples/duplex-streaming/**'

jobs:
  validate:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      - uses: actions/setup-go@v5
        with:
          go-version: '1.26'
      - run: make build-arena
      - name: Validate duplex configs
        working-directory: examples/duplex-streaming
        run: ../../bin/promptarena validate config.arena.yaml

  run-against-gemini:
    runs-on: ubuntu-latest
    if: github.event.pull_request.head.repo.full_name == github.repository
    steps:
      - uses: actions/checkout@v4
      - uses: actions/setup-go@v5
        with:
          go-version: '1.26'
      - run: make build-arena
      - name: Run duplex-tools scenario
        working-directory: examples/duplex-streaming
        env:
          GEMINI_API_KEY: ${{ secrets.GEMINI_API_KEY }}
          OPENAI_API_KEY: ${{ secrets.OPENAI_API_KEY }}
        run: ../../bin/promptarena run --scenario duplex-tools --provider gemini-2-flash --ci --formats html,json
```

The fork-aware `if:` check keeps the secret-bearing job from running on PRs from external forks.

## Mock mode limits

The mock-duplex provider emits a fixed `auto_respond` text instead of executing the scripted tool calls in `mock-responses.yaml`. That means conversation-level tool-call assertions fail in mock mode — useful for smoke-testing the pipeline (does everything wire up, does the conversation execute end-to-end), not for asserting correctness.

For deterministic mock runs where assertions can pass, see [examples/voice-ivr/](/arena/how-to/voice-ivr/), which uses a text-mode mock provider that does execute scripted tool calls.

A fully-mocked duplex provider that scripts both audio output and tool calls is a planned extension; until it lands, real providers are the only path to assertion-passing tool-call evaluation under voice.

## Extending it

- **More tools**: drop a new `.tool.yaml` into `tools/`, reference it in `config.arena.yaml`, mention it in the system prompt at `prompts/voice-assistant-tools.prompt.yaml`.
- **Argument validation**: replace `tools_called` with `tool_calls_with_args` and specify `expected_args` for the values the agent must pass.
- **Ordering**: add `tool_call_sequence` to assert the order the agent calls them in.
- **No-call assertions**: pair with `tools_not_called` to catch tools the agent should *not* invoke in a given path (e.g., `set_reminder` on a read-only query).
