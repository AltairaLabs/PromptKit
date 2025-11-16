---
layout: default
title: State Management
parent: Runtime Explanation
grand_parent: Runtime
nav_order: 4
---

# State Management

Understanding how Runtime manages conversation state.

## Overview

Runtime uses **session-based state management** to maintain conversation history across multiple turns.

## Core Concept

Every conversation has:

- **Session ID**: Unique identifier
- **Message History**: Past conversation turns
- **State Store**: Persistent storage

```
User → [Session: abc123] → Pipeline → LLM
         ↓                    ↓
    [Redis/Memory Store]   Response
         ↓
    Persisted History
```

## Why State Management?

### Problem: Stateless LLMs

LLMs don't remember previous interactions:

```go
// First call
response1 := llm.Complete("What's the capital of France?")
// Response: "Paris"

// Second call - LLM has no memory
response2 := llm.Complete("What about Germany?")
// Response: "Germany? What about it?" (No context!)
```

### Solution: Conversation History

Pass history with each request:

```go
messages := []Message{
    {Role: "user", Content: "What's the capital of France?"},
    {Role: "assistant", Content: "Paris"},
    {Role: "user", Content: "What about Germany?"},
}
response := llm.Complete(messages)
// Response: "The capital of Germany is Berlin"
```

## Session-Based Architecture

### Session ID

Each conversation has a unique ID:

```go
sessionID := "user-123-conv-456"

result, err := pipeline.ExecuteWithSession(ctx, sessionID, "user", "Hello")
```

Sessions enable:

- **Multi-user support**: Separate conversations
- **History isolation**: Users don't see each other's history
- **Concurrent access**: Multiple requests per session

### StateMiddleware

Manages state automatically:

```go
stateMiddleware := middleware.StateMiddleware(store, &middleware.StateMiddlewareConfig{
    MaxMessages: 10,
    TTL:         24 * time.Hour,
})

pipeline := pipeline.NewPipeline(
    stateMiddleware,
    // ... other middleware
)
```

**Before execution**:
- Load history for session ID
- Add to ExecutionContext.Messages

**After execution**:
- Save new messages
- Update store

## State Store Interface

All stores implement:

```go
type StateStore interface {
    Load(sessionID string) ([]Message, error)
    Save(sessionID string, messages []Message) error
    Delete(sessionID string) error
}
```

### In-Memory Store

Fast, but not persistent:

```go
store := statestore.NewInMemoryStateStore()
```

**Characteristics**:
- Speed: ~1-10µs per operation
- Persistence: Lost on restart
- Scalability: Single instance only
- Cleanup: Manual eviction needed

**Use cases**:
- Testing
- Development
- Single-instance deployments
- Short-lived sessions

### Redis Store

Persistent and scalable:

```go
redisClient := redis.NewClient(&redis.Options{
    Addr: "localhost:6379",
})
store := statestore.NewRedisStateStore(redisClient)
```

**Characteristics**:
- Speed: ~1-5ms per operation
- Persistence: Survives restarts
- Scalability: Multi-instance support
- Cleanup: TTL-based expiration

**Use cases**:
- Production deployments
- Multi-instance scaling
- Long-lived sessions
- High availability

## Design Decisions

### Why Session IDs?

**Decision**: Use session IDs to identify conversations

**Rationale**:
- **Isolation**: Separate conversations
- **Multi-user**: Support concurrent users
- **Flexibility**: Sessions can be user-scoped, conversation-scoped, or any other scope

**Alternative considered**: Implicit session based on user ID. Rejected because:
- Users may have multiple conversations
- No way to start new conversation
- Harder to manage session lifecycle

### Why Separate Store Interface?

**Decision**: StateStore is separate from middleware

**Rationale**:
- **Pluggable**: Easy to swap storage backends
- **Testable**: Mock stores for testing
- **Reusable**: Stores can be used outside pipelines

**Alternative considered**: Embedding storage in StateMiddleware. Rejected as too coupled.

### Why Message History?

**Decision**: Store full messages, not raw text

**Rationale**:
- **Rich context**: Preserve roles, tool calls, metadata
- **Accurate replay**: Reconstruct exact conversation
- **Provider compatibility**: Messages map to provider formats

