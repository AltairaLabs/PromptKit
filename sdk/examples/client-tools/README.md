# Client-Side Tools Example

Execute tool calls on the client side rather than via a live HTTP endpoint.

## What it shows

Two modes of client-side tool execution:

- **Synchronous** — an `OnClientTool` handler runs immediately when the LLM invokes the tool.
- **Deferred** — no handler is registered; the pipeline suspends, the caller supplies the result, then resumes.

## Running

```bash
export OPENAI_API_KEY=your-key
cd sdk/examples/client-tools
go run .
```
