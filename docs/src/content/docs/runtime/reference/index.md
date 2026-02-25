---
title: Runtime Reference
sidebar:
  order: 0
---
Complete API reference for the PromptKit Runtime components.

## Overview

The PromptKit Runtime provides the core execution engine for LLM interactions. It handles:

- **Pipeline Execution**: Stage-based processing with streaming support
- **Provider Integration**: Multi-LLM support (OpenAI, Anthropic, Google Gemini)
- **Tool Execution**: Function calling with MCP integration
- **State Management**: Conversation persistence and caching
- **Validation**: Content and response validation
- **Configuration**: Flexible runtime configuration

## Quick Reference

### Core Components

| Component | Description | Reference |
|-----------|-------------|-----------|
| **Pipeline** | Stage-based execution engine | [pipeline.md](pipeline) |
| **Providers** | LLM provider implementations | [providers.md](providers) |
| **Tools** | Function calling and MCP integration | [tools.md](tools) |
| **MCP** | Model Context Protocol support | [mcp.md](mcp) |
| **State Store** | Conversation persistence | [statestore.md](statestore) |
| **Validators** | Content validation | [validators.md](validators) |
| **Types** | Core data structures | [types.md](types) |
| **A2A** | Client, types, tool bridge, mock | [a2a.md](a2a) |
| **Logging** | Structured logging with context | [logging.md](logging) |
| **Telemetry** | OpenTelemetry trace export | [telemetry.md](telemetry) |

### Import Paths

```go
import (
    "github.com/AltairaLabs/PromptKit/runtime/pipeline"
    "github.com/AltairaLabs/PromptKit/runtime/providers"
    "github.com/AltairaLabs/PromptKit/runtime/tools"
    "github.com/AltairaLabs/PromptKit/runtime/mcp"
    "github.com/AltairaLabs/PromptKit/runtime/statestore"
    "github.com/AltairaLabs/PromptKit/runtime/hooks"
    "github.com/AltairaLabs/PromptKit/runtime/hooks/guardrails"
    "github.com/AltairaLabs/PromptKit/runtime/types"
    "github.com/AltairaLabs/PromptKit/runtime/logger"
    "github.com/AltairaLabs/PromptKit/runtime/telemetry"
)
```

## Basic Usage

### Simple Provider Usage

```go
import (
    "context"
    "github.com/AltairaLabs/PromptKit/runtime/providers"
    "github.com/AltairaLabs/PromptKit/runtime/providers/openai"
    "github.com/AltairaLabs/PromptKit/runtime/types"
)

// Create provider
provider := openai.NewProvider(
    "openai",
    "gpt-4o-mini",
    "", // default baseURL (uses env var for API key)
    providers.ProviderDefaults{Temperature: 0.7, MaxTokens: 1500},
    false, // includeRawOutput
)
defer provider.Close()

// Execute prediction
ctx := context.Background()
resp, err := provider.Predict(ctx, providers.PredictionRequest{
    Messages: []types.Message{
        {Role: "user", Content: "Hello!"},
    },
    Temperature: 0.7,
    MaxTokens:   1500,
})
if err != nil {
    log.Fatal(err)
}

fmt.Println(resp.Content)
```

### With MCP Tools

```go
import (
    "github.com/AltairaLabs/PromptKit/runtime/mcp"
)

// Register MCP server
mcpRegistry := mcp.NewRegistry()
defer mcpRegistry.Close()

mcpRegistry.RegisterServer(mcp.ServerConfig{
    Name:    "filesystem",
    Command: "npx",
    Args:    []string{"-y", "@modelcontextprotocol/server-filesystem", "/allowed"},
})

// Discover tools
ctx := context.Background()
serverTools, err := mcpRegistry.ListAllTools(ctx)
if err != nil {
    log.Fatal(err)
}

for serverName, tools := range serverTools {
    log.Printf("Server %s has %d tools\n", serverName, len(tools))
}
```

### Streaming Execution

```go
// Execute with streaming
streamChan, err := provider.PredictStream(ctx, providers.PredictionRequest{
    Messages:    []types.Message{{Role: "user", Content: "Write a story"}},
    Temperature: 0.7,
    MaxTokens:   1500,
})
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

    if chunk.FinishReason != nil {
        fmt.Printf("\n\nStream complete: %s\n", *chunk.FinishReason)
    }
}
```

## Configuration

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

provider := openai.NewProvider("openai", "gpt-4o-mini", "", defaults, false)
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

### Provider Errors

```go
resp, err := provider.Predict(ctx, req)
if err != nil {
    switch {
    case errors.Is(err, context.DeadlineExceeded):
        log.Println("Request timeout")
    default:
        log.Printf("Provider error: %v", err)
    }
}
```

### Tool Errors

```go
// MCP tool call errors
response, err := client.CallTool(ctx, "read_file", args)
if err != nil {
    log.Printf("Tool execution failed: %v", err)
}
```

## Best Practices

### Resource Management

```go
// Always close resources
defer provider.Close()
defer mcpRegistry.Close()
```

### Context Cancellation

```go
// Use context for cancellation
ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
defer cancel()

resp, err := provider.Predict(ctx, req)
```

### Streaming Cleanup

```go
// Always drain streaming channels
streamChan, err := provider.PredictStream(ctx, req)
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
