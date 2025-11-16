---
title: 'Tutorial 2: Multi-Turn Conversations'
docType: tutorial
order: 2
---
# Tutorial 2: Multi-Turn Conversations

Build a stateful chatbot that remembers conversation history.

**Time**: 20 minutes  
**Level**: Beginner

## What You'll Build

A chatbot that maintains conversation context across multiple exchanges.

## What You'll Learn

- Manage conversation state
- Use session IDs
- Implement state storage (Redis & in-memory)
- Handle context windows
- Build interactive chatbots

## Prerequisites

- Completed [Tutorial 1](01-first-pipeline)
- Redis (optional, for persistent state)

## Step 1: Install Redis (Optional)

### macOS

```bash
brew install redis
brew services start redis
```

### Linux

```bash
sudo apt-get install redis-server
sudo systemctl start redis
```

### Docker

```bash
docker run -d -p 6379:6379 redis
```

## Step 2: In-Memory State (Simple)

Start with in-memory state for development:

```go
package main

import (
    "bufio"
    "context"
    "fmt"
    "log"
    "os"
    "strings"
    
    "github.com/AltairaLabs/PromptKit/runtime/pipeline"
    "github.com/AltairaLabs/PromptKit/runtime/pipeline/middleware"
    "github.com/AltairaLabs/PromptKit/runtime/providers/openai"
    "github.com/AltairaLabs/PromptKit/runtime/statestore"
)

func main() {
    // Create provider
    provider := openai.NewOpenAIProvider(
        "openai",
        "gpt-4o-mini",
        os.Getenv("OPENAI_API_KEY"),
        openai.DefaultProviderDefaults(),
        false,
    )
    defer provider.Close()
    
    // Create in-memory state store
    store := statestore.NewInMemoryStateStore()
    
    // Build pipeline with state middleware
    pipe := pipeline.NewPipeline(
        middleware.StateMiddleware(store),
        middleware.ProviderMiddleware(provider, nil, nil, &middleware.ProviderMiddlewareConfig{
            MaxTokens:   500,
            Temperature: 0.7,
        }),
    )
    defer pipe.Shutdown(context.Background())
    
    // Session ID for this conversation
    sessionID := "user-123"
    ctx := context.Background()
    
    // Interactive chat loop
    scanner := bufio.NewScanner(os.Stdin)
    fmt.Println("Chatbot ready! Type 'exit' to quit.")
    fmt.Print("\nYou: ")
    
    for scanner.Scan() {
        input := strings.TrimSpace(scanner.Text())
        
        if input == "exit" {
            break
        }
        
        if input == "" {
            fmt.Print("You: ")
            continue
        }
        
        // Execute with context
        result, err := pipe.ExecuteWithContext(ctx, sessionID, "user", input)
        if err != nil {
            log.Printf("Error: %v\n", err)
            fmt.Print("You: ")
            continue
        }
        
        fmt.Printf("\nBot: %s\n\n", result.Response.Content)
        fmt.Printf("Tokens: %d | Cost: $%.6f\n", 
            result.Response.Usage.TotalTokens,
            result.Cost.TotalCost)
        fmt.Print("\nYou: ")
    }
    
    fmt.Println("\nGoodbye!")
}
```

## Step 3: Test Conversation Memory

Run the chatbot:

```bash
go run main.go
```

Try this conversation:

```
You: My name is Alice
Bot: Hello Alice! It's nice to meet you...

You: What's my name?
Bot: Your name is Alice...

You: I love pizza
Bot: That's great! Pizza is delicious...

You: What food do I love?
Bot: You mentioned that you love pizza!
```

The bot remembers your name and preferences! 🎉

## Step 4: Redis State (Production)

For production, use Redis for persistent state:

