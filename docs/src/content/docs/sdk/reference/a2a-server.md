---
title: A2A Server Reference
description: API reference for the SDK A2A server, task store, and conversation opener
sidebar:
  order: 7
---

API reference for the A2A server components in the `sdk` package: `A2AServer`, `A2ATaskStore`, `A2AConversationOpener`, and options.

---

## A2AServer

```go
import "github.com/AltairaLabs/PromptKit/sdk"
```

### NewA2AServer

```go
func NewA2AServer(opener A2AConversationOpener, opts ...A2AServerOption) *A2AServer
```

Creates a new A2A server. The `opener` function creates a conversation for each context ID.

### A2AServer Methods

| Method | Signature | Description |
|--------|-----------|-------------|
| `ListenAndServe` | `() error` | Starts the HTTP server on the configured port. |
| `Serve` | `(ln net.Listener) error` | Starts the server on the given listener. |
| `Shutdown` | `(ctx context.Context) error` | Gracefully shuts down: drains HTTP, cancels tasks, closes conversations. |
| `Handler` | `() http.Handler` | Returns the `http.Handler` for embedding in an existing server. |

### A2AServer Options

| Option | Description |
|--------|-------------|
| `WithA2ACard(card *a2a.AgentCard)` | Sets the agent card served at `/.well-known/agent.json`. |
| `WithA2APort(port int)` | Sets the TCP port for `ListenAndServe`. |
| `WithA2ATaskStore(store A2ATaskStore)` | Sets a custom task store. Defaults to in-memory. |

### HTTP Routes

| Route | Description |
|-------|-------------|
| `GET /.well-known/agent.json` | Returns the agent card as JSON. |
| `POST /a2a` | JSON-RPC 2.0 dispatch for all A2A methods. |

---

## A2AConversationOpener

```go
type A2AConversationOpener func(contextID string) (a2aConv, error)
```

Factory function passed to `NewA2AServer`. Called once per unique context ID. The returned conversation must support `Send()` and `Close()`. If it also supports `Stream()`, the server enables SSE streaming for `message/stream` requests.

### A2AOpener

```go
func A2AOpener(packPath, promptName string, opts ...Option) A2AConversationOpener
```

Creates an `A2AConversationOpener` that opens SDK conversations from a pack file:

```go
opener := sdk.A2AOpener("./assistant.pack.json", "chat")
server := sdk.NewA2AServer(opener, sdk.WithA2ACard(&card))
```

Each call to the opener creates a new `*Conversation` via `sdk.Open()`.

---

## A2ATaskStore

```go
type A2ATaskStore interface {
    Create(taskID, contextID string) (*a2a.Task, error)
    Get(taskID string) (*a2a.Task, error)
    SetState(taskID string, state a2a.TaskState, msg *a2a.Message) error
    AddArtifacts(taskID string, artifacts []a2a.Artifact) error
    Cancel(taskID string) error
    List(contextID string, limit, offset int) ([]*a2a.Task, error)
}
```

Interface for task persistence. Implement this for custom storage backends (database, Redis, etc.).

### InMemoryA2ATaskStore

```go
func NewInMemoryA2ATaskStore() *InMemoryA2ATaskStore
```

The default task store. Concurrency-safe but not persistent across restarts.

### Task Store Errors

```go
var (
    ErrTaskNotFound     = errors.New("a2a: task not found")
    ErrTaskAlreadyExists = errors.New("a2a: task already exists")
    ErrInvalidTransition = errors.New("a2a: invalid state transition")
    ErrTaskTerminal     = errors.New("a2a: task in terminal state")
)
```

### State Transition Rules

The task store enforces the A2A task state machine:

| From | Valid Transitions |
|------|-------------------|
| `submitted` | `working` |
| `working` | `completed`, `failed`, `canceled`, `input_required`, `auth_required`, `rejected` |
| `input_required` | `working`, `canceled` |
| `auth_required` | `working`, `canceled` |
| `completed` | *(terminal)* |
| `failed` | *(terminal)* |
| `canceled` | *(terminal)* |
| `rejected` | *(terminal)* |

---

## See Also

- [A2A Server Tutorial](/sdk/tutorials/10-a2a-server/) — build an A2A server step by step
- [A2A Concept](/concepts/a2a/) — protocol design and concepts
- [Runtime A2A Reference](/runtime/reference/a2a/) — client, types, bridge, mock API
