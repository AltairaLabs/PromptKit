---
layout: default
title: "Tutorial 4: State Management"
nav_order: 4
parent: SDK Tutorials
grand_parent: SDK
---

# Tutorial 4: State Management

Learn how to persist conversations across sessions with state stores.

## What You'll Learn

- Configure state persistence
- Restore conversations across sessions
- Manage conversation history
- Handle context windows
- Clean up old state

## Why State Management?

Persistent state enables:

- Multi-session conversations
- Conversation history across restarts
- Shared state across instances
- Long-term memory

## Prerequisites

Complete [Tutorial 1](01-first-conversation.md) and understand basic SDK usage.

## Step 1: Memory Store

Start with the simple in-memory store:

```go
package main

import (
    "context"
    "fmt"
    "log"
    "os"

    "github.com/AltairaLabs/PromptKit/sdk"
    "github.com/AltairaLabs/PromptKit/runtime/providers"
    "github.com/AltairaLabs/PromptKit/runtime/statestore"
)

func main() {
    ctx := context.Background()

    // 1. Create state store
    store := statestore.NewMemoryStore()

    // 2. Create manager with state
    apiKey := os.Getenv("OPENAI_API_KEY")
    provider := providers.NewOpenAIProvider(apiKey, "gpt-4o-mini", false)
    
    manager, err := sdk.NewConversationManager(
        sdk.WithProvider(provider),
        sdk.WithStateStore(store),
    )
    if err != nil {
        log.Fatal(err)
    }

    pack, err := manager.LoadPack("./assistant.pack.json")
    if err != nil {
        log.Fatal(err)
    }

    // 3. First session
    fmt.Println("=== Session 1 ===")
    conv1, err := manager.NewConversation(ctx, pack, sdk.ConversationConfig{
        UserID:         "alice",
        ConversationID: "persistent-chat",
        PromptName:     "assistant",
    })
    if err != nil {
        log.Fatal(err)
    }

    resp1, _ := conv1.Send(ctx, "My name is Alice")
    fmt.Printf("Assistant: %s\n\n", resp1.Content)

    // 4. Later session - same conversation ID
    fmt.Println("=== Session 2 ===")
    conv2, err := manager.GetConversation("persistent-chat")
    if err != nil {
        log.Fatal(err)
    }

    resp2, _ := conv2.Send(ctx, "What's my name?")
    fmt.Printf("Assistant: %s\n", resp2.Content)  // "Your name is Alice"
}
```

Output:

```
=== Session 1 ===
Assistant: Nice to meet you, Alice! How can I help you today?

=== Session 2 ===
Assistant: Your name is Alice.
```

The conversation remembered across sessions!

## Step 2: Persistent Chat Application

Build a multi-session chat app:

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
    "github.com/AltairaLabs/PromptKit/runtime/statestore"
)

func main() {
    ctx := context.Background()

    // Setup with state
    store := statestore.NewMemoryStore()
    
    apiKey := os.Getenv("OPENAI_API_KEY")
    provider := providers.NewOpenAIProvider(apiKey, "gpt-4o-mini", false)
    
    manager, err := sdk.NewConversationManager(
        sdk.WithProvider(provider),
        sdk.WithStateStore(store),
    )
    if err != nil {
        log.Fatal(err)
    }

    pack, err := manager.LoadPack("./assistant.pack.json")
    if err != nil {
        log.Fatal(err)
    }

    // Interactive sessions
    scanner := bufio.NewScanner(os.Stdin)
    
    fmt.Println("Persistent Chat (type 'new' for new conversation, 'quit' to exit)")
    fmt.Println()

    var currentConv *sdk.Conversation
    conversationID := fmt.Sprintf("conv-%d", time.Now().Unix())

    for {
        // Ensure we have a conversation
        if currentConv == nil {
            currentConv, err = manager.NewConversation(ctx, pack, sdk.ConversationConfig{
                UserID:         "user123",
                ConversationID: conversationID,
                PromptName:     "assistant",
            })
            if err != nil {
                log.Fatal(err)
            }
            fmt.Printf("[New conversation: %s]\n\n", conversationID)
        }

        fmt.Print("You: ")
        if !scanner.Scan() {
            break
        }

        message := strings.TrimSpace(scanner.Text())
        if message == "" {
            continue
        }

        switch message {
        case "quit":
            fmt.Println("Goodbye!")
            return
        case "new":
            currentConv = nil
            conversationID = fmt.Sprintf("conv-%d", time.Now().Unix())
            continue
        case "history":
            showHistory(ctx, store, conversationID)
            continue
        }

        response, err := currentConv.Send(ctx, message)
        if err != nil {
            fmt.Printf("Error: %v\n\n", err)
            continue
        }

        fmt.Printf("Assistant: %s\n\n", response.Content)
    }
}

