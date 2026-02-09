---
title: State Management
sidebar:
  order: 4
---
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
response1 := llm.Predict("What's the capital of France?")
// Response: "Paris"

// Second call - LLM has no memory
response2 := llm.Predict("What about Germany?")
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
response := llm.Predict(messages)
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

### State Management Flow

State is managed through the Store interface:

```go
// Load conversation state by ID
state, _ := store.Load(ctx, conversationID)

// Access message history
messages := state.Messages

// Execute with loaded context
// ... pipeline execution ...

// Save updated state
state.Messages = append(state.Messages, newMessages...)
store.Save(ctx, state)
```

**Before execution**:
- Load history for session ID

**After execution**:
- Save new messages to store

## State Store Interface

All stores implement:

```go
type Store interface {
    Load(ctx context.Context, id string) (*ConversationState, error)
    Save(ctx context.Context, state *ConversationState) error
    Fork(ctx context.Context, sourceID, newID string) error
}
```

The `ConversationState` struct holds all conversation data:

```go
type ConversationState struct {
    ID             string
    UserID         string
    Messages       []types.Message
    SystemPrompt   string
    Summaries      []string
    TokenCount     int
    LastAccessedAt time.Time
    Metadata       map[string]any
}
```

### In-Memory Store

Fast, but not persistent:

```go
store := statestore.NewMemoryStore()
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
store := statestore.NewRedisStore(redisClient)
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

**Decision**: Store is separate from pipeline logic

**Rationale**:
- **Pluggable**: Easy to swap storage backends
- **Testable**: Mock stores for testing
- **Reusable**: Stores can be used outside pipelines

**Alternative considered**: Embedding storage in pipeline. Rejected as too coupled.

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
// Load and trim messages before execution
state, _ := store.Load(ctx, conversationID)
if len(state.Messages) > 10 {
    state.Messages = state.Messages[len(state.Messages)-10:]
}
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

Use Redis TTL or application-level expiration to auto-expire sessions.

### Forking Sessions

Branch conversations:

```go
// Fork a conversation to try a different direction
store.Fork(ctx, sessionID, newSessionID)
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
// Fast: Load and trim to last 10 messages
state, _ := store.Load(ctx, conversationID)
if len(state.Messages) > 10 {
    state.Messages = state.Messages[len(state.Messages)-10:]
}

// Slow: Load all messages (no trimming)
state, _ := store.Load(ctx, conversationID)
```

**2. Lazy Loading**

Load on demand:

```go
type LazyStore struct {
    inner statestore.Store
    cache map[string]*statestore.ConversationState
}

func (s *LazyStore) Load(ctx context.Context, id string) (*statestore.ConversationState, error) {
    if cached, ok := s.cache[id]; ok {
        return cached, nil  // Cache hit
    }

    state, err := s.inner.Load(ctx, id)
    if err == nil {
        s.cache[id] = state
    }
    return state, err
}
```

**3. Compression**

Compress stored messages:

```go
type CompressedStore struct {
    inner statestore.Store
}

func (s *CompressedStore) Save(ctx context.Context, state *statestore.ConversationState) error {
    // Compress messages before saving
    state.Messages = compress(state.Messages)
    return s.inner.Save(ctx, state)
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

func (s *VersionedStore) Save(sessionID string, messages []Message, expectedVersion int) error {
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

```go
func TestStateManagement(t *testing.T) {
    store := statestore.NewMemoryStore()
    
    ctx := context.Background()

    // Test save
    state := &statestore.ConversationState{
        ID:       "session-1",
        Messages: []types.Message{},
    }
    err := store.Save(ctx, state)
    assert.NoError(t, err)

    // Test load
    loaded, err := store.Load(ctx, "session-1")
    assert.NoError(t, err)
    assert.Equal(t, state.Messages, loaded.Messages)
}
```

### Mock Store

For testing state management:

```go
type MockStore struct {
    states map[string]*statestore.ConversationState
}

func (m *MockStore) Load(ctx context.Context, id string) (*statestore.ConversationState, error) {
    state, ok := m.states[id]
    if !ok {
        return nil, statestore.ErrNotFound
    }
    return state, nil
}

func (m *MockStore) Save(ctx context.Context, state *statestore.ConversationState) error {
    m.states[state.ID] = state
    return nil
}

func (m *MockStore) Fork(ctx context.Context, sourceID, newID string) error {
    source, ok := m.states[sourceID]
    if !ok {
        return statestore.ErrNotFound
    }
    newState := *source
    newState.ID = newID
    newState.Messages = append([]types.Message{}, source.Messages...)
    m.states[newID] = &newState
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

### Branching Conversations

Fork a session to explore different directions:

```go
func branchConversation(userID, newBranchID string) {
    sessionID := fmt.Sprintf("user-%s", userID)
    newSessionID := fmt.Sprintf("user-%s-branch-%s", userID, newBranchID)
    store.Fork(ctx, sessionID, newSessionID)
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
state, err := store.Load(ctx, conversationID)
if err != nil {
    log.Printf("Failed to load state: %v", err)
    // Continue with empty state
    state = &statestore.ConversationState{ID: conversationID}
}
```

### Backup and Recovery

Redis persistence:

- **RDB**: Periodic snapshots
- **AOF**: Append-only file

Enable both for best durability.

### Privacy Compliance

- **Data deletion**: Implement cleanup mechanisms
- **Data export**: Allow users to export history
- **Encryption**: Encrypt messages at rest
- **TTL**: Automatically delete old data

## Example: Complete Setup

### Development (In-Memory)

```go
store := statestore.NewMemoryStore()

// Use store directly for state management
state, _ := store.Load(ctx, conversationID)
// ... execute pipeline with state.Messages ...
state.Messages = append(state.Messages, newMessages...)
store.Save(ctx, state)
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

store := statestore.NewRedisStore(redisClient)

// Use store directly for state management
state, _ := store.Load(ctx, conversationID)
// ... execute pipeline with state.Messages ...
state.Messages = append(state.Messages, newMessages...)
store.Save(ctx, state)
```

## Summary

State management provides:

✅ **Multi-turn conversations**: Maintain context across requests  
✅ **Multi-user support**: Isolated sessions per user  
✅ **Scalability**: Redis for distributed deployments  
✅ **Performance**: Configurable history limits  
✅ **Flexibility**: Pluggable storage backends  

## Related Topics

- [Pipeline Architecture](pipeline-architecture) - How state fits in pipelines
- [State Store Reference](../reference/statestore) - Store interface details
- [StateStore Reference](../reference/statestore) - Complete API
- [Multi-turn Conversations Tutorial](../tutorials/02-multi-turn) - Hands-on guide

## Further Reading

- Redis persistence strategies
- Session management patterns
- Distributed state management
- CAP theorem and consistency
