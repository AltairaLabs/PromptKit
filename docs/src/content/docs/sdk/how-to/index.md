---
title: SDK How-To Guides
sidebar:
  order: 0
---
Practical, task-focused guides for common SDK operations.

## Getting Started

- **[Open a Conversation](/sdk/how-to/initialize/)** - Use `sdk.Open()` to get started
- **[Send Messages](/sdk/how-to/send-messages/)** - Send messages with `Send()` and `Stream()`

## Tools & Functions

- **[Register Tools](/sdk/how-to/register-tools/)** - Add tools with `OnTool()`
- **[HTTP Tools](/sdk/how-to/http-tools/)** - External API calls with `OnToolHTTP()`
- **[Client-Side Tools](/sdk/how-to/client-tools/)** - Device tools with `OnClientTool()`

## Integrations

- **[Configure MCP Servers](/sdk/how-to/configure-mcp/)** - Connect MCP tool servers with `NewMCPServer()` and `WithMCPServer()`
- **[Connect A2A Agents](/sdk/how-to/connect-a2a-agents/)** - Register remote A2A agents with `NewA2AAgent()` and `WithA2AAgent()`

## Declarative Configuration

- **[Use RuntimeConfig](/sdk/how-to/use-runtime-config/)** - Configure the SDK declaratively with a YAML file
- **[Exec Tools](/sdk/how-to/exec-tools/)** - Bind tools to external subprocesses in any language
- **[Exec Hooks](/sdk/how-to/exec-hooks/)** - Add external hooks (provider, tool, session) via subprocesses

## Variables & Templates

- **[Manage Variables](/sdk/how-to/manage-state/)** - Use `SetVar()` and `GetVar()`

## Context Management

- **[Manage Context](/sdk/how-to/manage-context/)** - Configure token budgets and relevance-based truncation

## Media Processing

- **[Preprocess Images](/sdk/how-to/preprocess-images/)** - Automatically resize and optimize images before sending to LLM providers

## Evaluation

- **[Run Evals](/sdk/how-to/run-evals/)** - Evaluate conversations with `sdk.Evaluate()`

## Observability

- **[Monitor Events](/sdk/how-to/monitor-events/)** - Subscribe to events with `hooks.On()`

## Quick Reference

### Open a Conversation

```go
conv, err := sdk.Open("./app.pack.json", "assistant")
if err != nil {
    log.Fatal(err)
}
defer conv.Close()
```

### Send Message

```go
resp, err := conv.Send(ctx, "Hello!")
fmt.Println(resp.Text())
```

### Stream Response

```go
for chunk := range conv.Stream(ctx, "Tell me a story") {
    if chunk.Type == sdk.ChunkDone {
        break
    }
    fmt.Print(chunk.Text)
}
```

### Register Tool

```go
conv.OnTool("get_time", func(args map[string]any) (any, error) {
    return time.Now().Format(time.RFC3339), nil
})
```

### Set Variables

```go
conv.SetVar("user_name", "Alice")
conv.SetVars(map[string]any{
    "role":     "admin",
    "language": "en",
})
```

### Subscribe to Events

```go
hooks.On(conv, events.EventProviderCallCompleted, func(e *events.Event) {
    log.Printf("Provider call completed: %s", e.Type)
})
```

## By Use Case

### Building a Chatbot

1. [Open a Conversation](/sdk/how-to/initialize/)
2. [Send Messages](/sdk/how-to/send-messages/)
3. [Manage Variables](/sdk/how-to/manage-state/)

### Adding Function Calling

1. [Register Tools](/sdk/how-to/register-tools/)
2. [HTTP Tools](/sdk/how-to/http-tools/) (for external APIs)
3. [Configure MCP Servers](/sdk/how-to/configure-mcp/) (for MCP tools)

### Connecting External Agents

1. [Connect A2A Agents](/sdk/how-to/connect-a2a-agents/)
2. [Configure MCP Servers](/sdk/how-to/configure-mcp/)

### Declarative Setup

1. [Use RuntimeConfig](/sdk/how-to/use-runtime-config/) (replace boilerplate with YAML)
2. [Exec Tools](/sdk/how-to/exec-tools/) (tools in Python, Node.js, etc.)
3. [Exec Hooks](/sdk/how-to/exec-hooks/) (external guardrails and audit)

### Building Safe AI Agents

1. [Register Tools](/sdk/how-to/register-tools/) (see Async Tools section for HITL)
2. [Exec Hooks](/sdk/how-to/exec-hooks/) (external guardrails)
3. [Monitor Events](/sdk/how-to/monitor-events/)

### Evaluating Conversation Quality

1. [Run Evals](/sdk/how-to/run-evals/)
2. [Monitor Events](/sdk/how-to/monitor-events/)

## See Also

- **[Tutorials](/sdk/tutorials/)** - Step-by-step learning
- **[Reference Documentation](/sdk/reference/)** - API reference
- **[Examples](/sdk/examples/)** - Working code examples
