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
    Load(ctx context.Context, sessionID string) ([]types.Message, error)
    Save(ctx context.Context, sessionID string, messages []types.Message) error
    Fork(ctx context.Context, sourceSessionID string, newSessionID string) error
}
```

## Redis State Store

### Constructor

```go
func NewRedisStore(client *redis.Client, opts ...RedisStoreOption) *RedisStore
```

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
defer store.Close()
```

### Methods

**Save**:
```go
messages := []types.Message{
    {Role: "user", Content: "Hello"},
    {Role: "assistant", Content: "Hi there!"},
}

err := store.Save(ctx, "session-123", messages)
if err != nil {
    log.Fatal(err)
}
```

**Load**:
```go
messages, err := store.Load(ctx, "session-123")
if err != nil {
    log.Fatal(err)
}

for _, msg := range messages {
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
store.Save(ctx, "session-1", messages)
messages, _ := store.Load(ctx, "session-1")
```

## Usage with Pipeline

### With Pipeline

```go
import "github.com/AltairaLabs/PromptKit/runtime/statestore"

// Create state store
store := statestore.NewRedisStore(redisClient)

// Load state, execute, and save state
messages, _ := store.Load(ctx, sessionID)
// ... execute pipeline with messages ...
store.Save(ctx, sessionID, updatedMessages)
```

### Manual State Management

```go
// Load existing conversation
messages, _ := store.Load(ctx, sessionID)

// Execute with loaded context
execCtx := &pipeline.ExecutionContext{
    Messages: messages,
}

// Save updated state
execCtx.Messages = append(execCtx.Messages, types.Message{
    Role:    "user",
    Content: "New message",
})

store.Save(ctx, sessionID, execCtx.Messages)
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
    sessionID string,
) ([]types.Message, error) {
    data, err := s.backend.Get(ctx, sessionID)
    if err != nil {
        return nil, err
    }

    var messages []types.Message
    err = json.Unmarshal(data, &messages)
    return messages, err
}

func (s *CustomStateStore) Save(
    ctx context.Context,
    sessionID string,
    messages []types.Message,
) error {
    data, _ := json.Marshal(messages)
    return s.backend.Set(ctx, sessionID, data)
}

func (s *CustomStateStore) Fork(
    ctx context.Context,
    sourceSessionID string,
    newSessionID string,
) error {
    messages, err := s.Load(ctx, sourceSessionID)
    if err != nil {
        return err
    }
    return s.Save(ctx, newSessionID, messages)
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
messages, err := store.Load(ctx, sessionID)
if err != nil {
    // Start new conversation on any load error
    messages = []types.Message{}
}
```

### 3. Message Truncation

```go
// Limit conversation history to prevent memory issues
maxMessages := 50
if len(messages) > maxMessages {
    messages = messages[len(messages)-maxMessages:]
}

store.Save(ctx, sessionID, messages)
```

### 4. Concurrent Access

```go
// Use session locking for concurrent access
lock := acquireLock(sessionID)
defer lock.Release()

messages, _ := store.Load(ctx, sessionID)
// ... modify messages ...
store.Save(ctx, sessionID, messages)
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
