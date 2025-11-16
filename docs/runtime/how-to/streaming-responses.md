---
layout: default
title: Streaming Responses
parent: Runtime How-To
grand_parent: Runtime
nav_order: 6
---

# How to Handle Streaming Responses

Stream LLM responses for real-time output.

## Goal

Display LLM responses progressively as they're generated.

## Quick Start

```go
import "io"

stream, err := pipe.ExecuteStream(ctx, "user", "Write a story")
if err != nil {
    log.Fatal(err)
}
defer stream.Close()

for {
    chunk, err := stream.Next()
    if err == io.EOF {
        break
    }
    if err != nil {
        log.Printf("Stream error: %v", err)
        break
    }
    
    fmt.Print(chunk.Content)
}
```

## Basic Streaming

### Execute Stream

```go
ctx := context.Background()

stream, err := pipe.ExecuteStream(ctx, "user", "Generate a long document")
if err != nil {
    log.Fatal(err)
}
defer stream.Close()

// Process chunks
for {
    chunk, err := stream.Next()
    if err == io.EOF {
        break  // Stream complete
    }
    if err != nil {
        log.Fatal(err)
    }
    
    // Display chunk
    fmt.Print(chunk.Content)
}
```

### With Session Context

```go
sessionID := "user-123"

stream, err := pipe.ExecuteStreamWithContext(ctx, sessionID, "user", "Continue our conversation")
if err != nil {
    log.Fatal(err)
}
defer stream.Close()

for {
    chunk, err := stream.Next()
    if err == io.EOF {
        break
    }
    if err != nil {
        log.Fatal(err)
    }
    
    fmt.Print(chunk.Content)
}
```

## Chunk Processing

### Accumulate Content

```go
var accumulated string

stream, _ := pipe.ExecuteStream(ctx, "user", "Write code")
defer stream.Close()

for {
    chunk, err := stream.Next()
    if err == io.EOF {
        break
    }
    if err != nil {
        log.Fatal(err)
    }
    
    accumulated += chunk.Content
    fmt.Print(chunk.Content)
}

// Use final content
log.Printf("Total: %d chars", len(accumulated))
```

### Real-Time Display

```go
import "time"

stream, _ := pipe.ExecuteStream(ctx, "user", "Explain quantum physics")
defer stream.Close()

lastUpdate := time.Now()
minDelay := 50 * time.Millisecond

for {
    chunk, err := stream.Next()
    if err == io.EOF {
        break
    }
    if err != nil {
        break
    }
    
    // Throttle updates
    if time.Since(lastUpdate) >= minDelay {
        fmt.Print(chunk.Content)
        lastUpdate = time.Now()
    }
}
```

### Parse Structured Output

```go
import "strings"

stream, _ := pipe.ExecuteStream(ctx, "user", "List 5 items")
defer stream.Close()

var buffer string
var items []string

for {
    chunk, err := stream.Next()
    if err == io.EOF {
        break
    }
    if err != nil {
        break
    }
    
    buffer += chunk.Content
    
    // Parse lines as they arrive
    if strings.Contains(buffer, "\n") {
        lines := strings.Split(buffer, "\n")
        for i := 0; i < len(lines)-1; i++ {
            line := strings.TrimSpace(lines[i])
            if line != "" {
                items = append(items, line)
                log.Println("Found item:", line)
            }
        }
        buffer = lines[len(lines)-1]
    }
}

// Process remaining buffer
if buffer != "" {
    items = append(items, strings.TrimSpace(buffer))
}
```

## Web Streaming

### HTTP Server-Sent Events

```go
import "net/http"

func handleStream(w http.ResponseWriter, r *http.Request) {
    // Set SSE headers
    w.Header().Set("Content-Type", "text/event-stream")
    w.Header().Set("Cache-Control", "no-cache")
    w.Header().Set("Connection", "keep-alive")
    
    flusher, ok := w.(http.Flusher)
    if !ok {
        http.Error(w, "Streaming unsupported", http.StatusInternalServerError)
        return
    }
    
    prompt := r.URL.Query().Get("prompt")
    ctx := r.Context()
    
    stream, err := pipe.ExecuteStream(ctx, "user", prompt)
    if err != nil {
        http.Error(w, err.Error(), http.StatusInternalServerError)
        return
    }
    defer stream.Close()
    
    for {
        chunk, err := stream.Next()
        if err == io.EOF {
            fmt.Fprintf(w, "data: [DONE]\n\n")
            flusher.Flush()
            break
        }
        if err != nil {
            fmt.Fprintf(w, "event: error\ndata: %s\n\n", err.Error())
            flusher.Flush()
            break
        }
        
        // Send chunk
        fmt.Fprintf(w, "data: %s\n\n", chunk.Content)
        flusher.Flush()
    }
}

http.HandleFunc("/stream", handleStream)
http.ListenAndServe(":8080", nil)
```

