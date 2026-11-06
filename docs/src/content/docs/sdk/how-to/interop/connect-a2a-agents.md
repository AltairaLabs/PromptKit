---
title: Connect A2A Agents
description: Register remote A2A agents as tools, with authentication, headers, timeouts and skill filtering
sidebar:
  order: 7
verified:
  commit: 8489bf3d95c7498f9de0f024769db9399c24e884
  sources:
    - runtime/a2a/bridge.go
    - runtime/a2a/client.go
    - runtime/pipeline/stage/stages_provider.go
    - runtime/tools/types.go
    - sdk/capability_a2a.go
    - sdk/options.go
    - sdk/sdk.go
---

Connect remote [A2A](https://a2a-protocol.org) agents to a conversation so the model can delegate tasks to them. `Open` fetches each agent's card and registers every skill as a tool.

## Prerequisites

- A reachable A2A agent that serves `/.well-known/agent-card.json`.
- A pack and prompt to open the conversation from.

## Register an agent

1. Build the agent and pass it to `Open`:

   ```go
   import "github.com/AltairaLabs/PromptKit/sdk/v2"

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

   `Open` calls `/.well-known/agent-card.json` and registers each skill as a callable tool. The model receives these tools without an `allowed_tools` entry.

2. Add whatever else the agent needs. The builder methods chain:

   | Method | Description |
   |--------|-------------|
   | `WithAuth(scheme, token)` | Set auth credentials, such as `"Bearer"` and a token |
   | `WithAuthFromEnv(scheme, envVar)` | Read the token from an environment variable |
   | `WithHeader(key, value)` | Add a static header to every request |
   | `WithHeaderFromEnv(spec)` | Add a header from an environment variable (`"X-Key=ENV_VAR"`) |
   | `WithTimeout(ms)` | Timeout in milliseconds for discovery, each request and each tool call |
   | `WithRetryPolicy(max, initialMs, maxMs)` | Retry with exponential backoff |
   | `WithSkillFilter(filter)` | Control which skills are exposed |
   | `Required()` | Fail `Open` if the agent card cannot be fetched |

   ```go
   import (
       "github.com/AltairaLabs/PromptKit/runtime/v2/tools"
       "github.com/AltairaLabs/PromptKit/sdk/v2"
   )

   agent := sdk.NewA2AAgent("https://agent.example.com").
       WithAuth("Bearer", os.Getenv("AGENT_TOKEN")).
       WithHeader("X-Tenant-ID", "acme").
       WithTimeout(5000).
       WithRetryPolicy(3, 100, 2000).
       WithSkillFilter(&tools.A2ASkillFilter{
           Allowlist: []string{"forecast", "alerts"},
       })
   ```

   Without `WithTimeout`, discovery gives up after 10 seconds and requests after 60.

## Authenticate

Pass a token directly:

```go
agent := sdk.NewA2AAgent("https://agent.example.com").
    WithAuth("Bearer", "my-secret-token")
```

Or keep it out of code and read it from the environment:

```go
agent := sdk.NewA2AAgent("https://agent.example.com").
    WithAuthFromEnv("Bearer", "AGENT_TOKEN")
```

`Open` reads `AGENT_TOKEN` when it runs. If the variable is unset, the agent connects without auth.

## Add custom headers

```go
agent := sdk.NewA2AAgent("https://agent.example.com").
    WithHeader("X-Tenant-ID", "acme").
    WithHeader("X-Request-Source", "promptkit").
    WithHeaderFromEnv("X-API-Key=API_KEY_ENV")
```

`WithHeaderFromEnv` takes `"Header-Name=ENV_VAR_NAME"`. If the environment variable is unset, the header is skipped.

## Filter skills

When an agent exposes many skills and you need a few, use an allowlist:

```go
agent := sdk.NewA2AAgent("https://weather.example.com").
    WithSkillFilter(&tools.A2ASkillFilter{
        Allowlist: []string{"forecast", "alerts"},
    })
```

Or a blocklist:

```go
agent := sdk.NewA2AAgent("https://agent.example.com").
    WithSkillFilter(&tools.A2ASkillFilter{
        Blocklist: []string{"debug", "admin"},
    })
```

If both are set, the allowlist takes precedence.

## Connect several agents

Pass one `WithA2AAgent` per agent:

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

`Open` fails, naming both agents, if two of them register the same tool name.

## Fail when an agent is unreachable

By default, a failed discovery is logged as a warning and the conversation runs without that agent's tools. Mark the agent required to fail `Open` instead:

```go
agent := sdk.NewA2AAgent("https://agent.example.com").Required()
```

## Tool names

Each skill becomes a tool named `a2a__{agentName}__{skillId}`. The agent name comes from the agent card. Names are lowercased, with runs of non-alphanumeric characters replaced by underscores.

| Agent name | Skill ID | Tool name |
|------------|----------|-----------|
| `Research Agent` | `search_papers` | `a2a__research_agent__search_papers` |
| `Weather Bot` | `forecast` | `a2a__weather_bot__forecast` |

A tool's input requires a `query` string, plus `image_url` and `image_data` when the skill accepts image input, and `audio_data` when it accepts audio input. Its output has a `response` string, plus `media_url` and `media_type` when the skill produces image or audio output.

## Use a tool bridge directly

For lower-level control, create a `ToolBridge` yourself. The builder applies auth, headers, retries and skill filtering for you; with a bare bridge you configure the client yourself.

```go
import "github.com/AltairaLabs/PromptKit/runtime/v2/a2a"

client := a2a.NewClient("https://agent.example.com")
bridge := a2a.NewToolBridge(client)
if _, err := bridge.RegisterAgent(ctx); err != nil {
    log.Fatal(err)
}

conv, err := sdk.Open("./app.pack.json", "assistant",
    sdk.WithA2ATools(bridge),
)
```

Pass `WithA2ATools` once per bridge. `Open` fails, naming both bridges, if two register the same tool name.

## Complete example

```go
package main

import (
    "context"
    "fmt"
    "log"
    "os"

    "github.com/AltairaLabs/PromptKit/runtime/v2/tools"
    "github.com/AltairaLabs/PromptKit/sdk/v2"
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

## Verify

Open the conversation with info-level logging on. A successful discovery logs `a2a agent tools registered` with the agent URL and a tool count. Ask the model to use one of the agent's skills and check that the reply reflects the agent's answer.

## Troubleshoot

- `a2a agent tools not registered: discovery failed` in the logs: the card fetch failed. Check the URL, auth and `WithTimeout`, or add `Required()` to surface the error from `Open`.
- `a2a agent exposes no tools`: the card lists no skills, or the skill filter excluded all of them.

## Related

- [Use the tool bridge (runtime)](/runtime/how-to/a2a/use-a2a-tool-bridge/): the low-level bridge API
- [Test A2A agents (Arena)](https://promptarena.altairalabs.ai/arena/how-to/test-a2a-agents/): end-to-end testing with mock agents
- [Register tools](/sdk/how-to/tools/register-tools/): add custom Go tools alongside A2A tools
- [Choose an A2A server mode](/sdk/how-to/interop/choose-a2a-server-mode/): serve your own agent over A2A
