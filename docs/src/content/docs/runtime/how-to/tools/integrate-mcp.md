---
title: Integrate MCP
sidebar:
  order: 3
---
Connect Model Context Protocol servers for external tools.

## Goal

Set up MCP servers and integrate tools into your pipeline.

## Transports

MCP servers can be reached over three transports, selected by which `ServerConfig` fields are set:

- **stdio** (default when `Command` is set) — PromptKit spawns the MCP server as a local
  subprocess. Set `Command` and optional `Args` / `Env`.
- **Streamable HTTP** (default when `URL` is set) — the single-endpoint POST transport,
  introduced in MCP 2025-03-26. If the server does not host a Streamable HTTP endpoint at the
  URL (it refuses the POST with 400, 404 or 405), the client falls back to HTTP+SSE.
- **HTTP+SSE** — the deprecated two-endpoint transport from MCP 2024-11-05. Set
  `TransportName: mcp.TransportSSE` to use it without trying Streamable HTTP first.

Over stdio and Streamable HTTP the client speaks both generations of the protocol. It
first asks the server which revisions it supports (`server/discover`). A 2026-07-28 server is
then used statelessly: every request carries the protocol version and client capabilities, and
there is no handshake or session. A server that predates discovery gets the `initialize`
handshake of revisions up to 2025-11-25. Set `ClientOptions.DisableModernProtocol` to skip the
discovery probe for a server that misbehaves when it receives one.

## Spec support

<!-- BEGIN GENERATED: mcp-spec-support. Do not edit; run `make mcp-spec-docs`. -->

PromptKit's MCP client implements protocol revision **2025-11-25** (`mcp.ProtocolVersion`). CI checks every message type it sends or reads against that revision's published schema, so the table below is the complete list of spec fields the client does not carry; every other field is carried.

