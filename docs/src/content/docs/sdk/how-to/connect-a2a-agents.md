---
title: Connect A2A Agents
description: Register remote A2A agents as tools with authentication, headers, and skill filtering
sidebar:
  order: 7
---

Connect to remote [A2A](https://google.github.io/A2A) agents so the LLM can delegate tasks to them. The SDK discovers the agent's skills at startup and registers each skill as a tool.

---

## Quick Start

```go
import "github.com/AltairaLabs/PromptKit/sdk"

agent := sdk.NewA2AAgent("https://agent.example.com").
    WithAuth("Bearer", os.Getenv("AGENT_TOKEN"))

conv, err := sdk.Open("./app.pack.json", "assistant",
    sdk.WithA2AAgent(agent),
)
if err != nil {
    log.Fatal(err)
}
defer conv.Close()
```

The SDK calls `/.well-known/agent.json` to discover the agent's skills, then registers each skill as a callable tool.

---

## Builder Pattern

`NewA2AAgent` provides a fluent API for configuring authentication, headers, timeouts, retry policies, and skill filtering.

```go
import (
    "github.com/AltairaLabs/PromptKit/runtime/tools"
    "github.com/AltairaLabs/PromptKit/sdk"
)

agent := sdk.NewA2AAgent("https://agent.example.com").
    WithAuth("Bearer", os.Getenv("AGENT_TOKEN")).
    WithHeader("X-Tenant-ID", "acme").
    WithTimeout(5000).
    WithRetryPolicy(3, 100, 2000).
    WithSkillFilter(&tools.A2ASkillFilter{
        Allowlist: []string{"forecast", "alerts"},
    })

conv, err := sdk.Open("./app.pack.json", "assistant",
    sdk.WithA2AAgent(agent),
)
```

### Builder Methods

| Method | Description |
|--------|-------------|
| `WithAuth(scheme, token)` | Set auth credentials (e.g. `"Bearer"`, `"my-token"`) |
| `WithAuthFromEnv(scheme, envVar)` | Read the token from an environment variable |
| `WithHeader(key, value)` | Add a static header to every request |
| `WithHeaderFromEnv(spec)` | Add a header from an env var (`"X-Key=ENV_VAR"`) |
| `WithTimeout(ms)` | Per-request timeout in milliseconds |
| `WithRetryPolicy(max, initialMs, maxMs)` | Retry with exponential backoff |
| `WithSkillFilter(filter)` | Control which skills are exposed |

---

## Authentication

### Direct Token

```go
agent := sdk.NewA2AAgent("https://agent.example.com").
    WithAuth("Bearer", "my-secret-token")
```

### Token from Environment Variable

Keep secrets out of code:

```go
agent := sdk.NewA2AAgent("https://agent.example.com").
    WithAuthFromEnv("Bearer", "AGENT_TOKEN")
```

At startup, the SDK reads `os.Getenv("AGENT_TOKEN")`. If the variable is unset, the agent connects without auth.

---

## Custom Headers

Add static headers or headers resolved from environment variables:

```go
agent := sdk.NewA2AAgent("https://agent.example.com").
    WithHeader("X-Tenant-ID", "acme").
    WithHeader("X-Request-Source", "promptkit").
    WithHeaderFromEnv("X-API-Key=API_KEY_ENV")
```

`WithHeaderFromEnv` uses the format `"Header-Name=ENV_VAR_NAME"`. If the env var is unset, the header is silently skipped.

---

## Skill Filtering

When an agent exposes many skills but you only need a few, use a skill filter.

### Allowlist

```go
agent := sdk.NewA2AAgent("https://weather.example.com").
    WithSkillFilter(&tools.A2ASkillFilter{
        Allowlist: []string{"forecast", "alerts"},
    })
```

### Blocklist

```go
agent := sdk.NewA2AAgent("https://agent.example.com").
    WithSkillFilter(&tools.A2ASkillFilter{
        Blocklist: []string{"debug", "admin"},
    })
```

If both are set, allowlist takes precedence.

---

## Multiple Agents

Register multiple A2A agents in a single conversation:

```go
research := sdk.NewA2AAgent("https://research.example.com").
    WithAuth("Bearer", os.Getenv("RESEARCH_TOKEN"))

translation := sdk.NewA2AAgent("https://translate.example.com").
    WithAuth("Bearer", os.Getenv("TRANSLATE_TOKEN"))

conv, err := sdk.Open("./app.pack.json", "assistant",
    sdk.WithA2AAgent(research),
    sdk.WithA2AAgent(translation),
)
```

---

## Tool Naming

Each agent skill becomes a tool named:

```
a2a__{agentName}__{skillId}
```

Names are sanitized: lowercased, with non-alphanumeric runs replaced by underscores.

| Agent Name | Skill ID | Tool Name |
|------------|----------|-----------|
| `Research Agent` | `search_papers` | `a2a__research_agent__search_papers` |
| `Weather Bot` | `forecast` | `a2a__weather_bot__forecast` |

Reference these in `allowed_tools` to control LLM access:

```yaml
spec:
  allowed_tools:
    - a2a__research_agent__search_papers
```

---

## Legacy Bridge Pattern

For lower-level control, you can create a `ToolBridge` manually:

```go
import "github.com/AltairaLabs/PromptKit/runtime/a2a"

client := a2a.NewClient("https://agent.example.com")
bridge := a2a.NewToolBridge(client)
bridge.RegisterAgent(ctx)

conv, err := sdk.Open("./app.pack.json", "assistant",
    sdk.WithA2ATools(bridge),
)
```

The builder pattern (`NewA2AAgent`) is preferred for new code — it handles auth, headers, retries, and skill filtering automatically.

---

## Complete Example

```go
package main

import (
    "context"
    "fmt"
    "log"
    "os"

    "github.com/AltairaLabs/PromptKit/runtime/tools"
    "github.com/AltairaLabs/PromptKit/sdk"
)

func main() {
    agent := sdk.NewA2AAgent("http://localhost:9877").
        WithAuth("Bearer", os.Getenv("ECHO_AGENT_TOKEN")).
        WithHeader("X-Request-Source", "sdk-example").
        WithTimeout(5000).
        WithRetryPolicy(3, 100, 2000).
        WithSkillFilter(&tools.A2ASkillFilter{
            Allowlist: []string{"echo"},
        })

    conv, err := sdk.Open("./app.pack.json", "assistant",
        sdk.WithA2AAgent(agent),
    )
    if err != nil {
        log.Fatal(err)
    }
    defer conv.Close()

    ctx := context.Background()
    resp, err := conv.Send(ctx, "Echo: Hello from the SDK!")
    if err != nil {
        log.Fatal(err)
    }
    fmt.Println(resp.Text())
}
```

---

## Next Steps

- [Use Tool Bridge (Runtime)](/runtime/how-to/use-a2a-tool-bridge/) — low-level bridge API
- [Test A2A Agents (Arena)](/arena/how-to/test-a2a-agents/) — end-to-end testing with mock agents
- [Register Tools](/sdk/how-to/register-tools/) — add custom Go tools alongside A2A tools