**Alternative considered**: Store only text. Rejected because:
- Loses role information
- Can't reconstruct tool interactions
- Harder to debug

### Why TTL?

**Decision**: Messages expire after TTL

**Rationale**:
- **Cleanup**: Automatic deletion of old sessions
- **Privacy**: Don't store conversations forever
- **Cost**: Reduce storage costs

**Trade-off**: Active conversations may expire. Acceptable with reasonable TTL (e.g., 24 hours).

## Storage Strategies

### Message Limits

Limit history size:

```go
StateMiddleware(store, &StateMiddlewareConfig{
    MaxMessages: 10,  // Keep last 10 messages only
})
```

**Benefits**:
- **Performance**: Less data to load
- **Cost**: Fewer tokens sent to LLM
- **Relevance**: Focus on recent context

**Trade-off**: Loses older context. Use higher limits for long conversations.

### Sliding Window

Keep recent messages:

```
[Old messages...] [Recent 10 messages] ← Kept
        ↓
    Discarded
```

### Time-Based Expiration

Delete old sessions:

```go
StateMiddleware(store, &StateMiddlewareConfig{
    TTL: 24 * time.Hour,  // Sessions expire after 24 hours
})
```

### Manual Cleanup

Delete sessions explicitly:

```go
// End conversation
store.Delete(sessionID)
```

## Scaling Considerations

### Single-Instance (In-Memory)

```
User → [Instance] → In-Memory Store
```

**Limitations**:
- Single point of failure
- No persistence
- Limited to one instance

### Multi-Instance (Redis)

```
User → [Instance 1] ↘
                      [Redis Store]
User → [Instance 2] ↗
```

**Benefits**:
- High availability
- Persistence
- Horizontal scaling

### Session Affinity

Route users to same instance:

```
User (session-123) → Instance 1
User (session-456) → Instance 2
```

**Benefits**:
- Reduced latency (local cache)
- Lower Redis load

**Implementation**: Load balancer with session affinity

## State Loading Performance

### Load Time

Typical performance:

- **In-Memory**: 1-10µs
- **Redis (local)**: 1-2ms
- **Redis (remote)**: 5-10ms

### Optimization Strategies

**1. Message Limits**

```go
// Fast: Load last 10 messages
StateMiddleware(store, &StateMiddlewareConfig{
    MaxMessages: 10,
})

// Slow: Load all messages
StateMiddleware(store, &StateMiddlewareConfig{
    MaxMessages: -1,  // No limit
})
```

**2. Lazy Loading**

Load on demand:

```go
type LazyStateStore struct {
    inner StateStore
    cache map[string][]Message
}

func (s *LazyStateStore) Load(sessionID string) ([]Message, error) {
    if cached, ok := s.cache[sessionID]; ok {
        return cached, nil  // Cache hit
    }
    
    messages, err := s.inner.Load(sessionID)
    s.cache[sessionID] = messages
    return messages, err
}
```

**3. Compression**

Compress stored messages:

```go
type CompressedStore struct {
    inner StateStore
}

func (s *CompressedStore) Save(sessionID string, messages []Message) error {
    compressed := compress(messages)
    return s.inner.Save(sessionID, compressed)
}
```

**Trade-off**: CPU for storage. Worth it for large histories.

## Concurrency and Consistency

### Race Conditions

Multiple requests per session:

```
Request 1: Load → Execute → Save
Request 2:     Load → Execute → Save
```

**Problem**: Request 2 may overwrite Request 1's changes.

### Solution: Optimistic Locking

```go
type Message struct {
    // ... fields
    Version int
}

func (s *StateStore) Save(sessionID string, messages []Message, expectedVersion int) error {
    currentVersion := s.getVersion(sessionID)
    if currentVersion != expectedVersion {
        return ErrVersionMismatch
    }
    
    s.saveWithVersion(sessionID, messages, currentVersion+1)
    return nil
}
```

Retry on version mismatch.

### Eventual Consistency

With Redis:

- **Single-instance Redis**: Strongly consistent
- **Redis Cluster**: Eventually consistent

**Impact**: Rare edge cases where history may be stale. Acceptable for most applications.

## Testing State Management

### In-Memory for Tests

Use in-memory store for fast tests:

