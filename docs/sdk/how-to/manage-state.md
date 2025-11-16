---
layout: default
title: Manage Conversation State
nav_order: 7
parent: SDK How-To Guides
grand_parent: SDK
---

# How to Manage Conversation State

Persist and restore conversation history and context across sessions.

## Basic State Management

### Step 1: Create State Store

```go
import "github.com/AltairaLabs/PromptKit/runtime/statestore"

store := statestore.NewMemoryStore()
```

### Step 2: Configure Manager

```go
manager, _ := sdk.NewConversationManager(
    sdk.WithProvider(provider),
    sdk.WithStateStore(store),
)
```

### Step 3: Create Persistent Conversation

```go
conv, _ := manager.NewConversation(ctx, pack, sdk.ConversationConfig{
    UserID:         "user123",
    ConversationID: "conv-456",  // Reuse this ID
    PromptName:     "assistant",
})
```

## Complete Example

```go
package main

import (
    "context"
    "fmt"

    "github.com/AltairaLabs/PromptKit/sdk"
    "github.com/AltairaLabs/PromptKit/runtime/providers"
    "github.com/AltairaLabs/PromptKit/runtime/statestore"
)

func main() {
    ctx := context.Background()

    // 1. Create persistent state store
    store := statestore.NewMemoryStore()

    // 2. Create manager with state
    provider := providers.NewOpenAIProvider(apiKey, "gpt-4o", false)
    manager, _ := sdk.NewConversationManager(
        sdk.WithProvider(provider),
        sdk.WithStateStore(store),
    )

    pack, _ := manager.LoadPack("./assistant.pack.json")

    // 3. First session
    conv1, _ := manager.NewConversation(ctx, pack, sdk.ConversationConfig{
        UserID:         "user123",
        ConversationID: "persistent-conv",
        PromptName:     "assistant",
    })

    resp1, _ := conv1.Send(ctx, "My name is Alice")
    fmt.Println(resp1.Content)  // "Nice to meet you, Alice!"

    // 4. Later session - same conversation ID
    conv2, _ := manager.GetConversation("persistent-conv")
    resp2, _ := conv2.Send(ctx, "What's my name?")
    fmt.Println(resp2.Content)  // "Your name is Alice"
}
```

## State Store Types

### Memory Store (Default)

In-memory storage, lost on restart:

```go
store := statestore.NewMemoryStore()
```

### Redis Store

Distributed storage across instances:

```go
import "github.com/AltairaLabs/PromptKit/runtime/statestore/redis"

store, err := redis.NewRedisStore(redis.Config{
    Address:  "localhost:6379",
    Password: redisPassword,
    DB:       0,
    Prefix:   "promptkit:",
})
```

### PostgreSQL Store

Database-backed persistence:

```go
import "github.com/AltairaLabs/PromptKit/runtime/statestore/postgres"

store, err := postgres.NewPostgresStore(postgres.Config{
    ConnectionString: "postgresql://user:pass@localhost/db",
    TableName:        "conversation_state",
})
```

### Custom Store

Implement the StateStore interface:

```go
type CustomStore struct {
    // Your storage backend
}

func (s *CustomStore) Get(ctx context.Context, key string) (*statestore.State, error) {
    // Retrieve state
}

func (s *CustomStore) Set(ctx context.Context, key string, state *statestore.State) error {
    // Store state
}

func (s *CustomStore) Delete(ctx context.Context, key string) error {
    // Remove state
}
```

## State Operations

### Save State

State is automatically saved after each message:

```go
resp, err := conv.Send(ctx, "Hello")
// State automatically persisted
```

### Manual State Access

```go
// Get current state
state, err := store.Get(ctx, conversationID)

// Inspect state
fmt.Printf("Messages: %d\n", len(state.Messages))
fmt.Printf("Tokens: %d\n", state.TokenCount)

// Manually update state
state.Metadata["key"] = "value"
err = store.Set(ctx, conversationID, state)
```

### Clear State

```go
// Delete specific conversation
err := store.Delete(ctx, conversationID)

// Clear all state for user
prefix := fmt.Sprintf("user:%s:", userID)
err = store.DeleteByPrefix(ctx, prefix)
```

## State Structure

### State Object