func showHistory(ctx context.Context, store statestore.StateStore, conversationID string) {
    state, err := store.Get(ctx, conversationID)
    if err != nil {
        fmt.Println("No history found")
        return
    }

    fmt.Println("\n=== Conversation History ===")
    for i, msg := range state.Messages {
        fmt.Printf("%d. %s: %s\n", i+1, msg.Role, truncate(msg.Content, 60))
    }
    fmt.Printf("\nTotal messages: %d\n", len(state.Messages))
    fmt.Printf("Total tokens: %d\n", state.TokenCount)
    fmt.Printf("Total cost: $%.4f\n\n", state.TotalCost)
}

func truncate(s string, maxLen int) string {
    if len(s) <= maxLen {
        return s
    }
    return s[:maxLen-3] + "..."
}
```

Try it:

```
Persistent Chat (type 'new' for new conversation, 'quit' to exit)

[New conversation: conv-1699564800]

You: Remember that I like pizza
Assistant: Got it! I'll remember that you like pizza.

You: history

=== Conversation History ===
1. system: You are a helpful AI assistant...
2. user: Remember that I like pizza
3. assistant: Got it! I'll remember that you like pizza.

Total messages: 3
Total tokens: 127
Total cost: $0.0019

You: What do I like?
Assistant: You like pizza!

You: quit
Goodbye!
```

## Step 3: Redis Store

For production, use Redis for distributed state:

```go
import (
    "github.com/AltairaLabs/PromptKit/runtime/statestore/redis"
)

func main() {
    // Create Redis store
    store, err := redis.NewRedisStore(redis.Config{
        Address:  "localhost:6379",
        Password: os.Getenv("REDIS_PASSWORD"),
        DB:       0,
        Prefix:   "promptkit:",
        TTL:      24 * time.Hour,  // Auto-expire after 24h
    })
    if err != nil {
        log.Fatal(err)
    }

    manager, err := sdk.NewConversationManager(
        sdk.WithProvider(provider),
        sdk.WithStateStore(store),
    )
    
    // Rest of your code...
}
```

Start Redis with Docker:

```bash
docker run -d -p 6379:6379 redis:7
```

Now state persists across application restarts and scales across instances!

## Step 4: Context Window Management

Handle long conversations:

```go
// Configure token limits
conv, err := manager.NewConversation(ctx, pack, sdk.ConversationConfig{
    UserID:         "user123",
    ConversationID: "long-conv",
    PromptName:     "assistant",
    MaxTokens:      4000,  // Truncate at 4k tokens
})

// The SDK automatically truncates old messages
// when context exceeds MaxTokens
```

Manual truncation:

```go
func truncateOldMessages(ctx context.Context, store statestore.StateStore, conversationID string) error {
    state, err := store.Get(ctx, conversationID)
    if err != nil {
        return err
    }

    // Keep only last 20 messages
    if len(state.Messages) > 20 {
        state.Messages = state.Messages[len(state.Messages)-20:]
        return store.Set(ctx, conversationID, state)
    }

    return nil
}
```

Summarization strategy:

```go
func summarizeOldMessages(ctx context.Context, conv *sdk.Conversation, store statestore.StateStore) error {
    state, err := store.Get(ctx, conv.ID)
    if err != nil {
        return err
    }

    // If more than 8k tokens, summarize old messages
    if state.TokenCount > 8000 {
        oldMessages := state.Messages[:len(state.Messages)-10]
        
        // Ask LLM to summarize
        summary, err := conv.Send(ctx, fmt.Sprintf(
            "Summarize this conversation history in 2-3 sentences: %s",
            formatMessages(oldMessages),
        ))
        if err != nil {
            return err
        }

        // Replace old messages with summary
        state.Messages = []Message{
            {Role: "system", Content: state.Messages[0].Content},
            {Role: "assistant", Content: summary.Content},
        }
        state.Messages = append(state.Messages, state.Messages[len(state.Messages)-10:]...)
        
        return store.Set(ctx, conv.ID, state)
    }

    return nil
}
```

## Step 5: User-Scoped State

Organize state by user:

```go
func createConversation(ctx context.Context, manager *sdk.ConversationManager, pack *sdk.Pack, userID, sessionID string) (*sdk.Conversation, error) {
    // Use hierarchical conversation ID
    conversationID := fmt.Sprintf("user:%s:session:%s", userID, sessionID)
    
    return manager.NewConversation(ctx, pack, sdk.ConversationConfig{
        UserID:         userID,
        ConversationID: conversationID,
        PromptName:     "assistant",
    })
}

func listUserConversations(ctx context.Context, store statestore.StateStore, userID string) ([]string, error) {
    prefix := fmt.Sprintf("user:%s:", userID)
    return store.ListByPrefix(ctx, prefix)
}
```

## Step 6: State Cleanup

Clean up old conversations:

```go
func cleanupOldConversations(ctx context.Context, store statestore.StateStore, maxAge time.Duration) error {
    cutoff := time.Now().Add(-maxAge)

    conversations, err := store.ListAll(ctx)
    if err != nil {
        return err
    }

    for _, conv := range conversations {
        if conv.UpdatedAt.Before(cutoff) {
            if err := store.Delete(ctx, conv.ConversationID); err != nil {
                log.Printf("Failed to delete %s: %v", conv.ConversationID, err)
            }
        }
    }

    return nil
}