{% raw %}
```go
func TestStateManagement(t *testing.T) {
    store := statestore.NewInMemoryStateStore()
    
    // Test save
    messages := []Message{{Role: "user", Content: "test"}}
    err := store.Save("session-1", messages)
    assert.NoError(t, err)
    
    // Test load
    loaded, err := store.Load("session-1")
    assert.NoError(t, err)
    assert.Equal(t, messages, loaded)
}
```
{% endraw %}

### Mock Store

For testing middleware:

```go
type MockStateStore struct {
    messages map[string][]Message
}

func (m *MockStateStore) Load(sessionID string) ([]Message, error) {
    return m.messages[sessionID], nil
}

func (m *MockStateStore) Save(sessionID string, messages []Message) error {
    m.messages[sessionID] = messages
    return nil
}
```

## Common Patterns

### User Sessions

One session per user:

```go
sessionID := fmt.Sprintf("user-%s", userID)
result, err := pipeline.ExecuteWithSession(ctx, sessionID, "user", "Hello")
```

**Use case**: Single ongoing conversation per user

### Conversation Sessions

Multiple conversations per user:

```go
sessionID := fmt.Sprintf("user-%s-conv-%s", userID, conversationID)
result, err := pipeline.ExecuteWithSession(ctx, sessionID, "user", "Hello")
```

**Use case**: User can start multiple conversations

### Temporary Sessions

Anonymous sessions:

```go
sessionID := uuid.New().String()
result, err := pipeline.ExecuteWithSession(ctx, sessionID, "user", "Hello")
```

**Use case**: Guest users, temporary chats

### Cleanup on Logout

Delete user sessions:

```go
func handleLogout(userID string) {
    sessionID := fmt.Sprintf("user-%s", userID)
    store.Delete(sessionID)
}
```

## Production Considerations

### Monitoring

Track state metrics:

- **Load latency**: Time to load history
- **Save latency**: Time to save messages
- **History size**: Bytes per session
- **Active sessions**: Number of sessions

### Error Handling

Handle store failures gracefully:

```go
messages, err := store.Load(sessionID)
if err != nil {
    log.Printf("Failed to load state: %v", err)
    // Continue with empty history
    messages = []Message{}
}
```

### Backup and Recovery

Redis persistence:

- **RDB**: Periodic snapshots
- **AOF**: Append-only file

Enable both for best durability.

### Privacy Compliance

- **Data deletion**: Implement Delete()
- **Data export**: Allow users to export history
- **Encryption**: Encrypt messages at rest
- **TTL**: Automatically delete old data

## Example: Complete Setup

### Development (In-Memory)

```go
store := statestore.NewInMemoryStateStore()

stateMiddleware := middleware.StateMiddleware(store, &middleware.StateMiddlewareConfig{
    MaxMessages: 10,
    TTL:         1 * time.Hour,
})

pipeline := pipeline.NewPipeline(
    stateMiddleware,
    // ... other middleware
)
```

### Production (Redis)

```go
redisClient := redis.NewClient(&redis.Options{
    Addr:         "redis:6379",
    Password:     os.Getenv("REDIS_PASSWORD"),
    DB:           0,
    MaxRetries:   3,
    PoolSize:     10,
})

store := statestore.NewRedisStateStore(redisClient)

stateMiddleware := middleware.StateMiddleware(store, &middleware.StateMiddlewareConfig{
    MaxMessages: 20,
    TTL:         24 * time.Hour,
})

pipeline := pipeline.NewPipeline(
    stateMiddleware,
    // ... other middleware
)
```

## Summary

State management provides:

✅ **Multi-turn conversations**: Maintain context across requests  
✅ **Multi-user support**: Isolated sessions per user  
✅ **Scalability**: Redis for distributed deployments  
✅ **Performance**: Configurable history limits  
✅ **Flexibility**: Pluggable storage backends  

## Related Topics

- [Pipeline Architecture](pipeline-architecture.md) - How state fits in pipelines
- [Middleware Design](middleware-design.md) - StateMiddleware implementation
- [StateStore Reference](../reference/statestore.md) - Complete API
- [Multi-turn Conversations Tutorial](../tutorials/02-multi-turn.md) - Hands-on guide

## Further Reading

- Redis persistence strategies
- Session management patterns
- Distributed state management
- CAP theorem and consistency
