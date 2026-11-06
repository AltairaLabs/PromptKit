---
title: Stream with typed event callbacks
description: Receive text deltas, client tool requests and the final response as typed events with OnStreamEvent and StreamWithCallback.
sidebar:
  order: 9
verified:
  commit: 8489bf3d95c7498f9de0f024769db9399c24e884
  sources:
    - sdk/stream_events.go
---

Handle a streamed response through a callback that receives typed events, instead of switching on `StreamChunk.Type` yourself.

## Prerequisites

- An open conversation (see [Open a conversation](/sdk/how-to/conversations/initialize/)).

## Steps

### 1. Register a handler

`OnStreamEvent` stores one handler on the conversation. A second call replaces the first.

```go
conv.OnStreamEvent(func(event sdk.StreamEvent) {
	switch e := event.(type) {
	case sdk.TextDeltaEvent:
		fmt.Print(e.Delta)
	case sdk.ClientToolRequestEvent:
		fmt.Printf("\ntool %s needs fulfillment\n", e.ToolName)
	case sdk.StreamDoneEvent:
		fmt.Println("\ndone")
	}
})
```

### 2. Send the message

`StreamWithCallback` streams the reply, calls the handler for each event, and returns the complete response:

```go
resp, err := conv.StreamWithCallback(ctx, "Tell me a story")
if err != nil {
	return err
}
fmt.Println(resp.Text())
```

It returns `nil` and the stream's error when a chunk carries one.

## Event types

| Event | Emitted when | Fields |
|---|---|---|
| `TextDeltaEvent` | A text chunk arrives. | `Delta` |
| `ClientToolRequestEvent` | The model calls a client tool that your code must fulfill. | `CallID`, `ToolName`, `Args`, `ConsentMsg`, `Categories` |
| `StreamDoneEvent` | The stream completes. | `Response` |

Tool-call and media chunks produce no event. Read them with `Stream` when you need them.

With no handler registered, `StreamWithCallback` discards every chunk and returns the final response.

## Verify

The handler prints text as it arrives and `done` at the end, and `resp.Text()` holds the full reply.

## Related

- [Send messages](/sdk/how-to/conversations/send-messages/)
- [Register client-side tools](/sdk/how-to/tools/client-tools/)
