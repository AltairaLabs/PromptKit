---
title: Integrate MCP
description: Connect a Model Context Protocol server and make its tools available to a PromptKit pipeline.
sidebar:
  order: 3
verified:
  commit: 8489bf3d95c7498f9de0f024769db9399c24e884
  sources:
    - runtime/mcp/auth.go
    - runtime/mcp/client.go
    - runtime/mcp/client_test.go
    - runtime/mcp/elicitation.go
    - runtime/mcp/options.go
    - runtime/mcp/registry.go
    - runtime/mcp/types.go
    - runtime/pipeline/stage/stages_provider.go
    - runtime/pipeline/types.go
    - runtime/tools/mcp_executor.go
    - runtime/tools/registry.go
    - sdk/mcp_tool_sync.go
    - sdk/options.go
    - sdk/sdk.go
---
Connect a Model Context Protocol (MCP) server and let the model call its tools through the runtime pipeline.

## Prerequisites

- A Go module that imports `github.com/AltairaLabs/PromptKit/runtime/v2`.
- An MCP server to connect to: a command PromptKit can start (the examples use `npx`) or an HTTP URL.
- A provider for the pipeline (see [Configure Pipeline](/runtime/how-to/pipeline/configure-pipeline/)).

With the SDK, `sdk.WithMCP` and `sdk.WithMCPServer` do these steps for you. See [Configure MCP](/sdk/how-to/tools/configure-mcp/).

## Steps

### 1. Register the server

```go
import "github.com/AltairaLabs/PromptKit/runtime/v2/mcp"

registry := mcp.NewRegistry()
defer registry.Close()

err := registry.RegisterServer(mcp.ServerConfig{
    Name:    "filesystem",
    Command: "npx",
    Args:    []string{"-y", "@modelcontextprotocol/server-filesystem", "/allowed"},
    Env:     map[string]string{"API_KEY": os.Getenv("API_KEY")},
})
if err != nil {
    log.Fatal(err)
}
```

`Command` starts a local server over stdio. `Env` adds variables to the server process.

For an HTTP server, set `URL` instead of `Command`:

```go
mcp.ServerConfig{Name: "docs", URL: "https://mcp.example.com/mcp"}
```

The client tries Streamable HTTP first. If that connection fails, it tries HTTP+SSE. To skip the first attempt, set `TransportName: mcp.TransportSSE`; then `URL` must be the SSE endpoint itself (for example `http://host:3001/sse`). A URL that does not serve the stream is retried with `/sse` appended, and the client logs a warning when that is needed.

### 2. Discover the tools

```go
ctx := context.Background()
serverTools, err := registry.ListAllTools(ctx)
if err != nil {
    log.Fatal(err)
}
for serverName, tools := range serverTools {
    log.Printf("Server %s has %d tools", serverName, len(tools))
}
```

### 3. Register the tools with a tool registry

```go
toolRegistry := tools.NewRegistry()
toolRegistry.RegisterExecutor(tools.NewMCPExecutor(registry))

for _, mcpTools := range serverTools {
    for _, t := range mcpTools {
        err := toolRegistry.Register(&tools.ToolDescriptor{
            Name:        t.Name,
            Description: t.Description,
            InputSchema: t.InputSchema,
            Mode:        "mcp",
        })
        if err != nil {
            log.Fatal(err)
        }
    }
}
```

### 4. Give the tool registry to the provider stage

```go
pipe, err := stage.NewPipelineBuilder().
    Chain(stage.NewProviderStage(provider, toolRegistry, &pipeline.ToolPolicy{
        ToolChoice:          "auto",
        MaxRounds:           5,
        MaxToolCallsPerTurn: 10,
    }, &stage.ProviderConfig{MaxTokens: 1500, Temperature: 0.7})).
    Build()
if err != nil {
    log.Fatal(err)
}
defer pipe.Shutdown(ctx)
```

### 5. Tune the client (optional)

Pass a `ClientOptions` to a client constructor, or set it per server through `RegistryOptions.ConfigureClient`.

#### Timeouts and retries

```go
options := mcp.DefaultClientOptions()
options.RequestTimeout = 30 * time.Second   // every request, on every transport
options.MaxRetries = 3
options.RetryDelay = 100 * time.Millisecond // doubles on each retry

client := mcp.NewStdioClientWithOptions(config, options)
```

A request that outlives `RequestTimeout` is canceled and fails with `mcp.ErrServerUnresponsive`. `ServerConfig.TimeoutMs` sets the same limit for one server. The clock does not run while the host answers a server's request for user input.

Progress notifications from the server restart a `tools/call` timeout; the context passed to `CallTool` still bounds the call.

Retries apply only to connecting, and only to failures that are not the server's answer. `tools/call` is never retried, because a tool may have side effects. If the connection is lost, the next request reconnects, up to `MaxReconnectAttempts` times.

