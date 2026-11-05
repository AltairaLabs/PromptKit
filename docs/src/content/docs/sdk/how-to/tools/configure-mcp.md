---
title: Configure MCP Servers
description: Connect MCP tool servers to a conversation with WithMCP and the MCPServerBuilder
sidebar:
  order: 6
verified:
  commit: 004b9cbbd4d40f770a2e3c76053b600a55189355
  sources:
    - runtime/mcp/client.go
    - runtime/mcp/registry.go
    - runtime/mcp/types.go
    - runtime/pipeline/stage/stages_provider.go
    - runtime/prompt/pack.go
    - runtime/prompt/registry.go
    - runtime/tools/registry.go
    - runtime/tools/types.go
    - sdk/conversation_tools.go
    - sdk/mcp_tool_sync.go
    - sdk/options.go
---

Add [Model Context Protocol](https://modelcontextprotocol.io) servers to a conversation so the model can call their tools.

## Prerequisites

- A pack with a prompt to open (see [Register Tools](/sdk/how-to/tools/register-tools/)).
- The server's command on your `PATH`. The examples use `npx`.

:::caution
`WithMCP` and the builder start the command as a subprocess without sandboxing or validation. Take the command and arguments from trusted sources only.
:::

## Steps

### 1. Add a server

```go
import "github.com/AltairaLabs/PromptKit/sdk/v2"

conv, err := sdk.Open("./app.pack.json", "assistant",
    sdk.WithMCP("filesystem", "npx", "-y", "@modelcontextprotocol/server-filesystem", "/data"),
)
if err != nil {
    log.Fatal(err)
}
defer conv.Close()
```

`WithMCP` registers every tool the server lists.

### 2. Configure the server (optional)

Use `NewMCPServer` for environment variables, timeouts, working directories or tool filtering:

```go
import (
    "github.com/AltairaLabs/PromptKit/runtime/v2/mcp"
    "github.com/AltairaLabs/PromptKit/sdk/v2"
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

| Method | Description |
|--------|-------------|
| `WithEnv(key, value)` | Add an environment variable to the server process |
| `WithArgs(args...)` | Append command arguments |
| `WithWorkingDir(dir)` | Set the server process's working directory |
| `WithTimeout(ms)` | Set the per-request timeout in milliseconds |
| `WithToolFilter(filter)` | Choose which of the server's tools the conversation registers |

### 3. Filter tools (optional)

An `Allowlist` registers only the named tools. A `Blocklist` registers every tool except the named ones:

```go
server := sdk.NewMCPServer("db", "python", "mcp_server.py").
    WithToolFilter(&mcp.ToolFilter{
        Blocklist: []string{"drop_table", "truncate_table"},
    })
```

A trailing `*` in an entry matches a prefix, so `read_*` matches `read_file`. When both lists are set, the allowlist applies and the blocklist is ignored.

### 4. List the tools in your prompt

The SDK registers each tool as `mcp__{serverName}__{toolName}`:

| Server name | Tool name | Registered as |
|-------------|-----------|---------------|
| `github` | `search_repositories` | `mcp__github__search_repositories` |
| `filesystem` | `read_file` | `mcp__filesystem__read_file` |

The model sees an MCP tool only when the prompt lists it. In a pack, add the names, or a wildcard, to the prompt's `tools` array. A prompt config file uses `allowed_tools`:

```yaml
spec:
  allowed_tools:
    - mcp__github__search_repositories
    - mcp__github__get_file_contents
    - mcp__filesystem__*
```

`mcp__{serverName}__*` offers every tool of one server, and `mcp__*` offers every MCP tool.

### 5. Add more servers (optional)

Pass one option per server:

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

The server name keeps tool names apart.

## Verify

List the registered tools after opening the conversation:

```go
for _, name := range conv.ToolRegistry().List() {
    fmt.Println(name)
}
```

Each MCP tool appears as `mcp__{serverName}__{toolName}`. A tool that is registered but missing from the prompt's `tools` is never offered to the model.

## Troubleshoot

| Symptom | Cause |
|---|---|
| No `mcp__` tools in the registry | The server could not list its tools when the conversation opened. The SDK logs a warning and retries in the background until the server answers. |
| Tools are registered but the model never calls them | The prompt does not list them. Add the names or an `mcp__{serverName}__*` entry (step 4). |
| A tool is missing | The server's `ToolFilter` excludes it. |

## Related

- [Integrate MCP (Runtime)](/runtime/how-to/tools/integrate-mcp/): the MCP registry and pipeline integration
- [Register Tools](/sdk/how-to/tools/register-tools/): add custom Go tools alongside MCP tools
- [HTTP Tools](/sdk/how-to/tools/http-tools/): call REST APIs as tools
