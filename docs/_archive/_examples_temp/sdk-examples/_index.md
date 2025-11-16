---
layout: default
title: SDK Examples
parent: Guides
nav_order: 10
has_children: true
---

# PromptKit SDK Examples

This directory contains examples demonstrating the PromptKit SDK's high-level and low-level APIs.

## Directory Structure

- `basic/` - Simple conversation using ConversationManager (high-level API)
- `streaming/` - Streaming conversation with real-time responses
- `tools/` - Tool usage and execution
- `custom-middleware/` - Custom middleware with PipelineBuilder (low-level API)
- `observability/` - LangFuse/DataDog integration examples
- `web-api/` - Multi-tenant web API with concurrent conversations

## Quick Start

### High-Level API (ConversationManager)

```go
package main

import (
    "context"
    "fmt"
    "log"

    "github.com/AltairaLabs/PromptKit/sdk"
    "github.com/AltairaLabs/PromptKit/runtime/providers"
)

func main() {
    // Create provider
    provider := providers.NewOpenAIProvider("your-api-key", "gpt-4", false)
    
    // Create conversation manager
    manager, err := sdk.NewConversationManager(
        sdk.WithProvider(provider),
    )
    if err != nil {
        log.Fatal(err)
    }
    
    // Load pack
    pack, err := manager.LoadPack("./prompts/support.pack.json")
    if err != nil {
        log.Fatal(err)
    }
    
    // Create conversation
    ctx := context.Background()
    conv, err := manager.NewConversation(ctx, pack, sdk.ConversationConfig{
        UserID:     "user123",
        PromptName: "support",
        Variables: map[string]interface{}{
            "role":    "customer support",
            "company": "ACME Corp",
        },
    })
    if err != nil {
        log.Fatal(err)
    }
    
    // Send messages
    resp, err := conv.Send(ctx, "I need help with my order")
    if err != nil {
        log.Fatal(err)
    }
    
    fmt.Printf("Assistant: %s\n", resp.Content)
    fmt.Printf("Cost: $%.4f\n", resp.Cost)
}
```

### Low-Level API (PipelineBuilder)

```go
package main

import (
    "context"
    "fmt"
    "log"
    "time"

    "github.com/AltairaLabs/PromptKit/sdk"
    "github.com/AltairaLabs/PromptKit/runtime/pipeline"
    "github.com/AltairaLabs/PromptKit/runtime/providers"
)

// Custom observability middleware
type MetricsMiddleware struct {
    serviceName string
}

func (m *MetricsMiddleware) Process(execCtx *pipeline.ExecutionContext, next func() error) error {
    start := time.Now()
    
    // Execute pipeline
    err := next()
    
    duration := time.Since(start)
    
    // Record metrics
    fmt.Printf("[Metrics] Service: %s, Duration: %v, Tokens: %d, Cost: $%.4f\n",
        m.serviceName,
        duration,
        execCtx.CostInfo.InputTokens + execCtx.CostInfo.OutputTokens,
        execCtx.CostInfo.TotalCost,
    )
    
    return err
}

func (m *MetricsMiddleware) StreamChunk(execCtx *pipeline.ExecutionContext, chunk *providers.StreamChunk) error {
    return nil
}

func main() {
    // Create provider
    provider := providers.NewOpenAIProvider("your-api-key", "gpt-4", false)
    
    // Build custom pipeline
    pipe := sdk.NewPipelineBuilder().
        WithMiddleware(&MetricsMiddleware{serviceName: "chat-api"}).
        WithProvider(provider).
        Build()
    
    // Execute
    ctx := context.Background()
    result, err := pipe.Execute(ctx, "user", "What is the meaning of life?")
    if err != nil {
        log.Fatal(err)
    }
    
    fmt.Printf("Response: %s\n", result.Response.Content)
}
```

## Running Examples

Each example can be run with:

```bash
cd examples/basic
go run main.go
```

Make sure to set your API keys as environment variables:

```bash
export OPENAI_API_KEY=your-key
export ANTHROPIC_API_KEY=your-key
```
