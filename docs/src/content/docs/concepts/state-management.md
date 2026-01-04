---
title: State Management
sidebar:
  order: 5
---
Understanding conversation state and persistence in PromptKit.

## What is State Management?

**State management** maintains conversation history across multiple turns. It allows LLMs to remember previous interactions.

## Why Manage State?

**Context**: LLMs need history to understand conversations  
**Continuity**: Users expect the AI to remember  
**Multi-turn**: Enable back-and-forth dialogue  
**Personalization**: Remember user preferences  

## The Problem

LLMs are stateless:

```go
// First message
response1 := llm.Complete("What's the capital of France?")
// "Paris"

// Second message - no memory!
response2 := llm.Complete("What about Germany?")
// "What do you mean 'what about Germany'?"
```

## The Solution

Pass conversation history:

```go
messages := []Message{
    {Role: "user", Content: "What's the capital of France?"},
    {Role: "assistant", Content: "Paris"},
    {Role: "user", Content: "What about Germany?"},
}
response := llm.Complete(messages)
// "The capital of Germany is Berlin"
```

## State in PromptKit

### Session-Based Architecture

Each conversation has a **session ID**:

```go
sessionID := "user-123"
result, _ := pipeline.ExecuteWithSession(ctx, sessionID, "user", "Hello")
```

Sessions enable:
- **Multi-user support**: Separate conversations
- **History isolation**: Users don't see each other's messages
- **Concurrent access**: Multiple requests per session

### StateMiddleware

Manages state automatically:

```go
store := statestore.NewRedisStateStore(redisClient)

stateMiddleware := middleware.StateMiddleware(store, &middleware.StateMiddlewareConfig{
    MaxMessages: 10,
    TTL:         24 * time.Hour,
})

pipe := pipeline.NewPipeline(
    stateMiddleware,  // Loads history before, saves after
    middleware.ProviderMiddleware(provider, nil, nil, nil),
)
```

**Before execution**: Loads history  
**After execution**: Saves new messages  

## State Stores

### In-Memory Store

Fast, but not persistent:

```go
store := statestore.NewInMemoryStateStore()
```

**Pros**: Very fast (~1-10µs)  
**Cons**: Lost on restart, single-instance only  
**Use for**: Development, testing, demos  

### Redis Store

Persistent and scalable:

```go
redisClient := redis.NewClient(&redis.Options{
    Addr: "localhost:6379",
})
store := statestore.NewRedisStateStore(redisClient)
```

**Pros**: Persistent, multi-instance, TTL support  
**Cons**: Slower (~1-5ms), requires Redis  
**Use for**: Production, distributed systems  

## Configuration Options

### Message Limits

Control history size:

```go
config := &middleware.StateMiddlewareConfig{
    MaxMessages: 20,  // Keep last 20 messages
}
```

**Benefits**:
- Lower costs (fewer tokens)
- Faster loading
- More relevant context

### Time-To-Live (TTL)

Auto-delete old sessions:

```go
config := &middleware.StateMiddlewareConfig{
    TTL: 24 * time.Hour,  // Delete after 24h
}
```

**Benefits**:
- Automatic cleanup
- Privacy compliance
- Cost reduction

## Session Patterns

### User Sessions

One session per user:

```go
sessionID := fmt.Sprintf("user-%s", userID)
```

**Use case**: Single ongoing conversation per user

### Conversation Sessions

Multiple conversations per user:

```go
sessionID := fmt.Sprintf("user-%s-conv-%s", userID, conversationID)
```

**Use case**: User can start multiple conversations

### Temporary Sessions

Anonymous sessions:

```go
sessionID := uuid.New().String()
```

**Use case**: Guest users, no account required

## Best Practices

### Do's

✅ **Use Redis in production**
```go
// Production
store := statestore.NewRedisStateStore(redisClient)

// Development
store := statestore.NewInMemoryStateStore()
```

✅ **Set appropriate limits**
```go
// Balance context vs cost
MaxMessages: 10-20  // Good for most cases
```

✅ **Set TTL for privacy**
```go
TTL: 24 * time.Hour  // Delete old conversations
```

✅ **Handle errors gracefully**
```go
messages, err := store.Load(sessionID)
if err != nil {
    // Continue with empty history
    messages = []Message{}
}
```

### Don'ts

❌ **Don't store infinite history** - Cost and performance  
❌ **Don't use in-memory in production** - Not persistent  
❌ **Don't forget to clean up** - Privacy and storage  
❌ **Don't ignore errors** - Handle store failures  

## Multi-Instance Scaling

### With Redis

```
User A → [Instance 1] ↘
                        [Redis Store]
User B → [Instance 2] ↗
```

Benefits:
- High availability
- Horizontal scaling
- Shared state

### Session Affinity

Route user to same instance:

```
User (session-123) → Instance 1  (every time)
User (session-456) → Instance 2  (every time)
```

Benefits:
- Local caching
- Reduced Redis load
- Lower latency

## Performance Optimization

### Limit History Size

```go
// Fast: Load 10 messages
MaxMessages: 10

// Slow: Load all messages
MaxMessages: -1  // Unlimited
```

### Lazy Loading

Load on demand:

```go
// Only load when needed
if requiresHistory {
    messages, _ := store.Load(sessionID)
}
```

### Compression

For large histories:

```go
compressed := gzip.Compress(messages)
store.Save(sessionID, compressed)
```

## Monitoring State

### Track Metrics

```go
type StateMetrics struct {
    ActiveSessions   int
    AvgHistorySize   int
    LoadLatency      time.Duration
    StorageUsed      int64
}
```

### Set Alerts

```go
if metrics.ActiveSessions > 10000 {
    alert.Send("High active session count")
}

if metrics.LoadLatency > 100*time.Millisecond {
    alert.Send("Slow state loading")
}
```

## Testing State Management

### Unit Tests

```go
func TestStateStore(t *testing.T) {
    store := statestore.NewInMemoryStateStore()
    
    // Save messages
    messages := []types.Message{
        {Role: "user", Content: "Hello"},
    }
    err := store.Save("session-1", messages)
    assert.NoError(t, err)
    
    // Load messages
    loaded, err := store.Load("session-1")
    assert.NoError(t, err)
    assert.Equal(t, messages, loaded)
}
```

### Integration Tests

```go
func TestConversationFlow(t *testing.T) {
    pipe := createPipelineWithState()
    sessionID := "test-session"
    
    // First message
    result1, _ := pipe.ExecuteWithSession(ctx, sessionID, "user", "My name is Alice")
    
    // Second message - should remember
    result2, _ := pipe.ExecuteWithSession(ctx, sessionID, "user", "What's my name?")
    
    assert.Contains(t, result2.Response.Content, "Alice")
}
```

## Summary

State management provides:

✅ **Context** - LLMs remember conversations  
✅ **Continuity** - Multi-turn dialogue  
✅ **Scalability** - Redis for distributed systems  
✅ **Performance** - Configurable limits  
✅ **Privacy** - TTL-based cleanup  

## Related Documentation

- [State Management Explanation](../runtime/explanation/state-management) - Architecture details
- [Manage State How-To](../runtime/how-to/manage-state) - Implementation guide
- [Multi-turn Tutorial](../runtime/tutorials/02-multi-turn) - Step-by-step guide
- [StateStore Reference](../runtime/reference/statestore) - API documentation