```go
type State struct {
    ConversationID string
    UserID         string
    Messages       []Message
    TokenCount     int
    TotalCost      float64
    Metadata       map[string]interface{}
    CreatedAt      time.Time
    UpdatedAt      time.Time
}
```

### Message History

```go
// Access message history
state, _ := store.Get(ctx, conversationID)
for _, msg := range state.Messages {
    fmt.Printf("%s: %s\n", msg.Role, msg.Content)
}
```

### Metadata

```go
// Store custom metadata
conv, _ := manager.NewConversation(ctx, pack, sdk.ConversationConfig{
    UserID:         "user123",
    ConversationID: "conv-456",
    PromptName:     "assistant",
    Metadata: map[string]interface{}{
        "session_id": "sess-789",
        "tags":       []string{"support", "billing"},
    },
})

// Retrieve metadata
state, _ := store.Get(ctx, "conv-456")
sessionID := state.Metadata["session_id"].(string)
```

## Context Window Management

### Automatic Truncation

Configure token limits:

```go
conv, _ := manager.NewConversation(ctx, pack, sdk.ConversationConfig{
    UserID:         "user123",
    ConversationID: "conv-456",
    PromptName:     "assistant",
    MaxTokens:      4000,  // Truncate at 4k tokens
})
```

### Manual Truncation

```go
// Keep last N messages
state, _ := store.Get(ctx, conversationID)
if len(state.Messages) > 20 {
    state.Messages = state.Messages[len(state.Messages)-20:]
    store.Set(ctx, conversationID, state)
}

// Keep messages within time window
cutoff := time.Now().Add(-24 * time.Hour)
var recentMessages []Message
for _, msg := range state.Messages {
    if msg.Timestamp.After(cutoff) {
        recentMessages = append(recentMessages, msg)
    }
}
state.Messages = recentMessages
```

### Summarization

```go
// Summarize old messages
if state.TokenCount > 8000 {
    // Create summary of old messages
    oldMessages := state.Messages[:len(state.Messages)-10]
    summary := summarizeMessages(ctx, conv, oldMessages)

    // Replace old messages with summary
    state.Messages = []Message{
        {Role: "system", Content: summary},
    }
    state.Messages = append(state.Messages, state.Messages[len(state.Messages)-10:]...)
    
    store.Set(ctx, conversationID, state)
}
```

## Multi-User State Management

### User Isolation

```go
// Use user-specific conversation IDs
conversationID := fmt.Sprintf("user:%s:conv:%s", userID, sessionID)

conv, _ := manager.NewConversation(ctx, pack, sdk.ConversationConfig{
    UserID:         userID,
    ConversationID: conversationID,
    PromptName:     "assistant",
})
```

### List User Conversations

```go
// Get all conversations for a user
prefix := fmt.Sprintf("user:%s:", userID)
conversations, err := store.ListByPrefix(ctx, prefix)

for _, conv := range conversations {
    fmt.Printf("Conversation: %s (%d messages)\n",
        conv.ConversationID,
        len(conv.Messages))
}
```

## State Expiration

### TTL Configuration (Redis)

```go
store, _ := redis.NewRedisStore(redis.Config{
    Address: "localhost:6379",
    TTL:     24 * time.Hour,  // Auto-expire after 24h
})
```

### Manual Cleanup

```go
// Clean up old conversations
cutoff := time.Now().Add(-7 * 24 * time.Hour)

conversations, _ := store.ListAll(ctx)
for _, conv := range conversations {
    if conv.UpdatedAt.Before(cutoff) {
        store.Delete(ctx, conv.ConversationID)
    }
}
```

## Backup and Restore

### Export State

```go
// Export to JSON
state, _ := store.Get(ctx, conversationID)
data, _ := json.MarshalIndent(state, "", "  ")
os.WriteFile("backup.json", data, 0644)
```

### Import State

```go
// Import from JSON
data, _ := os.ReadFile("backup.json")
var state statestore.State
json.Unmarshal(data, &state)
store.Set(ctx, state.ConversationID, &state)
```

### Bulk Export

```go
// Export all conversations
conversations, _ := store.ListAll(ctx)
for _, conv := range conversations {
    filename := fmt.Sprintf("backup/%s.json", conv.ConversationID)
    data, _ := json.MarshalIndent(conv, "", "  ")
    os.WriteFile(filename, data, 0644)
}
```

## State Migration