That check covers message fields. Behaviour is checked by scenario tests and by the official [MCP conformance suite](https://github.com/modelcontextprotocol/conformance) (`make mcp-conformance`); known gaps are tracked in [#2100](https://github.com/AltairaLabs/PromptKit/issues/2100).

Spec fields PromptKit does not carry:

| Spec type | Field | Why |
|---|---|---|
| InitializeRequest params | `_meta` | the client sends no request _meta (progress tokens are not requested) |
| InitializeResult | `_meta` | _meta is not surfaced to callers |
| ClientCapabilities | `experimental` | the client does not implement this feature, so it does not advertise it |
| ClientCapabilities | `roots` | the client does not implement this feature, so it does not advertise it |
| ClientCapabilities | `tasks` | tasks are experimental in this revision; the client does not implement or advertise them |
| ServerCapabilities | `completions` | the client does not use completions |
| ServerCapabilities | `experimental` | experimental server capabilities are ignored |
| ServerCapabilities | `logging` | server log messages are not consumed |
| ServerCapabilities | `tasks` | tasks are experimental in this revision; the client does not implement or advertise them |
| ServerCapabilities resources | `subscribe` | the client does not use resources |
| ClientCapabilities sampling | `context` | sampling is not implemented or advertised (deprecated in 2026-07-28) |
| ClientCapabilities sampling | `tools` | sampling is not implemented or advertised (deprecated in 2026-07-28) |
| ListToolsRequest params | `_meta` | the client sends no request _meta (progress tokens are not requested) |
| ListToolsResult | `_meta` | _meta is not surfaced to callers |
| Tool | `_meta` | _meta is not surfaced to callers |
| Tool | `annotations` | tool behaviour hints (readOnlyHint, destructiveHint, ...) are not carried to tool descriptors |
| Tool | `outputSchema` | the declared output schema is not carried to tool descriptors, so results are not validated (#2100) |
| Tool | `execution` | execution hints (task support) are not carried; the client does not implement tasks |
| Tool | `icons` | display icons are not carried to tool descriptors |
| Tool | `title` | the display title is not carried to tool descriptors |
| CallToolRequest params | `_meta` | the client sends no request _meta (progress tokens are not requested) |
| CallToolRequest params | `task` | tasks are experimental in this revision; the client does not implement or advertise them |
| CallToolResult | `_meta` | _meta is not surfaced to callers |
| ContentBlock | `_meta` | _meta is not surfaced to callers |
| ContentBlock | `annotations` | content annotations (audience, priority) are not surfaced to the model |
| ContentBlock | `description` | resource_link description is not carried |
| ContentBlock | `icons` | resource_link icons are not carried |
| ContentBlock | `name` | resource_link name is not carried |
| ContentBlock | `resource` | embedded resource contents are dropped (#2100) |
| ContentBlock | `size` | resource_link size is not carried |
| ContentBlock | `title` | resource_link title is not carried |

Fields PromptKit declares that the spec does not define:

- ClientCapabilities `logging`: logging is a server capability, not a client one; the client never sets this field, so it is never sent. Exported, so it stays until the next major (see LoggingCapability)
- CallToolRequest params `inputResponses`: 2026-07-28 MRTR field, sent only to modern servers and only in answer to input_required
- CallToolRequest params `requestState`: 2026-07-28 MRTR field, sent only to modern servers and only in answer to input_required

<!-- END GENERATED: mcp-spec-support -->

## Quick Start

### Step 1: Create MCP Registry

```go
import "github.com/AltairaLabs/PromptKit/runtime/v2/mcp"

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
    
    "github.com/AltairaLabs/PromptKit/runtime/v2/mcp"
    "github.com/AltairaLabs/PromptKit/runtime/v2/pipeline"
    "github.com/AltairaLabs/PromptKit/runtime/v2/pipeline/middleware"
    "github.com/AltairaLabs/PromptKit/runtime/v2/providers/openai"
    "github.com/AltairaLabs/PromptKit/runtime/v2/tools"
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

### Timeouts and Retries

```go
options := mcp.DefaultClientOptions()
options.RequestTimeout = 30 * time.Second // every request, on every transport
options.MaxRetries = 3
options.RetryDelay = 100 * time.Millisecond // doubles on each retry

client := mcp.NewStdioClientWithOptions(config, options)
```

A request that outlives `RequestTimeout` fails with `mcp.ErrServerUnresponsive`
and the server is sent `notifications/cancelled`. Retries apply only to
`initialize` and `tools/list`, and only when the message could not be
exchanged at all. `tools/call` is never retried, and neither is a request the
server answered with an error: a tool may have side effects, and a timed-out
call may already have run.

### Elicitation

A server can ask the user for input partway through a tool call. PromptKit does
not talk to users, so the client advertises the `elicitation` capability only
when the host supplies a handler:

```go
options := mcp.DefaultClientOptions()
options.ElicitationHandler = func(ctx context.Context, server string, req mcp.ElicitRequest) (mcp.ElicitResult, error) {
    // Show req.Message and a form built from req.RequestedSchema to the user.
    return mcp.ElicitResult{Action: mcp.ElicitActionAccept, Content: answer}, nil
}
```

Without a handler, elicitation is not advertised and any elicitation request is
refused. With one, fields the user leaves out are filled from the defaults in
the requested schema. Only form mode is supported.

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

MCP reports two kinds of failure differently. A protocol error (unknown tool,
invalid arguments) is a JSON-RPC error, returned as `*mcp.RPCError`. A tool that
ran and failed returns a normal result with `IsError` set.

```go
response, err := client.CallTool(ctx, "read_file", args)
var rpcErr *mcp.RPCError
switch {
case errors.As(err, &rpcErr):
    log.Printf("Server rejected the call: %d %s", rpcErr.Code, rpcErr.Message)
    return
case err != nil:
    log.Printf("Call failed: %v", err)
    return
case response.IsError:
    log.Printf("Tool reported an error: %s", response.Content[0].Text)
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

- [Configure Pipeline](/runtime/how-to/pipeline/configure-pipeline/) - Complete setup

## See Also

- [MCP Reference](/runtime/reference/mcp/) - Complete API
- [MCP Tutorial](/runtime/tutorials/03-mcp-integration/) - Step-by-step guide
