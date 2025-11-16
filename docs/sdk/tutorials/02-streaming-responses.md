---
layout: default
title: "Tutorial 2: Streaming Responses"
nav_order: 2
parent: SDK Tutorials
grand_parent: SDK
---

# Tutorial 2: Streaming Responses

Learn how to implement real-time streaming for better user experience.

## What You'll Learn

- Stream LLM responses in real-time
- Display incremental content as it arrives
- Handle streaming errors
- Build a streaming chatbot

## Why Streaming?

Streaming provides immediate feedback:

**Without Streaming:**
```
[3 second wait...]
Here's a complete response about streaming...
```

**With Streaming:**
```
Here's a→ complete→ response→ about→ streaming...
```

Users see results immediately and can stop generation if needed.

## Prerequisites

Complete [Tutorial 1: Your First Conversation](01-first-conversation.md) or understand basic SDK usage.

## Step 1: Basic Streaming

Start with this simple example:

```go
package main

import (
    "context"
    "fmt"
    "log"
    "os"

    "github.com/AltairaLabs/PromptKit/sdk"
    "github.com/AltairaLabs/PromptKit/runtime/providers"
)

func main() {
    ctx := context.Background()

    // Setup (same as Tutorial 1)
    apiKey := os.Getenv("OPENAI_API_KEY")
    provider := providers.NewOpenAIProvider(apiKey, "gpt-4o-mini", false)
    
    manager, err := sdk.NewConversationManager(
        sdk.WithProvider(provider),
    )
    if err != nil {
        log.Fatal(err)
    }

    pack, err := manager.LoadPack("./assistant.pack.json")
    if err != nil {
        log.Fatal(err)
    }

    conv, err := manager.NewConversation(ctx, pack, sdk.ConversationConfig{
        UserID:     "user123",
        PromptName: "assistant",
    })
    if err != nil {
        log.Fatal(err)
    }

    // Stream a response
    fmt.Print("Assistant: ")
    
    stream, err := conv.SendStream(ctx, "Tell me a short story")
    if err != nil {
        log.Fatal(err)
    }

    for chunk := range stream {
        if chunk.Error != nil {
            log.Printf("Stream error: %v", chunk.Error)
            break
        }
        fmt.Print(chunk.Content)
    }
    
    fmt.Println()
}
```

Run it:

```bash
go run main.go
```

You'll see the response appear word by word!

## Understanding Streaming

### SendStream() Method

```go
stream, err := conv.SendStream(ctx, "Your message")
```

Returns a channel that emits chunks as they arrive from the LLM.

### Stream Chunks

Each chunk contains:

```go
type StreamChunk struct {
    Content string  // Incremental content
    Done    bool    // True for final chunk
    Error   error   // Non-nil if error occurred
}
```

### Reading from Stream

```go
for chunk := range stream {
    if chunk.Error != nil {
        // Handle error
        break
    }
    if chunk.Done {
        // Generation complete
        break
    }
    // Process chunk.Content
    fmt.Print(chunk.Content)
}
```

## Step 2: Interactive Streaming Chat

Build a full streaming chatbot:

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
    "github.com/AltairaLabs/PromptKit/runtime/providers"
)

func main() {
    ctx := context.Background()

    // Setup
    apiKey := os.Getenv("OPENAI_API_KEY")
    provider := providers.NewOpenAIProvider(apiKey, "gpt-4o-mini", false)
    
    manager, err := sdk.NewConversationManager(
        sdk.WithProvider(provider),
    )
    if err != nil {
        log.Fatal(err)
    }

    pack, err := manager.LoadPack("./assistant.pack.json")
    if err != nil {
        log.Fatal(err)
    }

    conv, err := manager.NewConversation(ctx, pack, sdk.ConversationConfig{
        UserID:     "user123",
        PromptName: "assistant",
    })
    if err != nil {
        log.Fatal(err)
    }

    // Interactive loop
    scanner := bufio.NewScanner(os.Stdin)
    fmt.Println("Streaming Chatbot ready! Type 'quit' to exit")
    fmt.Println()

    for {
        fmt.Print("You: ")
        if !scanner.Scan() {
            break
        }

        message := strings.TrimSpace(scanner.Text())
        if message == "" {
            continue
        }
        if message == "quit" {
            fmt.Println("Goodbye!")
            break
        }

        // Stream response
        fmt.Print("Assistant: ")
        
        stream, err := conv.SendStream(ctx, message)
        if err != nil {
            fmt.Printf("Error: %v\n\n", err)
            continue
        }

        // Display chunks as they arrive
        for chunk := range stream {
            if chunk.Error != nil {
                fmt.Printf("\nError: %v\n", chunk.Error)
                break
            }
            fmt.Print(chunk.Content)
        }
        
        fmt.Println()
        fmt.Println()
    }
}
```

Try it:

```bash
go run main.go
```

Watch responses stream in real-time!

## Step 3: Collecting Full Response

Sometimes you need both streaming display AND the full response:

```go
// Stream and collect
fmt.Print("Assistant: ")

