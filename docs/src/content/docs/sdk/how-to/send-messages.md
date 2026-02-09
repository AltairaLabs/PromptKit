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
import "github.com/AltairaLabs/PromptKit/runtime/types"

msg := types.Message{
    Role:    "user",
    Content: "Hello!",
}
resp, _ := conv.Send(ctx, &msg)
```

## Complete Example

```go
package main

import (
    "context"
    "fmt"
    "log"

    "github.com/AltairaLabs/PromptKit/sdk"
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

- [Open a Conversation](initialize)
- [Register Tools](register-tools)
- [Tutorial 2: Streaming](../tutorials/02-streaming-responses)
