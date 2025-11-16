---
layout: default
title: For Application Developers
parent: Getting Started
nav_order: 2
---

# Getting Started as an Application Developer

**Goal**: Build production-ready LLM applications using the PromptKit SDK.

**Tool**: PromptKit SDK

**Time to Success**: 15-20 minutes

---

## What You'll Accomplish

By the end of this guide, you'll have:

- ✅ SDK installed in your Go project
- ✅ Built a working chatbot application
- ✅ Loaded and used a PromptPack
- ✅ Managed conversation state

---

## Prerequisites

- Go 1.22 or later installed
- Basic Go programming knowledge
- API key for at least one LLM provider (OpenAI, Anthropic, or Google)
- A PromptPack (you can create one or use an example)

---

## Step 1: Create Your Go Project

```bash
mkdir my-chatbot
cd my-chatbot
go mod init github.com/yourname/my-chatbot
```

---

## Step 2: Install the SDK

```bash
go get github.com/AltairaLabs/PromptKit/sdk
```

---

## Step 3: Set Up Your API Key

```bash
export OPENAI_API_KEY="your-key-here"
```

---

## Step 4: Create Your First Chatbot

Create `main.go`:

```go
package main

import (
    "context"
    "fmt"
    "log"
    "os"

    "github.com/AltairaLabs/PromptKit/sdk"
)

func main() {
    ctx := context.Background()

    // Initialize the SDK
    config := &sdk.Config{
        Provider: "openai",
        Model:    "gpt-4",
        APIKey:   os.Getenv("OPENAI_API_KEY"),
    }

    // Create a conversation manager
    manager, err := sdk.NewConversationManager(ctx, config)
    if err != nil {
        log.Fatalf("Failed to create manager: %v", err)
    }
    defer manager.Close()

    // Start a new conversation
    conv, err := manager.NewConversation(ctx, "greeting-bot")
    if err != nil {
        log.Fatalf("Failed to create conversation: %v", err)
    }

    // Add system message
    if err := conv.AddMessage(ctx, "system", "You are a friendly assistant."); err != nil {
        log.Fatalf("Failed to add system message: %v", err)
    }

    // Send user message and get response
    response, err := conv.SendMessage(ctx, "Hello! How are you today?")
    if err != nil {
        log.Fatalf("Failed to send message: %v", err)
    }

    fmt.Printf("Assistant: %s\n", response)
}
```

---

## Step 5: Run Your Chatbot

```bash
go run main.go
```

You should see a friendly response from the LLM!

---

## Step 6: Use a PromptPack

PromptPacks are pre-tested, compiled prompts. Here's how to use one:

```go
package main

import (
    "context"
    "fmt"
    "log"
    "os"

    "github.com/AltairaLabs/PromptKit/sdk"
)

func main() {
    ctx := context.Background()

    // Load a PromptPack
    pack, err := sdk.LoadPromptPack("./prompts/customer-support.pack.json")
    if err != nil {
        log.Fatalf("Failed to load pack: %v", err)
    }

    // Create config with the pack
    config := &sdk.Config{
        Provider: "openai",
        Model:    "gpt-4",
        APIKey:   os.Getenv("OPENAI_API_KEY"),
        Pack:     pack,
    }

    manager, err := sdk.NewConversationManager(ctx, config)
    if err != nil {
        log.Fatalf("Failed to create manager: %v", err)
    }
    defer manager.Close()

    // Use a prompt from the pack
    conv, err := manager.NewConversationWithPrompt(ctx, "support-greeting")
    if err != nil {
        log.Fatalf("Failed to create conversation: %v", err)
    }

    // The system message is already loaded from the pack!
    response, err := conv.SendMessage(ctx, "I need help with my order")
    if err != nil {
        log.Fatalf("Failed to send message: %v", err)
    }

    fmt.Printf("Support Agent: %s\n", response)
}
```

---

## What's Next?

Now that you have a working chatbot, explore more SDK capabilities:

### 📚 **Tutorials** (Hands-on Learning)

- [Conversation State Management](/sdk/tutorials/02-conversation-state/) - Track context across turns
- [Tool Integration](/sdk/tutorials/03-tool-integration/) - Add function calling
- [Custom Middleware](/sdk/tutorials/04-custom-middleware/) - Logging, filtering, transformation
- [State Persistence](/sdk/tutorials/05-state-persistence/) - Redis, Postgres storage
- [Production Deployment](/sdk/tutorials/06-production-deployment/) - Deploy at scale

