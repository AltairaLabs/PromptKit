---
title: Monitor Events
docType: how-to
order: 5
---
# How to Monitor Events

Learn how to observe SDK operations with `Subscribe()`.

## Basic Subscription

```go
import "github.com/AltairaLabs/PromptKit/sdk/hooks"

conv.Subscribe(hooks.EventSend, func(e hooks.Event) {
    fmt.Printf("Sent: %v\n", e.Data["message"])
})
```

## Event Types

```go
const (
    EventSend       = "send"        // Message sent
    EventResponse   = "response"    // Response received
    EventToolCall   = "tool_call"   // Tool invoked
    EventToolResult = "tool_result" // Tool returned
    EventError      = "error"       // Error occurred
    EventStream     = "stream"      // Stream chunk
)
```

## Monitor Tool Calls

```go
conv.Subscribe(hooks.EventToolCall, func(e hooks.Event) {
    toolName := e.Data["tool"].(string)
    args := e.Data["args"]
    fmt.Printf("Tool called: %s(%v)\n", toolName, args)
})

conv.Subscribe(hooks.EventToolResult, func(e hooks.Event) {
    toolName := e.Data["tool"].(string)
    result := e.Data["result"]
    fmt.Printf("Tool result: %s -> %v\n", toolName, result)
})
```

## Monitor Errors

```go
conv.Subscribe(hooks.EventError, func(e hooks.Event) {
    err := e.Data["error"]
    log.Printf("Error: %v", err)
})
```

## Monitor Streaming

```go
var charCount int

conv.Subscribe(hooks.EventStream, func(e hooks.Event) {
    chunk := e.Data["chunk"].(string)
    charCount += len(chunk)
})
```

## Log All Events

```go
func attachLogger(conv *sdk.Conversation) {
    events := []string{
        hooks.EventSend,
        hooks.EventResponse,
        hooks.EventToolCall,
        hooks.EventToolResult,
        hooks.EventError,
    }
    
    for _, event := range events {
        name := event
        conv.Subscribe(name, func(e hooks.Event) {
            log.Printf("[%s] %v", name, e.Data)
        })
    }
}
```

## Event Structure

```go
type Event struct {
    Type      string         // Event type
    Timestamp time.Time      // When it occurred
    Data      map[string]any // Event-specific data
}
```

## Metrics Collection

```go
type Metrics struct {
    Messages  int64
    ToolCalls int64
    Errors    int64
    mu        sync.Mutex
}

func (m *Metrics) Attach(conv *sdk.Conversation) {
    conv.Subscribe(hooks.EventSend, func(e hooks.Event) {
        m.mu.Lock()
        m.Messages++
        m.mu.Unlock()
    })
    
    conv.Subscribe(hooks.EventToolCall, func(e hooks.Event) {
        m.mu.Lock()
        m.ToolCalls++
        m.mu.Unlock()
    })
    
    conv.Subscribe(hooks.EventError, func(e hooks.Event) {
        m.mu.Lock()
        m.Errors++
        m.mu.Unlock()
    })
}
```

## Debug Mode

```go
func enableDebug(conv *sdk.Conversation) {
    conv.Subscribe(hooks.EventSend, func(e hooks.Event) {
        fmt.Printf("📤 %v\n", e.Data)
    })
    conv.Subscribe(hooks.EventResponse, func(e hooks.Event) {
        fmt.Printf("📥 %v\n", e.Data)
    })
    conv.Subscribe(hooks.EventToolCall, func(e hooks.Event) {
        fmt.Printf("🔧 %v\n", e.Data)
    })
    conv.Subscribe(hooks.EventError, func(e hooks.Event) {
        fmt.Printf("❌ %v\n", e.Data)
    })
}
```

## Complete Example

```go
package main

import (
    "context"
    "fmt"
    "log"

    "github.com/AltairaLabs/PromptKit/sdk"
    "github.com/AltairaLabs/PromptKit/sdk/hooks"
)

func main() {
    conv, _ := sdk.Open("./app.pack.json", "chat")
    defer conv.Close()

    // Monitor all activity
    conv.Subscribe(hooks.EventSend, func(e hooks.Event) {
        log.Printf("→ Sending message")
    })
    
    conv.Subscribe(hooks.EventResponse, func(e hooks.Event) {
        log.Printf("← Got response")
    })
    
    conv.Subscribe(hooks.EventError, func(e hooks.Event) {
        log.Printf("⚠ Error: %v", e.Data["error"])
    })

    // Use normally
    ctx := context.Background()
    resp, _ := conv.Send(ctx, "Hello!")
    fmt.Println(resp.Text())
}
```

## See Also

- [Tutorial 6: Observability](../tutorials/06-media-storage)
- [Explanation: Observability](../explanation/observability)
