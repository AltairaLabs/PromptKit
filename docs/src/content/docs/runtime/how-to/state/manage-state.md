---
title: Manage state
description: Persist conversation history across pipeline executions with a state store.
sidebar:
  order: 4
verified:
  commit: 8489bf3d95c7498f9de0f024769db9399c24e884
  sources:
    - runtime/pipeline/stage/stages_core.go
    - runtime/pipeline/stage/stages_provider.go
    - runtime/pipeline/stage/stages_save.go
    - runtime/pipeline/types.go
    - runtime/statestore/interface.go
    - runtime/statestore/memory.go
    - runtime/statestore/redis.go
    - runtime/statestore/types.go
---

Persist conversation history so each turn sees the earlier ones, across executions and processes.

## Prerequisites

- A provider. See [Set up providers](/runtime/how-to/providers/setup-providers/).
- A pipeline built with `stage.NewPipelineBuilder`. See [Configure a pipeline](/runtime/how-to/pipeline/configure-pipeline/).

## 1. Create a state store

In-memory, for development, tests and single-instance servers:

```go
store := statestore.NewMemoryStore()
defer store.Close()
```

A memory store expires a conversation after one hour without use and holds at most 10,000 conversations, evicting the least recently used. Change the limits with options:

```go
store := statestore.NewMemoryStore(
	statestore.WithMemoryTTL(2*time.Hour),
	statestore.WithMemoryMaxEntries(1000),
)
```

`statestore.WithNoTTL()` and `statestore.WithNoMaxEntries()` remove a limit.

Redis, for production and multiple instances:

```go
import "github.com/redis/go-redis/v9"

client := redis.NewClient(&redis.Options{
	Addr:     os.Getenv("REDIS_HOST"),
	Password: os.Getenv("REDIS_PASSWORD"),
})

store := statestore.NewRedisStore(client,
	statestore.WithTTL(24*time.Hour), // default 24 hours; 0 disables expiry
	statestore.WithPrefix("myapp"),   // default "promptkit"
)
```

For both stores the TTL slides: reading or writing a conversation extends it.

## 2. Wire the store into the pipeline

Put the load stage first and the save stage last. The conversation ID selects the history to load and save.

```go
cfg := &pipeline.StateStoreConfig{
	Store:          store,
	ConversationID: "user-123-support",
	UserID:         "user-123", // optional
}

pipe, err := stage.NewPipelineBuilder().
	Chain(
		stage.NewStateStoreLoadStage(cfg),
		stage.NewProviderStage(provider, nil, nil, nil),
		stage.NewIncrementalSaveStage(&stage.IncrementalSaveConfig{StateStoreConfig: cfg}),
	).
	Build()
if err != nil {
	log.Fatal(err)
}
```

Each `ExecuteSync` loads the stored messages, sends them with the new message, and appends the new messages to the store:

```go
first := types.Message{Role: "user"}
first.AddTextPart("Hi, I'm Alice")
if _, err := pipe.ExecuteSync(ctx, stage.NewMessageElement(&first)); err != nil {
	log.Fatal(err)
}

second := types.Message{Role: "user"}
second.AddTextPart("What's my name?")
result, err := pipe.ExecuteSync(ctx, stage.NewMessageElement(&second))
```

The second call sends the first exchange to the model as history.

## 3. Read the stored state

```go
state, err := store.Load(ctx, "user-123-support")
if errors.Is(err, statestore.ErrNotFound) {
	// No such conversation, or it expired.
} else if err != nil {
	return err
}
for _, msg := range state.Messages {
	log.Printf("%s: %s", msg.Role, msg.GetContent())
}
```

The load stage treats `ErrNotFound` as an empty history. Any other load error fails the turn.

## 4. Branch a conversation

`Fork` copies a conversation to a new ID and leaves the original unchanged. It returns `ErrNotFound` when the source does not exist.

```go
err := store.Fork(ctx, "user-123-support", "user-123-new-topic")
```

## 5. Trim history

`Store` only reads and forks. Replacing a conversation needs the optional `BulkWriter` interface, which both built-in stores implement:

```go
func trim(ctx context.Context, store statestore.Store, id string, keep int) error {
	state, err := store.Load(ctx, id)
	if err != nil {
		return err
	}
	if len(state.Messages) <= keep {
		return nil
	}
	state.Messages = state.Messages[len(state.Messages)-keep:]
	writer, ok := store.(statestore.BulkWriter)
	if !ok {
		return errors.New("store does not support replacing state")
	}
	return writer.Save(ctx, state)
}
```

Run `trim` between turns, not during one.

## Verify

After two turns, `store.Load` returns four messages: user, assistant, user, assistant. A `Load` for an ID you never used returns `statestore.ErrNotFound`. With Redis, `redis-cli keys 'myapp:*'` lists the conversation's keys.

## Troubleshoot

History does not carry over: use the same `ConversationID` for every turn, and check that the load stage comes before the provider stage and the save stage after it.

History disappears: the conversation expired. Raise the TTL (`WithMemoryTTL` or `WithTTL`) or disable it.

The memory store grows too large: lower `WithMemoryMaxEntries`, trim long conversations, or move to Redis.

Redis connection fails: confirm the server answers `redis-cli ping` and that `Addr` and `Password` are correct. A failed connection fails the turn at the load stage.

## Related

- [Handle errors](/runtime/how-to/pipeline/handle-errors/)
- [Monitor costs](/runtime/how-to/observability/monitor-costs/)
- [Configure a pipeline](/runtime/how-to/pipeline/configure-pipeline/)
- [State store reference](/runtime/reference/statestore/)
- [Pipeline reference](/runtime/reference/pipeline/)
