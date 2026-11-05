---
title: Use the A2A tool bridge
description: Register a remote A2A agent's skills as tools the model can call
sidebar:
  order: 10
verified:
  commit: 004b9cbbd4d40f770a2e3c76053b600a55189355
  sources:
    - runtime/a2a/bridge.go
    - runtime/a2a/client.go
    - runtime/pipeline/stage/stages_provider.go
    - runtime/tools/types.go
    - sdk/capability_a2a.go
    - sdk/options.go
---

Let the model call a remote A2A agent by registering each of the agent's skills as a tool.

## Prerequisites

- The URL of an A2A agent. For tests, use the [mock server](/runtime/how-to/a2a/use-a2a-mock-server/).
- A pack with a prompt to attach the tools to.

## Steps

1. Create a client, build a bridge, and register the agent. `RegisterAgent` fetches the agent card and creates one tool descriptor per skill.

   ```go
   client := a2a.NewClient("http://localhost:9999")
   bridge := a2a.NewToolBridge(client)

   tools, err := bridge.RegisterAgent(ctx)
   if err != nil {
       log.Fatal(err)
   }
   for _, t := range tools {
       fmt.Printf("Registered: %s: %s\n", t.Name, t.Description)
   }
   ```

   Each tool is named `a2a__{agentName}__{skillId}`. Both parts are lowercased and every run of non-alphanumeric characters becomes one underscore.

   | Agent name | Skill ID | Tool name |
   |---|---|---|
   | Research Agent | search_papers | `a2a__research_agent__search_papers` |
   | Code Helper | lint-code | `a2a__code_helper__lint_code` |

2. Pass the bridge to the SDK.

   ```go
   conv, err := sdk.Open("assistant.pack.json", "chat",
       sdk.WithA2ATools(bridge),
   )
   if err != nil {
       log.Fatal(err)
   }
   defer conv.Close()

   resp, err := conv.Send(ctx, "Search for papers about quantum computing")
   if err != nil {
       log.Fatal(err)
   }
   fmt.Println(resp.Text())
   ```

   The SDK offers `a2a__` tools to the model without an `allowed_tools` entry. Each `WithA2ATools` call adds its bridge, so you can pass several. If two bridges register the same tool name, `sdk.Open` fails with an error naming both.

3. To use several agents, register each with `WithA2AAgent` instead. The SDK fetches each agent card when the conversation opens.

   ```go
   conv, err := sdk.Open("assistant.pack.json", "chat",
       sdk.WithA2AAgent(sdk.NewA2AAgent("http://localhost:9999")),
       sdk.WithA2AAgent(sdk.NewA2AAgent("http://localhost:9998").
           WithAuthFromEnv("Bearer", "TRANSLATOR_TOKEN").
           Required()),
   )
   ```

   If an agent cannot be reached, the conversation opens without its tools. `Required()` makes the open fail instead.

## Verify

`RegisterAgent` returns one descriptor per skill, and the loop in step 1 prints each tool name. Each tool takes a required `query` string argument and returns a `response` string.

The bridge adds fields from the skill's modes (falling back to the card's default modes):

| Mode | Added field |
|---|---|
| Input `image/*` | `image_url`, `image_data` arguments |
| Input `audio/*` | `audio_data` argument |
| Output `image/*` or `audio/*` | `media_url`, `media_type` result fields |

## Troubleshoot

- `a2a bridge: discover` error: the agent card is unreachable at `/.well-known/agent-card.json` (or the legacy `/.well-known/agent.json`). Check the URL.
- `sdk.Open` fails with `tool names must be unique across A2A bridges`: two bridges registered the same tool name, which happens when both point at agents with the same name and skill ID. Rename one agent or drop one bridge.

## Related

- [Use the A2A mock server](/runtime/how-to/a2a/use-a2a-mock-server/)
- [Test A2A agents in Arena](https://promptarena.altairalabs.ai/arena/how-to/test-a2a-agents/)
- [Runtime A2A reference](/runtime/reference/a2a/)
