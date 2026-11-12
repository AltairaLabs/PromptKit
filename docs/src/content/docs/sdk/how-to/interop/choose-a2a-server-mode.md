---
title: Choose an A2A Server Mode
description: Decide whether the A2A server owns conversations or your platform does, then serve, scope and scale it
sidebar:
  order: 8
verified:
  commit: 8489bf3d95c7498f9de0f024769db9399c24e884
  sources:
    - runtime/a2a/client.go
    - sdk/a2a.go
    - sdk/a2a_exports.go
    - server/a2a/conversation.go
    - server/a2a/handler.go
    - server/a2a/handler_send.go
    - server/a2a/ownership.go
    - server/a2a/protocol.go
    - server/a2a/server.go
    - server/a2a/server_stream.go
    - server/a2a/task_events.go
---

`server/a2a` speaks the A2A protocol. This guide picks the constructor that fits where your conversations live, then covers task storage, mounting, caller scoping and replicas.

## Prerequisites

- The `github.com/AltairaLabs/PromptKit/server/a2a/v2` module, imported as `a2aserver`.
- An agent card (`a2a.AgentCard`) describing your agent.
- For `NewServer`, a pack and prompt to open conversations from. For `NewStatelessServer`, something that can answer a message with a stream of events.

## Choose a mode

| | `NewServer` | `NewStatelessServer` |
|---|---|---|
| You supply | a `ConversationOpener` | a `MessageHandler` |
| The server | opens a conversation per `contextID`, caches it, reuses it, expires it on a TTL | keeps nothing between calls |
| Your code sees | the `contextID` | the `contextID` and the HTTP request's context |

Both serve the same protocol: JSON-RPC methods, task lifecycle, SSE streaming and agent card.

Choose `NewStatelessServer` if any of these is true:

- your runtime runs in a different process or service;
- you already have sessions, with their own storage and expiry;
- your handler needs to know who is calling;
- you want several replicas behind a load balancer without sharing conversation state;
- callers do not trust each other and you want to decide what a shared `contextID` means.

Otherwise `NewServer` is less to write.

## Serve conversations with `NewServer`

