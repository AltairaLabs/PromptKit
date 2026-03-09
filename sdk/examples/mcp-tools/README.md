# MCP Tools Example

Connect to MCP (Model Context Protocol) tool servers via the PromptKit SDK.

## What You'll Learn

- Simple MCP server registration with `WithMCP()`
- Builder pattern with `NewMCPServer()` for timeouts and tool filters
- Tool filtering to expose only specific tools from a server

## Prerequisites

- Go 1.21+
- OpenAI API key
- Node.js / npx (for the MCP server)
- `@modelcontextprotocol/server-everything` npm package

## Running the Example

```bash
export OPENAI_API_KEY=your-key
go run .
```

## Code Overview

### Pattern 1: Simple Registration

```go
conv, err := sdk.Open("./mcp-tools.pack.json", "assistant",
    sdk.WithMCP("everything", "npx", "@modelcontextprotocol/server-everything"),
)
```

### Pattern 2: Builder with Tool Filter

```go
server := sdk.NewMCPServer("everything", "npx", "@modelcontextprotocol/server-everything").
    WithTimeout(10000).
    WithToolFilter(&mcp.ToolFilter{
        Allowlist: []string{"echo", "get-sum"},
    })

conv, err := sdk.Open("./mcp-tools.pack.json", "assistant",
    sdk.WithMCPServer(server),
)
```

## Builder Options

| Method | Description |
|--------|-------------|
| `WithEnv(key, value)` | Set environment variable for the MCP server process |
| `WithArgs(args...)` | Append additional command-line arguments |
| `WithWorkingDir(dir)` | Set the working directory for the server process |
| `WithTimeout(ms)` | Set per-request timeout in milliseconds |
| `WithToolFilter(filter)` | Allowlist/blocklist to control which tools are exposed |

## Tool Naming

MCP tools are namespaced as `mcp__<server>__<tool>`:
- `mcp__everything__echo`
- `mcp__everything__get-sum`

## Key Concepts

1. **Auto-Discovery** - Tools are discovered from the MCP server at startup
2. **Tool Filtering** - Expose only the tools you need via allowlist/blocklist
3. **Timeouts** - Per-request timeouts protect against slow servers
4. **No Pack Changes** - MCP tools don't need to be defined in the pack file

## Next Steps

- [A2A Agent Example](../a2a-agent/) - Remote agent integration with auth
- [HTTP Transforms Example](../http-transforms/) - Argument transforms and response processing
- [Tools Example](../tools/) - Basic tool handling
