# Client-Side Tools Example

Execute tool calls on the client side rather than via a live HTTP endpoint.

## What it shows

The pack declares `get_location` without saying how it runs; each conversation binds it as a client tool. Three modes:

- **Synchronous** — `OnClientTool` binds a handler that runs when the LLM invokes the tool.
- **Deferred** — `DeferClientTool` binds the tool without a handler; the pipeline suspends, the caller supplies the result with `SendToolResult`, then calls `Resume`.
- **Streaming deferred** — the same with `Stream`: the call arrives as a `ChunkClientTool` chunk, and `ResumeStream` continues.

`go test .` runs all three against a mock model that calls the tool.

## Running

```bash
export OPENAI_API_KEY=your-key
cd sdk/examples/client-tools
go run .
```
