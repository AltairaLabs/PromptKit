---
title: 'Tutorial 2: Streaming Responses'
docType: tutorial
order: 2
---
# Tutorial 2: Streaming Responses

Learn how to implement real-time streaming for better user experience.

## What You'll Learn

- Stream LLM responses in real-time
- Process chunks as they arrive
- Handle streaming errors
- Track progress and completion

## Why Streaming?

Streaming provides immediate feedback:

**Without Streaming:**
```
[3 second wait...]
Here's a complete response about streaming...
```

**With Streaming:**
```
Here's→ a→ complete→ response→ about→ streaming...
```

Users see results immediately and can stop generation if needed.

## Prerequisites

Complete [Tutorial 1: Your First Conversation](01-first-conversation) or understand basic SDK usage.

## Basic Streaming

Use `conv.Stream()` instead of `conv.Send()`:

```go
package main

import (
    "context"
    "fmt"
    "log"

    "github.com/AltairaLabs/PromptKit/sdk"
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

## Understanding Stream Chunks

Each chunk contains:

```go
type StreamChunk struct {
    Type  ChunkType // ChunkText, ChunkToolCall, ChunkDone
    Text  string    // Text content (for ChunkText)
    Error error     // Non-nil if error occurred
}
```

### Chunk Types

- **`sdk.ChunkText`** - Text content arrived
- **`sdk.ChunkToolCall`** - Tool is being called
- **`sdk.ChunkDone`** - Stream completed

## Collecting Full Response

Track the complete response while streaming:

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

## Progress Tracking

Show progress indicators:

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

## Interactive Streaming Chat

Build a streaming chatbot:

```go
package main

import (
    "bufio"
    "context"
    "fmt"
    "log"
    "os"
    "strings"

    "github.com/AltairaLabs/PromptKit/sdk"
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

## Error Handling

Handle errors gracefully:

```go
for chunk := range conv.Stream(ctx, "Generate content") {
    if chunk.Error != nil {
        // Check error type
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

## Timeout with Context

Set a streaming timeout:

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

## What You've Learned

✅ Stream responses with `conv.Stream()`  
✅ Process chunks as they arrive  
✅ Track progress and completion  
✅ Handle streaming errors  
✅ Build interactive streaming apps  

## Next Steps

- **[Tutorial 3: Tools](03-tool-integration)** - Add function calling
- **[Tutorial 4: Variables](04-state-management)** - Template variables

## Complete Example

See the full example at `sdk/examples/streaming/`.
