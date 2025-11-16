---
title: Manage State
docType: how-to
order: 4
---
# How to Manage State

Persist conversation state across pipeline executions.

## Goal

Store and retrieve conversation state for continuity.

## Quick Start

### Step 1: Create State Store

```go
import "github.com/AltairaLabs/PromptKit/runtime/statestore"

// In-memory store (dev/testing)
store := statestore.NewInMemoryStateStore()

// Redis store (production)
store, err := statestore.NewRedisStateStore("localhost:6379", "", 0)
if err != nil {
    log.Fatal(err)
}
defer store.Close()
```

### Step 2: Add State Middleware

```go
import "github.com/AltairaLabs/PromptKit/runtime/pipeline/middleware"

pipe := pipeline.NewPipeline(
    middleware.StateMiddleware(store),
    middleware.ProviderMiddleware(provider, toolRegistry, policy, config),
)
```

### Step 3: Use Session IDs

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
store := statestore.NewInMemoryStateStore()
```

**Use Cases**:
- Development
- Testing
- Single-instance applications
- Non-persistent sessions

### Save and Load

```go
// Save state manually
sessionID := "session-1"
messages := []types.Message{
    {Role: "user", Content: "Hello"},
    {Role: "assistant", Content: "Hi there!"},
}

err := store.Save(ctx, sessionID, messages)
if err != nil {
    log.Fatal(err)
}

// Load state
loaded, err := store.Load(ctx, sessionID)
if err != nil {
    log.Fatal(err)
}

for _, msg := range loaded {
    log.Printf("%s: %s\n", msg.Role, msg.Content)
}
```

### Delete State

```go
err := store.Delete(ctx, sessionID)
if err != nil {
    log.Fatal(err)
}
```

## Redis State Store

### Basic Setup

```go
store, err := statestore.NewRedisStateStore(
    "localhost:6379",  // address
    "",                // password (empty for no auth)
    0,                 // database
)
if err != nil {
    log.Fatal(err)
}
defer store.Close()
```

### With Authentication

```go
store, err := statestore.NewRedisStateStore(
    os.Getenv("REDIS_HOST"),
    os.Getenv("REDIS_PASSWORD"),
    0,
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
    
    "github.com/AltairaLabs/PromptKit/runtime/pipeline"
    "github.com/AltairaLabs/PromptKit/runtime/pipeline/middleware"
    "github.com/AltairaLabs/PromptKit/runtime/providers/openai"
    "github.com/AltairaLabs/PromptKit/runtime/statestore"
)