#### Elicitation

A server can ask the user for input during a tool call. The client advertises the `elicitation` capability only when you supply a handler:

```go
options := mcp.DefaultClientOptions()
options.ElicitationHandler = func(ctx context.Context, server string, req mcp.ElicitRequest) (mcp.ElicitResult, error) {
    // Show req.Message and a form built from req.RequestedSchema to the user.
    return mcp.ElicitResult{Action: mcp.ElicitActionAccept, Content: answer}, nil
}
```

Without a handler, any elicitation request is refused. With one, fields the user leaves out are filled from the defaults in the requested schema. Only form mode is supported. The same handler answers a 2026-07-28 server's `input_required` results.

#### Authorization

For an HTTP server with static credentials, set `ServerConfig.Headers`. For [MCP authorization](https://modelcontextprotocol.io/specification/latest/basic/authorization) (OAuth 2.1), implement `mcp.Authorizer`. PromptKit runs no OAuth flow and stores no secrets: your code does both.

```go
type Authorizer interface {
    // Set credentials on every request (Authorization, DPoP, ...).
    Authorize(ctx context.Context, req *http.Request) error
    // Called on 401, or 403 with error="insufficient_scope". Obtain new
    // credentials and return nil to have the request sent again.
    Challenge(ctx context.Context, c *mcp.AuthChallenge) error
}
```

`AuthChallenge` carries the parsed `WWW-Authenticate` challenge, including the protected resource metadata URL and the scope the server needs. A request is challenged at most three times before it fails with `*mcp.AuthError`.

With the SDK, supply one per server:

```go
conv, _ := sdk.Open(packPath, "assistant",
    sdk.WithMCPAuthorizer(func(server string) mcp.Authorizer { return host.AuthorizerFor(server) }),
    sdk.WithMCPElicitation(host.Elicit),
)
```

With the runtime, set `ClientOptions.Authorizer` and `ElicitationHandler` in `RegistryOptions.ConfigureClient`.

#### Tool list changes

When a server sends `notifications/tools/list_changed`, the registry lists its tools again. To update your own tool registry, set `RegistryOptions.OnToolsChanged`:

```go
registry := mcp.NewRegistryWithOptions(mcp.RegistryOptions{
    OnToolsChanged: func(server string, tools []mcp.Tool) {
        // Replace this server's descriptors in your tools.Registry.
    },
})
```

The callback runs on its own goroutine with the server's complete new list. If the list cannot be read, the registry keeps the previous tools and does not call it. The SDK sets the callback for you: from the next turn, a conversation and its forks stop offering removed tools and offer new ones that the prompt allows, for example through an `mcp__<server>__*` entry in its tools.

### 6. Handle errors (optional)

A protocol error (unknown tool, invalid arguments) is a JSON-RPC error, returned as `*mcp.RPCError`. A tool that ran and failed returns a normal result with `IsError` set.

```go
response, err := client.CallTool(ctx, "read_file", args)
var rpcErr *mcp.RPCError
switch {
case errors.As(err, &rpcErr):
    log.Printf("Server rejected the call: %d %s", rpcErr.Code, rpcErr.Message)
case err != nil:
    log.Printf("Call failed: %v", err)
case response.IsError:
    log.Printf("Tool reported an error: %s", response.Content[0].Text)
}
```

## Verify

Call a discovered tool through the tool registry:

```go
result, err := toolRegistry.Execute(ctx, "read_file", json.RawMessage(`{"path": "/allowed/example.txt"}`))
if err != nil {
    log.Fatal(err)
}
if result.Error != "" {
    log.Fatalf("Tool failed: %s", result.Error)
}
log.Printf("Result: %s", result.Result)
```

`Execute` returns an error for an unknown tool, invalid arguments or a missing executor. An executor failure or a timeout arrives in `result.Error` with a nil error.

To skip the tool registry, call the client directly:

```go
client, err := registry.GetClientForTool(ctx, "read_file")
if err != nil {
    log.Fatal(err)
}
response, err := client.CallTool(ctx, "read_file", json.RawMessage(`{"path": "/allowed/example.txt"}`))
```

## Troubleshoot

### The server does not start

Run the command by hand (`npx -y @modelcontextprotocol/server-filesystem /data`) and check it is on `PATH`. `RegisterServer` does not start the server; the first `GetClient` or `ListAllTools` call does, and returns the error.

### `ListAllTools` returns no tools for a server

By default (`ClientOptions.EnableGracefulDegradation`), a failed `tools/list` returns no tools instead of an error. The client logs each such failure as a warning. Set `EnableGracefulDegradation` to `false` to make the client's `ListTools` return the error.

`ListAllTools` leaves out a server it cannot list and logs a warning. It returns an error only when no server could be listed.

The SDK turns degradation off for its clients. It names a server whose tools cannot be listed when the conversation opens in a warning, and retries in the background, from one second apart up to one minute apart, until the server answers or the conversation closes.

### A tool call times out

Raise `ClientOptions.RequestTimeout` or `ServerConfig.TimeoutMs`, and give `CallTool` a context with a deadline long enough for the tool.

### A server fails when the client connects

Over stdio and Streamable HTTP, the client first asks which revisions the server supports (`server/discover`). Set `ClientOptions.DisableModernProtocol` to skip that probe for a server that misbehaves when it receives one. A stdio server that never answers it is restarted with the `initialize` handshake after `ClientOptions.EraProbeTimeout` (3 seconds by default); raise it for a modern-only server that starts slowly.

## Spec support

<!-- BEGIN GENERATED: mcp-spec-support. Do not edit; run `make mcp-spec-docs`. -->

PromptKit's MCP client is built on the official [Go SDK](https://github.com/modelcontextprotocol/go-sdk), which owns the protocol. It implements revision **2026-07-28** (`mcp.ProtocolVersion`), the stateless revision, and **2025-11-25** (`mcp.LegacyProtocolVersion`), the newest revision with an `initialize` handshake, for servers that predate it. It detects which a server speaks, and also accepts the earlier handshake revisions a server may choose (2025-06-18, 2025-03-26, 2024-11-05).

Results reach callers as PromptKit's types (`mcp.Tool`, `mcp.ToolCallResponse`, ...). CI checks those types against both revisions' published schemas, so the table below is the complete list of spec fields they do not carry; every other field is carried.

That check covers message fields. Behaviour is checked by scenario tests and by the official [MCP conformance suite](https://github.com/modelcontextprotocol/conformance) (`make mcp-conformance`) against both revisions' requirements.

Spec fields PromptKit does not carry:

| Spec type | Field | Why |
|---|---|---|
| InitializeRequest params | `_meta` (2025-11-25 only) | the handshake request carries no metadata (progress tokens are not requested) |
| InitializeResult | `_meta` (2025-11-25 only) | _meta is not surfaced to callers |
| ClientCapabilities | `experimental` | the client does not implement this feature, so it does not advertise it |
| ClientCapabilities | `roots` | the client does not implement this feature, so it does not advertise it |
| ClientCapabilities | `tasks` (2025-11-25 only) | tasks are experimental in this revision; the client does not implement or advertise them |
| ServerCapabilities | `completions` | the client does not use completions |
| ServerCapabilities | `experimental` | experimental server capabilities are ignored |
| ServerCapabilities | `logging` | server log messages are not consumed |
| ServerCapabilities | `tasks` (2025-11-25 only) | tasks are experimental in this revision; the client does not implement or advertise them |
| ServerCapabilities resources | `subscribe` | the client does not use resources |
| ClientCapabilities sampling | `context` | sampling is not implemented or advertised (deprecated in 2026-07-28) |
| ClientCapabilities sampling | `tools` | sampling is not implemented or advertised (deprecated in 2026-07-28) |
| ListToolsRequest params | `_meta` | set by the session, not the caller: a 2026-07-28 request carries the protocol metadata (version, client info, capabilities); a handshake-era request carries none |
| CallToolRequest params | `_meta` | set by the client, not the caller: every call carries a progress token, and a 2026-07-28 call also the protocol metadata (version, client info, capabilities) |
| CallToolRequest params | `task` (2025-11-25 only) | tasks are experimental in this revision; the client does not implement or advertise them |
| InputRequest | `id` (2025-11-25 only) | in 2025-11-25 these are standalone JSON-RPC requests from the server, answered by the session; as 2026-07-28 input requests inside an input_required result they carry no envelope |
| InputRequest | `jsonrpc` (2025-11-25 only) | in 2025-11-25 these are standalone JSON-RPC requests from the server, answered by the session; as 2026-07-28 input requests inside an input_required result they carry no envelope |
| ElicitRequest params | `_meta` (2025-11-25 only) | _meta is not surfaced to callers |
| ElicitRequest params | `task` (2025-11-25 only) | tasks are experimental in this revision; the client does not implement or advertise them |
| ElicitRequest params | `elicitationId` (2025-11-25 only) | URL-mode elicitation is not advertised, so its correlation id is not used |
| ElicitResult | `_meta` (2025-11-25 only) | the client attaches no metadata to its answers |

Fields PromptKit declares that the spec does not define:

- ClientCapabilities `logging`: logging is a server capability, not a client one; the client never sets this field, so it is never sent. Exported, so it stays until the next major (see LoggingCapability)

<!-- END GENERATED: mcp-spec-support -->

## Related

- [Configure Pipeline](/runtime/how-to/pipeline/configure-pipeline/)
- [MCP reference](/runtime/reference/mcp/)
- [MCP tutorial](/runtime/tutorials/03-mcp-integration/)