stream, err := conv.SendStream(ctx, "Explain quantum computing")
if err != nil {
    log.Fatal(err)
}

var fullResponse strings.Builder

for chunk := range stream {
    if chunk.Error != nil {
        log.Fatal(chunk.Error)
    }
    
    // Display chunk
    fmt.Print(chunk.Content)
    
    // Collect full response
    fullResponse.WriteString(chunk.Content)
}

fmt.Println()

// Now you have the full response
fullText := fullResponse.String()
fmt.Printf("\n[Response was %d characters]\n", len(fullText))
```

## Step 4: Handling Errors

Handle streaming errors gracefully:

```go
stream, err := conv.SendStream(ctx, message)
if err != nil {
    log.Printf("Failed to start stream: %v", err)
    return
}

for chunk := range stream {
    if chunk.Error != nil {
        // Stream error occurred
        if strings.Contains(chunk.Error.Error(), "rate limit") {
            fmt.Println("\nRate limit exceeded. Please try again later.")
        } else if strings.Contains(chunk.Error.Error(), "context length") {
            fmt.Println("\nMessage too long. Please shorten your input.")
        } else {
            fmt.Printf("\nError: %v\n", chunk.Error)
        }
        break
    }
    
    fmt.Print(chunk.Content)
}
```

## Step 5: Streaming with Timeout

Add timeout protection:

```go
import "time"

// Create context with timeout
streamCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
defer cancel()

stream, err := conv.SendStream(streamCtx, message)
if err != nil {
    log.Fatal(err)
}

for chunk := range stream {
    if chunk.Error != nil {
        if errors.Is(chunk.Error, context.DeadlineExceeded) {
            fmt.Println("\nResponse timed out")
        } else {
            fmt.Printf("\nError: %v\n", chunk.Error)
        }
        break
    }
    
    fmt.Print(chunk.Content)
}
```

## Advanced: Streaming with UI Updates

For GUI applications:

```go
// Hypothetical UI framework
func streamToUI(ctx context.Context, conv *sdk.Conversation, message string) {
    stream, err := conv.SendStream(ctx, message)
    if err != nil {
        ui.ShowError(err)
        return
    }

    // Create message bubble
    bubble := ui.NewMessageBubble()
    
    for chunk := range stream {
        if chunk.Error != nil {
            ui.ShowError(chunk.Error)
            break
        }
        
        // Update UI with chunk
        bubble.Append(chunk.Content)
        ui.Refresh()
    }
}
```

## Streaming vs Non-Streaming

When to use each:

**Use Streaming:**
- Interactive chat applications
- Long responses (>100 tokens)
- User needs immediate feedback
- Web/mobile UIs

**Use Non-Streaming:**
- Batch processing
- Short responses
- No UI to update
- Response needs to be validated before display

## Performance Comparison

```go
import "time"

// Non-streaming
start := time.Now()
resp, _ := conv.Send(ctx, "Write a paragraph about Go")
fmt.Printf("Non-streaming: %v\n", time.Since(start))  // ~3s

