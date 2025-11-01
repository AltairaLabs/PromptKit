# StateStore Example

This example demonstrates how to use StateStore for conversation persistence in PromptKit Arena.

## Features

- **Memory StateStore**: In-memory conversation storage (default)
- **Redis StateStore**: Optional Redis-backed persistence for production
- **Multi-turn conversations**: Automatic state tracking across turns
- **Conversation IDs**: Unique identifiers for each conversation
- **Mock Provider**: No API keys needed - uses mock responses for testing

## Configuration

### Memory StateStore (Default)

```yaml
state_store:
  type: memory
```

### Redis StateStore (Production)

```yaml
state_store:
  type: redis
  redis:
    address: "localhost:6379"
    password: ""
    database: 0
    ttl: "24h"
    prefix: "promptkit"
```

## Usage

The StateStore is automatically integrated into the pipeline when configured in `arena.yaml`. Each conversation run gets a unique conversation ID based on the `runID + scenarioID`.

During execution:

1. **Load Middleware**: Loads existing conversation state (if any)
2. **Pipeline Execution**: Processes the turn
3. **Save Middleware**: Persists updated conversation state

## Benefits

- **Automatic**: No code changes needed - just configure in arena.yaml
- **Persistent**: Conversation history survives across runs
- **Debuggable**: Inspect conversation state for troubleshooting
- **Scalable**: Use Redis for distributed deployments

## Running the Example

```bash
# Run with memory store (default)
promptarena run -c arena.yaml

# Run with Redis (requires Redis server)
# Update arena.yaml to use redis type, then:
promptarena run -c arena.yaml
```
