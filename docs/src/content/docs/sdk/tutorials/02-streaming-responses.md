---
title: 'Tutorial 2: Streaming Responses'
sidebar:
  order: 2
verified:
  commit: 885cafa787a84bfba66f158fe51cda15a077ec23
  sources:
    - sdk/examples/streaming/main_interactive.go
    - sdk/streaming.go
---
Stream a response chunk by chunk with `conv.Stream()` and build a streaming chat.

## What you will build

Streaming shows text as the model produces it instead of after the full reply. We print a reply as it arrives, collect the full text, handle errors and timeouts, and finish with a streaming chat loop.

## Prerequisites

Complete [Tutorial 1: Your First Conversation](/sdk/tutorials/01-first-conversation/). The examples reuse its `hello.pack.json`.

## Basic streaming

`conv.Stream()` returns a channel of chunks. We replace `main.go` with:

```go
package main

import (
    "context"
    "fmt"
    "log"

    "github.com/AltairaLabs/PromptKit/sdk/v2"
)

func main() {
    conv, err := sdk.Open("./hello.pack.json", "chat")
    if err != nil {
        log.Fatal(err)
    }
    defer conv.Close()

    ctx := context.Background()

    fmt.Print("Assistant: ")
    for chunk := range conv.Stream(ctx, "Tell me a short story") {
        if chunk.Error != nil {
            log.Printf("Error: %v", chunk.Error)
            break
        }
        if chunk.Type == sdk.ChunkDone {
            fmt.Println("\n[Done]")
            break
        }
        fmt.Print(chunk.Text)
    }
}
```

## Understanding stream chunks

Each chunk has a `Type`, a `Text` string for text chunks, and an `Error` that is non-nil when the stream failed. The loop above relies on two types:

- `sdk.ChunkText`: text content arrived.
- `sdk.ChunkDone`: the stream completed. The chunk's `Message` holds the complete response.

The other types are `sdk.ChunkToolCall`, `sdk.ChunkMedia` and `sdk.ChunkClientTool`.

## Collecting the full response

Append each chunk's text to a string while you print it:

```go
var fullText string

for chunk := range conv.Stream(ctx, "Write a poem") {
    if chunk.Error != nil {
        log.Printf("Error: %v", chunk.Error)
        break
    }
    if chunk.Type == sdk.ChunkDone {
        break
    }

    fmt.Print(chunk.Text)  // Real-time display
    fullText += chunk.Text  // Collect for later
}

fmt.Printf("\n\nTotal length: %d characters\n", len(fullText))
```

## Progress tracking

Count characters as they arrive:

```go
charCount := 0

for chunk := range conv.Stream(ctx, "Tell me about AI") {
    if chunk.Error != nil {
        break
    }
    if chunk.Type == sdk.ChunkDone {
        fmt.Printf("\n\n[Complete - %d characters]\n", charCount)
        break
    }

    fmt.Print(chunk.Text)
    charCount += len(chunk.Text)
}
```

## Interactive streaming chat

We combine the loop with a terminal prompt:

```go
package main

import (
    "bufio"
    "context"
    "fmt"
    "log"
    "os"
    "strings"

    "github.com/AltairaLabs/PromptKit/sdk/v2"
)

func main() {
    conv, err := sdk.Open("./hello.pack.json", "chat")
    if err != nil {
        log.Fatal(err)
    }
    defer conv.Close()

    ctx := context.Background()
    scanner := bufio.NewScanner(os.Stdin)

    fmt.Println("Streaming chat ready! Type 'quit' to exit.")

    for {
        fmt.Print("\nYou: ")
        if !scanner.Scan() {
            break
        }

        msg := strings.TrimSpace(scanner.Text())
        if msg == "quit" {
            break
        }

        fmt.Print("Assistant: ")
        for chunk := range conv.Stream(ctx, msg) {
            if chunk.Error != nil {
                log.Printf("\nError: %v", chunk.Error)
                break
            }
            if chunk.Type == sdk.ChunkDone {
                fmt.Println()
                break
            }
            fmt.Print(chunk.Text)
        }
    }
}
```

## Error handling

An error arrives as a chunk with `Error` set. This snippet needs the `errors` import:

```go
for chunk := range conv.Stream(ctx, "Generate content") {
    if chunk.Error != nil {
        if errors.Is(chunk.Error, context.DeadlineExceeded) {
            fmt.Println("\n[Timeout - response truncated]")
        } else {
            fmt.Printf("\n[Error: %v]", chunk.Error)
        }
        break
    }

    if chunk.Type == sdk.ChunkDone {
        break
    }

    fmt.Print(chunk.Text)
}
```

## Timeout with context

Cancelling the context ends the stream, so a deadline bounds the whole reply. This snippet needs the `time` import:

```go
ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
defer cancel()

for chunk := range conv.Stream(ctx, "Tell me a long story") {
    if chunk.Error != nil {
        break
    }
    if chunk.Type == sdk.ChunkDone {
        break
    }
    fmt.Print(chunk.Text)
}
```

## Next steps

- [Tutorial 3: Tools](/sdk/tutorials/03-tool-integration/): add function calling
- [Tutorial 4: Variables](/sdk/tutorials/04-state-management/): template variables

The complete example is in `sdk/examples/streaming/`.
