---
title: SDK How-To Guides
sidebar:
  order: 0
---
Practical, task-focused guides for common SDK operations.

## Getting Started

- **[Open a Conversation](initialize)** - Use `sdk.Open()` to get started
- **[Send Messages](send-messages)** - Send messages with `Send()` and `Stream()`

## Tools & Functions

- **[Register Tools](register-tools)** - Add tools with `OnTool()`
- **[HTTP Tools](http-tools)** - External API calls with `OnToolHTTP()`
- **[Client-Side Tools](client-tools)** - Device tools with `OnClientTool()`

## Integrations

- **[Configure MCP Servers](configure-mcp)** - Connect MCP tool servers with `NewMCPServer()` and `WithMCPServer()`
- **[Connect A2A Agents](connect-a2a-agents)** - Register remote A2A agents with `NewA2AAgent()` and `WithA2AAgent()`

## Declarative Configuration

- **[Use RuntimeConfig](use-runtime-config)** - Configure the SDK declaratively with a YAML file
- **[Exec Tools](exec-tools)** - Bind tools to external subprocesses in any language
- **[Exec Hooks](exec-hooks)** - Add external hooks (provider, tool, session) via subprocesses

## Variables & Templates

- **[Manage Variables](manage-state)** - Use `SetVar()` and `GetVar()`

## Context Management

- **[Manage Context](manage-context)** - Configure token budgets and relevance-based truncation

## Media Processing

- **[Preprocess Images](preprocess-images)** - Automatically resize and optimize images before sending to LLM providers

## Evaluation

- **[Run Evals](run-evals)** - Evaluate conversations with `sdk.Evaluate()`

## Observability

- **[Monitor Events](monitor-events)** - Subscribe to events with `hooks.On()`

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

1. [Open a Conversation](initialize)
2. [Send Messages](send-messages)
3. [Manage Variables](manage-state)

### Adding Function Calling

1. [Register Tools](register-tools)
2. [HTTP Tools](http-tools) (for external APIs)
3. [Configure MCP Servers](configure-mcp) (for MCP tools)

### Connecting External Agents

1. [Connect A2A Agents](connect-a2a-agents)
2. [Configure MCP Servers](configure-mcp)

### Declarative Setup

1. [Use RuntimeConfig](use-runtime-config) (replace boilerplate with YAML)
2. [Exec Tools](exec-tools) (tools in Python, Node.js, etc.)
3. [Exec Hooks](exec-hooks) (external guardrails and audit)

### Building Safe AI Agents

1. [Register Tools](register-tools) (see Async Tools section for HITL)
2. [Exec Hooks](exec-hooks) (external guardrails)
3. [Monitor Events](monitor-events)

### Evaluating Conversation Quality

1. [Run Evals](run-evals)
2. [Monitor Events](monitor-events)

## See Also

- **[Tutorials](/sdk/tutorials/)** - Step-by-step learning
- **[Reference Documentation](/sdk/reference/)** - API reference
- **[Examples](/sdk/examples/)** - Working code examples
