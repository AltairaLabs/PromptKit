---
title: Runtime Reference
sidebar:
  order: 0
---
Complete API reference for the PromptKit Runtime components.

## Overview

The PromptKit Runtime provides the core execution engine for LLM interactions. It handles:

- **Pipeline Execution**: Middleware-based processing with streaming support
- **Provider Integration**: Multi-LLM support (OpenAI, Anthropic, Google Gemini)
- **Tool Execution**: Function calling with MCP integration
- **State Management**: Conversation persistence and caching
- **Validation**: Content and response validation
- **Configuration**: Flexible runtime configuration

## Quick Reference

### Core Components

| Component | Description | Reference |
|-----------|-------------|-----------|
| **Pipeline** | Middleware-based execution engine | [pipeline.md](pipeline) |
| **Providers** | LLM provider implementations | [providers.md](providers) |
| **Tools** | Function calling and MCP integration | [tools.md](tools) |
| **MCP** | Model Context Protocol support | [mcp.md](mcp) |
| **State Store** | Conversation persistence | [statestore.md](statestore) |
| **Validators** | Content validation | [validators.md](validators) |
| **Types** | Core data structures | [types.md](types) |
| **Logging** | Structured logging with context | [logging.md](logging) |

### Import Paths

```go
import (
    "github.com/AltairaLabs/PromptKit/runtime/pipeline"
    "github.com/AltairaLabs/PromptKit/runtime/providers"
    "github.com/AltairaLabs/PromptKit/runtime/tools"
    "github.com/AltairaLabs/PromptKit/runtime/mcp"
    "github.com/AltairaLabs/PromptKit/runtime/statestore"
    "github.com/AltairaLabs/PromptKit/runtime/validators"
    "github.com/AltairaLabs/PromptKit/runtime/types"
    "github.com/AltairaLabs/PromptKit/runtime/logger"
)
```

## Basic Usage

### Simple Pipeline

```go
import (
    "context"
    "github.com/AltairaLabs/PromptKit/runtime/pipeline"
    "github.com/AltairaLabs/PromptKit/runtime/pipeline/middleware"
    "github.com/AltairaLabs/PromptKit/runtime/providers/openai"
)

// Create provider
provider := openai.NewOpenAIProvider(
    "openai",
    "gpt-4o-mini",
    "", // default baseURL
    openai.DefaultProviderDefaults(),
    false, // includeRawOutput
)

// Build pipeline with middleware
pipe := pipeline.NewPipeline(
    middleware.ProviderMiddleware(provider, nil, nil, &middleware.ProviderMiddlewareConfig{
        MaxTokens:   1500,
        Temperature: 0.7,
    }),
)

// Execute
result, err := pipe.Execute(ctx, "user", "Hello!")
if err != nil {
    log.Fatal(err)
}

fmt.Println(result.Response.Content)
```

### With Tools

```go
import (
    "github.com/AltairaLabs/PromptKit/runtime/tools"
    "github.com/AltairaLabs/PromptKit/runtime/mcp"
)

// Create tool registry
toolRegistry := tools.NewRegistry()

// Register MCP server
mcpRegistry := mcp.NewRegistry()
mcpRegistry.RegisterServer(mcp.ServerConfig{
    Name:    "filesystem",
    Command: "npx",
    Args:    []string{"-y", "@modelcontextprotocol/server-filesystem", "/allowed"},
})

// Discover and register MCP tools
mcpExecutor := tools.NewMCPExecutor(mcpRegistry)
toolRegistry.RegisterExecutor(mcpExecutor)

// Use in pipeline
pipe := pipeline.NewPipeline(
    middleware.ProviderMiddleware(provider, toolRegistry, &pipeline.ToolPolicy{
        ToolChoice: "auto",
        MaxRounds:  5,
    }, config),
)
```

### Streaming Execution

```go
// Execute with streaming
streamChan, err := pipe.ExecuteStream(ctx, "user", "Write a story")
if err != nil {
    log.Fatal(err)
}

// Process chunks
for chunk := range streamChan {
    if chunk.Error != nil {
        log.Printf("Error: %v\n", chunk.Error)
        break
    }
    
    if chunk.Delta != "" {
        fmt.Print(chunk.Delta)
    }
    
    if chunk.FinalResult != nil {
        fmt.Printf("\n\nTotal tokens: %d\n", chunk.FinalResult.CostInfo.InputTokens)
    }
}
```

