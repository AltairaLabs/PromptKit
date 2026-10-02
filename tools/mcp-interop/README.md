# MCP interop matrix

Runs PromptKit's MCP client (`runtime/mcp`) against real MCP servers built with
the official SDKs, over every transport and both protocol eras, and prints what
each tool call delivered to the model.

```bash
make mcp-interop          # or: tools/mcp-interop/run.sh
```

Needs Go, Python 3.10 or later (as `python3`), Node and npm. The first run installs the pinned servers into
`tools/mcp-interop/.cache/` (gitignored); later runs take about 15 seconds. Local
ports start at `MCP_INTEROP_PORT_BASE` (default 18100).

The official conformance suite (`make mcp-conformance`) checks the client against
scripted scenarios. This checks it against implementations people actually run,
which is how bugs the suite missed were found: the HTTP+SSE endpoint URL, 4xx
responses being retried, stdio servers killed instead of shut down, and tools
that can only run as tasks.

## Servers

| Server | Built with | Transports | Protocol |
|---|---|---|---|
| `servers/python_server.py` | Python SDK `mcp` 2.2.0 | stdio, Streamable HTTP (stateful and stateless) | 2026-07-28; 2025-11-25 when forced |
| `goserver/` | Go SDK v1.8.0 | stdio, Streamable HTTP (stateful and stateless), HTTP+SSE | 2026-07-28 (stateful HTTP serves 2025-11-25) |
| `@modelcontextprotocol/server-everything` | TypeScript SDK | stdio, Streamable HTTP, HTTP+SSE | 2025-11-25 |
| `@modelcontextprotocol/server-filesystem` | TypeScript SDK | stdio | 2025-11-25 |
| `servers/silent_stdio.py` | none | stdio | 2025-11-25, never answers `server/discover` |

"URL-only" rows give the client a URL and no transport, so it tries Streamable
HTTP and falls back to HTTP+SSE. Rows marked `-handshake` force the 2025-11-25
handshake (`DisableModernProtocol`).

## Reading the output

The run fails if any server cannot be connected to or cannot list its tools. A
failed tool call is reported, not fatal. These are expected:

| Call | Why it fails |
|---|---|
| `boom` | fails by design |
| Python `ask` (not `-handshake`) | the Python SDK's `ctx.elicit` only works on handshake-era sessions; `ask2` elicits the 2026-07-28 way |
| Go `ask2` | `MISSING`: only the Python server defines it |
| server-everything `simulate-research-query` | can only be called as a task. PromptKit leaves such tools out, but go-sdk v1.8.0 drops the `execution` field that marks them, so the tool still reaches the model |
| server-filesystem `read_text_file` on `/etc/hostname` | outside the allowed directory, by design |
