# Codegen Sandbox via MCP Endpoint Resolver

SDK example exercising `sdk.WithMCPEndpoints` — pack declares the MCP
server by name only, host injects the lookup.

## What this shows

- `sdk.NewMCPServerByName("codegen")` — declare an MCP server with no
  URL/Command. The pack-author surface stays free of provisioning
  concerns.
- `sdk.WithMCPEndpoints(resolver)` — host injects a single resolver. At
  conversation open the SDK calls `resolver.Resolve("codegen")` and
  plugs the URL+Headers into the runtime MCP registry.
- Real model + real container. Claude Sonnet 4.6 talks to the
  codegen-sandbox via the resolved endpoint, writes a Go module from
  scratch, runs the tests.

In production your resolver would call a sandbox pool, service
discovery, or some host-managed orchestrator. Here it's a tiny
`StaticMCPEndpointResolver` and the example provisions the container
itself for a single self-contained demo.

## Prerequisites

- Docker daemon running.
- `ANTHROPIC_API_KEY` in environment.
- `ghcr.io/altairalabs/codegen-sandbox:latest` pulled (or the example
  pulls it on first `docker run`).

## Run

```bash
export ANTHROPIC_API_KEY=...
docker pull ghcr.io/altairalabs/codegen-sandbox:latest
go run .
```

Expected output:

```
sandbox ready: http://localhost:42813 (container 5f3a92b1c0d4)
=== Agent reply ===
All 7 tests pass with 100% coverage. ...
```

The container is stopped + removed on exit.

## Cost

A single run is typically 5–10k input + ~1k output tokens against
Sonnet ≈ $0.05–$0.10.

## See also

- The arena variant: [`examples/codegen-anthropic/`](../../../examples/codegen-anthropic/) — same idea but driven through arena's source-backed MCP machinery instead of an SDK resolver. Better for batch evaluation.
- `sdk/examples/mcp-tools/` — non-resolver MCP example (declare with
  `WithMCP("name", "command", args...)`).
