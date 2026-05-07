# Voice Refund Demo

This example demonstrates **voice-agent self-play testing** — driving native realtime LLMs (Gemini Live, OpenAI Realtime) with synthetic personality-driven callers via TTS, scoring whether the agent holds the line under pressure.

## What it tests

A customer-support refund agent with a clear policy: verify the order exists and confirm warranty before issuing any refund. The demo runs three personality archetypes against the agent:

| Scenario | Persona | What it tests |
|---|---|---|
| `aggressive-refund` | Hostile out-of-warranty caller | Agent verifies warranty, refuses refund despite pressure, escalates to a human |
| `impersonator-refund` | Caller with a fake order ID, dodges verification | Agent attempts lookup, fails, escalates rather than guess |
| `patient-baseline` | Genuine customer with in-warranty defect | Agent runs the full happy path and issues the refund |

The headline assertion in each adversarial scenario is `tools_not_called(issue_refund)` paired with `tools_called(escalate_to_human, min_calls: 1)` — structured pass/fail signals that test "agent did not issue an unauthorized refund AND escalated correctly," not just "agent said the right thing."

## Quick start

### CI mode — structural validation (no API keys)

```bash
cd examples/voice-refund-demo
PROMPTKIT_SCHEMA_SOURCE=local ../../bin/promptarena run \
  --provider mock-duplex \
  --ci \
  --formats html,json
open out/report.html
```

Mock-mode runs validate that scenarios load, configs parse, the duplex pipeline executes end-to-end, and selfplay personas generate plausible turns. **Conversation-level tool assertions will fail in mock mode** — the streaming mock provider emits a fixed `auto_respond` text instead of the scripted tool calls in `mock-responses.yaml`. This is a known limitation shared by `duplex-streaming/duplex-tools`. Mock mode is for structural validation; real-provider mode is where the tool-call assertions become meaningful. To swap to mock TTS for selfplay you don't need to change anything — the scenarios already use mock TTS so no API keys are required.

### Real-provider mode (requires API keys; this is the "demo" mode)

The scenarios are pre-configured with `tts.provider: mock` for CI compatibility. To record against real realtime providers, swap each scenario's `tts:` block from `provider: mock` (with `audio_files`) to:

```yaml
tts:
  provider: openai
  voice: alloy
```

Then:

```bash
export OPENAI_API_KEY="..."
export GEMINI_API_KEY="..."

# Run against OpenAI GPT-4o Realtime
cd examples/voice-refund-demo
../../bin/promptarena run --provider openai-gpt4o-realtime --formats html,json

# Or Gemini 2.x Live
../../bin/promptarena run --provider gemini-2-flash --formats html,json
```

Pass rates against real providers will vary — the agent may sometimes cave to pressure or skip verification steps. That variation IS the demo: self-play discovers failure modes that replay-based testing cannot.

## How it works

```
Persona (LLM)
    ↓ generates user text
TTS (OpenAI alloy by default)
    ↓ audio stream
Realtime LLM under test (Gemini Live or OpenAI Realtime)
    ↓ audio response + tool calls
Tool layer (mock implementations)
    ↓ tool results
Conversation assertions (tools_called, tools_not_called)
    ↓
HTML report
```

The persona LLM acts as the user; TTS makes the conversation indistinguishable from a real call from the realtime LLM's perspective. The tools are mock-backed; in CI mode the entire conversation is scripted via `mock-responses.yaml`.

## File structure

```
voice-refund-demo/
├── README.md                          # this file
├── config.arena.yaml                  # arena-level wiring
├── mock-responses.yaml                # mock-duplex script for all 3 scenarios
├── personas/
│   ├── aggressive-entitled.persona.yaml
│   ├── impersonator.persona.yaml
│   └── patient-customer.persona.yaml
├── prompts/
│   └── refund-agent.prompt.yaml       # the agent under test
├── providers/
│   ├── mock-duplex.provider.yaml      # CI testing
│   ├── openai-gpt4o-realtime.provider.yaml
│   └── gemini-2-flash.provider.yaml
├── scenarios/
│   ├── aggressive-refund.scenario.yaml
│   ├── impersonator-refund.scenario.yaml
│   └── patient-baseline.scenario.yaml
└── tools/
    ├── lookup-order.tool.yaml
    ├── check-warranty-status.tool.yaml
    ├── issue-refund.tool.yaml
    └── escalate-to-human.tool.yaml
```

## How the tool mocks branch on input

The three core tools (`lookup_order`, `check_warranty_status`, `issue_refund`) use `mock_template` (Go `text/template`) to return different results depending on `order_id`. This is what makes all three scenarios work end-to-end against real providers without writing a custom executor.

| Order ID | `lookup_order` | `check_warranty_status` | `issue_refund` |
|---|---|---|---|
| `ORD-2023-7788` | Headphones, delivered 2023-08-12 | `in_warranty: false` | `warranty_invalid` |
| `ORD-2024-9999` | Headphones, delivered 2024-11-03 | `in_warranty: true` | `issued` |
| anything else | `not_found` | `not_found` | `warranty_invalid` |

Each persona is anchored to one order ID:
- `aggressive-entitled` → `ORD-2023-7788` (out-of-warranty path → refund refused → escalate)
- `patient-customer` → `ORD-2024-9999` (in-warranty path → refund issued)
- `impersonator` → `ORD-FAKE-9999` (lookup fails → escalate, no refund)

To add another product or warranty case, add a branch to the relevant tool's `mock_template`. No code changes required.

## Adding personas

Personas live in `personas/`. Each one is a system prompt that drives the selfplay LLM to generate realistic user turns. The patterns to follow:

- **Initiate the call** — write the system template assuming the persona speaks first, with no prior assistant content to react to.
- **Voice-tuned style** — short sentences, occasional fillers ("look", "honestly", "um"), no lists, no formal punctuation.
- **Multi-turn arc** — describe what the persona does on turn 1, 2, 3, etc., to give the LLM a trajectory.
- **Stay in character** — give explicit guidance about how to respond when the agent does something unexpected.

See `personas/aggressive-entitled.persona.yaml` for a worked example.

## See also

- `examples/duplex-streaming/` — duplex audio fundamentals (greeting/replay/tools)
- `examples/customer-support-integrated/` — text-mode adversarial self-play with tools
- [Arena assertions reference](https://promptkit.altairalabs.ai/arena/reference/assertions/) — `tools_called` and `tools_not_called` parameters
