---
title: 'Tutorial 2: Streaming responses'
sidebar:
  order: 2
verified:
  commit: 885cafa787a84bfba66f158fe51cda15a077ec23
  sources:
    - sdk/examples/streaming/main_interactive.go
    - sdk/streaming.go
---
## What we will build

In this tutorial we build a streaming chatbot that prints the reply as the model produces it, instead of after the full reply. We stream one reply, collect its full text, bound it with a timeout, and finish with a streaming chat loop.

## Prerequisites

Complete [Tutorial 1: Your first conversation](/sdk/tutorials/01-first-conversation/). We reuse its project, its `hello.pack.json` and the `OPENAI_API_KEY` it sets.

## Step 1: Stream a reply

`conv.Stream()` returns a channel of chunks. Replace `main.go` with:

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

    conv.SetVar("user_name", "World")
    ctx := context.Background()

    fmt.Print("Assistant: ")
    for chunk := range conv.Stream(ctx, "Tell me a short story") {
        if chunk.Error != nil {
            log.Fatalf("\nError: %v", chunk.Error)
        }
        if chunk.Type == sdk.ChunkDone {
            fmt.Println("\n[Done]")
            break
        }
        fmt.Print(chunk.Text)
    }
}
```

Each chunk carries a `Type`, the `Text` of a text chunk, and an `Error` when the stream failed; `sdk.ChunkDone` marks the end. The [`StreamChunk` reference](/sdk/reference/conversation-manager/#StreamChunk) lists every field.

Run it:

```bash
go run main.go
```

The story appears a few words at a time, and `[Done]` follows the last word. The story itself varies; the output typically looks like this:

```text
Assistant: Once upon a time, a small robot found a seed in a crack in the pavement...
[Done]
```

## Step 2: Collect the full response

We print each chunk and also append it to a string. Replace `main.go` with:

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

    conv.SetVar("user_name", "World")
    ctx := context.Background()

    var fullText string
    for chunk := range conv.Stream(ctx, "Write a short poem") {
        if chunk.Error != nil {
            log.Fatalf("\nError: %v", chunk.Error)
        }
        if chunk.Type == sdk.ChunkDone {
            break
        }
        fmt.Print(chunk.Text)   // show it now
        fullText += chunk.Text  // keep it for later
    }

    fmt.Printf("\n\nTotal length: %d characters\n", len(fullText))
}
```

Run it with `go run main.go`. The poem streams in as before, then a blank line and the total, for example `Total length: 312 characters`. The count depends on the poem.

## Step 3: Bound the reply with a timeout

Canceling the context ends the stream, and the failure arrives as a chunk with `Error` set. Replace `main.go` with:

```go
package main

import (
    "context"
    "errors"
    "fmt"
    "log"
    "time"

    "github.com/AltairaLabs/PromptKit/sdk/v2"
)

func main() {
    conv, err := sdk.Open("./hello.pack.json", "chat")
    if err != nil {
        log.Fatal(err)
    }
    defer conv.Close()

    conv.SetVar("user_name", "World")

    // Give the whole reply at most 30 seconds
    ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
    defer cancel()

    for chunk := range conv.Stream(ctx, "Tell me a long story") {
        if chunk.Error != nil {
            if errors.Is(chunk.Error, context.DeadlineExceeded) {
                fmt.Println("\n[Timeout - response truncated]")
            } else {
                fmt.Printf("\n[Error: %v]\n", chunk.Error)
            }
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

Run it with `go run main.go`. Within 30 seconds the story completes and `[Done]` prints. Change the limit to `100*time.Millisecond` and run it again: the reply stops after a few words, and the program prints `[Timeout - response truncated]`.

## Step 4: Build a streaming chat

We combine the stream loop with a terminal prompt. Replace `main.go` with:

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

    conv.SetVar("user_name", "World")

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

Run it with `go run main.go`. The program prints `Streaming chat ready! Type 'quit' to exit.` and a `You: ` prompt. Each reply streams in after `Assistant: `; type `quit` to exit.

## Next steps

- [Tutorial 3: Tool integration](/sdk/tutorials/03-tool-integration/) adds function calling.
- [Tutorial 4: Variables and templates](/sdk/tutorials/04-state-management/) covers template variables.
- The complete example is in `sdk/examples/streaming/`.