### WebSocket Streaming

```go
import "github.com/gorilla/websocket"

var upgrader = websocket.Upgrader{
    CheckOrigin: func(r *http.Request) bool { return true },
}

func handleWebSocket(w http.ResponseWriter, r *http.Request) {
    conn, err := upgrader.Upgrade(w, r, nil)
    if err != nil {
        log.Println("Upgrade error:", err)
        return
    }
    defer conn.Close()
    
    // Read message
    _, message, err := conn.ReadMessage()
    if err != nil {
        return
    }
    
    ctx := r.Context()
    stream, err := pipe.ExecuteStream(ctx, "user", string(message))
    if err != nil {
        conn.WriteMessage(websocket.TextMessage, []byte(err.Error()))
        return
    }
    defer stream.Close()
    
    for {
        chunk, err := stream.Next()
        if err == io.EOF {
            conn.WriteMessage(websocket.TextMessage, []byte("[DONE]"))
            break
        }
        if err != nil {
            conn.WriteMessage(websocket.TextMessage, []byte(err.Error()))
            break
        }
        
        err = conn.WriteMessage(websocket.TextMessage, []byte(chunk.Content))
        if err != nil {
            break
        }
    }
}
```

## Stream Control

### Cancel Stream

```go
import "context"

ctx, cancel := context.WithCancel(context.Background())

stream, _ := pipe.ExecuteStream(ctx, "user", "Long response")
defer stream.Close()

// Cancel after 10 chunks
count := 0
for {
    chunk, err := stream.Next()
    if err == io.EOF {
        break
    }
    if err != nil {
        break
    }
    
    fmt.Print(chunk.Content)
    
    count++
    if count >= 10 {
        cancel()  // Stop stream
        break
    }
}
```

### Timeout Stream

```go
ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
defer cancel()

stream, err := pipe.ExecuteStream(ctx, "user", "Complex query")
if err != nil {
    log.Fatal(err)
}
defer stream.Close()

for {
    chunk, err := stream.Next()
    if err == io.EOF {
        break
    }
    if err != nil {
        if errors.Is(err, context.DeadlineExceeded) {
            log.Println("Stream timed out")
        }
        break
    }
    
    fmt.Print(chunk.Content)
}
```

## Error Handling

### Recover from Stream Errors

```go
stream, _ := pipe.ExecuteStream(ctx, "user", "Generate content")
defer stream.Close()

var accumulated string
var lastGoodChunk string

for {
    chunk, err := stream.Next()
    if err == io.EOF {
        break
    }
    if err != nil {
        log.Printf("Stream error: %v", err)
        
        // Use accumulated content
        if len(accumulated) > 0 {
            log.Println("Using partial response")
            fmt.Println(accumulated)
        }
        break
    }
    
    accumulated += chunk.Content
    lastGoodChunk = chunk.Content
    fmt.Print(chunk.Content)
}
```

### Retry on Error

```go
func streamWithRetry(pipe *pipeline.Pipeline, ctx context.Context, role, content string, maxRetries int) error {
    for i := 0; i < maxRetries; i++ {
        stream, err := pipe.ExecuteStream(ctx, role, content)
        if err != nil {
            log.Printf("Stream start failed (attempt %d/%d): %v", i+1, maxRetries, err)
            time.Sleep(time.Second * time.Duration(i+1))
            continue
        }
        defer stream.Close()
        
        // Process stream
        for {
            chunk, err := stream.Next()
            if err == io.EOF {
                return nil  // Success
            }
            if err != nil {
                log.Printf("Stream error (attempt %d/%d): %v", i+1, maxRetries, err)
                break  // Retry
            }
            
            fmt.Print(chunk.Content)
        }
        
        time.Sleep(time.Second * time.Duration(i+1))
    }
    
    return fmt.Errorf("stream failed after %d retries", maxRetries)
}
```

## Complete Example

