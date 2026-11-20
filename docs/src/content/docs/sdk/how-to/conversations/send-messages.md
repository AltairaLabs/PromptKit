---
title: Send Messages
description: Send messages, stream replies, read history, handle errors and attach metadata to a turn.
sidebar:
  order: 2
verified:
  commit: 8489bf3d95c7498f9de0f024769db9399c24e884
  sources:
    - pkg/config/types.go
    - runtime/events/redact.go
    - runtime/events/types.go
    - runtime/pipeline/stage/message_metadata_template.go
    - runtime/providers/registry.go
    - runtime/types/message.go
    - sdk/conversation.go
    - sdk/errors.go
    - sdk/errors_wrap.go
    - sdk/json_input.go
    - sdk/message_metadata.go
    - sdk/options.go
    - sdk/response.go
    - sdk/streaming.go
---
Send a message to a conversation and read the reply, as a whole or as a stream.

## Prerequisites

An open conversation (`conv`). See [Open a Conversation](/sdk/how-to/conversations/initialize/).

## Send a message

1. Call `Send` and print the text:

   ```go
   ctx := context.Background()
   resp, err := conv.Send(ctx, "What is the capital of France?")
   if err != nil {
       log.Fatal(err)
   }
   fmt.Println(resp.Text())
   ```

   The conversation keeps its history, so the next `Send` sees earlier turns.

2. Check the response for tool calls and for tool calls waiting on approval:

   ```go
   for _, call := range resp.ToolCalls() {
       fmt.Printf("Tool: %s\n", call.Name)
   }
   for _, p := range resp.PendingTools() {
       fmt.Printf("Awaiting approval: %s\n", p.Name)
   }
   ```

3. Pass a `*types.Message` instead of a string when you build the message yourself. `Send` accepts a string or a `*types.Message`:

   ```go
   msg := &types.Message{Role: "user", Content: "Hello!"}
   resp, err := conv.Send(ctx, msg)
   ```

   `types` is `github.com/AltairaLabs/PromptKit/runtime/v2/types`.

## Stream a reply

Range over `Stream`. Text chunks carry `Text`, the final chunk has type `sdk.ChunkDone` and holds the complete response in `Message`, and a failure arrives as a chunk with `Error` set:

```go
var full string
for chunk := range conv.Stream(ctx, "Tell me a story") {
    if chunk.Error != nil {
        log.Printf("stream failed: %v", chunk.Error)
        break
    }
    if chunk.Type == sdk.ChunkDone {
        break
    }
    full += chunk.Text
    fmt.Print(chunk.Text)
}
```

With a streaming output guardrail (`banned_words`, `length`), each text chunk reaches you only after the guardrail has checked it. `banned_words` also holds back the end of a chunk that could be the start of a banned word, so a word the provider splits across chunks never arrives in pieces. When the guardrail cuts the reply, its message arrives as the last text chunk with `ReplacesText` set: that text replaces everything the turn streamed before it, and it alone is the reply's text in `ChunkDone`. Honor the marker. A client that only concatenates chunks shows the text that came before the banned word followed by the message:

```go
var shown strings.Builder
for chunk := range conv.Stream(ctx, "Tell me a story") {
    if chunk.Type != sdk.ChunkText {
        continue
    }
    if chunk.ReplacesText {
        shown.Reset() // and clear what the UI displays
    }
    shown.WriteString(chunk.Text)
}
```

