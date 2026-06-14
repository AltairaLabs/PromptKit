# SDK example — Workflow States as Agents (RFC 0011)

Demonstrates [RFC 0011 — Workflow States as Agents](https://promptpack.org/docs/rfcs/workflow-states-as-agents)
using the SDK, against the pack compiled from the sibling PromptArena example.

## The pipeline

```
../config.arena.yaml  --packc-->  ../workflow-agent-loops.pack.json  --sdk-->  this demo
```

The PromptArena example and this SDK demo share **one** source of truth: the
Arena `config.arena.yaml` is compiled by `packc` into `workflow-agent-loops.pack.json`,
which this program loads. To regenerate the pack after editing the config:

```bash
packc compile -c ../config.arena.yaml -o ../workflow-agent-loops.pack.json
```

## What it shows

The pack's `agents` block declares two agents:

- `researcher` — `state: research`, so it is opened as the pack **workflow
  pipeline entered at the `research` state** (RFC 0011). Invoking it runs the
  research → summarize → done workflow, not a single prompt.
- `summarizer` — a plain single-prompt agent (RFC 0007). The only difference is
  the one line `state: research`.

`OpenMultiAgent` opens each agent as a sendable pipeline; the demo inspects them
and prints which is workflow-backed and at what state.

## Run

```bash
go run .
```

It uses a built-in mock provider, so no API keys are needed — it returns a fixed
reply, which is enough to show the wiring (the researcher is a workflow pipeline
sitting at `research`; the summarizer is a plain conversation). Set a real
provider key (e.g. `OPENAI_API_KEY`) and swap in a real provider to drive the
full loop, which behaves exactly like running the workflow directly.
