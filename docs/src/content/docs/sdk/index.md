---
title: PromptKit SDK
description: "The pack-first Go SDK: open a pack, send messages, register tools, stream responses and run workflows"
sidebar:
  order: 0
verified:
  commit: 885cafa787a84bfba66f158fe51cda15a077ec23
  sources:
    - runtime/a2a/types.go
    - runtime/evals/handlers/skill_activated.go
    - runtime/events/types.go
    - runtime/prompt/schema/promptpack.schema.json
    - runtime/skills/types.go
    - runtime/workflow/types.go
    - sdk/a2a_exports.go
    - sdk/conversation_tools.go
    - sdk/errors.go
    - sdk/hooks/events.go
    - sdk/options.go
    - sdk/sdk.go
    - sdk/streaming.go
    - sdk/tools/http.go
    - sdk/workflow.go
---

## What is the PromptKit SDK?

The SDK is a Go library that runs conversations from a pack, the compiled JSON file that holds prompts, tools and validators:

- Pack-first: load prompts tested with Arena and compiled with PackC
- Tools: register handlers with `OnTool`; results are serialized to JSON
- Skills: [AgentSkills.io](https://agentskills.io) support with progressive disclosure
- Workflows: event-driven state machines with orchestration modes
- A2A: serve an agent, or call other agents
- Streaming: channel-based streaming with `Stream()`
- Human-in-the-loop: approval workflows for sensitive operations
- Variables: `SetVar`/`GetVar` with concurrent access
- Observability: event bus integration for monitoring

---

## Quick Start

```go
package main

import (
    "context"
    "fmt"
    "log"

    "github.com/AltairaLabs/PromptKit/sdk/v2"
)

func main() {
    // Open a conversation from a pack file
    conv, err := sdk.Open("./hello.pack.json", "chat")
    if err != nil {
        log.Fatal(err)
    }
    defer conv.Close()

    // Send a message and get a response
    resp, _ := conv.Send(context.Background(), "Hello!")
    fmt.Println(resp.Text())
}
```

---

## Core API

### Opening Conversations

```go
// Open from a pack file with a specific prompt
conv, err := sdk.Open("./myapp.pack.json", "assistant")

// Open with options
conv, err := sdk.Open("./myapp.pack.json", "assistant",
    sdk.WithModel("gpt-4o"),
)
```

### Sending Messages

```go
// Simple send
resp, err := conv.Send(ctx, "What's the weather?")
fmt.Println(resp.Text())

// Multi-turn conversations (context is maintained)
resp1, _ := conv.Send(ctx, "My name is Alice")
resp2, _ := conv.Send(ctx, "What's my name?") // "Alice"
```

### Template Variables

```go
// Set variables for prompt templates
conv.SetVar("user_name", "Alice")
conv.SetVar("context", "admin role context")

// Get variables
name, ok := conv.GetVar("user_name")

// Bulk operations
conv.SetVars(map[string]any{
    "user_name": "Alice",
    "language":  "en",
})
```

---

## Tool Handling

Register handlers that the LLM can call:

```go
conv.OnTool("get_weather", func(args map[string]any) (any, error) {
    city := args["city"].(string)
    
    // Return any JSON-serializable value
    return map[string]any{
        "city":        city,
        "temperature": 22.5,
        "conditions":  "Sunny",
    }, nil
})

// The LLM can now call this tool
resp, _ := conv.Send(ctx, "What's the weather in London?")
```

### HTTP Tools

For external API calls:

```go
import "github.com/AltairaLabs/PromptKit/sdk/v2/tools"

conv.OnToolHTTP("stock_price", tools.NewHTTPToolConfig(
    "https://api.stocks.example.com/v1/price",
    tools.WithMethod("GET"),
    tools.WithHeader("Authorization", "Bearer "+apiKey),
))
```

---

## Streaming

Real-time response streaming:

```go
// Channel-based streaming
for chunk := range conv.Stream(ctx, "Tell me a story") {
    if chunk.Error != nil {
        log.Printf("Error: %v", chunk.Error)
        break
    }
    if chunk.Type == sdk.ChunkDone {
        break
    }
    fmt.Print(chunk.Text)
}
```

---

## Human-in-the-Loop (HITL)

Approval workflows for sensitive operations:

```go
import "github.com/AltairaLabs/PromptKit/sdk/v2/tools"

conv.OnToolAsync(
    "process_refund",
    // Check if approval is needed
    func(args map[string]any) tools.PendingResult {
        amount := args["amount"].(float64)
        if amount > 100 {
            return tools.PendingResult{
                Reason:  "high_value",
                Message: fmt.Sprintf("$%.2f refund requires approval", amount),
            }
        }
        return tools.PendingResult{} // Auto-approve
    },
    // Execute after approval
    func(args map[string]any) (any, error) {
        return map[string]any{"status": "completed"}, nil
    },
)

// Handle pending approvals
resp, _ := conv.Send(ctx, "Refund $150 for order #123")
for _, pending := range resp.PendingTools() {
    fmt.Printf("Pending: %s - %s\n", pending.Name, pending.Message)
    
    // Approve or reject
    result, _ := conv.ResolveTool(ctx, pending.ID)  // Approve
    // result, _ := conv.RejectTool(ctx, pending.ID, "Not authorized")  // Reject
}
```

---

## Observability

Monitor events with hooks:

```go
import (
    "github.com/AltairaLabs/PromptKit/sdk/v2/hooks"
    "github.com/AltairaLabs/PromptKit/runtime/v2/events"
)

// Subscribe to events
hooks.On(conv, events.EventProviderCallCompleted, func(e *events.Event) {
    fmt.Printf("Provider call completed: %s\n", e.Type)
})

hooks.OnToolCall(conv, func(name string, args map[string]any) {
    fmt.Printf("Tool called: %s\n", name)
})
```

---

## Error Handling

`Open` returns a `*sdk.PackError` when the pack file is missing, unreadable or invalid, and sentinels for the other setup failures:

```go
conv, err := sdk.Open("./app.pack.json", "chat")
if err != nil {
    var packErr *sdk.PackError
    switch {
    case errors.Is(err, sdk.ErrPackNotFound):
        // Pack file doesn't exist (also a *PackError)
    case errors.As(err, &packErr):
        // Pack failed to load or validate: packErr.Path, packErr.Cause
    case errors.Is(err, sdk.ErrPromptNotFound):
        // Prompt ID not in pack
    case errors.Is(err, sdk.ErrProviderNotDetected):
        // No provider API key found
    }
}
```

`Send` and `Stream` return a `*sdk.ProviderError` when the provider's API answers with an error status or the connection fails, and a `*sdk.ValidationError` when a hook denies the turn. Each wraps the runtime error, so `errors.As` still finds `*providers.ProviderHTTPError` or `*hooks.HookDeniedError`:

```go
resp, err := conv.Send(ctx, input)
if err != nil {
    var provErr *sdk.ProviderError
    switch {
    case errors.As(err, &provErr):
        // provErr.Provider, provErr.StatusCode
    default:
        if vErr, ok := sdk.AsValidationError(err); ok {
            // vErr.ValidatorType, vErr.Message
        }
    }
}
```

A tool handler approved with `ResolveTool` that fails sets a `*sdk.ToolError` as the resolution's `Error`. `ResolveTool` returns an error matching `sdk.ErrToolNotRegistered` when the held call's tool has no handler.

---

## Examples

Working examples are available in the `sdk/examples/` directory:

- **[hello](/sdk/examples/hello/)** - Basic conversation in 5 lines
- **[tools](/sdk/examples/tools/)** - Tool registration and execution
- **[streaming](/sdk/examples/streaming/)** - Real-time response streaming
- **[hitl](/sdk/examples/hitl/)** - Human-in-the-loop approval workflows
- **[workflow-external](/sdk/examples/workflow-external/)** - External orchestration via HTTP

---

## Getting Help

- **Questions and bugs**: [GitHub Issues](https://github.com/AltairaLabs/PromptKit/issues)
- **Examples**: [SDK Examples](/sdk/examples/)

---

## Workflows

Build stateful, multi-step conversations with event-driven state machines:

```go
// Open a workflow from a pack with a workflow section
wf, err := sdk.OpenWorkflow("./support.pack.json")
if err != nil {
    log.Fatal(err)
}
defer wf.Close()

// Send a message in the current state
resp, _ := wf.Send(ctx, "I need help with billing")
fmt.Println(resp.Text())

// Trigger a state transition
newState, _ := wf.Transition("Escalate")
fmt.Printf("Now in state: %s\n", newState)

// Query workflow state
fmt.Println(wf.CurrentState())      // "specialist"
fmt.Println(wf.IsComplete())        // false
fmt.Println(wf.AvailableEvents())   // ["Resolve"]
```

### Orchestration Modes

States can declare an orchestration mode that controls who drives transitions:

```go
mode := wf.OrchestrationMode() // "internal", "external", "hybrid" or "composition"
```

- **Internal** — The agent drives transitions automatically
- **External** — External callers (HTTP endpoints, queues) drive transitions via `Transition()`
- **Hybrid** — Both the agent and external callers can trigger transitions
- **Composition** — The state runs a declarative step graph instead of a model turn

### Resuming Workflows

Restore a saved workflow:

```go
wf, err := sdk.ResumeWorkflow("workflow-id", "./support.pack.json")
```

### Context Carry-Forward

When enabled, a summary of the previous state's conversation is injected as context into the next state:

```go
wf, err := sdk.OpenWorkflow("./support.pack.json",
    sdk.WithContextCarryForward(),
)
```

---

## A2A Server

Expose your agent as an A2A-compliant service:

```go
opener := sdk.A2AOpener("./assistant.pack.json", "chat")
card := a2a.AgentCard{Name: "assistant"} // github.com/AltairaLabs/PromptKit/runtime/v2/a2a
server := sdk.NewA2AServer(opener,
    sdk.WithA2ACard(&card),
    sdk.WithA2APort(9999),
)
server.ListenAndServe()
```

- **[A2A Server Tutorial](/sdk/tutorials/10-a2a-server/)** — step-by-step guide
- **[A2A Server Reference](/sdk/reference/a2a-server/)** — complete API docs

---

## Skills

Load knowledge and instructions on demand using the [AgentSkills.io](https://agentskills.io) standard:

```go
// Skills auto-detected from pack's skills section
conv, _ := sdk.Open("./support.pack.json", "assistant")

// Or configure programmatically
conv, _ := sdk.Open("./base.pack.json", "assistant",
    sdk.WithSkillsDir("./skills"),
    sdk.WithMaxActiveSkillsOption(5),
)
```

Skills use progressive disclosure — only name + description (~50 tokens each) are loaded at startup. Full instructions load on demand when the model calls `skill__activate`. Skills can also extend the available tool set (capped by the pack's declared tools).

Workflow states can scope which skills are available by pointing to a skills subdirectory:

```yaml
workflow:
  states:
    billing:
      prompt_task: billing_agent
      skills: ./skills/billing     # Only billing skills available
```

- **[Skills Concept](/concepts/skills/)** — how skills work, progressive disclosure, three-level tool scoping

---

## Related Tools

- **Arena**: [Test prompts before using them](https://promptarena.altairalabs.ai/arena/)
- **PackC**: [Compile prompts into packs](https://promptarena.altairalabs.ai/packc/)
- **Runtime**: [Extend the SDK with custom providers](/runtime/)
- **Complete Workflow**: [See all tools together](/)
