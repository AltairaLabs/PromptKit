---
id: mock-providers
title: Mock providers simulate the LLM, not the tools
summary: A mock provider replays canned model output; tools, workflow state, and memory still execute for real.
tags: [providers, testing, mock]
---
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
