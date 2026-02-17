# Workflow Support Example

This example demonstrates how to use PromptKit Arena to test a workflow-based customer support bot. The workflow defines a state machine with `intake`, `specialist`, and `closed` states, driven by `Escalate` and `Resolve` events.

## Workflow

```
          Escalate             Resolve
intake ─────────→ specialist ─────────→ closed
  │                                        ▲
  └────────────────────────────────────────┘
                   Resolve
```

**States:**
- `intake` — Initial customer contact and triage
- `specialist` — Escalated handling for complex issues
- `closed` — Terminal state, conversation complete

**Events:**
- `Escalate` — Moves from intake to specialist
- `Resolve` — Moves from intake or specialist to closed

## Files

- `config.arena.yaml` — Arena configuration referencing the pack, providers, and scenarios
- `support.pack.json` — Pack file with `prompts` and `workflow` sections
- `prompts/support-workflow.yaml` — PromptConfig with workflow-aware system prompt
- `providers/mock-provider.yaml` — Mock provider for deterministic testing
- `mock-responses.yaml` — Mock responses keyed by scenario and turn
- `scenarios/basic-flow.scenario.yaml` — Happy path through the full workflow
- `scenarios/state-assertions.scenario.yaml` — Tests `state_is`, `transitioned_to`, and `workflow_complete` assertions

## Running

```bash
cd examples/workflow-support
promptarena run -c config.arena.yaml
```

### Running a Specific Scenario

```bash
promptarena run -c config.arena.yaml --scenario basic-flow
```

## Workflow Assertions

This example demonstrates three workflow-specific assertion types:

- **`state_is`** — Verifies the workflow is in a specific state at that point
- **`transitioned_to`** — Checks that a state was visited in the transition history
- **`workflow_complete`** — Confirms the workflow reached a terminal state (no outgoing events)

## Pack Format

The `support.pack.json` file includes both a `prompts` map and a `workflow` section. Each workflow state references a prompt via `prompt_task`, and transitions are triggered by named events in `on_event`.
