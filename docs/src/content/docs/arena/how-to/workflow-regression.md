---
title: Run workflow scenarios as a regression suite
description: Workflow state machines are first-class test subjects in PromptArena. Drive an agent through a stateful conversation, assert the transitions, and gate merges on the lifecycle reaching the expected end state.
---

This how-to walks through `examples/workflow-support/` and `examples/workflow-order-processing/` — two scenarios that use PromptKit's workflow primitive (state machine in the pack, `workflow__transition` tool the agent calls). Scenarios assert that the agent drives the expected lifecycle.

## What it proves

Workflow-driven agents fail in ways pure single-turn eval can't catch: the agent reaches the wrong terminal state, skips a required transition, or loops forever. Eval-only frameworks don't have a workflow concept at all — every demo of "agent state" ends up being a custom log scraper. PromptArena ships the state machine as a pack primitive and assertions can observe the transitions.

Two examples ship with the runtime:

- `examples/workflow-support/` — three-state support workflow (intake → specialist → closed) with two scenarios covering different paths.
- `examples/workflow-order-processing/` — four-state order lifecycle (new_order → payment → fulfillment → complete) with full and partial-flow scenarios.

## The assertion shape

Workflow scenarios drive a stateful agent across multiple turns. The agent calls `workflow__transition` to advance the state machine; the assertions observe that those calls happened.

Working pattern (used in both examples after this PR):

```yaml
conversation_assertions:
  - type: tools_called
    params:
      tool_names: [workflow__transition]
      min_calls: 3
    message: "Agent must drive three workflow transitions across the lifecycle"

  - type: tool_call_sequence
    params:
      sequence:
        - workflow__transition
        - workflow__transition
        - workflow__transition
    message: "Workflow transitions must occur in order"
```

`tools_called` checks the agent called the transition tool the expected number of times. `tool_call_sequence` enforces ordering. Both pass deterministically against the mock provider's scripted responses.

## Known limitation: state-aware assertions

PromptKit ships richer state-aware assertions (`state_is`, `transitioned_to`, `workflow_complete`, `workflow_transition_order`) that operate on the workflow metadata directly. These are currently affected by a bridge bug ([#1169](https://github.com/AltairaLabs/PromptKit/issues/1169)) — the workflow metadata isn't reaching the eval context, so the assertions report "no workflow state in context."

Both examples used to assert via those handlers and now use the tool-call shape above as a workaround. The state machine still runs (you can see the transitions in the log and the HTML report timeline); only the assertion-side bridge needs fixing. Once #1169 lands, the scenarios can move back to the state-aware assertion shape — the scenarios become tighter (asserting on actual state, not the tool call that drives it) but the runtime behaviour stays the same.

## Running

```bash
cd examples/workflow-support
../../bin/promptarena run --ci --formats html,json
open out/report.html
```

The HTML report's timeline view shows each transition with its from/to states and the event that triggered it. Even with the state-aware assertions disabled, the transition history is fully observable in the report.

Same shape for the order-processing example:

```bash
cd examples/workflow-order-processing
../../bin/promptarena run --ci --formats html,json
```

## What's a regression suite, then?

Two concrete shapes:

1. **Did the agent drive the lifecycle?** — `tools_called` with `min_calls` matching the expected number of transitions. Catches "agent stopped triggering the state machine" regressions.
2. **In the right order?** — `tool_call_sequence` matching the expected lifecycle order. Catches "agent skipped a state" or "agent went backwards" regressions.

Combine with content checks (`content_includes`, `content_excludes`) on each turn for "agent confirmed payment received" / "agent provided tracking number" assertions, and the suite gives a deterministic deploy gate for any prompt change that might break the workflow.

## CI gate

```yaml
# .github/workflows/workflow-regression.yml
name: Workflow regression

on:
  pull_request:
    paths:
      - 'examples/workflow-support/**'
      - 'examples/workflow-order-processing/**'

jobs:
  test:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      - uses: actions/setup-go@v5
        with:
          go-version: '1.26'
      - run: make build-arena
      - name: Run workflow scenarios
        run: |
          for dir in examples/workflow-support examples/workflow-order-processing; do
            (cd "$dir" && ../../bin/promptarena run --ci --formats json) || exit 1
          done
```

Keyless: both examples use mock providers with scripted `workflow__transition` calls.

## Unit-testing a single stage

The scenarios above drive the whole lifecycle from the entry state. To exercise
**one** stage in isolation — without first walking the agent through every earlier
transition — pin the scenario's `task_type` to that stage's `prompt_task`:

```yaml
spec:
  id: unit-fulfillment-stage
  task_type: fulfillment   # start in the non-entry 'fulfillment' stage
  turns:
    - role: user
      content: "Where is my package?"
      assertions:
        - type: state_is
          params:
            state: fulfillment
```

The per-run state machine starts in that stage instead of the workflow entry, so
the turn runs the `fulfillment` prompt directly. Full-lifecycle scenarios (no
`task_type`, or `task_type` set to the entry) and single-stage unit tests can live
in the same config. A `task_type` that names no workflow state's `prompt_task` is a
hard error at run time — it is never silently rerouted through the entry. See
`examples/workflow-order-processing` for both shapes side by side.

## Extending it

- **Add a new state**: drop it in the pack's `workflow:` block with `on_event:` mappings. Add a scenario that exercises the new path. Assertion shape stays the same — extend `min_calls` and the `sequence` to match.
- **Test state isolation**: `workflow_tool_access` (once #1169 is fixed) lets you assert that the agent only uses certain tools in certain states. Today, fall back to combining `tools_called` per-turn with `when:` clauses.
- **Multi-path scenarios**: register multiple scenarios that exercise different lifecycle branches (e.g., happy-path, escalation-path, error-path). The same `tool_call_sequence` shape works per scenario.

## Related how-tos

- [Voice IVR with workflow state machine](/arena/how-to/voice-ivr/) — same workflow primitive paired with the voice harness. Uses the same `tools_called`-based assertion pattern.
- The voice-ivr scenario is the reference for the workflow + voice pairing; the workflow-support / workflow-order-processing scenarios are the reference for the text-only workflow lifecycle pattern.
