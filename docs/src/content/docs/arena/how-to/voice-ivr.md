---
title: Test a Voice IVR with a Workflow State Machine
description: Pair PromptKit's workflow primitive with the voice harness to drive an IVR through scripted state transitions and assert the tool-call pattern of each path.
---

This how-to walks through `examples/voice-ivr/` — a workflow-driven bank IVR that uses PromptKit's workflow state machine to route callers to a self-service balance lookup or a human-agent handoff. The demo runs deterministically against a mock provider (no API keys); the same scenarios work against a live duplex voice provider with a one-line config swap.

## What it proves

Voice IVRs have structural failure modes that pure single-turn eval can't catch: did the agent verify identity before discussing the account, did it route to the right terminal state, did it call only the tools that the current state permits? PromptArena lets you express that as a state machine in the pack and assert the resulting tool-call pattern from scenarios.

- A **workflow** primitive in `config.arena.yaml` defines the IVR shape: an entry `verifying` state that branches via `ServeBalance` (self-service) or `EscalateToAgent` (human handoff) to terminal states.
- The agent under test calls `workflow__transition` to drive the machine. Each transition fires deferred-commit during execution and lands at end-of-turn — visible in the HTML report timeline.
- Tools run for real: `lookup_account`, `check_balance`, `transfer_to_agent` are mock-backed handlers that produce real results that feed back into the conversation.
- **Conversation-level assertions** check the tool-call pattern (`tools_called`, `tools_not_called`) per scenario. The balance scenario fails if the agent transfers to a human; the handoff scenario fails if the agent fetches a balance.

The differentiator: a workflow state machine plus voice plus runtime tool execution plus structured assertions, all in one config. Competitor frameworks either skip workflow entirely or treat it as opaque execution state with no test-side visibility.

## Run it

```bash
cd examples/voice-ivr
promptarena serve
```

`serve` opens the web UI and loads both scenarios. The timeline view shows each tool call (including `workflow__transition`) on the same axis as the agent's response, so the state machine progress is visible at a glance.

For headless / CI:

```bash
promptarena run --ci --formats html,json
open out/report.html
```

For the dev loop:

```bash
promptarena run --tui
```

All three surfaces share the same config; the mock provider runs deterministically so CI is stable.

## How the assertions work

Each scenario lives at `examples/voice-ivr/scenarios/*.scenario.yaml`. The pattern:

```yaml
conversation_assertions:
  - type: tools_called
    params:
      tool_names: ["lookup_account"]
      min_calls: 1
    message: "Agent must verify identity before serving account data"
  - type: tools_called
    params:
      tool_names: ["check_balance"]
      min_calls: 1
  - type: tools_not_called
    params:
      tool_names: ["transfer_to_agent"]
    message: "Agent must NOT transfer the caller — this is self-service"
```

The pack's workflow definition (`config.arena.yaml`) handles the state machine; the scenarios assert what the agent must (and must not) do along the way. State transitions are visible in the HTML report alongside the tool-call timeline.

## CI gate

The mock-provider path runs without API keys, so this fits a fork-safe CI job:

```yaml
# .github/workflows/voice-ivr.yml
name: Voice IVR

on:
  pull_request:
    paths:
      - 'examples/voice-ivr/**'

jobs:
  test:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      - uses: actions/setup-go@v5
        with:
          go-version: '1.26'
      - run: make build-arena
      - name: Run voice-ivr scenarios
        working-directory: examples/voice-ivr
        run: ../../bin/promptarena run --ci --formats json
      - name: Upload report
        if: always()
        uses: actions/upload-artifact@v4
        with:
          name: voice-ivr-report
          path: examples/voice-ivr/out/
```

## Switching to live voice

The scenarios are voice-agnostic by design — to drive the same workflow through a duplex provider:

1. Add a duplex provider (e.g., `providers/openai-realtime.provider.yaml`) and register it in `config.arena.yaml` under `providers:`.
2. Add a `duplex:` block to each scenario (see `examples/voice-refund-demo/scenarios/*.yaml` for the shape).
3. Optionally swap the scripted text user turns for `role: selfplay-user` with personas — again, `voice-refund-demo` is the reference.
4. Run with provider keys in your environment.

The workflow definition, tools, prompts, and assertions stay the same; only the I/O layer changes.

## Extending

- **Add another self-service path** (recent transactions, transfer initiated, fraud alert): define a new terminal state in `config.arena.yaml`, add an `on_event:` mapping in `verifying`, add a prompt and scenario.
- **Add an intermediate state** (multi-factor auth challenge, identity escalation): insert between `verifying` and the terminal states. The `workflow_tool_access` assertion can constrain which tools each state permits.
- **Stricter assertions**: add `tool_call_sequence` to assert the order of tool calls (`lookup_account` before `check_balance`), or `tool_calls_with_args` to assert specific argument values.
