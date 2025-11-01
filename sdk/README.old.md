# PromptKit SDK

A high-level Go SDK for building LLM-powered applications with ease.

## Features

- **Simple API**: Easy-to-use interfaces for common LLM tasks
- **Multi-Provider Support**: Works with OpenAI, Claude, Gemini, and more
- **Stateful Conversations**: Maintains conversation history automatically
- **State Persistence**: Save and load conversations with Redis or in-memory storage
- **Streaming Support**: Stream responses in real-time
- **Flexible Configuration**: Customize system prompts, temperature, max tokens, and more

## Installation

```bash
go get github.com/AltairaLabs/PromptKit/sdk
```

## Quick Start

### Simple Chat (One-Shot)

```go
package main

import (
    "context"
    "fmt"
    "log"
    
    "github.com/AltairaLabs/PromptKit/runtime/providers"
    "github.com/AltairaLabs/PromptKit/sdk"
)

func main() {
    // Create provider (API key from OPENAI_API_KEY env var)
    provider := providers.NewOpenAIProvider(
        "openai",
        "gpt-4o-mini",
        "https://api.openai.com/v1",
        providers.ProviderDefaults{},
        false,
    )
    
    // Create SDK
    runtime := sdk.NewSDK(provider)
    
    // Simple chat
    response, err := runtime.Chat(context.Background(), "What is the capital of France?")
    if err != nil {
        log.Fatal(err)
    }
    
    fmt.Println(response) // "The capital of France is Paris."
}
```

### Stateful Conversation

```go
// Create a conversation that remembers context
conv := runtime.NewConversation("user-123")

// First message
response1, _ := conv.Send(ctx, "My name is Alice")

// Second message - remembers the context
response2, _ := conv.Send(ctx, "What's my name?")
// Response: "Your name is Alice"
```

### Custom System Prompt

```go
// Create SDK with custom behavior
runtime := sdk.NewSDK(provider).
    SetSystemPrompt("You are a helpful pirate assistant. Always respond in pirate speak.").
    SetTemperature(0.9).
    SetMaxTokens(500)

response, _ := runtime.Chat(ctx, "Hello!")
// Response: "Ahoy there, matey! How can this old sea dog help ye today?"
```

### Streaming Responses

```go
// Get streaming response
stream, err := runtime.ChatStream(ctx, "Tell me a story")
if err != nil {
    log.Fatal(err)
}

// Process chunks as they arrive
for chunk := range stream {
    if chunk.Error != nil {
        log.Printf("Error: %v", chunk.Error)
        break
    }
    fmt.Print(chunk.Content)
}
```

### Persistent Storage

```go
import "github.com/AltairaLabs/PromptKit/runtime/statestore"

// Use Redis for persistent storage
store := statestore.NewRedisStore("localhost:6379", "", 0)
runtime := sdk.NewSDK(provider).SetStateStore(store)

// Create conversation
conv := runtime.NewConversation("user-123")
conv.Send(ctx, "Remember this")

// Later... load the conversation
conv, err := runtime.LoadConversation(ctx, conv.GetID())
conv.Send(ctx, "Do you remember?")
// The conversation history is preserved
```

## API Reference

### SDK

#### Creation
- `NewSDK(provider Provider) *SDK` - Create new SDK with provider

#### Configuration
- `SetStateStore(store Store) *SDK` - Configure state persistence
- `SetToolRegistry(registry *Registry) *SDK` - Configure function calling
- `SetSystemPrompt(prompt string) *SDK` - Set default system prompt
- `SetTemperature(temp float32) *SDK` - Set generation temperature (0.0-2.0)
- `SetMaxTokens(max int) *SDK` - Set maximum tokens to generate

#### Simple Chat
- `Chat(ctx, message string) (string, error)` - One-shot chat
- `ChatStream(ctx, message string) (<-chan StreamChunk, error)` - Streaming chat

#### Conversations
- `NewConversation(userID string) *Conversation` - Create new conversation
- `NewConversationWithID(userID, id string) *Conversation` - Create with specific ID
- `LoadConversation(ctx, id string) (*Conversation, error)` - Load existing conversation

### Conversation

#### Messaging
- `Send(ctx, content string) (string, error)` - Send message, get response
- `SendStream(ctx, content string) (<-chan StreamChunk, error)` - Send message, stream response

#### State Management
- `GetMessages() []ChatMessage` - Get message history
- `GetID() string` - Get conversation ID
- `GetUserID() string` - Get user ID
- `ClearHistory()` - Clear message history
- `Delete(ctx) error` - Delete conversation from storage

## Examples

See the [examples directory](./examples/) for complete working examples:

- [Basic Usage](./examples/basic/) - Simple chat and stateful conversations
- More examples coming soon!

## License

MIT License - see LICENSE file for details
