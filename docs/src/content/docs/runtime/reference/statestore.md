---
title: State Store
sidebar:
  order: 6
---
Conversation persistence and state management.

## Overview

State stores provide persistent storage for conversation history, enabling:

- **Session continuity**: Resume conversations across restarts
- **Multi-turn conversations**: Maintain context across requests
- **State sharing**: Share conversation state across instances
- **Debugging**: Inspect conversation history

## Supported Backends

- **Redis**: Production-ready distributed state
- **In-memory**: Development and testing
- **Custom**: Implement `Store` interface

## Core Interface

```go
type Store interface {
    Load(ctx context.Context, id string) (*ConversationState, error)
    Save(ctx context.Context, state *ConversationState) error
    Fork(ctx context.Context, sourceID, newID string) error
}
```

### ConversationState

```go
type ConversationState struct {
    ID             string
    UserID         string
    Messages       []types.Message
    SystemPrompt   string
    Summaries      []Summary
    TokenCount     int
    LastAccessedAt time.Time
    Metadata       map[string]interface{}
}
```

## Redis State Store

### Constructor

```go
func NewRedisStore(client *redis.Client, opts ...RedisOption) *RedisStore
```

**Options**:
- `WithTTL(ttl time.Duration)` - Set TTL for conversation states (default: 24 hours)
- `WithPrefix(prefix string)` - Set key prefix (default: "promptkit")

**Example**:
```go
import (
    "github.com/redis/go-redis/v9"
    "github.com/AltairaLabs/PromptKit/runtime/statestore"
)

// Create Redis client
redisClient := redis.NewClient(&redis.Options{
    Addr: "localhost:6379",
    DB:   0,
})

// Create state store
store := statestore.NewRedisStore(redisClient)
```

### Methods

**Save**:
```go
state := &statestore.ConversationState{
    ID: "session-123",
    Messages: []types.Message{
        {Role: "user", Content: "Hello"},
        {Role: "assistant", Content: "Hi there!"},
    },
}

err := store.Save(ctx, state)
if err != nil {
    log.Fatal(err)
}
```

**Load**:
```go
state, err := store.Load(ctx, "session-123")
if err != nil {
    log.Fatal(err)
}

for _, msg := range state.Messages {
    fmt.Printf("%s: %s\n", msg.Role, msg.Content)
}
```

**Fork**:
```go
err := store.Fork(ctx, "session-123", "session-456")
if err != nil {
    log.Fatal(err)
}
```

## In-Memory State Store

For development and testing.

```go
store := statestore.NewMemoryStore()

// Same interface as Redis store
state := &statestore.ConversationState{
    ID:       "session-1",
    Messages: messages,
}
store.Save(ctx, state)
loaded, _ := store.Load(ctx, "session-1")
```

## Usage with Pipeline

### With Pipeline

```go
import "github.com/AltairaLabs/PromptKit/runtime/statestore"

// Create state store
store := statestore.NewRedisStore(redisClient)

// Load state, execute, and save state
state, _ := store.Load(ctx, sessionID)
// ... execute pipeline with state.Messages ...
state.Messages = append(state.Messages, newMessages...)
store.Save(ctx, state)
```

### Manual State Management

```go
// Load existing conversation
state, _ := store.Load(ctx, sessionID)

// Add new message
state.Messages = append(state.Messages, types.Message{
    Role:    "user",
    Content: "New message",
})

// Save updated state
store.Save(ctx, state)
```

## Configuration

### Redis Configuration

```go
redisClient := redis.NewClient(&redis.Options{
    Addr:         "localhost:6379",
    Password:     "",        // No password
    DB:           0,         // Default DB
    DialTimeout:  5 * time.Second,
    ReadTimeout:  3 * time.Second,
    WriteTimeout: 3 * time.Second,
    PoolSize:     10,
    MinIdleConns: 5,
})

store := statestore.NewRedisStore(redisClient)
```

### TTL Management

```go
// Set expiration on session keys via Redis client configuration
// or use RedisStoreOption when creating the store
```

## Custom State Store

### Implementation

```go
type CustomStateStore struct {
    backend Database
}

func (s *CustomStateStore) Load(
    ctx context.Context,
    id string,
) (*statestore.ConversationState, error) {
    data, err := s.backend.Get(ctx, id)
    if err != nil {
        return nil, err
    }

    var state statestore.ConversationState
    err = json.Unmarshal(data, &state)
    return &state, err
}

func (s *CustomStateStore) Save(
    ctx context.Context,
    state *statestore.ConversationState,
) error {
    data, _ := json.Marshal(state)
    return s.backend.Set(ctx, state.ID, data)
}

func (s *CustomStateStore) Fork(
    ctx context.Context,
    sourceID string,
    newID string,
) error {
    state, err := s.Load(ctx, sourceID)
    if err != nil {
        return err
    }
    state.ID = newID
    return s.Save(ctx, state)
}
```

## Best Practices

### 1. Session Management

```go
// Use meaningful session IDs
sessionID := fmt.Sprintf("user-%s-%d", userID, time.Now().Unix())

// Fork a session for branching conversations
store.Fork(ctx, sessionID, newSessionID)
```

### 2. Error Handling

```go
state, err := store.Load(ctx, sessionID)
if err != nil {
    // Start new conversation on any load error
    state = &statestore.ConversationState{
        ID:       sessionID,
        Messages: []types.Message{},
    }
}
```

### 3. Message Truncation

```go
// Limit conversation history to prevent memory issues
state, _ := store.Load(ctx, sessionID)
maxMessages := 50
if len(state.Messages) > maxMessages {
    state.Messages = state.Messages[len(state.Messages)-maxMessages:]
}

store.Save(ctx, state)
```

### 4. Concurrent Access

```go
// Use session locking for concurrent access
lock := acquireLock(sessionID)
defer lock.Release()

state, _ := store.Load(ctx, sessionID)
// ... modify state.Messages ...
store.Save(ctx, state)
```

## Performance Considerations

### Latency

- **Redis**: 1-5ms per operation (network dependent)
- **In-memory**: <1ms per operation

### Throughput

- **Redis**: 10,000+ ops/sec (single instance)
- **In-memory**: 100,000+ ops/sec

### Storage

- Average conversation: 1-10 KB
- 1M conversations: 1-10 GB storage
- Implement TTL to manage storage growth

## See Also

- [Pipeline Reference](pipeline) - Using state stores in pipelines
- [State Store How-To](../how-to/manage-state) - State management patterns
- [State Store Tutorial](../tutorials/05-stateful-conversations) - Building stateful apps
