---
title: Shut down conversations gracefully
description: Track every open conversation with a ShutdownManager and close them all on SIGTERM or SIGINT before the process exits.
sidebar:
  order: 7
verified:
  commit: 8489bf3d95c7498f9de0f024769db9399c24e884
  sources:
    - sdk/conversation.go
    - sdk/options.go
    - sdk/sdk.go
    - sdk/shutdown.go
---

Close every open conversation, within a deadline, when your service receives a termination signal.

## Prerequisites

- A long-running process that opens conversations, such as an HTTP server.

## Steps

### 1. Create a manager and listen for signals

`GracefulShutdown` waits for `SIGTERM` or `SIGINT`, then calls `Shutdown` on the manager with the timeout you give it. Run it in its own goroutine:

```go
mgr := sdk.NewShutdownManager()
go sdk.GracefulShutdown(mgr, 30*time.Second)
```

`GracefulShutdown` returns after the shutdown finishes. It does not exit the process, so end `main` yourself once your server has stopped.

### 2. Attach the manager to each conversation

```go
conv, err := sdk.Open("./chat.pack.json", "assistant",
	sdk.WithShutdownManager(mgr),
)
if err != nil {
	return err
}
defer conv.Close()
```

`Open` and `OpenDuplex` register the conversation with the manager. `Close` deregisters it, so the manager tracks only live conversations.

### 3. Handle a late open

After `Shutdown` runs, the manager rejects new registrations with `sdk.ErrShutdownManagerClosed`. `Open` then closes the new conversation and returns an error that wraps it:

```go
_, err := sdk.Open("./chat.pack.json", "assistant", sdk.WithShutdownManager(mgr))
if errors.Is(err, sdk.ErrShutdownManagerClosed) {
	// The service is stopping: refuse the request.
}
```

## Verify

Send the process `SIGTERM` while a conversation is open. The log shows `All conversations closed successfully`, and `mgr.Len()` is 0.

## Shut down on your own trigger

Call `Shutdown` directly when something other than a signal ends the service:

```go
ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
defer cancel()
if err := mgr.Shutdown(ctx); err != nil {
	log.Printf("shutdown: %v", err)
}
```

`Shutdown` closes at most 100 conversations at a time. It returns the `ctx` error if the deadline passes first, and otherwise returns the errors from individual `Close` calls joined with `errors.Join`.

## Track other closers

`Register(id, closer)` accepts any `io.Closer`, and `Deregister(id)` removes one. `Deregister` is safe to call with an ID that was never registered. `Len()` returns the number of tracked closers.

## Related

- [Open a conversation](/sdk/how-to/conversations/initialize/)
- [Conversation reference](/sdk/reference/conversation-manager/)
