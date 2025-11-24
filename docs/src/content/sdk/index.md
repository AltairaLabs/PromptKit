---
title: PromptKit SDK
description: Production-ready Go library for building robust LLM applications
docType: guide
order: 4
---
# 🚀 PromptKit SDK

**Production-ready Go library for building robust LLM applications**

---

## What is the PromptKit SDK?

The SDK is a comprehensive Go library that helps you:

- **Build conversational AI** with type-safe, production-ready code
- **Load PromptPacks** tested with Arena and compiled with PackC
- **Manage state** with Redis, Postgres, or in-memory storage
- **Handle streaming** responses with elegant APIs
- **Abstract providers** to switch between OpenAI, Anthropic, Google seamlessly
- **Add middleware** for logging, filtering, and custom processing

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
    ctx := context.Background()

    config := &sdk.Config{
        Provider: "openai",
        Model:    "gpt-4",
    }

    manager, err := sdk.NewConversationManager(ctx, config)
    if err != nil {
        log.Fatal(err)
    }
    defer manager.Close()

    conv, _ := manager.NewConversation(ctx, "my-bot")
    response, _ := conv.SendMessage(ctx, "Hello!")
    
    fmt.Println(response)
}
```

**Next**: [Build Your First Chatbot Tutorial](/sdk/tutorials/01-first-chatbot/)

---

## Documentation by Type

### 📚 Tutorials (Learn by Doing)

Step-by-step guides that teach you the SDK through building real applications:

1. [Your First Chatbot](/sdk/tutorials/01-first-chatbot/) - Build in 15 minutes
2. [Conversation State](/sdk/tutorials/02-conversation-state/) - Manage context
3. [Tool Integration](/sdk/tutorials/03-tool-integration/) - Add function calling
4. [Custom Middleware](/sdk/tutorials/04-custom-middleware/) - Extend the pipeline
5. [State Persistence](/sdk/tutorials/05-state-persistence/) - Redis and Postgres
6. [Production Deployment](/sdk/tutorials/06-production-deployment/) - Deploy at scale

### 🔧 How-To Guides (Accomplish Specific Tasks)

Focused guides for specific SDK tasks:

- [Installation](/sdk/how-to/installation/) - Add SDK to your project
- [Load PromptPacks](/sdk/how-to/load-promptpacks/) - Use compiled prompts
- [Manage Conversations](/sdk/how-to/manage-conversations/) - Conversation lifecycle
- [Implement Tools](/sdk/how-to/implement-tools/) - MCP and custom tools
- [Add Middleware](/sdk/how-to/add-middleware/) - Custom processing
- [Configure State](/sdk/how-to/configure-state/) - State management options
- [Configure Media Storage](/sdk/how-to/configure-media-storage/) - Optimize memory for media
- [Handle Streaming](/sdk/how-to/handle-streaming/) - Real-time responses
- [Error Handling](/sdk/how-to/error-handling/) - Robust error management
- [Deploy to Production](/sdk/how-to/deploy-production/) - Deployment patterns

### 💡 Explanation (Understand the Concepts)

Deep dives into SDK architecture and design:

- [Conversation Lifecycle](/sdk/explanation/conversation-lifecycle/) - How conversations work
- [Pipeline Architecture](/sdk/explanation/pipeline-architecture/) - Request/response flow
- [Middleware System](/sdk/explanation/middleware-system/) - Processing layers
- [State Management](/sdk/explanation/state-management/) - Persistence patterns
- [Provider Abstraction](/sdk/explanation/provider-abstraction/) - Multi-provider support

### 📖 Reference (Look Up Details)

Complete API documentation:

- [ConversationManager](/sdk/reference/api/conversation-manager/) - Core conversation API
- [PipelineBuilder](/sdk/reference/api/pipeline-builder/) - Build custom pipelines
- [PackLoader](/sdk/reference/api/pack-loader/) - Load PromptPacks
- [ToolRegistry](/sdk/reference/api/tool-registry/) - Register and manage tools
- [StateStore](/sdk/reference/api/state-store/) - State storage operations
- [SDK Configuration](/sdk/reference/configuration/sdk-config/) - Config options
- [Provider Configuration](/sdk/reference/configuration/provider-config/) - Provider setup

---

## Key Features

### PromptPack Integration

Load pre-tested, compiled prompts:

```go
pack, _ := sdk.LoadPromptPack("./customer-support.pack.json")
config := &sdk.Config{
    Provider: "openai",
    Pack:     pack,
}
conv, _ := manager.NewConversationWithPrompt(ctx, "support-greeting")
```

### Media Storage

Automatic externalization for images, audio, and video:

```go
import "github.com/AltairaLabs/PromptKit/runtime/storage/local"