### 🔧 **How-To Guides** (Specific Tasks)

- [Load PromptPacks](/sdk/how-to/load-promptpacks/) - Work with compiled prompts
- [Manage Conversations](/sdk/how-to/manage-conversations/) - Conversation lifecycle
- [Implement Tools](/sdk/how-to/implement-tools/) - MCP and custom tools
- [Add Middleware](/sdk/how-to/add-middleware/) - Custom processing pipeline
- [Configure State](/sdk/how-to/configure-state/) - State management options
- [Handle Streaming](/sdk/how-to/handle-streaming/) - Real-time responses
- [Error Handling](/sdk/how-to/error-handling/) - Robust error management

### 💡 **Concepts** (Understanding)

- [Conversation Lifecycle](/sdk/explanation/conversation-lifecycle/) - How conversations work
- [Pipeline Architecture](/sdk/explanation/pipeline-architecture/) - Request/response flow
- [Middleware System](/sdk/explanation/middleware-system/) - Processing layers
- [State Management](/sdk/explanation/state-management/) - State persistence patterns
- [Provider Abstraction](/sdk/explanation/provider-abstraction/) - Multi-provider support

### 📖 **Reference** (Look Up Details)

- [ConversationManager API](/sdk/reference/api/conversation-manager/) - Core API
- [PipelineBuilder API](/sdk/reference/api/pipeline-builder/) - Build pipelines
- [PackLoader API](/sdk/reference/api/pack-loader/) - Load prompts
- [ToolRegistry API](/sdk/reference/api/tool-registry/) - Register tools
- [StateStore API](/sdk/reference/api/state-store/) - State operations

---

## Common Patterns for Developers

### Multi-Turn Conversations

```go
conv, _ := manager.NewConversation(ctx, "chat-session")

// First turn
response1, _ := conv.SendMessage(ctx, "What's the weather?")

// Second turn (context is preserved)
response2, _ := conv.SendMessage(ctx, "What about tomorrow?")
```

### Streaming Responses

```go
stream, err := conv.SendMessageStream(ctx, "Tell me a story")
if err != nil {
    log.Fatal(err)
}

for chunk := range stream {
    fmt.Print(chunk)
}
```

### Error Handling

```go
response, err := conv.SendMessage(ctx, userInput)
if err != nil {
    switch {
    case errors.Is(err, sdk.ErrRateLimited):
        // Handle rate limiting
    case errors.Is(err, sdk.ErrInvalidRequest):
        // Handle invalid input
    default:
        // Handle other errors
    }
}
```

---

## Troubleshooting

### Import Errors

```bash
# Update dependencies
go mod tidy
go get -u github.com/AltairaLabs/PromptKit/sdk
```

### API Key Issues

```go
// Check if key is set
if os.Getenv("OPENAI_API_KEY") == "" {
    log.Fatal("OPENAI_API_KEY not set")
}
```

### Context Cancelled

```go
// Use timeout context for long-running operations
ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
defer cancel()
```

---

## Production Checklist

Before deploying to production:

- ✅ Configure state persistence (Redis/Postgres)
- ✅ Add error handling and retries
- ✅ Implement rate limiting
- ✅ Set up monitoring and logging
- ✅ Use environment-based configuration
- ✅ Add graceful shutdown handling

See [Production Deployment Tutorial](/sdk/tutorials/06-production-deployment/) for details.

---

## Join the Community

- **Questions**: [GitHub Discussions](https://github.com/AltairaLabs/PromptKit/discussions)
- **Examples**: [SDK Examples](/sdk/examples/)
- **Issues**: [Report a Bug](https://github.com/AltairaLabs/PromptKit/issues)

---

## Related Guides

- **For Prompt Engineers**: [Arena Getting Started](/getting-started/prompt-engineer/) - Test prompts before using them
- **For DevOps**: [PackC Getting Started](/getting-started/devops-engineer/) - Compile and package prompts
- **Complete Workflow**: [End-to-End Guide](/getting-started/complete-workflow/) - See all tools together
