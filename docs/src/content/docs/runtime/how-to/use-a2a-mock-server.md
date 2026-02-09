---
title: Use Mock Server
description: Set up a mock A2A server for deterministic testing
sidebar:
  order: 11
---

The `a2a/mock` package provides a lightweight, in-process A2A server backed by `httptest.Server`. Use it for fast, deterministic tests without needing a real agent.

---

## Why Mock A2A?

- **Fast** — no network overhead, no LLM calls
- **Deterministic** — canned responses for reproducible tests
- **No external dependencies** — runs entirely in-process
- **Configurable** — skill routing, input matching, latency/error injection

---

## Basic Setup

```go
package main

import (
    "context"
    "fmt"
    "log"

    "github.com/AltairaLabs/PromptKit/runtime/a2a"
    "github.com/AltairaLabs/PromptKit/runtime/a2a/mock"
)

func main() {
    ctx := context.Background()

    text := "Found 3 papers on quantum computing"
    server := mock.NewA2AServer(
        &a2a.AgentCard{
            Name: "Research Agent",
            Skills: []a2a.AgentSkill{
                {ID: "search_papers", Name: "Search Papers"},
            },
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

    // Use the mock like any A2A server.
    client := a2a.NewClient(url)
    card, _ := client.Discover(ctx)
    fmt.Printf("Agent: %s\n", card.Name)
}
```

---

## Rule Matching

Rules are evaluated in order — the first match wins. Each rule targets a skill ID and optionally applies a matcher to the message content.

### Skill ID Routing

The mock extracts the skill ID from message or request metadata (`metadata.skillId`) and matches it against rules:

```go
mock.WithSkillResponse("search_papers", mock.Response{
    Parts: []a2a.Part{{Text: &searchResult}},
})

mock.WithSkillResponse("summarize", mock.Response{
    Parts: []a2a.Part{{Text: &summary}},
})
```

### Input Matchers

Use `WithInputMatcher` for content-based routing:

```go
quantumResult := "Papers about quantum computing..."
generalResult := "General research results..."

mock.WithInputMatcher("search_papers",
    func(msg a2a.Message) bool {
        for _, p := range msg.Parts {
            if p.Text != nil && strings.Contains(*p.Text, "quantum") {
                return true
            }
        }
        return false
    },
    mock.Response{Parts: []a2a.Part{{Text: &quantumResult}}},
)

// Fallback: matches any message to search_papers.
mock.WithSkillResponse("search_papers", mock.Response{
    Parts: []a2a.Part{{Text: &generalResult}},
})
```

More specific matchers should come before general fallbacks since first match wins.

---

## Latency Injection

Add a delay before each response to test timeout handling:

```go
server := mock.NewA2AServer(card,
    mock.WithLatency(2 * time.Second),
    mock.WithSkillResponse("search_papers", response),
)
```

---

## Error Injection

Return a failed task for a specific skill:

```go
server := mock.NewA2AServer(card,
    mock.WithSkillError("search_papers", "Service unavailable"),
)
```

The mock returns a task with `status.state: "failed"` and the error message in the status message.

---

## Config-Driven Rules

For Arena integration or file-based configuration, define rules as `AgentConfig`:

```go
import "github.com/AltairaLabs/PromptKit/runtime/a2a/mock"

cfg := &mock.AgentConfig{
    Name: "research_agent",
    Card: a2a.AgentCard{
        Name: "Research Agent",
        Skills: []a2a.AgentSkill{
            {ID: "search_papers", Name: "Search Papers"},
        },
    },
    Responses: []mock.RuleConfig{
        {
            Skill: "search_papers",
            Match: &mock.MatchConfig{Contains: "quantum"},
            Response: &mock.ResponseConfig{
                Parts: []mock.PartConfig{{Text: "Quantum papers found"}},
            },
        },
        {
            Skill: "search_papers",
            Response: &mock.ResponseConfig{
                Parts: []mock.PartConfig{{Text: "General papers found"}},
            },
        },
    },
}

opts := mock.OptionsFromConfig(cfg)
server := mock.NewA2AServer(&cfg.Card, opts...)
```

### Match Config

| Field | Description |
|-------|-------------|
| `contains` | Case-insensitive substring match on message text |
| `regex` | Regular expression match on message text |

Both conditions must match if both are specified.

---

## YAML Configuration

The same config works in YAML (used by Arena):

```yaml
a2a_agents:
  - name: research_agent
    card:
      name: Research Agent
      description: Mock research agent
      skills:
        - id: search_papers
          name: Search Papers
          description: Search for academic papers
          tags: [research]
    responses:
      - skill: search_papers
        match:
          contains: quantum
        response:
          parts:
            - text: "Found 3 papers on quantum computing"
      - skill: search_papers
        response:
          parts:
            - text: "Found 1 general paper"
```

---

## Next Steps

- [Test A2A Agents in Arena](/arena/how-to/test-a2a-agents/) — use mock agents in Arena scenarios
- [Use Tool Bridge](/runtime/how-to/use-a2a-tool-bridge/) — pair mock server with tool bridge for integration tests
- [Runtime A2A Reference](/runtime/reference/a2a/) — mock server API details
