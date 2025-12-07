---
title: SDK How-To Guides
docType: how-to
order: 2
---
# SDK v2 How-To Guides

Practical, task-focused guides for common SDK operations.

## Getting Started

- **[Open a Conversation](initialize)** - Use `sdk.Open()` to get started
- **[Send Messages](send-messages)** - Send messages with `Send()` and `Stream()`

## Tools & Functions

- **[Register Tools](register-tools)** - Add tools with `OnTool()`
- **[HTTP Tools](http-tools)** - External API calls with `OnToolHTTP()`
- **[HITL Workflows](hitl-workflows)** - Approval with `OnToolAsync()`

## Variables & Templates

- **[Manage Variables](manage-state)** - Use `SetVar()` and `GetVar()`

## Observability

- **[Monitor Events](monitor-events)** - Subscribe to events with `Subscribe()`

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
conv.Subscribe(hooks.EventSend, func(e hooks.Event) {
    log.Printf("Sent: %v", e.Data["message"])
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

### Building Safe AI Agents

1. [HITL Workflows](hitl-workflows)
2. [Monitor Events](monitor-events)

## See Also

- **[Tutorials](../tutorials/)** - Step-by-step learning
- **[Reference Documentation](../reference/)** - API reference
- **[Examples](/sdk/examples/)** - Working code examples
