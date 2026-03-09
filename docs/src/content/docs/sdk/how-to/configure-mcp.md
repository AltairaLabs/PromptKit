---
title: Configure MCP Servers
description: Connect MCP tool servers to your conversation using the SDK builder pattern
sidebar:
  order: 6
---

Add [Model Context Protocol](https://modelcontextprotocol.io) servers to a conversation so the LLM can call their tools.

---

## Quick Start

```go
import "github.com/AltairaLabs/PromptKit/sdk"

conv, err := sdk.Open("./app.pack.json", "assistant",
    sdk.WithMCP("filesystem", "npx", "-y", "@modelcontextprotocol/server-filesystem", "/data"),
)
if err != nil {
    log.Fatal(err)
}
defer conv.Close()
```

`WithMCP` registers all tools from the server. For finer control, use the builder pattern.

---

## Builder Pattern

Use `NewMCPServer` when you need environment variables, timeouts, working directories, or tool filtering.

```go
import (
    "github.com/AltairaLabs/PromptKit/runtime/mcp"
    "github.com/AltairaLabs/PromptKit/sdk"
)

server := sdk.NewMCPServer("github", "npx", "@modelcontextprotocol/server-github").
    WithEnv("GITHUB_TOKEN", os.Getenv("GITHUB_TOKEN")).
    WithTimeout(10000).
    WithToolFilter(&mcp.ToolFilter{
        Allowlist: []string{"search_repositories", "get_file_contents"},
    })

conv, err := sdk.Open("./app.pack.json", "assistant",
    sdk.WithMCPServer(server),
)
```

### Builder Methods

| Method | Description |
|--------|-------------|
| `WithEnv(key, value)` | Add an environment variable to the server process |
| `WithArgs(args...)` | Append additional command arguments |
| `WithWorkingDir(dir)` | Set the working directory for the server process |
| `WithTimeout(ms)` | Set per-request timeout in milliseconds |
| `WithToolFilter(filter)` | Control which tools are exposed to the LLM |

---

## Tool Filtering

Use `ToolFilter` to limit which tools the LLM can see. This is useful when a server exposes many tools but you only need a few.

### Allowlist

Only expose specific tools:

```go
server := sdk.NewMCPServer("everything", "npx", "@modelcontextprotocol/server-everything").
    WithToolFilter(&mcp.ToolFilter{
        Allowlist: []string{"echo", "get-sum"},
    })
```

### Blocklist

Expose all tools except specific ones:

```go
server := sdk.NewMCPServer("db", "python", "mcp_server.py").
    WithToolFilter(&mcp.ToolFilter{
        Blocklist: []string{"drop_table", "truncate_table"},
    })
```

If both are set, allowlist takes precedence.

---

## Multiple Servers

Register multiple MCP servers in a single conversation:

```go
github := sdk.NewMCPServer("github", "npx", "@modelcontextprotocol/server-github").
    WithEnv("GITHUB_TOKEN", os.Getenv("GITHUB_TOKEN"))

filesystem := sdk.NewMCPServer("fs", "npx",
    "-y", "@modelcontextprotocol/server-filesystem", "/data")

conv, err := sdk.Open("./app.pack.json", "assistant",
    sdk.WithMCPServer(github),
    sdk.WithMCPServer(filesystem),
)
```

Tool names are namespaced as `mcp__{serverName}__{toolName}` to avoid collisions.

---

## Tool Naming

MCP tools registered through the SDK follow this naming pattern:

```
mcp__{serverName}__{toolName}
```

| Server Name | Tool Name | Registered As |
|-------------|-----------|---------------|
| `github` | `search_repositories` | `mcp__github__search_repositories` |
| `filesystem` | `read_file` | `mcp__filesystem__read_file` |

Reference these names in your prompt config's `allowed_tools` if you want to restrict which tools are available:

```yaml
spec:
  allowed_tools:
    - mcp__github__search_repositories
    - mcp__github__get_file_contents
```

---

## Complete Example

```go
package main

import (
    "context"
    "fmt"
    "log"
    "os"

    "github.com/AltairaLabs/PromptKit/runtime/mcp"
    "github.com/AltairaLabs/PromptKit/sdk"
)

func main() {
    server := sdk.NewMCPServer("everything", "npx",
        "@modelcontextprotocol/server-everything").
        WithTimeout(10000).
        WithToolFilter(&mcp.ToolFilter{
            Allowlist: []string{"echo", "get-sum"},
        })

    conv, err := sdk.Open("./app.pack.json", "assistant",
        sdk.WithMCPServer(server),
    )
    if err != nil {
        log.Fatal(err)
    }
    defer conv.Close()

    ctx := context.Background()
    resp, err := conv.Send(ctx, "What is 17 + 25?")
    if err != nil {
        log.Fatal(err)
    }
    fmt.Println(resp.Text())
}
```

---

## Next Steps

- [Integrate MCP (Runtime)](/runtime/how-to/integrate-mcp/) — low-level MCP registry and pipeline integration
- [Register Tools](/sdk/how-to/register-tools/) — add custom Go tools alongside MCP tools
- [HTTP Tools](/sdk/how-to/http-tools/) — call REST APIs as tools
