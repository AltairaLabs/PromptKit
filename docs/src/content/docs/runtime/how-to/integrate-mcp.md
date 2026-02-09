---
title: Integrate MCP
sidebar:
  order: 3
---
Connect Model Context Protocol servers for external tools.

## Goal

Set up MCP servers and integrate tools into your pipeline.

## Quick Start

### Step 1: Create MCP Registry

```go
import "github.com/AltairaLabs/PromptKit/runtime/mcp"

registry := mcp.NewRegistry()
defer registry.Close()
```

### Step 2: Register MCP Server

```go
err := registry.RegisterServer(mcp.ServerConfig{
    Name:    "filesystem",
    Command: "npx",
    Args:    []string{"-y", "@modelcontextprotocol/server-filesystem", "/allowed"},
})
if err != nil {
    log.Fatal(err)
}
```

### Step 3: Discover Tools

```go
ctx := context.Background()
serverTools, err := registry.ListAllTools(ctx)
if err != nil {
    log.Fatal(err)
}

for serverName, tools := range serverTools {
    log.Printf("Server %s has %d tools\n", serverName, len(tools))
}
```

### Step 4: Integrate with Pipeline

```go
// Create tool registry
toolRegistry := tools.NewRegistry()

// Register MCP executor
mcpExecutor := tools.NewMCPExecutor(registry)
toolRegistry.RegisterExecutor(mcpExecutor)

// Register MCP tools
for _, mcpTools := range serverTools {
    for _, mcpTool := range mcpTools {
        toolRegistry.Register(&tools.ToolDescriptor{
            Name:        mcpTool.Name,
            Description: mcpTool.Description,
            InputSchema: mcpTool.InputSchema,
            Mode:        "mcp",
        })
    }
}

// Use in pipeline
pipe := pipeline.NewPipeline(
    middleware.ProviderMiddleware(provider, toolRegistry, &pipeline.ToolPolicy{
        ToolChoice: "auto",
    }, config),
)
```

## Common MCP Servers

### Filesystem Server

```go
registry.RegisterServer(mcp.ServerConfig{
    Name:    "filesystem",
    Command: "npx",
    Args:    []string{"-y", "@modelcontextprotocol/server-filesystem", "/data"},
})
```

**Available Tools**:
- `read_file`: Read file contents
- `write_file`: Write file contents
- `list_directory`: List directory contents
- `create_directory`: Create directory
- `delete_file`: Delete file

### Memory Server

```go
registry.RegisterServer(mcp.ServerConfig{
    Name:    "memory",
    Command: "npx",
    Args:    []string{"-y", "@modelcontextprotocol/server-memory"},
})
```

**Available Tools**:
- `store_memory`: Store key-value data
- `retrieve_memory`: Retrieve stored data

### Custom Python Server

```go
registry.RegisterServer(mcp.ServerConfig{
    Name:    "database",
    Command: "python",
    Args:    []string{"/path/to/mcp_server.py"},
    Env: map[string]string{
        "DB_CONNECTION": os.Getenv("DB_CONNECTION"),
    },
})
```

## Complete Example

```go
package main

import (
    "context"
    "log"
    
    "github.com/AltairaLabs/PromptKit/runtime/mcp"
    "github.com/AltairaLabs/PromptKit/runtime/pipeline"
    "github.com/AltairaLabs/PromptKit/runtime/pipeline/middleware"
    "github.com/AltairaLabs/PromptKit/runtime/providers/openai"
    "github.com/AltairaLabs/PromptKit/runtime/tools"
)

func main() {
    // Create MCP registry
    mcpRegistry := mcp.NewRegistry()
    defer mcpRegistry.Close()
    
    // Register filesystem server
    mcpRegistry.RegisterServer(mcp.ServerConfig{
        Name:    "filesystem",
        Command: "npx",
        Args:    []string{"-y", "@modelcontextprotocol/server-filesystem", "/data"},
    })
    
    // Create tool registry
    toolRegistry := tools.NewRegistry()
    toolRegistry.RegisterExecutor(tools.NewMCPExecutor(mcpRegistry))
    
    // Discover and register tools
    ctx := context.Background()
    serverTools, _ := mcpRegistry.ListAllTools(ctx)
    for _, mcpTools := range serverTools {
        for _, mcpTool := range mcpTools {
            toolRegistry.Register(&tools.ToolDescriptor{
                Name:        mcpTool.Name,
                Description: mcpTool.Description,
                InputSchema: mcpTool.InputSchema,
                Mode:        "mcp",
            })
        }
    }
    
    // Create provider
    provider := openai.NewProvider(
        "openai",
        "gpt-4o-mini",
        "",
        providers.ProviderDefaults{Temperature: 0.7, MaxTokens: 2000},
        false,
    )
    defer provider.Close()
    
    // Build pipeline with tools
    pipe := pipeline.NewPipeline(
        middleware.ProviderMiddleware(provider, toolRegistry, &pipeline.ToolPolicy{
            ToolChoice: "auto",
            MaxRounds:  5,
        }, &middleware.ProviderMiddlewareConfig{
            MaxTokens:   1500,
            Temperature: 0.7,
        }),
    )
    defer pipe.Shutdown(context.Background())
    
    // Execute with tool access
    result, err := pipe.Execute(ctx, "user", "Read the contents of /data/example.txt")
    if err != nil {
        log.Fatal(err)
    }
    
    log.Printf("Response: %s\n", result.Response.Content)
}
```

