# Multi-Agent Demo

Demonstrates multi-agent orchestration testing in Arena using the `agent_invoked`, `agent_not_invoked`, and `agent_response_contains` assertions.

## What This Example Shows

- Two mock A2A agents: **research_agent** and **translation_agent**
- Mock LLM responses that delegate to each agent via tool calls
- Turn-level assertions verifying which agents were invoked and what they returned
- Conversation-level assertions verifying overall delegation patterns

## Running

```bash
# From the project root
./bin/promptarena run -c examples/multi-agent-demo
```

No API keys are needed — this example uses a mock provider with deterministic responses.

## Files

| File | Description |
|------|-------------|
| `config.arena.yaml` | Arena config with two mock A2A agents |
| `providers/mock-provider.yaml` | Mock LLM provider |
| `prompts/assistant.yaml` | Prompt config with both agent tools allowed |
| `mock-responses.yaml` | Mock LLM responses that trigger agent tool calls |
| `scenarios/multi-agent-delegation.yaml` | Scenario with agent assertions (turn + conversation level) |
