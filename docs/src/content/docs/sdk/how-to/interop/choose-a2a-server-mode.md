---
title: Choose an A2A Server Mode
description: Decide whether the A2A server owns conversations or your platform does
sidebar:
  order: 8
---

`server/a2a` speaks the A2A protocol. Who owns the conversations behind it is
your choice, and it is the only choice that matters when you set the server up.

## The two modes

| | `NewServer` | `NewStatelessServer` |
|---|---|---|
| You supply | a `ConversationOpener` | a `MessageHandler` |
| The server | opens a conversation per `contextID`, caches it, reuses it, expires it on a TTL | keeps nothing between calls |
| Your code sees | the `contextID` | the `contextID` **and the HTTP request's context** |
| Fits | A2A and the runtime in one process | a runtime elsewhere, or a platform that already tracks sessions |

Both serve the identical protocol: same JSON-RPC methods, same task lifecycle,
same SSE streaming, same agent card. A client cannot tell which one it is
talking to.

## Conversation-owning: `NewServer`

```go
opener := sdk.A2AOpener("./assistant.pack.json", "chat")

srv := a2aserver.NewServer(opener,
    a2aserver.WithCard(&card),
    a2aserver.WithPort(9999),
)
```

The server does the bookkeeping. This is the shortest path when everything runs
in one binary, and it is what the [A2A server
tutorial](/sdk/tutorials/10-a2a-server/) uses.

What you are accepting: the server decides conversation lifetime, and its cache
is keyed on the `contextID` the **caller** sends. Two callers presenting the
same one share a conversation. If your callers are not mutually trusting, that
is a reason to choose the other mode.

## Stateless: `NewStatelessServer`

```go
type runtimeHandler struct{ client *grpcRuntimeClient }

func (h *runtimeHandler) Handle(
    ctx context.Context, req a2aserver.MessageRequest,
) <-chan a2aserver.StreamEvent {
    identity := auth.IdentityFromContext(ctx) // your middleware put it there
    return h.client.Converse(ctx, h.sessionFor(req.ContextID, identity), req.Message)
}

srv := a2aserver.NewStatelessServer(&runtimeHandler{client: c},
    a2aserver.WithTaskStore(redisStore),
    a2aserver.WithCardProvider(cards),
)
```

Every message reaches your handler with the context of the request it arrived
on, so caller identity, tenant and trace are readable exactly where you decide
what to do. You map `contextID` to whatever a session means in your system —
including deciding that two callers sharing one share nothing.

Nothing is held in the process, so any replica can serve any request; the task
store is the only thing that has to be shared.

### Answering with events

Your handler returns a channel and closes it when the turn is over:

| Event | Meaning |
|---|---|
| `EventText` | a chunk of the reply |
| `EventMedia` | a media part of the reply |
| `EventClientTool` | you need the caller to run a tool; the task goes to `input_required` |
| `EventPending` | you are waiting on something the server cannot see, such as a human approval |
| `EventDone` | the turn is complete |
| `Error` set | the turn failed |

`EventPending` exists because a pause is invisible from outside. Without it a
turn waiting on an approval would be reported `completed`, which is a control
that did not run reporting clean.

For `message/send`, the server drains your stream and builds the result: the
text is the concatenation of your text events, and the parts are one per text
run and one per media event. If you need a distinct part, emit a distinct event.

### Client tools

Implement `ToolResultHandler` as well, and the caller's tool results come back
to you:

```go
func (h *runtimeHandler) HandleToolResult(
    ctx context.Context, req a2aserver.ToolResultRequest,
) <-chan a2aserver.StreamEvent {
    return h.client.Resume(ctx, h.sessionFor(req.ContextID, ...), req.Results)
}
```

A stateless server whose handler does not implement it refuses tool-result
messages rather than delivering them as a fresh turn — it is holding nothing to
resume, and pretending otherwise would produce a turn with no history behind it.

## Which one

Choose `NewStatelessServer` if any of these is true:

- your runtime runs in a different process or service;
- you already have sessions, with their own storage and expiry;
- your handler needs to know who is calling;
- you want to run more than one replica behind a load balancer;
- callers are not mutually trusting and may present each other's `contextID`.

Otherwise `NewServer` is less to write.
