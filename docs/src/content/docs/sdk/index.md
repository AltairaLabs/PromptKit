---
title: PromptKit SDK
description: Pack-first Go SDK for building LLM applications with minimal boilerplate
sidebar:
  order: 0
---
**Pack-first Go SDK that reduces boilerplate by ~80%**

---

## What is the PromptKit SDK?

The SDK uses a **pack-first architecture** that dramatically simplifies LLM application development:

- **5 lines to hello world** - Open a pack, send a message, done
- **Pack-first design** - Load prompts tested with Arena, compiled with PackC
- **Built-in tools** - Register handlers with `OnTool`, auto JSON serialization
- **Streaming support** - Channel-based streaming with `Stream()`
- **Human-in-the-Loop** - Approval workflows for sensitive operations
- **Type-safe variables** - `SetVar`/`GetVar` with concurrent access
- **Observability** - EventBus integration for monitoring

---

## Quick Start

```go
package main

import (
    "context"
    "fmt"
    "log"

    "github.com/AltairaLabs/PromptKit/sdk"
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

That's it. Five lines of functional code.

---

## Core API

### Opening Conversations

```go
// Open from a pack file with a specific prompt
conv, err := sdk.Open("./myapp.pack.json", "assistant")

// Open with options
conv, err := sdk.Open("./myapp.pack.json", "assistant",
    sdk.WithModel("gpt-4o"),
    sdk.WithTemperature(0.7),
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
conv.SetVar("context", map[string]any{"role": "admin"})

// Get variables
name := conv.GetVar("user_name")

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
import "github.com/AltairaLabs/PromptKit/sdk/tools"

conv.OnToolHTTP("stock_price", &tools.HTTPToolConfig{
    BaseURL: "https://api.stocks.example.com",
    Method:  "GET",
    Path:    "/v1/price",
    Headers: map[string]string{"Authorization": "Bearer " + apiKey},
})
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
import "github.com/AltairaLabs/PromptKit/sdk/tools"

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
    result, _ := conv.ResolveTool(pending.ID)  // Approve
    // result, _ := conv.RejectTool(pending.ID, "Not authorized")  // Reject
}
```

---

## Observability

Monitor events with hooks:

```go
import "github.com/AltairaLabs/PromptKit/sdk/hooks"

// Subscribe to events
conv.Subscribe(hooks.EventSend, func(e hooks.Event) {
    fmt.Printf("Sending: %s\n", e.Data["message"])
})

conv.Subscribe(hooks.EventToolCall, func(e hooks.Event) {
    fmt.Printf("Tool called: %s\n", e.Data["tool"])
})
```

---

## Error Handling

```go
resp, err := conv.Send(ctx, input)
if err != nil {
    switch {
    case errors.Is(err, sdk.ErrPackNotFound):
        // Pack file doesn't exist
    case errors.Is(err, sdk.ErrPromptNotFound):
        // Prompt ID not in pack
    case errors.Is(err, sdk.ErrProviderError):
        // LLM provider error
    case errors.Is(err, sdk.ErrToolNotRegistered):
        // Tool handler missing
    default:
        log.Printf("Unexpected error: %v", err)
    }
}
```

---

## Examples

Working examples are available in the `sdk/examples/` directory:

- **[hello](/sdk/examples/hello/)** - Basic conversation in 5 lines
- **[tools](/sdk/examples/tools/)** - Tool registration and execution
- **[streaming](/sdk/examples/streaming/)** - Real-time response streaming
- **[hitl](/sdk/examples/hitl/)** - Human-in-the-loop approval workflows

---

## Getting Help

- **Questions**: [GitHub Discussions](https://github.com/AltairaLabs/PromptKit/discussions)
- **Issues**: [Report a Bug](https://github.com/AltairaLabs/PromptKit/issues)
- **Examples**: [SDK Examples](/sdk/examples/)

---

## A2A Server

Expose your agent as an A2A-compliant service:

```go
opener := sdk.A2AOpener("./assistant.pack.json", "chat")
server := sdk.NewA2AServer(opener,
    sdk.WithA2ACard(&card),
    sdk.WithA2APort(9999),
)
server.ListenAndServe()
```

- **[A2A Server Tutorial](/sdk/tutorials/10-a2a-server/)** — step-by-step guide
- **[A2A Server Reference](/sdk/reference/a2a-server/)** — complete API docs

---

## Related Tools

- **Arena**: [Test prompts before using them](/arena/)
- **PackC**: [Compile prompts into packs](/packc/)
- **Runtime**: [Extend the SDK with custom providers](/runtime/)
- **Complete Workflow**: [See all tools together](/getting-started/complete-workflow/)