1. Build an opener and pass it with the card and port:

   ```go
   opener := sdk.A2AOpener("./assistant.pack.json", "chat")

   srv := a2aserver.NewServer(opener,
       a2aserver.WithCard(&card),
       a2aserver.WithPort(9999),
   )
   ```

   The server caches each conversation under the `contextID` the caller sends. Two callers presenting the same `contextID` share a conversation unless you scope tasks by caller with `WithTaskOwner` (see [Callers and replicas](#callers-and-replicas)).

   `sdk.NewA2AServer` wraps this constructor, and the `sdk.WithA2A*` options wrap the card, port, task store, timeout, body-size and TTL options. Pass options that have no wrapper, such as `a2aserver.WithTaskOwner`, to `sdk.NewA2AServer` directly. The [server reference](/sdk/reference/a2a-server/) lists every option, and the [A2A server tutorial](/sdk/tutorials/10-a2a-server/) builds one end to end.

2. Optionally keep tasks in your own store. Tasks live in memory by default. Implement `a2aserver.TaskStore` (`sdk.A2ATaskStore`) and pass it in:

   ```go
   srv := a2aserver.NewServer(opener,
       a2aserver.WithCard(&card),
       a2aserver.WithPort(9999),
       a2aserver.WithTaskStore(myStore),
   )
   ```

3. Start it with `srv.ListenAndServe()`, or mount it on an existing HTTP server. `Handler()` returns an `http.Handler` serving the agent card, `POST /a2a`, `/healthz` and `/readyz`. Mount it at the root of your mux:

   ```go
   srv := a2aserver.NewServer(opener, a2aserver.WithCard(&card))

   mux := http.NewServeMux()
   mux.Handle("/", srv.Handler())

   log.Fatal(http.ListenAndServe(":8080", mux))
   ```

   The card is then at `http://localhost:8080/.well-known/agent-card.json`. A card that declares no interfaces points callers at `/a2a` on that host.

## Serve through your own handler with `NewStatelessServer`

1. Implement `MessageHandler`. Every message reaches `Handle` with the context of the request it arrived on, so caller identity, tenant and trace are readable there. Map `contextID` to whatever a session means in your system:

   ```go
   type runtimeHandler struct{ client *grpcRuntimeClient }

   func (h *runtimeHandler) Handle(
       ctx context.Context, req a2aserver.MessageRequest,
   ) <-chan a2aserver.StreamEvent {
       identity := auth.IdentityFromContext(ctx) // your middleware put it there
       return h.client.Converse(ctx, h.sessionFor(req.ContextID, identity), req.Message)
   }
   ```

2. Pass the handler to the constructor. The server holds no conversation, so any replica can serve any message as long as the task store is shared:

   ```go
   srv := a2aserver.NewStatelessServer(&runtimeHandler{client: c},
       a2aserver.WithTaskStore(redisStore),
       a2aserver.WithCardProvider(cards),
   )
   ```

3. Answer with events. Return a channel and close it when the turn is over:

   | Event | Meaning |
   |---|---|
   | `EventText` | a chunk of the reply |
   | `EventMedia` | a media part of the reply |
   | `EventClientTool` | you need the caller to run a tool; the task goes to `input_required` |
   | `EventPending` | you are waiting on something the server cannot see, such as a human approval; the task goes to `input_required` |
   | `EventDone` | the turn is complete |
   | `Error` set | the turn failed |

   For `message/send`, the server drains your stream and builds the result. The text is the concatenation of your text events, and the parts are one per text run and one per media event, text first. To get a distinct part, emit a distinct event.

4. If callers run client tools, also implement `ToolResultHandler`, and their results come back to you:

   ```go
   func (h *runtimeHandler) HandleToolResult(
       ctx context.Context, req a2aserver.ToolResultRequest,
   ) <-chan a2aserver.StreamEvent {
       identity := auth.IdentityFromContext(ctx)
       return h.client.Resume(ctx, h.sessionFor(req.ContextID, identity), req.Results)
   }
   ```

   A stateless server whose handler does not implement it refuses tool-result messages.

## Callers and replicas

Both modes take the same options for serving more than one caller and running more than one instance.

### Scope tasks to the caller that created them

```go
srv := a2aserver.NewStatelessServer(handler,
    a2aserver.WithTaskStore(store),
    a2aserver.WithTaskOwner(func(r *http.Request) string {
        return auth.IdentityFromContext(r.Context()).Subject
    }),
)
```

With `WithTaskOwner`, every task records the caller that created it. For anyone else, `GetTask`, `CancelTask` and `SubscribeToTask` answer `TaskNotFound`, so the task's existence is not revealed. `ListTasks` returns only the caller's tasks, and works without a `contextId`. A request whose owner is empty is refused with 401.

Without `WithTaskOwner`, the server cannot tell callers apart, so `ListTasks` requires a `contextId`.

With `NewServer`, a conversation belongs to the caller that opened it, and another caller naming its `contextId` is refused. That holds after the conversation leaves the cache, for as long as the caller's tasks in that context remain. With `NewStatelessServer`, your handler owns contexts and decides what a shared one means. An opener that restores conversation state from a store shared across replicas should key that state on the caller as well as the context.

The task store must implement `OwnedTaskStore` (`CreateOwned`, `Owner`). The in-memory store does. A store that also implements `TaskQuerier` must filter by `TaskQuery.Owner`. Both constructors panic if `WithTaskOwner` is given a store that cannot record owners.

### Run several replicas

A task's turn runs on the instance that received its message. Two operations have to reach that instance:

- `CancelTask` marks the task canceled in the shared store, then stops the running turn through a `TaskCanceler`.
- `SubscribeToTask` receives the turn's updates through a `TaskEventBus`.

Both default to in-process. For several replicas behind one task store, either pin each caller's requests to one replica (session affinity), or supply shared implementations such as Redis pub/sub or NATS:

```go
srv := a2aserver.NewServer(opener,
    a2aserver.WithTaskStore(redisStore),
    a2aserver.WithTaskEventBus(redisBus),     // Publish / Subscribe
    a2aserver.WithTaskCanceler(redisCancels), // Cancel / Listen
)
```

A `TaskEvent` is version-neutral JSON, so it can cross a process boundary. Each subscriber encodes it for its own protocol version and request id. The server calls `TaskCanceler.Listen` once at start-up and passes it the function that stops a turn running locally. That function does nothing for tasks the instance is not running, so a broadcast canceler only has to deliver every cancel to every instance.

### Logging and refused state changes

The server writes its diagnostics to PromptKit's logger, so `logger.SetLogger` and `logger.Configure` apply to it. Give it a logger of its own with `WithLogger`:

```go
srv := a2aserver.NewServer(opener, a2aserver.WithLogger(slog.Default().With("component", "a2a")))
```

A task store can refuse a state change as part of its own policy, for example to keep a task working while one of its tool calls waits for approval. Return an error that wraps `a2aserver.ErrTransitionRefused`. The server then logs the refusal at debug level rather than as a warning. As with any `SetState` error, it does not publish the state, and the task keeps the state the store holds. A message sent to a task whose resume the store refuses this way gets the "task is not waiting for input" error (`UnsupportedOperation`), not an internal error.

Records carry the request's context where the server has one, so fields a logging handler reads from the context (such as PromptKit's `logger.WithRequestID`) are attached. A failed authentication is logged at debug level, because any caller can send a bad credential.

## Verify

Fetch the agent card and the liveness probe:

```bash
curl http://localhost:9999/.well-known/agent-card.json
curl http://localhost:9999/healthz
```

The card's `supportedInterfaces` lists a JSON-RPC interface, and `/healthz` returns `{"status":"ok"}`.

## Related

- [A2A server reference](/sdk/reference/a2a-server/): every option and interface
- [A2A server tutorial](/sdk/tutorials/10-a2a-server/): build a server step by step
- [Connect A2A agents](/sdk/how-to/interop/connect-a2a-agents/): call remote agents from a conversation
