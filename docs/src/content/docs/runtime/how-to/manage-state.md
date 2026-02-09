---
title: Manage State
sidebar:
  order: 4
---
Persist conversation state across pipeline executions.

## Goal

Store and retrieve conversation state for continuity.

## Quick Start

### Step 1: Create State Store

```go
import "github.com/AltairaLabs/PromptKit/runtime/statestore"

// In-memory store (dev/testing)
store := statestore.NewMemoryStore()

// Redis store (production)
store := statestore.NewRedisStore(redisClient)
```

### Step 2: Use Session IDs

```go
ctx := context.Background()
sessionID := "user-123-conversation"

// First message - state auto-saved
result, err := pipe.ExecuteWithContext(ctx, sessionID, "user", "Hi, I'm Alice")

// Subsequent messages - state auto-loaded
result2, err := pipe.ExecuteWithContext(ctx, sessionID, "user", "What's my name?")
// Response: "Your name is Alice"
```

## In-Memory State Store

### Basic Setup

```go
store := statestore.NewMemoryStore()
```

**Use Cases**:
- Development
- Testing
- Single-instance applications
- Non-persistent sessions

### Save and Load

```go
// Save state manually
state := &statestore.ConversationState{
    ID: "session-1",
    Messages: []types.Message{
        {Role: "user", Content: "Hello"},
        {Role: "assistant", Content: "Hi there!"},
    },
}

err := store.Save(ctx, state)
if err != nil {
    log.Fatal(err)
}

// Load state
loaded, err := store.Load(ctx, "session-1")
if err != nil {
    log.Fatal(err)
}

for _, msg := range loaded.Messages {
    log.Printf("%s: %s\n", msg.Role, msg.Content)
}
```

### Fork State

```go
// Fork creates a copy of the session state under a new session ID
err := store.Fork(ctx, sourceSessionID, newSessionID)
if err != nil {
    log.Fatal(err)
}
```

## Redis State Store

### Basic Setup

```go
import "github.com/redis/go-redis/v9"

redisClient := redis.NewClient(&redis.Options{
    Addr: "localhost:6379",
})

store := statestore.NewRedisStore(redisClient)
```

### With Authentication and Options

```go
redisClient := redis.NewClient(&redis.Options{
    Addr:     os.Getenv("REDIS_HOST"),
    Password: os.Getenv("REDIS_PASSWORD"),
    DB:       0,
})

store := statestore.NewRedisStore(redisClient,
    statestore.WithTTL(24*time.Hour),
    statestore.WithPrefix("myapp:"),
)
```

### Connection Pooling

```go
// Redis client auto-manages connection pool
// Default settings work for most cases
// For high load, tune Redis server config
```

### TTL and Expiration

```go
// Redis keys auto-expire based on server TTL settings
// Set global TTL in Redis config:
// config set maxmemory-policy volatile-lru

// Or use EXPIRE in custom implementation:
redisClient.Expire(ctx, key, 24*time.Hour)
```

## Complete Examples

### Multi-User Chat

```go
package main

import (
    "context"
    "log"
    
    "github.com/redis/go-redis/v9"
    "github.com/AltairaLabs/PromptKit/runtime/pipeline"
    "github.com/AltairaLabs/PromptKit/runtime/providers/openai"
    "github.com/AltairaLabs/PromptKit/runtime/statestore"
)

func main() {
    // Create Redis store
    redisClient := redis.NewClient(&redis.Options{
        Addr: "localhost:6379",
    })
    store := statestore.NewRedisStore(redisClient)

    // Create provider
    provider := openai.NewProvider(
        "openai",
        "gpt-4o-mini",
        "",
        providers.ProviderDefaults{Temperature: 0.7, MaxTokens: 2000},
        false,
    )
    defer provider.Close()

    ctx := context.Background()

    // User 1 conversation - save state
    state1 := &statestore.ConversationState{
        ID:       "user-alice",
        Messages: []types.Message{{Role: "user", Content: "My favorite color is blue"}},
    }
    store.Save(ctx, state1)

    // User 2 conversation - separate state
    state2 := &statestore.ConversationState{
        ID:       "user-bob",
        Messages: []types.Message{{Role: "user", Content: "I love pizza"}},
    }
    store.Save(ctx, state2)

    // Load each user's state independently
    loaded1, _ := store.Load(ctx, "user-alice")
    log.Printf("User 1 messages: %d\n", len(loaded1.Messages))

    loaded2, _ := store.Load(ctx, "user-bob")
    log.Printf("User 2 messages: %d\n", len(loaded2.Messages))
}
```