A judge guardrail can hold the reply back until it has judged it, per segment or whole (its `stream_hold` param; see [Stream hold](/reference/checks/#stream-hold)). With `stream_hold: segment`, text arrives a sentence or a few at a time, each part once the judge has passed it. With `stream_hold: turn`, the whole reply arrives as one chunk once it passed. When the guardrail fails a segment after earlier ones arrived, its message arrives with `ReplacesText` set, as above. Tune the segments and the judging with `sdk.WithStreamHoldSegments` and `sdk.WithStreamHoldJudges`:

```go
conv, err := sdk.Open("./chat.pack.json", "assistant",
    sdk.WithStreamHoldSegments(120, 40, 800), // larger segments: fewer judge calls
    sdk.WithStreamHoldJudges(4, 3*time.Second),
)
```

`StreamRaw` chunks, `TextDeltaEvent` and `stream.CollectText` carry the same marker. Over A2A, the server restarts the text artifact with the message, sending its update with `append: false`. Over AG-UI, the adapter ends the streamed text message and sends the guardrail's message as a new one.

A turn that calls tools streams each round in order. A round's text comes first. Once it has streamed, a `sdk.ChunkToolCall` chunk carries each call the round made (in `ToolCall`), before the calls run. Once they have run, a `sdk.ChunkToolResult` chunk carries each result the model is given (in `ToolResult`). Both carry `Round`, the round's number from 1. Text after them is the next round's reply. A round whose calls end the turn, such as a client tool waiting for an answer or a call held for approval, sends its calls; its results are in the `ChunkDone` response's `TurnMessages`.

`ChunkToolResult` carries what your server-side tools returned. Drop it before forwarding chunks to a client that must not see tool output.

A call held for approval (`OnToolAsync`) is not a client tool: no `ChunkClientTool` is sent for it. The `ChunkDone` response lists it in `PendingTools()`, and the turn is paused until you resolve it and call `Continue`. `StreamWithCallback` reports it as a `ToolApprovalEvent` before `StreamDoneEvent`.

## Read and clear history

```go
for _, msg := range conv.Messages(ctx) {
    fmt.Printf("[%s] %s\n", msg.Role, msg.GetContent())
}

if err := conv.Clear(); err != nil {
    log.Fatal(err)
}
```

`Messages` returns a copy. `Clear` removes the messages and keeps the system prompt and variables.

## Set a timeout

Pass a context with a deadline:

```go
ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
defer cancel()

resp, err := conv.Send(ctx, "Tell me a long story")
if errors.Is(err, context.DeadlineExceeded) {
    log.Println("request timed out")
}
```

If the model's stream fails after it has started replying, `Send` returns the error and a non-nil `Response` holding the partial reply, with finish reason `types.FinishReasonInterrupted`.

## Handle errors

```go
resp, err := conv.Send(ctx, message)
var provErr *sdk.ProviderError
switch {
case errors.Is(err, sdk.ErrConversationClosed):
    log.Fatal("conversation was closed")
case errors.Is(err, context.DeadlineExceeded):
    log.Println("request timed out")
case errors.As(err, &provErr):
    log.Printf("provider %s failed (status %d)", provErr.Provider, provErr.StatusCode)
case err != nil:
    log.Printf("send failed: %v", err)
}
```

- A hook or guardrail that denies the turn makes `Send` return a `*sdk.ValidationError`. `sdk.AsValidationError(err)` extracts it, and `errors.As` still finds the runtime's `*hooks.HookDeniedError` inside.
- A provider API error status or a failed connection makes `Send` return a `*sdk.ProviderError`; `StatusCode` is 0 for a connection failure. `providers.IsTransient(err)` still works on it.
- `Stream` delivers the same error types in `chunk.Error`.

## Attach metadata to a turn

`WithMessageMetadata` attaches key-value data to the user message of one `Send` or `Stream` call. Use it for what your application knows about the turn that is not chat text, such as the shapes selected on a canvas:

```go
resp, err := conv.Send(ctx, "Align these",
    sdk.WithMessageMetadata(map[string]any{
        "selection": []string{"s1", "s2", "s3"},
        "view":      "canvas",
    }))
```

The model does not receive the metadata unless you set a template (next section). The metadata is stored on the user message, so it appears in `Messages()`, the state store, and the evals that read them. Read it back with `Metadata()`:

```go
for _, m := range conv.Messages(ctx) {
    if m.Role == "user" {
        fmt.Println(m.Metadata())
    }
}
```

The message's `message.created` event carries it as `Metadata`, on the event bus and in the event store. A subscriber wrapped in `events.Redacting` receives it with its strings passed through the redactor as the `message.metadata` field.

Values must be JSON-serializable, or `Send` returns an error. The SDK keeps them in decoded JSON form, the form a state store returns: a number reads back as `float64`, a slice as `[]any`, and a struct or typed map as `map[string]any`. `Metadata()` returns a copy, so changing it does not change the message. When you pass a `*types.Message` to `Send`, the metadata goes on a copy, so reusing your message for the next turn carries no metadata over.

### Show metadata to the model

Set `MessageMetadataTemplate` on the provider to render the metadata as text the model reads. Each `{{key}}` is filled from the message's metadata:

```go
conv, err := sdk.Open("./app.pack.json", "chat",
    sdk.WithLLMProvider(sdk.ProviderSpec{
        Type:                    "claude",
        Model:                   "claude-sonnet-4-20250514",
        MessageMetadataTemplate: "<context>Selected: {{selection}}</context>",
    }))
```

With the `Send` call above, the model receives the user message followed by:

```text
<context>Selected: ["s1","s2","s3"]</context>
```

- A string value is inserted as is, and any other value as compact JSON. A key the message does not carry renders as empty.
- Every message in the history that has metadata is rendered, including earlier turns.
- The rendered text is added to the request only. `Messages()`, the state store and `message.created` keep the user's own text.
- A provider built with `providers.CreateProviderFromSpec` takes the same template as `providers.ProviderSpec.MessageMetadataTemplate`.
- In a provider file or a [RuntimeConfig](/sdk/reference/runtime-config/) provider entry, the field is `message_metadata_template`.

To give the model structured input that fills the pack's template variables instead, use `WithJSONInput`.

## Verify

```go
package main

import (
    "context"
    "fmt"
    "log"

    "github.com/AltairaLabs/PromptKit/sdk/v2"
)

func main() {
    conv, err := sdk.Open("./app.pack.json", "chat")
    if err != nil {
        log.Fatal(err)
    }
    defer conv.Close()

    ctx := context.Background()
    for _, q := range []string{"My name is Alice", "What is my name?"} {
        resp, err := conv.Send(ctx, q)
        if err != nil {
            log.Fatal(err)
        }
        fmt.Printf("Q: %s\nA: %s\n\n", q, resp.Text())
    }
}
```

The second answer uses the first turn, which shows the conversation keeps its history.

## Related

- [Open a Conversation](/sdk/how-to/conversations/initialize/)
- [Register Tools](/sdk/how-to/tools/register-tools/)
- [Tutorial 2: Streaming](/sdk/tutorials/02-streaming-responses/)