```go
package main

import (
    "bufio"
    "context"
    "fmt"
    "log"
    "os"
    "strings"
    
    "github.com/AltairaLabs/PromptKit/runtime/pipeline"
    "github.com/AltairaLabs/PromptKit/runtime/pipeline/middleware"
    "github.com/AltairaLabs/PromptKit/runtime/providers/openai"
    "github.com/AltairaLabs/PromptKit/runtime/statestore"
)

func main() {
    // Create provider
    provider := openai.NewOpenAIProvider(
        "openai",
        "gpt-4o-mini",
        os.Getenv("OPENAI_API_KEY"),
        openai.DefaultProviderDefaults(),
        false,
    )
    defer provider.Close()
    
    // Create Redis state store
    store, err := statestore.NewRedisStateStore("localhost:6379", "", 0)
    if err != nil {
        log.Fatal(err)
    }
    defer store.Close()
    
    // Build pipeline with state
    pipe := pipeline.NewPipeline(
        middleware.StateMiddleware(store),
        middleware.ProviderMiddleware(provider, nil, nil, &middleware.ProviderMiddlewareConfig{
            MaxTokens:   500,
            Temperature: 0.7,
        }),
    )
    defer pipe.Shutdown(context.Background())
    
    // Get or create session ID
    sessionID := os.Getenv("SESSION_ID")
    if sessionID == "" {
        sessionID = fmt.Sprintf("user-%d", os.Getpid())
    }
    
    fmt.Printf("Session: %s\n", sessionID)
    fmt.Println("Chatbot ready! Type 'exit' to quit.")
    
    ctx := context.Background()
    scanner := bufio.NewScanner(os.Stdin)
    fmt.Print("\nYou: ")
    
    for scanner.Scan() {
        input := strings.TrimSpace(scanner.Text())
        
        if input == "exit" {
            break
        }
        
        if input == "" {
            fmt.Print("You: ")
            continue
        }
        
        result, err := pipe.ExecuteWithContext(ctx, sessionID, "user", input)
        if err != nil {
            log.Printf("Error: %v\n", err)
            fmt.Print("You: ")
            continue
        }
        
        fmt.Printf("\nBot: %s\n", result.Response.Content)
        fmt.Printf("(Tokens: %d | Cost: $%.6f)\n\n", 
            result.Response.Usage.TotalTokens,
            result.Cost.TotalCost)
        fmt.Print("You: ")
    }
    
    fmt.Println("\nGoodbye!")
}
```

With Redis, conversations persist! Restart the app with the same session ID to continue.

## Understanding State Management

### How State Middleware Works

```go
pipe := pipeline.NewPipeline(
    middleware.StateMiddleware(store),  // Must be first!
    middleware.ProviderMiddleware(...),
)
```

State middleware:
1. Loads previous messages before execution
2. Adds new message to history
3. Sends all messages to LLM
4. Saves updated history

### Session IDs

Session IDs identify conversations:

```go
// User-based: one conversation per user
sessionID := fmt.Sprintf("user-%s", userID)

// Feature-based: separate conversations per feature
sessionID := fmt.Sprintf("support-%s", ticketID)

// Time-based: new conversation daily
sessionID := fmt.Sprintf("user-%s-%s", userID, time.Now().Format("2006-01-02"))
```

## Managing Context Windows

LLMs have token limits. Keep conversations manageable:

### Option 1: Trim by Message Count

```go
import "github.com/AltairaLabs/PromptKit/runtime/types"

// Load state
messages, _ := store.Load(ctx, sessionID)

// Keep only recent 10 messages
maxMessages := 10
if len(messages) > maxMessages {
    messages = messages[len(messages)-maxMessages:]
    store.Save(ctx, sessionID, messages)
}
```

### Option 2: Trim by Token Count

```go
import "github.com/AltairaLabs/PromptKit/runtime/prompt"

// Load state
messages, _ := store.Load(ctx, sessionID)

// Keep only messages within token limit
maxTokens := 4000
trimmed := prompt.TruncateMessages(messages, maxTokens)
store.Save(ctx, sessionID, trimmed)
```

## Complete Multi-User Chatbot

Here's a production-ready chatbot:

