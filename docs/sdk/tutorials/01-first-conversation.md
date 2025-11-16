---
layout: docs
title: "Tutorial 1: Your First Conversation"
nav_order: 1
parent: SDK Tutorials
grand_parent: SDK
---

# Tutorial 1: Your First Conversation

Build a simple chatbot in 5 minutes using the PromptKit SDK.

## What You'll Learn

- Initialize the SDK with a provider
- Create a PromptPack
- Start a conversation
- Send and receive messages

## Prerequisites

- Go 1.21+ installed
- OpenAI API key (get one at [platform.openai.com](https://platform.openai.com))

## Step 1: Set Up Your Project

Create a new Go module:

```bash
mkdir my-chatbot
cd my-chatbot
go mod init my-chatbot
```

Install the SDK:

```bash
go get github.com/AltairaLabs/PromptKit/sdk
go get github.com/AltairaLabs/PromptKit/runtime/providers
```

## Step 2: Create a PromptPack

Create `assistant.pack.json`:

```json
{
  "version": "1.0",
  "prompts": {
    "assistant": {
      "name": "assistant",
      "description": "A helpful AI assistant",
      "system_prompt": "You are a helpful AI assistant. Be concise and friendly.",
      "model_config": {
        "temperature": 0.7,
        "max_tokens": 1000
      }
    }
  }
}
```

This pack defines your assistant's behavior and configuration.

## Step 3: Write Your First Chatbot

Create `main.go`:

```go
package main

import (
    "context"
    "fmt"
    "log"
    "os"

    "github.com/AltairaLabs/PromptKit/sdk"
    "github.com/AltairaLabs/PromptKit/runtime/providers"
)

func main() {
    ctx := context.Background()

    // 1. Create provider
    apiKey := os.Getenv("OPENAI_API_KEY")
    if apiKey == "" {
        log.Fatal("OPENAI_API_KEY not set")
    }
    
    provider := providers.NewOpenAIProvider(apiKey, "gpt-4o-mini", false)

    // 2. Create conversation manager
    manager, err := sdk.NewConversationManager(
        sdk.WithProvider(provider),
    )
    if err != nil {
        log.Fatalf("Failed to create manager: %v", err)
    }

    // 3. Load pack
    pack, err := manager.LoadPack("./assistant.pack.json")
    if err != nil {
        log.Fatalf("Failed to load pack: %v", err)
    }

    // 4. Create conversation
    conv, err := manager.NewConversation(ctx, pack, sdk.ConversationConfig{
        UserID:     "user123",
        PromptName: "assistant",
    })
    if err != nil {
        log.Fatalf("Failed to create conversation: %v", err)
    }

    // 5. Send message
    response, err := conv.Send(ctx, "Hello! What can you help me with?")
    if err != nil {
        log.Fatalf("Failed to send message: %v", err)
    }

    // 6. Display response
    fmt.Println("Assistant:", response.Content)
}
```

## Step 4: Run Your Chatbot

Set your API key:

```bash
export OPENAI_API_KEY="your-api-key-here"
```

Run the program:

```bash
go run main.go
```

You should see:

```
Assistant: Hello! I'm here to help you with various tasks...
```

🎉 **Congratulations!** You've built your first chatbot.

## Understanding the Code

Let's break down what each part does:

### 1. Provider

```go
provider := providers.NewOpenAIProvider(apiKey, "gpt-4o-mini", false)
```

The provider handles communication with the LLM API. PromptKit supports:
- OpenAI (GPT-4, GPT-3.5)
- Anthropic (Claude)
- Google (Gemini)

### 2. Conversation Manager

```go
manager, err := sdk.NewConversationManager(
    sdk.WithProvider(provider),
)
```

The manager orchestrates conversations and applies your configuration.

### 3. PromptPack

```go
pack, err := manager.LoadPack("./assistant.pack.json")
```

Packs define prompt templates, model configuration, and behavior.

### 4. Conversation

```go
conv, err := manager.NewConversation(ctx, pack, sdk.ConversationConfig{
    UserID:     "user123",
    PromptName: "assistant",
})
```

Each conversation maintains its own message history and state.

### 5. Send Message

```go
response, err := conv.Send(ctx, "Hello!")
```

`Send()` processes your message through the pipeline and returns the LLM's response.

## Adding More Interactions

Make it interactive with a simple loop:

```go
package main

import (
    "bufio"
    "context"
    "fmt"
    "log"
    "os"
    "strings"

    "github.com/AltairaLabs/PromptKit/sdk"
    "github.com/AltairaLabs/PromptKit/runtime/providers"
)

func main() {
    ctx := context.Background()

    // Setup (same as before)
    apiKey := os.Getenv("OPENAI_API_KEY")
    provider := providers.NewOpenAIProvider(apiKey, "gpt-4o-mini", false)
    
    manager, err := sdk.NewConversationManager(
        sdk.WithProvider(provider),
    )
    if err != nil {
        log.Fatal(err)
    }

    pack, err := manager.LoadPack("./assistant.pack.json")
    if err != nil {
        log.Fatal(err)
    }

    conv, err := manager.NewConversation(ctx, pack, sdk.ConversationConfig{
        UserID:     "user123",
        PromptName: "assistant",
    })
    if err != nil {
        log.Fatal(err)
    }

    // Interactive loop
    scanner := bufio.NewScanner(os.Stdin)
    fmt.Println("Chatbot ready! Type your messages (or 'quit' to exit)")
    fmt.Println()

    for {
        fmt.Print("You: ")
        if !scanner.Scan() {
            break
        }

        message := strings.TrimSpace(scanner.Text())
        if message == "" {
            continue
        }
        if message == "quit" {
            fmt.Println("Goodbye!")
            break
        }

        // Send message
        response, err := conv.Send(ctx, message)
        if err != nil {
            fmt.Printf("Error: %v\n", err)
            continue
        }

        // Display response
        fmt.Printf("Assistant: %s\n\n", response.Content)
    }
}
```

Run it:

```bash
go run main.go
```

Try a conversation:

```
Chatbot ready! Type your messages (or 'quit' to exit)

You: What's the capital of France?
Assistant: The capital of France is Paris.

You: Tell me an interesting fact about it
Assistant: The Eiffel Tower was originally intended to be temporary...

You: quit
Goodbye!
```

## Trying Different Providers

### Anthropic (Claude)

```go
provider := providers.NewAnthropicProvider(apiKey, "claude-3-5-sonnet-20241022", false)
```

### Google (Gemini)

```go
provider := providers.NewGeminiProvider(apiKey, "gemini-1.5-pro", false)
```

Just change the provider - everything else stays the same!

## Customizing Your Assistant

Modify `assistant.pack.json` to change behavior:

```json
{
  "version": "1.0",
  "prompts": {
    "pirate": {
      "name": "pirate",
      "description": "A pirate assistant",
      "system_prompt": "You are a pirate captain. Speak like a pirate and give advice about sailing.",
      "model_config": {
        "temperature": 0.9,
        "max_tokens": 500
      }
    }
  }
}
```

Update the prompt name:

```go
conv, err := manager.NewConversation(ctx, pack, sdk.ConversationConfig{
    UserID:     "user123",
    PromptName: "pirate",  // Changed
})
```

## Common Issues

### "OPENAI_API_KEY not set"

Set your environment variable:

```bash
export OPENAI_API_KEY="sk-..."
```

### "Failed to load pack"

Check that `assistant.pack.json` exists in the current directory:

```bash
ls -la assistant.pack.json
```

### "Failed to send message"

Check your API key is valid and you have credits available.

## What You've Learned

✅ Initialize the SDK with a provider  
✅ Create and load a PromptPack  
✅ Create conversations  
✅ Send messages and receive responses  
✅ Build an interactive chatbot  
✅ Customize assistant behavior  

## Next Steps

Continue to [Tutorial 2: Streaming Responses](02-streaming-responses.md) to learn how to implement real-time streaming for better UX.

## Complete Code

The complete code for this tutorial is available at:
- [examples/sdk-basics](../../../examples/sdk-basics/)

## Further Reading

- [How to Initialize the SDK](../how-to/initialize.md)
- [How to Send Messages](../how-to/send-messages.md)
- [ConversationManager Reference](../reference/conversation-manager.md)