### Session Cleanup

```go
package main

import (
    "context"
    "log"
    "time"
    
    "github.com/AltairaLabs/PromptKit/runtime/statestore"
)

func cleanupOldSessions(store statestore.Store) {
    // Redis TTL handles expiration automatically via WithTTL option
    // For manual cleanup, load and re-save trimmed state
    ctx := context.Background()
    conversationIDs := []string{"old-session-1", "old-session-2"}

    for _, id := range conversationIDs {
        state, err := store.Load(ctx, id)
        if err != nil {
            log.Printf("Failed to load conversation %s: %v", id, err)
            continue
        }
        // Trim old messages
        if len(state.Messages) > 20 {
            state.Messages = state.Messages[len(state.Messages)-20:]
            if err := store.Save(ctx, state); err != nil {
                log.Printf("Failed to save conversation %s: %v", id, err)
            }
        }
    }
}
```

## State Management Patterns

### Session ID Strategy

```go
// User-based sessions
sessionID := fmt.Sprintf("user-%s", userID)

// Conversation-based sessions
sessionID := fmt.Sprintf("conv-%s", conversationID)

// Time-based sessions
sessionID := fmt.Sprintf("user-%s-%s", userID, time.Now().Format("2006-01-02"))

// Feature-based sessions
sessionID := fmt.Sprintf("support-%s", ticketID)
```

### Context Window Management

```go
// Load existing state
state, err := store.Load(ctx, conversationID)
if err != nil {
    log.Fatal(err)
}

// Keep only recent messages (sliding window)
maxMessages := 10
if len(state.Messages) > maxMessages {
    state.Messages = state.Messages[len(state.Messages)-maxMessages:]
}

// Save trimmed state
err = store.Save(ctx, state)
```

### Token Limit Management

```go
// Load state
state, _ := store.Load(ctx, conversationID)

// Estimate and trim by token count
// Rough estimate: 4 chars per token
totalChars := 0
for _, msg := range state.Messages {
    totalChars += len(msg.Content)
}
estimatedTokens := totalChars / 4

// Trim if too many tokens
maxTokens := 4000
for estimatedTokens > maxTokens && len(state.Messages) > 1 {
    state.Messages = state.Messages[1:]
    totalChars = 0
    for _, msg := range state.Messages {
        totalChars += len(msg.Content)
    }
    estimatedTokens = totalChars / 4
}

// Save trimmed state
store.Save(ctx, state)
```

### State Sharing (Fork)

```go
// Fork state to a new session
sourceSession := "user-123-original"
targetSession := "user-123-new-topic"

err := store.Fork(ctx, sourceSession, targetSession)
if err != nil {
    log.Fatal(err)
}
```

## Error Handling

### Connection Errors

```go
state, err := store.Load(ctx, conversationID)
if err != nil {
    log.Printf("Failed to load state: %v", err)
    // Start with empty state
    state = &statestore.ConversationState{ID: conversationID}
}
```

### Save Failures

```go
err := store.Save(ctx, state)
if err != nil {
    log.Printf("Warning: Failed to save state: %v", err)
    // Continue execution, state may be lost
}
```

### Retry Logic