func main() {
    // Create Redis store
    store, err := statestore.NewRedisStateStore("localhost:6379", "", 0)
    if err != nil {
        log.Fatal(err)
    }
    defer store.Close()
    
    // Create provider
    provider := openai.NewOpenAIProvider(
        "openai",
        "gpt-4o-mini",
        "",
        openai.DefaultProviderDefaults(),
        false,
    )
    defer provider.Close()
    
    // Build pipeline with state
    pipe := pipeline.NewPipeline(
        middleware.StateMiddleware(store),
        middleware.ProviderMiddleware(provider, nil, nil, &middleware.ProviderMiddlewareConfig{
            MaxTokens:   1000,
            Temperature: 0.7,
        }),
    )
    defer pipe.Shutdown(context.Background())
    
    ctx := context.Background()
    
    // User 1 conversation
    userID1 := "user-alice"
    pipe.ExecuteWithContext(ctx, userID1, "user", "My favorite color is blue")
    result1, _ := pipe.ExecuteWithContext(ctx, userID1, "user", "What's my favorite color?")
    log.Printf("User 1: %s\n", result1.Response.Content)
    
    // User 2 conversation (separate state)
    userID2 := "user-bob"
    pipe.ExecuteWithContext(ctx, userID2, "user", "I love pizza")
    result2, _ := pipe.ExecuteWithContext(ctx, userID2, "user", "What food do I love?")
    log.Printf("User 2: %s\n", result2.Response.Content)
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

func cleanupOldSessions(store statestore.StateStore, maxAge time.Duration) {
    // For in-memory store
    if memStore, ok := store.(*statestore.InMemoryStateStore); ok {
        // Custom cleanup logic
        // InMemoryStateStore doesn't expose session listing
        // Implement custom session tracking if needed
    }
    
    // For Redis store
    if redisStore, ok := store.(*statestore.RedisStateStore); ok {
        // Redis TTL handles expiration automatically
        // Or manually delete specific sessions:
        ctx := context.Background()
        sessionIDs := []string{"old-session-1", "old-session-2"}
        
        for _, id := range sessionIDs {
            if err := redisStore.Delete(ctx, id); err != nil {
                log.Printf("Failed to delete session %s: %v", id, err)
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
messages, err := store.Load(ctx, sessionID)
if err != nil {
    log.Fatal(err)
}

// Keep only recent messages (sliding window)
maxMessages := 10
if len(messages) > maxMessages {
    messages = messages[len(messages)-maxMessages:]
}

// Save trimmed state
err = store.Save(ctx, sessionID, messages)
```

### Token Limit Management

```go
import "github.com/AltairaLabs/PromptKit/runtime/prompt"

// Load state
messages, _ := store.Load(ctx, sessionID)

// Truncate by tokens
maxTokens := 4000
truncated := prompt.TruncateMessages(messages, maxTokens)

// Save truncated state
store.Save(ctx, sessionID, truncated)
```

### State Sharing

```go
// Copy state between sessions
sourceSession := "user-123-original"
targetSession := "user-123-new-topic"

messages, err := store.Load(ctx, sourceSession)
if err != nil {
    log.Fatal(err)
}

// Save to new session
err = store.Save(ctx, targetSession, messages)
```

## Error Handling

### Connection Errors

```go
messages, err := store.Load(ctx, sessionID)
if err != nil {
    log.Printf("Failed to load state: %v", err)
    // Start with empty state
    messages = []types.Message{}
}
```

### Save Failures

```go
err := store.Save(ctx, sessionID, messages)
if err != nil {
    log.Printf("Warning: Failed to save state: %v", err)
    // Continue execution, state may be lost
}
```

### Retry Logic

```go
func saveWithRetry(store statestore.StateStore, sessionID string, messages []types.Message, maxRetries int) error {
    ctx := context.Background()
    
    for i := 0; i < maxRetries; i++ {
        err := store.Save(ctx, sessionID, messages)
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

1. Verify state middleware is registered:
   ```go
   pipe := pipeline.NewPipeline(
       middleware.StateMiddleware(store),  // Must be first
       middleware.ProviderMiddleware(provider, nil, nil, config),
   )
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
   testMessages := []types.Message
   store.Save(ctx, "test-session", testMessages)
   loaded, err := store.Load(ctx, "test-session")
   if err != nil || len(loaded) == 0 {
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
   store, err := statestore.NewRedisStateStore("localhost:6379", "", 0)
   if err != nil {
       log.Printf("Connection failed: %v", err)
       // Check host, port, password
   }
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
3. Limit messages per session:
   ```go
   messages, _ := store.Load(ctx, sessionID)
   if len(messages) > 50 {
       messages = messages[len(messages)-50:]
       store.Save(ctx, sessionID, messages)
   }
   ```

## Best Practices

1. **Use Redis for production**:
   ```go
   // Production
   store, _ := statestore.NewRedisStateStore(os.Getenv("REDIS_URL"), "", 0)
   
   // Development
   store := statestore.NewInMemoryStateStore()
   ```

2. **Always close store**:
   ```go
   defer store.Close()
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
   messages, err := store.Load(ctx, sessionID)
   if err != nil {
       log.Printf("State load failed: %v", err)
       messages = []types.Message{}  // Start fresh
   }
   ```

6. **Manage context window**:
   ```go
   // Trim old messages
   maxMessages := 20
   if len(messages) > maxMessages {
       messages = messages[len(messages)-maxMessages:]
   }
   ```

## Next Steps

- [Handle Errors](handle-errors) - Error management
- [Monitor Costs](monitor-costs) - Track usage
- [Configure Pipeline](configure-pipeline) - Complete setup

## See Also

- [StateStore Reference](../reference/statestore) - Complete API
- [Pipeline Reference](../reference/pipeline) - Middleware order
