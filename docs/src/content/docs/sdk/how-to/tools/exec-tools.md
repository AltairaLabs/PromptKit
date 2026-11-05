---
title: Define exec tools
description: Run a tool as an external subprocess in any language that reads and writes JSON.
sidebar:
  order: 16
verified:
  commit: 004b9cbbd4d40f770a2e3c76053b600a55189355
  sources:
    - runtime/tools/exec_executor.go
    - runtime/tools/registry.go
    - runtime/tools/server_executor.go
    - sdk/conversation.go
    - sdk/conversation_tools.go
    - sdk/exec_tools.go
    - sdk/runtime_config.go
    - sdk/tool_descriptor_override.go
---
Bind a tool declared in your pack to an external subprocess with the `exec` block in RuntimeConfig. The subprocess reads JSON from stdin and writes JSON to stdout, so you can write the tool in Python, Node.js, Rust or any other language.

## Prerequisites

- A pack that declares the tool and lists it in a prompt's `tools`.
- A RuntimeConfig file. See [Use RuntimeConfig](/sdk/how-to/conversations/use-runtime-config/).

## 1. Declare the tool in the pack

The pack gives the model the tool's name, description and parameter schema:

```json
{
  "tools": {
    "sentiment_check": {
      "name": "sentiment_check",
      "description": "Analyze the sentiment of a text string",
      "parameters": {
        "type": "object",
        "properties": {
          "text": {
            "type": "string",
            "description": "The text to analyze"
          }
        },
        "required": ["text"]
      }
    }
  },
  "prompts": {
    "assistant": {
      "tools": ["sentiment_check"]
    }
  }
}
```

## 2. Write the script

A one-shot tool reads one JSON object from stdin and writes one JSON object to stdout:

```python
#!/usr/bin/env python3
import json, sys

request = json.loads(sys.stdin.read())
text = request["args"].get("text", "")

score = 0.85 if "amazing" in text.lower() else 0.3
sentiment = "positive" if score > 0.5 else "negative"

print(json.dumps({"result": {"sentiment": sentiment, "score": score}}))
```

The request is `{"args": {"text": "This product is amazing!"}}`. Return `{"result": ...}` on success or `{"error": "model not loaded"}` on failure. The runtime reports an error response, a non-zero exit or invalid JSON to the model as a tool error.

## 3. Bind the script in RuntimeConfig

The key under `spec.tools` is the tool name from the pack:

```yaml
spec:
  tools:
    sentiment_check:
      exec:
        command: python3
        args: [./tools/sentiment-check.py]
        timeout_ms: 5000
        env: [NLTK_DATA]
```

| Field | Meaning |
|---|---|
| `command`, `args` | The executable and its arguments. |
| `timeout_ms` | How long the runtime waits for a result. Defaults to 30000 (30 seconds). |
| `env` | Host environment variable names to pass to the subprocess. |
| `runtime` | `exec` (default) starts a new process per call. `server` keeps one process running. |

When `env` lists at least one name, a one-shot subprocess receives only those variables. When `env` is empty or omitted, it inherits the host environment.

After `timeout_ms` the runtime kills a one-shot subprocess and returns an error to the model.

## 4. Open the conversation

```go
conv, err := sdk.Open("./assistant.pack.json", "assistant",
    sdk.WithRuntimeConfig("./runtimeconfig.yaml"),
)
if err != nil {
    log.Fatal(err)
}
defer conv.Close()

resp, err := conv.Send(ctx, "Is this review positive? 'This product is amazing!'")
if err != nil {
    log.Fatal(err)
}
fmt.Println(resp.Text())
```

Exec-bound tools need no `OnTool` registration.

## Keep a process running (server mode)

Set `runtime: server` for tools with expensive startup, connection pools or other state:

```yaml
spec:
  tools:
    db_query:
      exec:
        command: ./tools/db-query
        runtime: server
        timeout_ms: 10000
        env: [DATABASE_URL]
```

The runtime starts the process on the first call, restarts it if it exits, and stops it when you call `conv.Close()`. It speaks JSON-RPC 2.0 over stdio, one JSON object per line.

```json
{"jsonrpc": "2.0", "id": 1, "method": "execute", "params": {"args": {"query": "SELECT count(*) FROM users"}}}
```

Answer each request with the same `id`:

```json
{"jsonrpc": "2.0", "id": 1, "result": {"count": 42}}
```

Return an `error` object (`{"code": -32000, "message": "..."}`) instead of `result` to report a failure. Keep reading stdin until the runtime closes the pipe.

A server process inherits the host environment, and `env` adds to it. On a timeout the call returns an error and the process keeps running.

## Verify

Send a message that makes the model call the tool. The tool's result appears in the model's reply. To confirm the binding applied, check that the logs hold no `exec tool config skipped` warning.

## Troubleshoot

A `tools:` key that matches no tool in the pack is skipped with a warning that names the key and the registered tools:

```text
WARN exec tool config skipped: tool not registered  name=fetch_invoice registered_tools=[fetch_invoices send_email]
```

The tool the entry was meant for keeps its pack-declared mode, and other entries still apply. Check the key for a misspelling or a renamed tool. [`WithToolDescriptorOverride`](/sdk/how-to/tools/override-capability-tools/#troubleshoot) treats an unknown name the same way.

## Related

- [Register tools](/sdk/how-to/tools/register-tools/): register tools in Go
- [HTTP tools](/sdk/how-to/tools/http-tools/): bind tools to HTTP APIs
- [Exec protocol](/sdk/reference/exec-protocol/): the stdin/stdout contract
- [RuntimeConfig reference](/sdk/reference/runtime-config/): every `spec` field
- [Exec hooks](/sdk/how-to/hooks/exec-hooks/): lifecycle hooks in external processes