## Configuration

### Pipeline Configuration

```go
config := &pipeline.PipelineRuntimeConfig{
    MaxConcurrentExecutions: 100,        // Concurrent pipeline executions
    StreamBufferSize:        100,        // Stream chunk buffer size
    ExecutionTimeout:        30 * time.Second,  // Per-execution timeout
    GracefulShutdownTimeout: 10 * time.Second,  // Shutdown grace period
}

pipe := pipeline.NewPipelineWithConfig(config, middleware...)
```

### Provider Configuration

```go
defaults := providers.ProviderDefaults{
    Temperature: 0.7,
    TopP:        0.95,
    MaxTokens:   2000,
    Pricing: providers.Pricing{
        InputCostPer1K:  0.00015,  // $0.15 per 1M tokens
        OutputCostPer1K: 0.0006,   // $0.60 per 1M tokens
    },
}

provider := openai.NewOpenAIProvider("openai", "gpt-4o-mini", "", defaults, false)
```

### Tool Policy

```go
policy := &pipeline.ToolPolicy{
    ToolChoice:          "auto",     // "auto", "required", "none", or specific tool
    MaxRounds:           5,          // Max tool execution rounds
    MaxToolCallsPerTurn: 10,         // Max tools per LLM response
    Blocklist:           []string{"dangerous_tool"},  // Blocked tools
}
```

## Error Handling

### Pipeline Errors

```go
result, err := pipe.Execute(ctx, "user", "Hello")
if err != nil {
    switch {
    case errors.Is(err, pipeline.ErrPipelineShuttingDown):
        log.Println("Pipeline is shutting down")
    case errors.Is(err, context.DeadlineExceeded):
        log.Println("Execution timeout")
    default:
        log.Printf("Execution failed: %v", err)
    }
}
```

### Provider Errors

```go
result, err := provider.Predict(ctx, req)
if err != nil {
    // Check for rate limiting, API errors, network errors
    log.Printf("Provider error: %v", err)
}
```

### Tool Errors

```go
result, err := toolRegistry.Execute(ctx, "tool_name", argsJSON)
if err != nil {
    log.Printf("Tool execution failed: %v", err)
}
```

## Best Practices

### Resource Management

```go
// Always close resources
defer pipe.Shutdown(context.Background())
defer provider.Close()
defer mcpRegistry.Close()
```

### Context Cancellation

```go
// Use context for cancellation
ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
defer cancel()

result, err := pipe.Execute(ctx, "user", "Hello")
```

### Streaming Cleanup

```go
// Always drain streaming channels
streamChan, err := pipe.ExecuteStream(ctx, "user", "Hello")
if err != nil {
    return err
}

for chunk := range streamChan {
    // Process chunks
    if chunk.Error != nil {
        break
    }
}
```

### Error Recovery

```go
// Handle partial results on error
result, err := pipe.Execute(ctx, "user", "Hello")
if err != nil {
    // Check if we got partial execution data
    if result != nil && len(result.Messages) > 0 {
        log.Printf("Partial execution: %d messages", len(result.Messages))
    }
}
```

## Performance Considerations

### Concurrency Control

- Configure `MaxConcurrentExecutions` based on provider rate limits
- Use semaphores to prevent overwhelming providers
- Consider graceful degradation under load

### Streaming vs Non-Streaming

- **Use streaming** for interactive applications (chatbots, UIs)
- **Use non-streaming** for batch processing, testing, analytics
- Streaming has ~10% overhead but better UX

### Tool Execution

- MCP tools run in separate processes (stdio overhead)
- Consider tool execution timeouts
- Use repository executors for fast in-memory tools

### Memory Management

- Pipeline creates fresh `ExecutionContext` per call (prevents contamination)
- Large conversation histories can increase memory usage
- Consider state store cleanup strategies

## See Also

- [Pipeline Reference](pipeline) - Detailed pipeline API
- [Provider Reference](providers) - Provider implementations
- [Tools Reference](tools) - Tool registry and execution
- [MCP Reference](mcp) - Model Context Protocol integration
- [Types Reference](types) - Core data structures

## Next Steps

- [How-To Guides](../how-to/) - Task-focused guides
- [Tutorials](../tutorials/) - Learn by building
- [Explanation](../explanation/) - Architecture and concepts
