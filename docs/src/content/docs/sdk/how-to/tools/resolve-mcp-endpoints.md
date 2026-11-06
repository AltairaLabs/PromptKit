---
title: Resolve MCP server endpoints at runtime
description: Declare MCP servers by name and let the host supply their URLs and headers at open time with WithMCPEndpoints.
sidebar:
  order: 7
verified:
  commit: 8489bf3d95c7498f9de0f024769db9399c24e884
  sources:
    - sdk/mcp_resolver.go
    - sdk/options.go
    - sdk/sdk.go
---

Declare an MCP server by name and let your host decide where it lives, so the pack and the code that declares the server stay free of addresses and credentials.

## Prerequisites

- An MCP server reachable over HTTP (see [Configure MCP servers](/sdk/how-to/tools/configure-mcp/)).

## Steps

### 1. Declare the server by name

`sdk.NewMCPServerByName` has no command and no URL:

```go
sdk.WithMCPServer(sdk.NewMCPServerByName("codegen"))
```

### 2. Register a resolver

`sdk.WithMCPEndpoints` takes an `MCPEndpointResolver`. At open time the SDK calls `Resolve` once per name-only server and uses the returned URL and headers.

Use `StaticMCPEndpointResolver` when all MCP traffic goes through one gateway:

```go
conv, err := sdk.Open("./assistant.pack.json", "assistant",
	sdk.WithMCPServer(sdk.NewMCPServerByName("codegen")),
	sdk.WithMCPEndpoints(&sdk.StaticMCPEndpointResolver{
		URL:     "http://localhost:9000",
		Headers: map[string]string{"Authorization": "Bearer " + token},
	}),
)
```

Use `MapMCPEndpointResolver` for a separate endpoint per server:

```go
sdk.WithMCPEndpoints(&sdk.MapMCPEndpointResolver{
	Endpoints: map[string]sdk.MCPEndpoint{
		"codegen":    {URL: "http://codegen-pool.svc:8080"},
		"filesystem": {URL: "http://fs.svc:8080"},
	},
})
```

### 3. Write your own resolver

Implement `Resolve(serverName string) sdk.MCPEndpoint` to look the endpoint up in service discovery or a sandbox pool. `MCPEndpoint` has a `URL` and a `Headers` map. The SDK attaches the headers to every request to that server.

## Verify

Open the conversation. The server's tools are available to the model, and `Open` returns no error.

## Failure cases

`Open` returns an error in each of these cases:

- A server is declared by name and no resolver is configured.
- The resolver returns an empty `URL` for the name. `MapMCPEndpointResolver` does this for a name it does not list.

A server declared with a URL or a command bypasses the resolver.

## Related

- [Configure MCP servers](/sdk/how-to/tools/configure-mcp/)
- [Connect A2A agents](/sdk/how-to/interop/connect-a2a-agents/)
