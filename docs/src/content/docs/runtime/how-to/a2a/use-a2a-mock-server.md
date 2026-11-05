---
title: Use the A2A mock server
description: Run an in-process mock A2A agent with canned responses for deterministic tests
sidebar:
  order: 11
verified:
  commit: 004b9cbbd4d40f770a2e3c76053b600a55189355
  sources:
    - runtime/a2a/mock/config.go
    - runtime/a2a/mock/mock.go
---

Start an in-process A2A server that returns canned responses, so tests exercise A2A calls without a real agent or an LLM.

The `a2a/mock` package serves the agent card and `message/send` over an `httptest.Server`.

## Prerequisites

- A Go test or program that imports `github.com/AltairaLabs/PromptKit/runtime/v2/a2a` and `.../a2a/mock`.

## Steps

1. Define the agent card and a response for each skill, then start the server.

   ```go
   package main

   import (
       "context"
       "fmt"
       "log"

       "github.com/AltairaLabs/PromptKit/runtime/v2/a2a"
       "github.com/AltairaLabs/PromptKit/runtime/v2/a2a/mock"
   )

   func main() {
       text := "Found 3 papers on quantum computing"
       server := mock.NewA2AServer(
           &a2a.AgentCard{
               Name:   "Research Agent",
               Skills: []a2a.AgentSkill{{ID: "search_papers", Name: "Search Papers"}},
           },
           mock.WithSkillResponse("search_papers", mock.Response{
               Parts: []a2a.Part{{Text: &text}},
           }),
       )

       url, err := server.Start()
       if err != nil {
           log.Fatal(err)
       }
       defer server.Close()

       card, err := a2a.NewClient(url).Discover(context.Background())
       if err != nil {
           log.Fatal(err)
       }
       fmt.Println("Agent:", card.Name)
   }
   ```

2. Route on message content. The mock evaluates rules in the order you pass them and the first match wins, so list specific matchers before a general fallback for the same skill.

   ```go
   quantum := "Papers about quantum computing..."
   general := "General research results..."

   server := mock.NewA2AServer(card,
       mock.WithInputMatcher("search_papers",
           func(msg a2a.Message) bool {
               for _, p := range msg.Parts {
                   if p.Text != nil && strings.Contains(*p.Text, "quantum") {
                       return true
                   }
               }
               return false
           },
           mock.Response{Parts: []a2a.Part{{Text: &quantum}}},
       ),
       mock.WithSkillResponse("search_papers", mock.Response{
           Parts: []a2a.Part{{Text: &general}},
       }),
   )
   ```

   The mock reads the skill ID from `metadata.skillId` on the message, then on the request. A rule with an empty skill ID matches any request.

3. Inject latency or errors to test timeout and failure handling.

   ```go
   server := mock.NewA2AServer(card,
       mock.WithLatency(2*time.Second),
       mock.WithSkillError("summarize", "Service unavailable"),
   )
   ```

   `WithLatency` delays every `message/send` response. `WithSkillError` returns a task in the `failed` state with the message as its status message.

4. To load rules from data instead of code, build an `AgentConfig` and convert it with `OptionsFromConfig`.

   ```go
   cfg := &mock.AgentConfig{
       Name: "research_agent",
       Card: a2a.AgentCard{
           Name:   "Research Agent",
           Skills: []a2a.AgentSkill{{ID: "search_papers", Name: "Search Papers"}},
       },
       Responses: []mock.RuleConfig{
           {
               Skill: "search_papers",
               Match: &mock.MatchConfig{Contains: "quantum"},
               Response: &mock.ResponseConfig{
                   Parts: []mock.PartConfig{{Text: "Quantum papers found"}},
               },
           },
           {Skill: "summarize", Error: "Service unavailable"},
       },
   }

   server := mock.NewA2AServer(&cfg.Card, mock.OptionsFromConfig(cfg)...)
   ```

   | Match field | Behavior |
   |---|---|
   | `contains` | Case-insensitive substring match on the message text |
   | `regex` | Regular expression match on the message text |

   When both are set, both must match. A rule with `error` set returns a failed task and ignores `response`.

   The same structure loads from YAML in Arena:

   ```yaml
   a2a_agents:
     - name: research_agent
       card:
         name: Research Agent
         skills:
           - id: search_papers
             name: Search Papers
       responses:
         - skill: search_papers
           match:
             contains: quantum
           response:
             parts:
               - text: "Found 3 papers on quantum computing"
         - skill: summarize
           error: Service unavailable
   ```

## Verify

Send a message with a client and read the artifact text.

```go
query := "papers about quantum computing"
task, err := a2a.NewClient(url).SendMessage(ctx, &a2a.SendMessageRequest{
    Message: a2a.Message{
        MessageID: "m1",
        Role:      a2a.RoleUser,
        Parts:     []a2a.Part{{Text: &query}},
    },
    Metadata: map[string]any{"skillId": "search_papers"},
})
if err != nil {
    log.Fatal(err)
}
fmt.Println(task.Status.State, *task.Artifacts[0].Parts[0].Text)
```

With the rules from step 2, this prints `completed` and the quantum response.

## Troubleshoot

- The call fails with `no matching rule`: no rule matched the skill ID and message. Check that the request carries `metadata.skillId` and that the skill ID matches a rule.
- A specific matcher never fires: an earlier rule for the same skill matched first. Move the specific rule up.
- `URL()` panics: call `Start()` first.

## Related

- [Use the A2A tool bridge](/runtime/how-to/a2a/use-a2a-tool-bridge/) to expose the mock agent's skills as tools.
- [Test A2A agents in Arena](https://promptarena.altairalabs.ai/arena/how-to/test-a2a-agents/)
- [Runtime A2A reference](/runtime/reference/a2a/)
