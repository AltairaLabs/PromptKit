# A2A Demo

This example demonstrates the Agent-to-Agent (A2A) protocol integration in PromptKit.

## Prerequisites

```bash
export OPENAI_API_KEY=sk-...
```

## SDK Examples

### 1. Server — Expose an agent via A2A

```bash
go run ./examples/a2a-demo/server
```

The server listens on `http://localhost:9999` and serves:
- Agent card at `/.well-known/agent.json`
- JSON-RPC endpoint at `/a2a`

### 2. Client — Discover and call the agent

```bash
# In a second terminal:
go run ./examples/a2a-demo/client
```

Discovers the agent card, sends a message, and prints the response.

### 3. Tools — Use A2A agent as a tool

```bash
# In a second terminal (server must be running):
go run ./examples/a2a-demo/tools
```

Bridges the remote agent's skills into tool descriptors and uses them in an SDK conversation.

## Arena Example

The `arena/` directory contains an Arena configuration that uses mock A2A agents for automated testing.

```bash
cd examples/a2a-demo/arena
promptarena run
```

This starts mock A2A agents defined in `config.arena.yaml`, registers their skills as tools, and runs the scenario in `scenarios/delegated_research.yaml`.