```go
func saveWithRetry(store statestore.Store, state *statestore.ConversationState, maxRetries int) error {
    ctx := context.Background()

    for i := 0; i < maxRetries; i++ {
        err := store.Save(ctx, state)
        if err == nil {
            return nil
        }

        log.Printf("Save failed (attempt %d/%d): %v", i+1, maxRetries, err)
        time.Sleep(time.Second * time.Duration(i+1))
    }

    return fmt.Errorf("failed after %d retries", maxRetries)
}
```

## Troubleshooting

### Issue: State Not Persisting

**Problem**: Messages not saved between requests.

**Solutions**:

1. Verify store is properly initialized:
   ```go
   store := statestore.NewMemoryStore()
   // or
   store := statestore.NewRedisStore(redisClient)
   ```

2. Check session ID consistency:
   ```go
   // Use same session ID for both calls
   sessionID := "user-123"
   pipe.ExecuteWithContext(ctx, sessionID, "user", "First message")
   pipe.ExecuteWithContext(ctx, sessionID, "user", "Second message")
   ```

3. Verify store connection:

   ```go
   // Test save/load
   testState := &statestore.ConversationState{ID: "test-session"}
   store.Save(ctx, testState)
   loaded, err := store.Load(ctx, "test-session")
   if err != nil {
       log.Fatal("Store not working")
   }
   ```
   

### Issue: Redis Connection Failed

**Problem**: Cannot connect to Redis.

**Solutions**:

1. Check Redis is running:
   ```bash
   redis-cli ping
   # Should return: PONG
   ```

2. Verify connection details:
   ```go
   redisClient := redis.NewClient(&redis.Options{
       Addr: "localhost:6379",
   })
   store := statestore.NewRedisStore(redisClient)
   ```

3. Test network connectivity:
   ```bash
   telnet localhost 6379
   ```

### Issue: Memory Growth

**Problem**: In-memory store consuming too much memory.

**Solutions**:

1. Switch to Redis for production
2. Implement periodic cleanup
3. Limit messages per conversation:
   ```go
   state, _ := store.Load(ctx, conversationID)
   if len(state.Messages) > 50 {
       state.Messages = state.Messages[len(state.Messages)-50:]
       store.Save(ctx, state)
   }
   ```

## Best Practices

1. **Use Redis for production**:
   ```go
   // Production
   redisClient := redis.NewClient(&redis.Options{
       Addr: os.Getenv("REDIS_URL"),
   })
   store := statestore.NewRedisStore(redisClient)

   // Development
   store := statestore.NewMemoryStore()
   ```

2. **Use TTL for Redis store**:
   ```go
   store := statestore.NewRedisStore(redisClient, statestore.WithTTL(24*time.Hour))
   ```

3. **Use descriptive session IDs**:
   ```go
   // Good
   sessionID := fmt.Sprintf("user-%s-support", userID)
   
   // Bad
   sessionID := "session1"
   ```

4. **Implement session cleanup**:
   ```go
   // Set Redis TTL
   config set maxmemory-policy volatile-lru
   
   // Or periodic cleanup
   go cleanupOldSessions(store, 24*time.Hour)
   ```

5. **Handle state errors gracefully**:
   ```go
   state, err := store.Load(ctx, conversationID)
   if err != nil {
       log.Printf("State load failed: %v", err)
       state = &statestore.ConversationState{ID: conversationID}  // Start fresh
   }
   ```

6. **Manage context window**:
   ```go
   // Trim old messages
   maxMessages := 20
   if len(state.Messages) > maxMessages {
       state.Messages = state.Messages[len(state.Messages)-maxMessages:]
   }
   ```

## Next Steps

- [Handle Errors](handle-errors) - Error management
- [Monitor Costs](monitor-costs) - Track usage
- [Configure Pipeline](configure-pipeline) - Complete setup

## See Also

- [StateStore Reference](../reference/statestore) - Complete API
- [Pipeline Reference](../reference/pipeline) - Middleware order
