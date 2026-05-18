---
title: Test multi-agent A2A delegation patterns
description: Assert which A2A agents the supervisor invokes for which user requests. Walk through examples/multi-agent-demo/ — supervisor routes research queries to a research agent and translation queries to a translation agent.
---

This how-to walks through `examples/multi-agent-demo/` — a supervisor assistant routes user requests to two specialised A2A agents (research, translation). The scenario asserts the delegation pattern: research queries hit the research agent, translation queries hit the translation agent, and both produce the expected content.

## What it proves

Multi-agent systems fail in two distinct ways:

- **Routing failure** — the supervisor delegates to the wrong specialist (or no specialist at all).
- **Specialist failure** — the right specialist gets called but produces unusable output.

Pure single-agent eval misses the first; pure agent-output eval misses the supervisor's contribution. PromptArena asserts both: which agent got invoked AND what the agent returned.

The pack registers two mock A2A agents (`research_agent`, `translation_agent`). The supervisor (a single assistant prompt) decides which to call based on the user's request. Assertions check both the routing and the specialist response.

## The assertion shape

`examples/multi-agent-demo/scenarios/multi-agent-delegation.yaml`:

```yaml
turns:
  - role: user
    content: "Search for papers about quantum computing"
    assertions:
      - type: agent_invoked
        params:
          agents:
            - a2a__research_agent__search_papers
      - type: agent_response_contains
        params:
          agent: a2a__research_agent__search_papers
          contains: "Quantum Computing Fundamentals"

  - role: user
    content: "Translate the summary to French"
    assertions:
      - type: agent_invoked
        params:
          agents:
            - a2a__translation_agent__translate
      - type: agent_response_contains
        params:
          agent: a2a__translation_agent__translate
          contains: "informatique quantique"

conversation_assertions:
  - type: agent_invoked
    params:
      agents:
        - a2a__research_agent__search_papers
        - a2a__translation_agent__translate
```

Per-turn `agent_invoked` checks routing. Per-turn `agent_response_contains` checks specialist content. Conversation-level `agent_invoked` checks both agents were exercised across the run.

## Mock A2A agents

The pack defines the agents inline with `responses:` blocks that match on input:

```yaml
a2a_agents:
  - name: research_agent
    card:
      name: Research Agent
      description: A mock research agent that searches for academic papers
      skills:
        - id: search_papers
          name: Search Papers
          tags: [research, papers, search]
    responses:
      - skill: search_papers
        match:
          contains: quantum
        response:
          parts:
            - text: |
                Found 3 papers on quantum computing:
                1. Quantum Computing Fundamentals (2024)
                ...
```

Each response can match on input fragments, fall back to a default, or return structured parts. The mock A2A server runs in-process; no external server, no Docker, no provider keys.

## Run it

```bash
cd examples/multi-agent-demo
promptarena serve
```

Headless / CI:

```bash
promptarena run --ci --formats html,json
```

Keyless: both the supervisor (mock-assistant) and the A2A agents are mocked.

## Switching to real A2A servers

To test against a real A2A server (running locally or remote):

1. Drop the inline `a2a_agents:` block.
2. Add `a2a_servers:` pointing at the real endpoint URLs.
3. The supervisor and assertions stay the same — they don't know whether the A2A endpoint is mocked or remote.

See [Test A2A Agents](/arena/how-to/test-a2a-agents/) for the full reference covering authentication, headers, and skill filtering.

## Adding agents

- **Add a third specialist**: drop another `a2a_agents:` entry in `config.arena.yaml` with its skills + canned responses. Add a turn to the scenario asserting on the new agent.
- **Stricter ordering**: `tool_call_sequence` over the `a2a__*` tool names asserts the supervisor called the agents in a specific order.
- **Tool-arg assertions**: `tool_calls_with_args` lets you assert the supervisor passed the right arguments to each agent (e.g., the translation agent must receive `target_language: French`).

## CI gate

```yaml
# .github/workflows/multi-agent-demo.yml
name: A2A multi-agent

on:
  pull_request:
    paths:
      - 'examples/multi-agent-demo/**'

jobs:
  test:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      - uses: actions/setup-go@v5
        with:
          go-version: '1.26'
      - run: make build-arena
      - name: Run multi-agent demo
        working-directory: examples/multi-agent-demo
        run: ../../bin/promptarena run --ci --formats json
```

Keyless and fork-safe. The mock A2A servers run in-process.
