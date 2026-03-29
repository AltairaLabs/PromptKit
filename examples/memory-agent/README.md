# Memory Agent Example

Demonstrates PromptKit's agentic memory system — cross-session knowledge that persists beyond a single conversation.

## What You'll Learn

- How to define `memory__recall` and `memory__remember` tools for Arena testing
- How to assert that an agent uses memory tools appropriately
- Multi-turn scenarios that test preference storage and recall
- How mock responses simulate memory-enabled LLM behavior

## Running the Example

With a real provider (API key required):

```bash
cd examples/memory-agent
PROMPTKIT_SCHEMA_SOURCE=local ../../bin/promptarena run --ci --formats html,json
```

With mock provider (no API key, assertions will fail since mock doesn't call tools):

```bash
cd examples/memory-agent
PROMPTKIT_SCHEMA_SOURCE=local ../../bin/promptarena run --mock-provider --ci --formats json
```

## Scenarios

### preference-recall
Tests that the agent:
1. Uses `memory__remember` when told a preference
2. Uses `memory__recall` when asked about preferences
3. Includes recalled information in its response

### cross-turn-memory
Tests that the agent:
1. Stores key facts about the user (name, role, project)
2. Provides relevant recommendations
3. Recalls stored context when asked

## SDK Usage

In the SDK, memory is registered as a capability — no explicit tool files needed:

```go
store := memory.NewInMemoryStore()
scope := map[string]string{"user_id": "craig"}

conv, _ := sdk.Open("./app.pack.json", "assistant",
    sdk.WithMemory(store, scope),
)
```

The `WithMemory` option automatically registers the 4 memory tools (`memory__recall`, `memory__remember`, `memory__list`, `memory__forget`) and optionally wires retrieval/extraction pipeline stages.