fileStore := local.NewFileStore(local.FileStoreConfig{
    BaseDir:             "./media",
    EnableDeduplication: true,
})

manager, _ := sdk.NewConversationManager(
    sdk.WithProvider(provider),
    sdk.WithMediaStorage(fileStore),
)

// Large media automatically stored to disk, reducing memory by 70-90%
```

### State Persistence

Multiple storage backends:

```go
// Redis
config.StateStore = &sdk.RedisStateStore{
    Addr: "localhost:6379",
}

// Postgres
config.StateStore = &sdk.PostgresStateStore{
    ConnString: "postgres://...",
}

// In-memory
config.StateStore = &sdk.InMemoryStateStore{}
```

### Streaming Support

Real-time response streaming:

```go
stream, _ := conv.SendMessageStream(ctx, "Tell me a story")
for chunk := range stream {
    fmt.Print(chunk)
}
```

### Middleware Pipeline

Custom processing layers:

```go
config.Middleware = []sdk.Middleware{
    sdk.LoggingMiddleware(),
    sdk.RateLimitMiddleware(100),
    sdk.MetricsMiddleware(metrics),
    MyCustomMiddleware(),
}
```

### Provider Abstraction

Switch providers without code changes:

```go
// OpenAI
config.Provider = "openai"

// Anthropic
config.Provider = "anthropic"

// Google
config.Provider = "google"
```

---

## Use Cases

### For Application Developers

- Build production chatbots
- Integrate LLMs into existing apps
- Manage complex conversation flows
- Handle tool calling and function execution

### For Backend Engineers

- Deploy scalable LLM services
- Implement state persistence
- Add monitoring and observability
- Handle errors and retries gracefully

### For DevOps Engineers

- Deploy SDK-based services
- Configure for different environments
- Monitor performance and costs
- Scale horizontally

---

## Examples

Real-world SDK applications:

- [Basic Chat](/sdk/examples/basic-chat/) - Simple chatbot implementation
- [Custom Middleware](/sdk/examples/custom-middleware/) - Extend the pipeline
- [Tool Integration](/sdk/examples/tool-integration/) - MCP tool usage
- [State Persistence](/sdk/examples/state-persistence/) - Redis state management
- [Streaming Responses](/sdk/examples/streaming/) - Real-time streaming

---

## Production Patterns

### Error Handling

```go
response, err := conv.SendMessage(ctx, input)
if err != nil {
    switch {
    case errors.Is(err, sdk.ErrRateLimited):
        // Retry with backoff
    case errors.Is(err, sdk.ErrInvalidRequest):
        // Handle invalid input
    default:
        // Log and report
    }
}
```

### Graceful Shutdown

```go
defer func() {
    ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
    defer cancel()
    manager.Close(ctx)
}()
```

### Configuration Management

```go
config := &sdk.Config{
    Provider: os.Getenv("LLM_PROVIDER"),
    Model:    os.Getenv("LLM_MODEL"),
    APIKey:   os.Getenv("LLM_API_KEY"),
    Timeout:  30 * time.Second,
}
```

---

## Getting Help

- **Quick Start**: [Getting Started Guide](/getting-started/app-developer/)
- **Questions**: [GitHub Discussions](https://github.com/AltairaLabs/PromptKit/discussions)
- **Issues**: [Report a Bug](https://github.com/AltairaLabs/PromptKit/issues)
- **Examples**: [SDK Examples](/sdk/examples/)

---

## Related Tools

- **Arena**: [Test prompts before using them](/arena/)
- **PackC**: [Compile prompts into packs](/packc/)
- **Runtime**: [Extend the SDK with custom providers](/runtime/)
- **Complete Workflow**: [See all tools together](/getting-started/complete-workflow/)
