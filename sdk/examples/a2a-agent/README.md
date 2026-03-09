# A2A Agent Example

Connect to remote A2A (Agent-to-Agent) agents with authentication via the PromptKit SDK.

## What You'll Learn

- Builder pattern with `NewA2AAgent()` for remote agent configuration
- Bearer token authentication with `WithAuth()`
- Custom headers with `WithHeader()`
- Skill filtering to expose only specific agent skills
- Retry policies for resilient connections

## Prerequisites

- Go 1.21+
- OpenAI API key
- A running A2A agent (this example uses the echo server from `examples/a2a-auth-test`)

## Running the Example

```bash
# Terminal 1: Start the echo server
go run ./examples/a2a-auth-test/server

# Terminal 2: Run this example
export OPENAI_API_KEY=your-key
go run .
```

## Code Overview

```go
echoAgent := sdk.NewA2AAgent("http://localhost:9877").
    WithAuth("Bearer", "test-token-123").
    WithHeader("X-Request-Source", "sdk-example").
    WithTimeout(5000).
    WithRetryPolicy(3, 100, 2000).
    WithSkillFilter(&tools.A2ASkillFilter{
        Allowlist: []string{"echo"},
    })

conv, err := sdk.Open("./a2a-agent.pack.json", "assistant",
    sdk.WithA2AAgent(echoAgent),
)
```

## Builder Options

| Method | Description |
|--------|-------------|
| `WithAuth(scheme, token)` | Set auth credentials (e.g. Bearer token) |
| `WithAuthFromEnv(scheme, envVar)` | Auth token from environment variable |
| `WithHeader(key, value)` | Add a static header to all requests |
| `WithHeaderFromEnv(headerEnv)` | Header value from environment variable |
| `WithTimeout(ms)` | Request timeout in milliseconds |
| `WithRetryPolicy(max, initialMs, maxMs)` | Exponential backoff retry policy |
| `WithSkillFilter(filter)` | Allowlist/blocklist for agent skills |

## Tool Naming

A2A tools are namespaced as `a2a__<agent>__<skill>`:
- `a2a__echo_agent__echo`
- `a2a__echo_agent__reverse` (blocked by skill filter in this example)

## Environment Variables

| Variable | Default | Description |
|----------|---------|-------------|
| `ECHO_AGENT_URL` | `http://localhost:9877` | Echo server URL |
| `ECHO_AGENT_TOKEN` | `test-token-123` | Bearer token for auth |

## Key Concepts

1. **Auto-Discovery** - Agent skills are discovered from the agent card at startup
2. **Skill Filtering** - Expose only the skills you need
3. **Authentication** - Bearer, Basic, or custom auth schemes
4. **Retry Policy** - Exponential backoff for transient failures
5. **No Pack Changes** - A2A tools don't need to be defined in the pack file

## Next Steps

- [MCP Tools Example](../mcp-tools/) - MCP server integration
- [HTTP Transforms Example](../http-transforms/) - Argument transforms and response processing
- [Tools Example](../tools/) - Basic tool handling
