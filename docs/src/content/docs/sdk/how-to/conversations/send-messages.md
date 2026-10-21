---
title: Send Messages
sidebar:
  order: 2
---
Learn how to send messages and receive responses.

## Basic Send

```go
ctx := context.Background()
resp, err := conv.Send(ctx, "What is the capital of France?")
if err != nil {
    log.Fatal(err)
}

fmt.Println(resp.Text())  // "The capital of France is Paris."
```

## Response Methods

### Get Text

```go
text := resp.Text()
```

### Check for Tool Calls

```go
if resp.HasToolCalls() {
    for _, call := range resp.ToolCalls() {
        fmt.Printf("Tool: %s\n", call.Name)
    }
}
```

### Check for Pending Approvals

```go
pending := resp.PendingTools()
if len(pending) > 0 {
    // Handle HITL approvals
}
```

## Streaming

### Basic Stream

```go
for chunk := range conv.Stream(ctx, "Tell me a story") {
    if chunk.Error != nil {
        log.Printf("Error: %v", chunk.Error)
        break
    }
    if chunk.Type == sdk.ChunkDone {
        break
    }
    fmt.Print(chunk.Text)
}
```

### Collect Full Response

```go
var fullText string
for chunk := range conv.Stream(ctx, "Write a poem") {
    if chunk.Type == sdk.ChunkDone {
        break
    }
    if chunk.Error != nil {
        break
    }
    fullText += chunk.Text
    fmt.Print(chunk.Text)  // Show progress
}
```

## Multi-Turn Context

The SDK maintains conversation history:

```go
// Turn 1
resp1, _ := conv.Send(ctx, "My name is Alice")

// Turn 2 - context remembered
resp2, _ := conv.Send(ctx, "What's my name?")
fmt.Println(resp2.Text())  // "Your name is Alice."
```

## Message History

### Get All Messages

```go
ctx := context.Background()
messages := conv.Messages(ctx)
for _, msg := range messages {
    fmt.Printf("[%s] %s\n", msg.Role, msg.Content)
}
```

### Clear History

```go
_ = conv.Clear()  // Starts fresh, returns error
```

## Timeouts

### With Context Timeout

```go
ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
defer cancel()

resp, err := conv.Send(ctx, "Tell me a long story")
if errors.Is(err, context.DeadlineExceeded) {
    log.Println("Request timed out")
}
```

## Error Handling

```go
resp, err := conv.Send(ctx, message)
if err != nil {
    switch {
    case errors.Is(err, sdk.ErrConversationClosed):
        log.Fatal("Conversation was closed")
    case errors.Is(err, sdk.ErrProviderNotDetected):
        log.Printf("Provider not detected: %v", err)
    case errors.Is(err, context.DeadlineExceeded):
        log.Println("Request timed out")
    default:
        log.Printf("Send failed: %v", err)
    }
}
```

## Sending Structured Messages

### String Message

```go
resp, _ := conv.Send(ctx, "Hello!")
```

### Message Type

```go
import "github.com/AltairaLabs/PromptKit/runtime/v2/types"

msg := types.Message{
    Role:    "user",
    Content: "Hello!",
}
resp, _ := conv.Send(ctx, &msg)
```

## Attach Metadata to a Turn

`WithMessageMetadata` attaches key-value data to the user message of one `Send` or `Stream` call. Use it for what your application knows about the turn that is not chat text, such as the shapes selected on a canvas:

```go
resp, err := conv.Send(ctx, "Align these",
    sdk.WithMessageMetadata(map[string]any{
        "selection": []string{"s1", "s2", "s3"},
        "view":      "canvas",
    }))
```

The model never receives the metadata. To give the model structured input, use `WithJSONInput`.

The metadata is stored on the user message, so it appears in `Messages()`, the state store, and the evals that read them. Read it back with `Metadata()`:

```go
for _, m := range conv.Messages(ctx) {
    if m.Role == "user" {
        fmt.Println(m.Metadata())
    }
}
```

The message's `message.created` event carries it as `Metadata`, on the event bus and in the event store. A subscriber wrapped in `events.Redacting` receives it with its strings passed through the redactor as the `message.metadata` field.

Values must be JSON-serializable, or `Send` returns an error. A state store keeps them as JSON, so numbers read back from it as `float64`.

## Complete Example

```go
package main

import (
    "context"
    "fmt"
    "log"

    "github.com/AltairaLabs/PromptKit/sdk/v2"
)

func main() {
    conv, _ := sdk.Open("./app.pack.json", "chat")
    defer conv.Close()

    ctx := context.Background()

    // Multi-turn conversation
    questions := []string{
        "What is machine learning?",
        "How is it different from AI?",
        "Give me an example.",
    }

    for _, q := range questions {
        fmt.Printf("Q: %s\n", q)
        
        resp, err := conv.Send(ctx, q)
        if err != nil {
            log.Printf("Error: %v", err)
            continue
        }
        
        fmt.Printf("A: %s\n\n", resp.Text())
    }
}
```

## See Also

- [Open a Conversation](/sdk/how-to/conversations/initialize/)
- [Register Tools](/sdk/how-to/tools/register-tools/)
- [Tutorial 2: Streaming](/sdk/tutorials/02-streaming-responses/)