```go
package main

import (
    "bufio"
    "context"
    "fmt"
    "log"
    "os"
    "strings"
    "time"
    
    "github.com/AltairaLabs/PromptKit/runtime/pipeline"
    "github.com/AltairaLabs/PromptKit/runtime/pipeline/middleware"
    "github.com/AltairaLabs/PromptKit/runtime/providers/openai"
    "github.com/AltairaLabs/PromptKit/runtime/statestore"
)

func main() {
    // Configuration
    apiKey := os.Getenv("OPENAI_API_KEY")
    if apiKey == "" {
        log.Fatal("OPENAI_API_KEY not set")
    }
    
    redisAddr := os.Getenv("REDIS_ADDR")
    if redisAddr == "" {
        redisAddr = "localhost:6379"
    }
    
    username := os.Getenv("USER")
    if username == "" {
        username = "guest"
    }
    
    // Create provider
    provider := openai.NewOpenAIProvider(
        "openai",
        "gpt-4o-mini",
        apiKey,
        openai.DefaultProviderDefaults(),
        false,
    )
    defer provider.Close()
    
    // Create state store (fallback to in-memory if Redis fails)
    var store statestore.StateStore
    redisStore, err := statestore.NewRedisStateStore(redisAddr, "", 0)
    if err != nil {
        log.Printf("Redis unavailable, using in-memory store: %v", err)
        store = statestore.NewInMemoryStateStore()
    } else {
        store = redisStore
        defer redisStore.Close()
    }
    
    // Build pipeline
    config := &middleware.ProviderMiddlewareConfig{
        MaxTokens:   500,
        Temperature: 0.7,
    }
    
    pipe := pipeline.NewPipeline(
        middleware.StateMiddleware(store),
        middleware.ProviderMiddleware(provider, nil, nil, config),
    )
    defer pipe.Shutdown(context.Background())
    
    // Session setup
    sessionID := fmt.Sprintf("chat-%s-%s", username, time.Now().Format("2006-01-02"))
    fmt.Printf("=== Chatbot ===\n")
    fmt.Printf("Session: %s\n", sessionID)
    fmt.Printf("Commands: 'exit' to quit, 'clear' to reset conversation\n\n")
    
    ctx := context.Background()
    scanner := bufio.NewScanner(os.Stdin)
    totalCost := 0.0
    
    fmt.Print("You: ")
    for scanner.Scan() {
        input := strings.TrimSpace(scanner.Text())
        
        switch input {
        case "exit":
            fmt.Printf("\nTotal cost this session: $%.6f\n", totalCost)
            fmt.Println("Goodbye!")
            return
            
        case "clear":
            store.Delete(ctx, sessionID)
            fmt.Println("\n[Conversation cleared]\n")
            fmt.Print("You: ")
            continue
            
        case "":
            fmt.Print("You: ")
            continue
        }
        
        // Execute
        result, err := pipe.ExecuteWithContext(ctx, sessionID, "user", input)
        if err != nil {
            log.Printf("\nError: %v\n\n", err)
            fmt.Print("You: ")
            continue
        }
        
        // Display response
        fmt.Printf("\nBot: %s\n", result.Response.Content)
        
        // Update metrics
        totalCost += result.Cost.TotalCost
        fmt.Printf("\n[Tokens: %d | This: $%.6f | Total: $%.6f]\n\n", 
            result.Response.Usage.TotalTokens,
            result.Cost.TotalCost,
            totalCost)
        
        fmt.Print("You: ")
    }
}
```

## Experiment

### 1. System Prompt

Add personality to your bot:

```go
// Add system message before first user message
systemPrompt := "You are a helpful AI assistant who speaks like a pirate."

// Insert at start of conversation
messages := []types.Message{
    {Role: "system", Content: systemPrompt},
}
store.Save(ctx, sessionID, messages)
```

### 2. Multiple Users

Run multiple chatbot instances with different usernames:

```bash
USER=alice go run main.go  # Terminal 1
USER=bob go run main.go    # Terminal 2
```

Each user has their own conversation history!

### 3. Conversation Reset

Add a command to clear history:

```go
if input == "/clear" {
    store.Delete(ctx, sessionID)
    fmt.Println("Conversation cleared!")
    continue
}
```

## Common Issues

### Bot forgets things

**Problem**: State middleware not registered or session ID changes.

**Solution**: 
- Ensure `StateMiddleware` is first
- Use consistent session IDs
- Check state store connection

### Context length exceeded

**Problem**: Conversation too long for model.

**Solution**: Trim messages:
```go
messages, _ := store.Load(ctx, sessionID)
if len(messages) > 20 {
    messages = messages[len(messages)-20:]
    store.Save(ctx, sessionID, messages)
}
```

### Redis connection failed

**Problem**: Redis not running or wrong address.

**Solution**: Check Redis:
```bash
redis-cli ping  # Should return PONG
```

## What You've Learned

✅ Manage conversation state  
✅ Use session IDs  
✅ Implement Redis and in-memory storage  
✅ Handle context windows  
✅ Build interactive chatbots  
✅ Support multiple users  

## Next Steps

Continue to [Tutorial 3: MCP Integration](03-mcp-integration) to add external tools to your chatbot.

## See Also

- [Manage State](../how-to/manage-state) - Advanced state management
- [StateStore Reference](../reference/statestore) - Complete API
