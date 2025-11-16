---
layout: docs
title: SDK How-To Guides
nav_order: 2
parent: SDK
has_children: true
---

# SDK How-To Guides

Practical, task-focused guides for common SDK operations.

## Getting Started

- **[Initialize the SDK](initialize.md)** - Set up SDK in your application
- **[Load PromptPacks](load-packs.md)** - Load and validate .pack.json files
- **[Send Messages](send-messages.md)** - Send user messages and get responses

## Conversations

- **[Create Conversations](create-conversations.md)** - Start new conversations
- **[Manage State](manage-state.md)** - Persist conversation history
- **[Stream Responses](stream-responses.md)** - Use streaming for real-time output

## Tools & Functions

- **[Register Tools](register-tools.md)** - Add function calling capabilities
- **[Handle Tool Calls](handle-tool-calls.md)** - Process LLM tool requests
- **[Human-in-the-Loop](hitl-workflows.md)** - Implement approval workflows

## Advanced Topics

- **[Custom Middleware](custom-middleware.md)** - Add observability and metrics
- **[Error Handling](error-handling.md)** - Handle failures gracefully
- **[Configure Context](configure-context.md)** - Manage token budgets
- **[Test Applications](test-sdk-apps.md)** - Unit and integration testing

## Quick Links

### Common Tasks

**Initialize SDK:**
```go
manager, _ := sdk.NewConversationManager(
    sdk.WithProvider(provider),
)
```

**Load Pack:**
```go
pack, _ := manager.LoadPack("./prompts.pack.json")
```

**Create Conversation:**
```go
conv, _ := manager.NewConversation(ctx, pack, sdk.ConversationConfig{
    UserID:     "user123",
    PromptName: "assistant",
})
```

**Send Message:**
```go
resp, _ := conv.Send(ctx, "Hello")
```

**Stream Response:**
```go
ch, _ := conv.SendStream(ctx, "Tell me a story")
for event := range ch {
    fmt.Print(event.Chunk.Text)
}
```

## By Use Case

### Building a Chatbot

1. [Initialize the SDK](initialize.md)
2. [Load PromptPacks](load-packs.md)
3. [Create Conversations](create-conversations.md)
4. [Send Messages](send-messages.md)
5. [Manage State](manage-state.md)

### Adding Function Calling

1. [Register Tools](register-tools.md)
2. [Handle Tool Calls](handle-tool-calls.md)
3. [Error Handling](error-handling.md)

### Production Deployment

1. [Manage State](manage-state.md) - Use Redis/Postgres
2. [Configure Context](configure-context.md) - Manage token costs
3. [Custom Middleware](custom-middleware.md) - Add metrics
4. [Error Handling](error-handling.md) - Production patterns

### Advanced Customization

1. [Custom Middleware](custom-middleware.md)
2. [Human-in-the-Loop](hitl-workflows.md)
3. [Test Applications](test-sdk-apps.md)

## See Also

- **[Reference Documentation](../reference/)** - Complete API reference
- **[Tutorials](../tutorials/)** - Step-by-step learning
- **[Examples](https://github.com/AltairaLabs/PromptKit/tree/main/sdk/examples)** - Code examples