```go
package main

import (
    "context"
    "fmt"
    "io"
    "log"
    "net/http"
    
    "github.com/AltairaLabs/PromptKit/runtime/pipeline"
    "github.com/AltairaLabs/PromptKit/runtime/pipeline/middleware"
    "github.com/AltairaLabs/PromptKit/runtime/providers/openai"
)

func main() {
    // Create provider
    provider := openai.NewOpenAIProvider(
        "openai",
        "gpt-4o-mini",
        "",
        openai.DefaultProviderDefaults(),
        false,
    )
    defer provider.Close()
    
    // Build pipeline
    pipe := pipeline.NewPipeline(
        middleware.ProviderMiddleware(provider, nil, nil, &middleware.ProviderMiddlewareConfig{
            MaxTokens:   1500,
            Temperature: 0.7,
        }),
    )
    defer pipe.Shutdown(context.Background())
    
    // Set up HTTP endpoint
    http.HandleFunc("/stream", func(w http.ResponseWriter, r *http.Request) {
        handleStreamRequest(w, r, pipe)
    })
    
    log.Println("Server running on :8080")
    http.ListenAndServe(":8080", nil)
}

func handleStreamRequest(w http.ResponseWriter, r *http.Request, pipe *pipeline.Pipeline) {
    // SSE headers
    w.Header().Set("Content-Type", "text/event-stream")
    w.Header().Set("Cache-Control", "no-cache")
    w.Header().Set("Connection", "keep-alive")
    
    flusher, ok := w.(http.Flusher)
    if !ok {
        http.Error(w, "Streaming unsupported", http.StatusInternalServerError)
        return
    }
    
    prompt := r.URL.Query().Get("prompt")
    if prompt == "" {
        http.Error(w, "Missing prompt parameter", http.StatusBadRequest)
        return
    }
    
    ctx := r.Context()
    stream, err := pipe.ExecuteStream(ctx, "user", prompt)
    if err != nil {
        http.Error(w, err.Error(), http.StatusInternalServerError)
        return
    }
    defer stream.Close()
    
    for {
        chunk, err := stream.Next()
        if err == io.EOF {
            fmt.Fprintf(w, "data: [DONE]\n\n")
            flusher.Flush()
            break
        }
        if err != nil {
            fmt.Fprintf(w, "event: error\ndata: %s\n\n", err.Error())
            flusher.Flush()
            break
        }
        
        fmt.Fprintf(w, "data: %s\n\n", chunk.Content)
        flusher.Flush()
    }
}
```

## Troubleshooting

### Issue: Chunks Arrive Slowly

**Problem**: Long delays between chunks.

**Solutions**:

1. Check network latency
2. Use faster model:
   ```go
   provider := openai.NewOpenAIProvider("openai", "gpt-4o-mini", ...)
   ```
3. Reduce max tokens:
   ```go
   config.MaxTokens = 500
   ```

### Issue: Stream Hangs

**Problem**: `stream.Next()` blocks indefinitely.

**Solutions**:

1. Add timeout:
   ```go
   ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
   defer cancel()
   ```

2. Check provider connection:
   ```go
   // Test non-streaming first
   result, err := pipe.Execute(ctx, "user", "test")
   ```

### Issue: Incomplete Responses

**Problem**: Stream ends prematurely.

**Solutions**:

1. Check for errors:
   ```go
   if err != io.EOF {
       log.Printf("Stream error: %v", err)
   }
   ```

2. Accumulate content:
   ```go
   if len(accumulated) > 0 {
       // Use partial response
   }
   ```

## Best Practices

1. **Always close streams**:
   ```go
   defer stream.Close()
   ```

2. **Handle EOF correctly**:
   ```go
   if err == io.EOF {
       break  // Normal completion
   }
   ```

3. **Set timeouts**:
   ```go
   ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
   defer cancel()
   ```

4. **Flush HTTP streams**:
   ```go
   flusher.Flush()
   ```

5. **Accumulate content for recovery**:
   ```go
   var accumulated string
   accumulated += chunk.Content
   ```

6. **Throttle UI updates**:
   ```go
   if time.Since(lastUpdate) >= minDelay {
       fmt.Print(chunk.Content)
   }
   ```

7. **Handle context cancellation**:
   ```go
   select {
   case <-ctx.Done():
       return
   default:
       // Continue
   }
   ```

## Next Steps

- [Handle Errors](handle-errors.md) - Error strategies
- [Monitor Costs](monitor-costs.md) - Track usage
- [Configure Pipeline](configure-pipeline.md) - Complete setup

## See Also

- [Pipeline Reference](../reference/pipeline.md) - Stream API
- [Providers Reference](../reference/providers.md) - Streaming support
