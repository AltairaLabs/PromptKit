---
title: Send Messages
description: Send messages, stream replies, read history, handle errors and attach metadata to a turn.
sidebar:
  order: 2
verified:
  commit: 004b9cbbd4d40f770a2e3c76053b600a55189355
  sources:
    - pkg/config/types.go
    - runtime/events/redact.go
    - runtime/events/types.go
    - runtime/pipeline/stage/message_metadata_template.go
    - runtime/providers/registry.go
    - runtime/types/message.go
    - sdk/conversation.go
    - sdk/errors.go
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
switch {
case errors.Is(err, sdk.ErrConversationClosed):
    log.Fatal("conversation was closed")
case errors.Is(err, context.DeadlineExceeded):
    log.Println("request timed out")
case err != nil:
    log.Printf("send failed: %v", err)
}
```

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
