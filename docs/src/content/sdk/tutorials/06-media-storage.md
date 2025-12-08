---
title: 'Tutorial 6: Observability'
docType: tutorial
order: 6
---
# Tutorial 6: Observability

Learn how to monitor and observe your SDK applications with the hooks package.

## What You'll Learn

- Subscribe to SDK events
- Monitor tool calls and responses
- Track conversation flow
- Debug issues

## Why Observability?

Observability helps you:

- **Debug** - Understand what's happening in your application
- **Monitor** - Track performance and usage
- **Audit** - Log all actions for compliance
- **Optimize** - Find bottlenecks and improve

## Prerequisites

Complete [Tutorial 1](01-first-conversation) and understand basic SDK usage.

## Basic Event Subscription

Use `Subscribe()` to listen for events:

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
    conv, err := sdk.Open("./hello.pack.json", "chat")
    if err != nil {
        log.Fatal(err)
    }
    defer conv.Close()

    // Subscribe to send events
    conv.Subscribe(hooks.EventSend, func(e hooks.Event) {
        fmt.Printf("[SEND] Message: %v\n", e.Data["message"])
    })

    // Subscribe to response events
    conv.Subscribe(hooks.EventResponse, func(e hooks.Event) {
        fmt.Printf("[RESPONSE] Got reply\n")
    })

    ctx := context.Background()
    resp, _ := conv.Send(ctx, "Hello!")
    fmt.Println(resp.Text())
}
```

## Event Types

The hooks package provides these event types:

```go
const (
    EventSend       = "send"        // Message sent
    EventResponse   = "response"    // Response received
    EventToolCall   = "tool_call"   // Tool invoked
    EventToolResult = "tool_result" // Tool returned
    EventError      = "error"       // Error occurred
    EventStream     = "stream"      // Stream chunk received
)
```

## Monitoring Tool Calls

Track tool execution:

```go
conv.Subscribe(hooks.EventToolCall, func(e hooks.Event) {
    toolName := e.Data["tool"].(string)
    args := e.Data["args"]
    fmt.Printf("[TOOL] Calling %s with %v\n", toolName, args)
})

conv.Subscribe(hooks.EventToolResult, func(e hooks.Event) {
    toolName := e.Data["tool"].(string)
    result := e.Data["result"]
    fmt.Printf("[TOOL] %s returned: %v\n", toolName, result)
})
```

## Logging All Events

Create a comprehensive logger:

```go
func logAllEvents(conv *sdk.Conversation) {
    events := []string{
        hooks.EventSend,
        hooks.EventResponse,
        hooks.EventToolCall,
        hooks.EventToolResult,
        hooks.EventError,
        hooks.EventStream,
    }
    
    for _, event := range events {
        eventName := event // Capture for closure
        conv.Subscribe(eventName, func(e hooks.Event) {
            log.Printf("[%s] %s: %v", 
                e.Timestamp.Format("15:04:05"),
                eventName,
                e.Data,
            )
        })
    }
}

// Usage
conv, _ := sdk.Open("./pack.json", "chat")
logAllEvents(conv)
```

## Error Monitoring

Track and alert on errors:

```go
conv.Subscribe(hooks.EventError, func(e hooks.Event) {
    err := e.Data["error"]
    
    // Log the error
    log.Printf("[ERROR] %v", err)
    
    // Alert if critical
    if isCritical(err) {
        alertTeam(err)
    }
})
```

## Stream Monitoring

Track streaming progress:

```go
var charCount int

conv.Subscribe(hooks.EventStream, func(e hooks.Event) {
    chunk := e.Data["chunk"].(string)
    charCount += len(chunk)
    
    // Log progress every 100 characters
    if charCount % 100 == 0 {
        log.Printf("[STREAM] %d characters received", charCount)
    }
})
```

## Metrics Collection

Collect metrics for monitoring systems:

```go
type Metrics struct {
    MessageCount  int64
    ToolCalls     int64
    Errors        int64
    TotalTokens   int64
    mu            sync.Mutex
}

func (m *Metrics) Attach(conv *sdk.Conversation) {
    conv.Subscribe(hooks.EventSend, func(e hooks.Event) {
        m.mu.Lock()
        m.MessageCount++
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

Enable verbose debugging:

```go
func enableDebug(conv *sdk.Conversation) {
    conv.Subscribe(hooks.EventSend, func(e hooks.Event) {
        fmt.Printf("📤 SEND: %v\n", e.Data)
    })
    
    conv.Subscribe(hooks.EventResponse, func(e hooks.Event) {
        fmt.Printf("📥 RESPONSE: %v\n", e.Data)
    })
    
    conv.Subscribe(hooks.EventToolCall, func(e hooks.Event) {
        fmt.Printf("🔧 TOOL CALL: %s(%v)\n", 
            e.Data["tool"], e.Data["args"])
    })
    
    conv.Subscribe(hooks.EventToolResult, func(e hooks.Event) {
        fmt.Printf("✅ TOOL RESULT: %v\n", e.Data["result"])
    })
    
    conv.Subscribe(hooks.EventError, func(e hooks.Event) {
        fmt.Printf("❌ ERROR: %v\n", e.Data["error"])
    })
}
```

## What You've Learned

✅ Subscribe to events with `Subscribe()`  
✅ Monitor tool calls and responses  
✅ Track errors and stream progress  
✅ Build metrics collection  
✅ Enable debug logging  

## Next Steps

- **[How-To: Monitor Events](../how-to/monitor-events)** - Advanced monitoring
- **[Explanation: Observability](../explanation/observability)** - Architecture deep-dive

## Complete Example

See observability patterns in `sdk/examples/`.