### Version Compatibility

```go
type StateV1 struct {
    ConversationID string
    Messages       []Message
}

type StateV2 struct {
    ConversationID string
    UserID         string
    Messages       []Message
    TokenCount     int
}

func MigrateV1ToV2(v1 *StateV1) *StateV2 {
    return &StateV2{
        ConversationID: v1.ConversationID,
        Messages:       v1.Messages,
        // Set defaults for new fields
        UserID:     "unknown",
        TokenCount: calculateTokens(v1.Messages),
    }
}
```

## Error Handling

### Handle Missing State

```go
state, err := store.Get(ctx, conversationID)
if errors.Is(err, statestore.ErrNotFound) {
    // Start new conversation
    conv, _ := manager.NewConversation(ctx, pack, config)
} else if err != nil {
    return fmt.Errorf("state error: %w", err)
}
```

### Retry Failed Saves

```go
var lastErr error
for i := 0; i < 3; i++ {
    if err := store.Set(ctx, conversationID, state); err == nil {
        break
    } else {
        lastErr = err
        time.Sleep(time.Second * time.Duration(i+1))
    }
}
if lastErr != nil {
    return fmt.Errorf("failed to save state: %w", lastErr)
}
```

## Best Practices

### State Keys

Use hierarchical keys:

```go
// Good: structured, queryable
key := fmt.Sprintf("org:%s:user:%s:conv:%s", orgID, userID, convID)

// Bad: flat, hard to query
key := conversationID
```

### Optimize Storage

```go
// Don't store everything
type LeanState struct {
    Messages   []Message              // Required
    TokenCount int                    // Useful
    // Omit: full response objects, debug info
}
```

### Monitor State Size

```go
state, _ := store.Get(ctx, conversationID)
if state.TokenCount > 100000 {
    log.Printf("Warning: large conversation state: %d tokens", state.TokenCount)
    // Consider summarization or archival
}
```

## Performance Optimization

### Caching

```go
type CachedStore struct {
    store statestore.StateStore
    cache *lru.Cache
}

func (s *CachedStore) Get(ctx context.Context, key string) (*statestore.State, error) {
    // Check cache first
    if val, ok := s.cache.Get(key); ok {
        return val.(*statestore.State), nil
    }

    // Load from store
    state, err := s.store.Get(ctx, key)
    if err == nil {
        s.cache.Add(key, state)
    }
    return state, err
}
```

### Batch Operations

```go
// Load multiple conversations
conversationIDs := []string{"conv1", "conv2", "conv3"}
states := make([]*statestore.State, len(conversationIDs))

for i, id := range conversationIDs {
    states[i], _ = store.Get(ctx, id)
}
```

## Testing

### Mock State Store

```go
type MockStore struct {
    states map[string]*statestore.State
}

func (m *MockStore) Get(ctx context.Context, key string) (*statestore.State, error) {
    if state, ok := m.states[key]; ok {
        return state, nil
    }
    return nil, statestore.ErrNotFound
}

func TestWithMockStore(t *testing.T) {
    mock := &MockStore{states: make(map[string]*statestore.State)}
    manager, _ := sdk.NewConversationManager(
        sdk.WithProvider(mockProvider),
        sdk.WithStateStore(mock),
    )

    // Test state operations
}
```

## Troubleshooting

### State Not Persisting

Check:
1. StateStore is configured
2. ConversationID is provided
3. No errors from store operations

```go
// Enable logging
if err := store.Set(ctx, key, state); err != nil {
    log.Printf("State save failed: %v", err)
}
```

### State Conflicts

Handle concurrent updates:

```go
// Optimistic locking
state, _ := store.Get(ctx, conversationID)
state.Version++
if err := store.Set(ctx, conversationID, state); err != nil {
    // Handle conflict
}
```

## Next Steps

- **[Create Conversations](create-conversations.md)** - Conversation setup
- **[Tutorial: Persistent Chat](../tutorials/02-persistent-chat.md)** - Complete guide
- **[StateStore Reference](../reference/statestore.md)** - API details

## See Also

- [StateStore Package](https://pkg.go.dev/github.com/AltairaLabs/PromptKit/runtime/statestore)
- [Redis Store](https://pkg.go.dev/github.com/AltairaLabs/PromptKit/runtime/statestore/redis)
