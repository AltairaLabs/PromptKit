---
title: State Store
docType: reference
order: 6
---
# State Store Reference

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
- **Custom**: Implement `StateStore` interface

## Core Interface

```go
type StateStore interface {
    Save(ctx context.Context, sessionID string, messages []types.Message) error
    Load(ctx context.Context, sessionID string) ([]types.Message, error)
    Delete(ctx context.Context, sessionID string) error
    List(ctx context.Context) ([]string, error)
}
```

## Redis State Store

### Constructor

```go
func NewRedisStateStore(client *redis.Client) *RedisStateStore
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
store := statestore.NewRedisStateStore(redisClient)
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

**Delete**:
```go
err := store.Delete(ctx, "session-123")
if err != nil {
    log.Fatal(err)
}
```

**List**:
```go
sessionIDs, err := store.List(ctx)
if err != nil {
    log.Fatal(err)
}

fmt.Printf("Active sessions: %v\n", sessionIDs)
```

## In-Memory State Store

For development and testing.

```go
store := statestore.NewInMemoryStateStore()

// Same interface as Redis store
store.Save(ctx, "session-1", messages)
messages, _ := store.Load(ctx, "session-1")
```

## Usage with Pipeline

### State Middleware

```go
import (
    "github.com/AltairaLabs/PromptKit/runtime/pipeline/middleware"
    "github.com/AltairaLabs/PromptKit/runtime/statestore"
)

// Create state store
store := statestore.NewRedisStateStore(redisClient)

// Add to pipeline
pipe := pipeline.NewPipeline(
    middleware.StateStoreMiddleware(store, "session-123"),
    middleware.ProviderMiddleware(provider, nil, nil, config),
)

// State automatically saved after each execution
result, err := pipe.Execute(ctx, "user", "Hello")
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

store := statestore.NewRedisStateStore(redisClient)
```

### TTL Management

```go
// Set expiration on session keys
err := store.SaveWithTTL(ctx, sessionID, messages, 24*time.Hour)
```

## Custom State Store

### Implementation

```go
type CustomStateStore struct {
    backend Database
}

func (s *CustomStateStore) Save(
    ctx context.Context,
    sessionID string,
    messages []types.Message,
) error {
    data, _ := json.Marshal(messages)
    return s.backend.Set(ctx, sessionID, data)
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

func (s *CustomStateStore) Delete(
    ctx context.Context,
    sessionID string,
) error {
    return s.backend.Delete(ctx, sessionID)
}

func (s *CustomStateStore) List(
    ctx context.Context,
) ([]string, error) {
    return s.backend.ListKeys(ctx, "session:*")
}
```

## Best Practices

### 1. Session Management

```go
// Use meaningful session IDs
sessionID := fmt.Sprintf("user-%s-%d", userID, time.Now().Unix())

// Clean up old sessions
for _, sessionID := range oldSessions {
    store.Delete(ctx, sessionID)
}
```

### 2. Error Handling

```go
messages, err := store.Load(ctx, sessionID)
if err != nil {
    if err == statestore.ErrSessionNotFound {
        // Start new conversation
        messages = []types.Message{}
    } else {
        return err
    }
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
