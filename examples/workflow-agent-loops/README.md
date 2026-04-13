# Agent Loops

End-to-end demo of [RFC 0009 — Agent Loop Extension](https://promptpack.org/docs/rfcs/agent-loops) running on PromptArena.

Demonstrates, in a single runnable example:

- **Self-transitioning states** — `research` loops back to itself via `on_event: More → research`.
- **`max_visits` with `on_max_visits` redirect** — `research` is capped at 3 iterations; the fourth attempted `More` is transparently redirected to `summarize`.
- **Workflow-level budgets** — `engine.budget` caps total visits, tool calls, and wall time.
- **Artifacts** — the `findings` artifact uses `mode: append`, so successive `workflow__set_artifact` calls accumulate notes across iterations. The summariser state reads them back via `{{artifacts.findings}}`.
- **Observability events** — PromptKit emits `workflow.transitioned`, `workflow.max_visits_exceeded`, `workflow.budget_exhausted`, and `workflow.completed` on the event bus, so any listener can observe loop progress and termination.

## Scenarios

| Scenario | What it shows |
|---|---|
| `loop-converges` | LLM gathers three notes and chooses `Done` on its own. Workflow ends via the normal path: `research → summarize → done`. |
| `loop-hits-max-visits` | LLM keeps asking for `More` past the cap. On the fourth attempt, the state machine redirects to `summarize` regardless of what the LLM decided. A `workflow.max_visits_exceeded` event fires. |

## Run

```bash
# Build promptarena if you haven't
make build-arena

# Run the example
cd examples/workflow-agent-loops
PROMPTKIT_SCHEMA_SOURCE=local ../../bin/promptarena run --ci --formats html,json

# Open the HTML report
open out/report.html
```

The example uses a mock provider so no API keys are needed — the canned LLM responses are in `mock-responses.yaml`.

## What to look for in the report

**`loop-converges`**

- Three successive `research → research` transitions, each with `workflow__set_artifact` appending a note.
- A `research → summarize` transition on `Done`.
- A `summarize → done` transition on `Complete`, followed by `workflow.completed`.

**`loop-hits-max-visits`**

- Three `research → research` transitions.
- A fourth transition attempt that appears as `research → summarize` with `redirected: true` and `original_target: research`.
- A `workflow.max_visits_exceeded` event alongside the redirect.
- Eventual `workflow.completed`.

## Anatomy

```yaml
# config.arena.yaml excerpt
workflow:
  version: 2
  entry: research
  engine:
    budget:
      max_total_visits: 10
      max_tool_calls: 50
      max_wall_time_sec: 60
  states:
    research:
      prompt_task: research
      max_visits: 3
      on_max_visits: summarize         # forced exit when cap is hit
      artifacts:
        findings:
          type: text/plain
          mode: append                  # successive set_artifact calls concat
      on_event:
        More: research                  # self-transition
        Done: summarize
    summarize:
      prompt_task: summarize
      on_event: { Complete: done }
    done:
      terminal: true
```

The research prompt reads the accumulated findings via the template variable
the runtime injects on each transition:

```
{{artifacts.findings}}
```

## Why this example exists

The three features exercised here are each individually simple, but their
interaction is the whole point of RFC 0009: let an agent iterate, bound
the iteration cheaply, and accumulate structured state across the loop
without reaching for session memory. If you're implementing an agent
with any iterative behaviour (research, code-gen, planning, extract &
refine), this is the shape.