// Run cleanup periodically
go func() {
    ticker := time.NewTicker(1 * time.Hour)
    for range ticker.C {
        cleanupOldConversations(ctx, store, 7*24*time.Hour)
    }
}()
```

## Complete Example: Multi-User Chat

Production-ready multi-user chat with state:

```go
package main

import (
    "context"
    "fmt"
    "log"
    "os"
    "time"

    "github.com/AltairaLabs/PromptKit/sdk"
    "github.com/AltairaLabs/PromptKit/runtime/providers"
    "github.com/AltairaLabs/PromptKit/runtime/statestore"
)

type ChatService struct {
    manager *sdk.ConversationManager
    store   statestore.StateStore
    pack    *sdk.Pack
}

func NewChatService() (*ChatService, error) {
    ctx := context.Background()

    // Setup state store
    store := statestore.NewMemoryStore()

    // Setup manager
    apiKey := os.Getenv("OPENAI_API_KEY")
    provider := providers.NewOpenAIProvider(apiKey, "gpt-4o-mini", false)
    
    manager, err := sdk.NewConversationManager(
        sdk.WithProvider(provider),
        sdk.WithStateStore(store),
    )
    if err != nil {
        return nil, err
    }

    pack, err := manager.LoadPack("./assistant.pack.json")
    if err != nil {
        return nil, err
    }

    service := &ChatService{
        manager: manager,
        store:   store,
        pack:    pack,
    }

    // Start cleanup routine
    go service.cleanupRoutine()

    return service, nil
}

func (s *ChatService) SendMessage(ctx context.Context, userID, message string) (string, error) {
    // Get or create conversation
    conversationID := fmt.Sprintf("user:%s:active", userID)
    
    conv, err := s.manager.GetConversation(conversationID)
    if err != nil {
        // Create new conversation
        conv, err = s.manager.NewConversation(ctx, s.pack, sdk.ConversationConfig{
            UserID:         userID,
            ConversationID: conversationID,
            PromptName:     "assistant",
            MaxTokens:      4000,
        })
        if err != nil {
            return "", err
        }
    }

    // Send message
    response, err := conv.Send(ctx, message)
    if err != nil {
        return "", err
    }

    return response.Content, nil
}

func (s *ChatService) GetHistory(ctx context.Context, userID string) ([]Message, error) {
    conversationID := fmt.Sprintf("user:%s:active", userID)
    
    state, err := s.store.Get(ctx, conversationID)
    if err != nil {
        return nil, err
    }

    return state.Messages, nil
}

func (s *ChatService) ClearHistory(ctx context.Context, userID string) error {
    conversationID := fmt.Sprintf("user:%s:active", userID)
    return s.store.Delete(ctx, conversationID)
}

func (s *ChatService) cleanupRoutine() {
    ticker := time.NewTicker(1 * time.Hour)
    defer ticker.Stop()

    for range ticker.C {
        s.cleanup(context.Background())
    }
}

func (s *ChatService) cleanup(ctx context.Context) {
    cutoff := time.Now().Add(-24 * time.Hour)

    conversations, err := s.store.ListAll(ctx)
    if err != nil {
        log.Printf("Cleanup error: %v", err)
        return
    }

    for _, conv := range conversations {
        if conv.UpdatedAt.Before(cutoff) {
            s.store.Delete(ctx, conv.ConversationID)
        }
    }
}

func main() {
    service, err := NewChatService()
    if err != nil {
        log.Fatal(err)
    }

    ctx := context.Background()

    // Simulate multiple users
    users := []string{"alice", "bob"}

    for _, userID := range users {
        fmt.Printf("\n=== User: %s ===\n", userID)

        // First message
        resp1, _ := service.SendMessage(ctx, userID, fmt.Sprintf("My name is %s", userID))
        fmt.Printf("Assistant: %s\n", resp1)

        // Second message
        resp2, _ := service.SendMessage(ctx, userID, "What's my name?")
        fmt.Printf("Assistant: %s\n", resp2)

        // Show history
        history, _ := service.GetHistory(ctx, userID)
        fmt.Printf("History: %d messages\n", len(history))
    }
}
```

## What You've Learned

✅ Configure state persistence  
✅ Create persistent conversations  
✅ Manage conversation history  
✅ Handle context windows  
✅ Clean up old state  
✅ Build multi-user chat systems  

## Next Steps

Continue to [Tutorial 5: Custom Pipelines](05-custom-pipelines.md) to learn how to build custom processing pipelines.

## Further Reading

- [How to Manage State](../how-to/manage-state.md)
- [StateStore Reference](../reference/statestore.md)
- [Redis Store Configuration](../reference/redis-store.md)
