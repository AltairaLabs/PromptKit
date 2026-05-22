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

- **Redis**: Production-ready distributed state.
- **File**: Single-machine durable state, no daemon required (see [File-Backed Store](#file-backed-store)).
- **In-memory**: Development and testing.
- **Custom**: Implement `Store` interface.

### Choosing a backend

| Backend  | Durable across restarts | Multi-process | Ops surface          |
|----------|-------------------------|---------------|----------------------|
| In-memory | No                      | No            | None                 |
| File     | Yes                      | No (single process per root) | Local disk only      |
| Redis    | Yes                      | Yes           | Redis server         |

File-backed state closes the gap between volatile memory and a network-attached Redis: solo developers, single-box deployments, and one-process-per-agent setups (e.g. Omnia function-mode AgentRuntimes) get crash-survivable mid-tool-loop state without standing up Redis. Two processes pointed at the same root will corrupt each other's data — use Redis for horizontally-scaled deployments.

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

## Optional Interfaces

The core `Store` interface covers basic load/save operations. For better performance with long conversations, stores can implement additional opt-in interfaces. Pipeline stages type-assert for these interfaces and fall back to `Load`/`Save` when they are not available.

### MessageReader

Enables loading only the tail of the conversation without deserializing the full state:

```go
type MessageReader interface {
    LoadRecentMessages(ctx context.Context, id string, n int) ([]types.Message, error)
    MessageCount(ctx context.Context, id string) (int, error)
}
```

Used by `ContextAssemblyStage` to load only the hot window of recent messages.

### MessageAppender

Enables appending new messages without the full load+replace+save cycle:

```go
type MessageAppender interface {
    AppendMessages(ctx context.Context, id string, messages []types.Message) error
}
```

Used by `IncrementalSaveStage` to append only new messages after each turn.

### MessageLog

Enables per-round write-through persistence during tool loops. Messages are appended after each LLM round, ensuring durability even if the pipeline is interrupted mid-loop:

```go
type MessageLog interface {
    LogAppend(ctx context.Context, id string, startSeq int, messages []types.Message) (int, error)
    LogLoad(ctx context.Context, id string, recent int) ([]types.Message, error)
    LogLen(ctx context.Context, id string) (int, error)
}
```

`LogAppend` uses sequence-based idempotent append: if `startSeq` is behind the current length, already-persisted messages are skipped. Used via `sdk.WithMessageLog()`.

### SummaryAccessor

Enables reading and writing summaries independently of the full conversation state:

```go
type SummaryAccessor interface {
    LoadSummaries(ctx context.Context, id string) ([]Summary, error)
    SaveSummary(ctx context.Context, id string, summary Summary) error
}
```

Used by auto-summarization to store and retrieve compressed conversation history.

### Summary

The `Summary` type represents a compressed version of conversation turns:

```go
type Summary struct {
    StartTurn  int       // First turn included in this summary
    EndTurn    int       // Last turn included in this summary
    Content    string    // Summarized content
    TokenCount int       // Token count of the summary
    CreatedAt  time.Time // When this summary was created
}
```

Summaries are stored in `ConversationState.Summaries` and prepended to the context as system messages during context assembly.

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

RedisStore implements all optional interfaces (`MessageReader`, `MessageAppender`, `SummaryAccessor`) using Redis Lists for O(1) append and tail reads, making it well-suited for long conversations.

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

## File-Backed Store

`statestore/file` persists each conversation to disk as a small `state.json` snapshot plus append-only JSONL files for messages, summaries, and `ListAccessor` lists. Single-machine durable, no daemon. Implements every interface (`Store`, `BulkWriter`, `MessageLog`, `MessageReader`, `MessageAppender`, `MetadataAccessor`, `SummaryAccessor`, `ListAccessor`).

### Constructor

```go
import "github.com/AltairaLabs/PromptKit/runtime/statestore/file"

store, err := file.NewStore(file.Options{
    Root:  "/var/lib/promptkit/conversations",
    FSync: file.FSyncOnSave,    // off | on-save (default) | on-append
    TTL:   30 * 24 * time.Hour, // optional; cleanup sweep at NewStore time
})
```

### Configuration via RuntimeConfig

```yaml
state_store:
  type: file
  file:
    root: /var/lib/promptkit/conversations
    fsync: on-save
    ttl_days: 30
```

### On-disk layout

```
<root>/
  conv-<id>/
    state.json
    messages.jsonl
    summaries.jsonl
    lists/
      <list-name>.jsonl
```

Files are plaintext — `cat`-friendly for debugging.

### When to use

- Solo developer / single-box deployments where Redis is overkill.
- Per-process agents (e.g. Omnia function-mode AgentRuntimes) on a node with a persistent volume.
- Anywhere mid-tool-loop crash recovery matters more than horizontal scale.

### When NOT to use

- Two PromptKit processes pointed at the same `Root` — undefined behaviour. Use Redis for horizontally-scaled state. A future revision may add OS-level file locking.

## In-Memory State Store

For development and testing. MemoryStore implements all optional interfaces (`MessageReader`, `MessageAppender`, `SummaryAccessor`).

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

Custom stores can optionally implement `MessageReader`, `MessageAppender`, and `SummaryAccessor` for better performance with long conversations. For example, to support incremental saves:

```go
func (s *CustomStateStore) AppendMessages(
    ctx context.Context,
    id string,
    messages []types.Message,
) error {
    return s.backend.ListAppend(ctx, id+":messages", messages)
}
```

Pipeline stages will automatically detect and use these interfaces when available.

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

- [Pipeline Reference](/runtime/reference/pipeline/) - Using state stores in pipelines
- [State Store How-To](/runtime/how-to/manage-state/) - State management patterns
- [State Store Tutorial](/runtime/tutorials/05-production-deployment/) - Building stateful apps