## MCP Client Configuration

### Timeouts

```go
options := mcp.ClientOptions{
    RequestTimeout: 30 * time.Second,
    MaxRetries:     3,
    RetryBackoff:   time.Second,
}

client := mcp.NewStdioClientWithOptions(config, options)
```

### Manual Client Usage

```go
// Get client for specific server
client, err := registry.GetClient(ctx, "filesystem")
if err != nil {
    log.Fatal(err)
}

// List available tools
tools, err := client.ListTools(ctx)
for _, tool := range tools {
    log.Printf("Tool: %s - %s\n", tool.Name, tool.Description)
}

// Call tool directly
args := json.RawMessage(`{"path": "/data/file.txt"}`)
response, err := client.CallTool(ctx, "read_file", args)
if err != nil {
    log.Fatal(err)
}

// Process response
for _, content := range response.Content {
    if content.Type == "text" {
        log.Println(content.Text)
    }
}
```

## Tool Discovery

### Automatic Discovery

```go
// Discover all tools from all servers
serverTools, err := registry.ListAllTools(ctx)
if err != nil {
    log.Fatal(err)
}

for serverName, tools := range serverTools {
    log.Printf("Server: %s\n", serverName)
    for _, tool := range tools {
        log.Printf("  - %s: %s\n", tool.Name, tool.Description)
        log.Printf("    Schema: %s\n", tool.InputSchema)
    }
}
```

### Get Tool Schema

```go
schema, err := registry.GetToolSchema(ctx, "read_file")
if err != nil {
    log.Fatal(err)
}

log.Printf("Tool: %s\n", schema.Name)
log.Printf("Description: %s\n", schema.Description)
log.Printf("Schema: %s\n", schema.InputSchema)
```

## Tool Execution

### Through Tool Registry

```go
// Register MCP tool
toolRegistry.Register(&tools.ToolDescriptor{
    Name:        "read_file",
    Description: "Read file contents",
    InputSchema: json.RawMessage(`{"type":"object","properties":{"path":{"type":"string"}}}`),
    Mode:        "mcp",
})

// Execute
args := json.RawMessage(`{"path": "/data/file.txt"}`)
result, err := toolRegistry.Execute(toolDescriptor, args)
if err != nil {
    log.Fatal(err)
}

log.Printf("Result: %s\n", result)
```

### Direct MCP Call

```go
client, _ := registry.GetClientForTool(ctx, "read_file")
response, err := client.CallTool(ctx, "read_file", args)
```

## Error Handling

### Server Connection Errors

```go
client, err := registry.GetClient(ctx, "filesystem")
if err != nil {
    if strings.Contains(err.Error(), "not found") {
        log.Println("Server not registered")
    } else {
        log.Printf("Connection error: %v", err)
    }
    return
}
```

### Tool Execution Errors

```go
response, err := client.CallTool(ctx, "read_file", args)
if err != nil {
    log.Printf("Tool execution failed: %v", err)
    return
}

// Check response for errors
for _, content := range response.Content {
    if content.Type == "error" {
        log.Printf("Tool error: %s", content.Text)
    }
}
```

## Troubleshooting

### Issue: Server Won't Start

**Problem**: MCP server command fails.

**Solutions**:
1. Check command is installed:
   ```bash
   which npx
   npx -v
   ```

2. Test command manually:
   ```bash
   npx -y @modelcontextprotocol/server-filesystem /data
   ```

3. Check server logs:
   ```go
   // Enable debug logging
   config.Env = map[string]string{"DEBUG": "1"}
   ```

### Issue: Tools Not Discovered

**Problem**: `ListAllTools` returns empty.

**Solution**: Ensure server initialized:
```go
// Force initialization
client, err := registry.GetClient(ctx, "filesystem")
if err != nil {
    log.Fatal(err)
}

// Now list tools
tools, err := client.ListTools(ctx)
```

### Issue: Tool Call Timeout

**Problem**: Tool execution hangs.

**Solution**: Increase timeout:
```go
ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
defer cancel()

response, err := client.CallTool(ctx, "read_file", args)
```

## Best Practices

1. **Always close MCP registry**:
   ```go
   defer registry.Close()
   ```

2. **Handle tool errors gracefully**:
   ```go
   if err != nil {
       log.Printf("Tool failed: %v", err)
       // Provide fallback behavior
   }
   ```

3. **Limit allowed paths for filesystem server**:
   ```go
   Args: []string{"-y", "@modelcontextprotocol/server-filesystem", "/allowed/path"},
   ```

4. **Use environment variables for sensitive config**:
   ```go
   Env: map[string]string{
       "API_KEY": os.Getenv("API_KEY"),
   },
   ```

5. **Set reasonable tool policies**:
   ```go
   policy := &pipeline.ToolPolicy{
       MaxRounds:           5,
       MaxToolCallsPerTurn: 10,
   }
   ```

## Next Steps

- [Implement Tools](implement-tools) - Create custom tools
- [Validate Tools](validate-tools) - Tool validation
- [Configure Pipeline](configure-pipeline) - Complete setup

## See Also

- [MCP Reference](../reference/tools-mcp) - Complete API
- [MCP Tutorial](../tutorials/03-mcp-integration) - Step-by-step guide