// Streaming (time to first token)
start = time.Now()
stream, _ := conv.SendStream(ctx, "Write a paragraph about Go")
firstChunk := <-stream
fmt.Printf("First token: %v\n", time.Since(start))     // ~0.5s
```

Streaming shows results 6x faster!

## Provider Support

Streaming support by provider:

| Provider | Streaming | Notes |
|----------|-----------|-------|
| OpenAI | ✅ Yes | All GPT models |
| Anthropic | ✅ Yes | Claude 3+ |
| Google | ✅ Yes | Gemini models |

## Common Issues

### Chunks Not Appearing

Check buffering:

```go
// Ensure output isn't buffered
import "os"

os.Stdout.Sync()  // Force flush
```

### Stream Hangs

Add timeout:

```go
ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
defer cancel()

stream, _ := conv.SendStream(ctx, message)
```

### Memory Usage

For very long generations, clear old chunks:

```go
const maxChunks = 1000
chunks := []string{}

for chunk := range stream {
    chunks = append(chunks, chunk.Content)
    if len(chunks) > maxChunks {
        chunks = chunks[1:]  // Drop oldest
    }
}
```

## Complete Example: Rich Streaming

Here's a production-ready streaming implementation:

```go
package main

import (
    "bufio"
    "context"
    "fmt"
    "log"
    "os"
    "strings"
    "time"

    "github.com/AltairaLabs/PromptKit/sdk"
    "github.com/AltairaLabs/PromptKit/runtime/providers"
)

func main() {
    ctx := context.Background()

    // Setup
    apiKey := os.Getenv("OPENAI_API_KEY")
    provider := providers.NewOpenAIProvider(apiKey, "gpt-4o-mini", false)
    
    manager, err := sdk.NewConversationManager(
        sdk.WithProvider(provider),
    )
    if err != nil {
        log.Fatal(err)
    }

    pack, err := manager.LoadPack("./assistant.pack.json")
    if err != nil {
        log.Fatal(err)
    }

    conv, err := manager.NewConversation(ctx, pack, sdk.ConversationConfig{
        UserID:     "user123",
        PromptName: "assistant",
    })
    if err != nil {
        log.Fatal(err)
    }

    // Interactive loop
    scanner := bufio.NewScanner(os.Stdin)
    fmt.Println("Streaming Chatbot (Ctrl+C to exit)")
    fmt.Println()

    for {
        fmt.Print("You: ")
        if !scanner.Scan() {
            break
        }

        message := strings.TrimSpace(scanner.Text())
        if message == "" {
            continue
        }

        // Stream with metrics
        streamMessage(ctx, conv, message)
    }
}

func streamMessage(ctx context.Context, conv *sdk.Conversation, message string) {
    // Add timeout
    streamCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
    defer cancel()

    // Start streaming
    fmt.Print("Assistant: ")
    startTime := time.Now()

    stream, err := conv.SendStream(streamCtx, message)
    if err != nil {
        fmt.Printf("Error: %v\n\n", err)
        return
    }

    var (
        fullResponse strings.Builder
        chunkCount   int
        firstChunk   time.Duration
    )

    for chunk := range stream {
        if chunk.Error != nil {
            fmt.Printf("\nError: %v\n\n", chunk.Error)
            return
        }

        // Track first chunk time
        if chunkCount == 0 {
            firstChunk = time.Since(startTime)
        }

        // Display and collect
        fmt.Print(chunk.Content)
        os.Stdout.Sync()
        
        fullResponse.WriteString(chunk.Content)
        chunkCount++
    }

    // Show metrics
    totalTime := time.Since(startTime)
    fmt.Printf("\n\n[%d chunks, first in %v, total %v]\n\n",
        chunkCount, firstChunk, totalTime)
}
```

## What You've Learned

✅ Stream responses in real-time  
✅ Handle stream chunks and errors  
✅ Build interactive streaming chatbots  
✅ Collect full responses while streaming  
✅ Add timeouts and error handling  
✅ Optimize user experience with streaming  

## Next Steps

Continue to [Tutorial 3: Tool Integration](03-tool-integration.md) to learn how to add function calling to your LLM.

## Further Reading

- [How to Send Messages](../how-to/send-messages.md)
- [Streaming Best Practices](../explanation/streaming-architecture.md)
- [Provider Comparison](../../arena/explanation/provider-comparison.md)
