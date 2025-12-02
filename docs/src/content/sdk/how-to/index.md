---
title: SDK How-To Guides
docType: how-to
order: 2
---
# SDK How-To Guides

Practical, task-focused guides for common SDK operations.

## Getting Started

- **[Initialize the SDK](initialize)** - Set up SDK in your application
- **[Load PromptPacks](load-packs)** - Load and validate .pack.json files
- **[Send Messages](send-messages)** - Send user messages and get responses

## Conversations

- **[Create Conversations](create-conversations)** - Start new conversations
- **[Manage State](manage-state)** - Persist conversation history
- **[Stream Responses](stream-responses)** - Use streaming for real-time output

## Tools & Functions

- **[Register Tools](register-tools)** - Add function calling capabilities
- **[Handle Tool Calls](handle-tool-calls)** - Process LLM tool requests
- **[Human-in-the-Loop](hitl-workflows)** - Implement approval workflows

## Observability & Monitoring

- **[Monitor Pipeline Events](monitor-events)** - Track execution with the event system
- **[Custom Middleware](custom-middleware)** - Add observability and metrics

## Advanced Topics

- **[Configure Media Storage](configure-media-storage)** - Optimize memory for media
- **[Error Handling](error-handling)** - Handle failures gracefully
- **[Configure Context](configure-context)** - Manage token budgets
- **[Test Applications](test-sdk-apps)** - Unit and integration testing

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

1. [Initialize the SDK](initialize)
2. [Load PromptPacks](load-packs)
3. [Create Conversations](create-conversations)
4. [Send Messages](send-messages)
5. [Manage State](manage-state)

### Adding Function Calling

1. [Register Tools](register-tools)
2. [Handle Tool Calls](handle-tool-calls)
3. [Error Handling](error-handling)

### Production Deployment

1. [Manage State](manage-state) - Use Redis/Postgres
2. [Configure Context](configure-context) - Manage token costs
3. [Custom Middleware](custom-middleware) - Add metrics
4. [Error Handling](error-handling) - Production patterns

### Working with Media

1. [Configure Media Storage](configure-media-storage) - Images, audio, video
2. [Handle Streaming](stream-responses) - Real-time media processing

### Advanced Customization

1. [Custom Middleware](custom-middleware)
2. [Human-in-the-Loop](hitl-workflows)
3. [Test Applications](test-sdk-apps)

## See Also

- **[Reference Documentation](../reference/)** - Complete API reference
- **[Tutorials](../tutorials/)** - Step-by-step learning
- **[Examples](https://github.com/AltairaLabs/PromptKit/tree/main/sdk/examples)** - Code examples
