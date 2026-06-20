---
name: promptarena-authoring
description: Author valid PromptArena kit configs; use when building or editing scenarios, providers, prompts, tools.
---

# Authoring PromptArena Kits

Generated from the PromptArena agent knowledge base. Discover more with `promptarena explain --list` and `promptarena examples list`. Run `promptarena schema <type>` for authoritative config structure and `promptarena validate` to check your work.

## Assertions judge; evals measure

PromptArena is an **assertion** framework. Eval handlers are *inputs* to assertions:
an eval handler emits `Score` as a raw signal (0..1) and nothing else. The pass/fail
threshold lives on a `type: assertion` wrapper:

```yaml
assertions:
  - type: assertion
    eval:
      type: toxicity        # eval handler — emits a raw score
    max_score: 0.2          # threshold lives HERE, on the assertion
```

Putting `min_score`/`max_score` on the inner eval is a common trap — the eval must
stay a pure primitive. Guardrails reuse the same eval primitives but enforce in
production; assertions are test-only and may observe guardrail firings.

## Mock providers simulate the LLM, not the tools

A provider with `type: mock` simulates **only the LLM's decisions** — the text it
returns and which tools it calls. The tools themselves run for real (InMemoryStore,
workflow state machine, memory). Point the provider at a response file:

```yaml
spec:
  type: mock
  additional_config:
    mock_config: mock-responses.yaml   # relative to the arena config directory
```

Response keys MUST match the scenario's `metadata.name`, NOT `spec.id`:

```yaml
scenarios:
  my-scenario-name:
    turns:
      1:
        response: "I'll look that up"
        tool_calls:
          - name: memory__recall
            arguments: { query: "user preferences" }
      2: "Based on what I found..."
```

The `--mock-provider` CLI flag is different: it replaces ALL providers with a generic
mock that ignores `mock-responses.yaml`. If your example ships a `providers/mock-provider.yaml`,
run it WITHOUT `--mock-provider`.

## Validate against the binary's own schemas

Every config type (scenario, provider, prompt, tool, arena) has a JSON schema. The
schema embedded in your installed `promptarena` binary is the source of truth — it is
the exact version `promptarena validate` enforces. Prefer it over the public web copy,
which may be a different release.

- `promptarena schema <type>` — print the authoritative schema for a type.
- `promptarena validate` — check your configs before running.

Author configs to the schema first; don't guess field names.

