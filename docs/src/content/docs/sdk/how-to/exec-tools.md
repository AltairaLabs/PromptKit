---
title: Exec Tools
description: Bind tools to external subprocesses in any language
sidebar:
  order: 16
---
Bind tool definitions to external subprocesses using the `exec` block in RuntimeConfig. This lets you implement tool logic in Python, Node.js, Rust, or any language that reads JSON from stdin and writes JSON to stdout.

## Quick Start

Define a tool in your pack and bind it to a script in `runtimeconfig.yaml`:

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

When the LLM calls `sentiment_check`, the runtime spawns the subprocess, sends the tool arguments as JSON on stdin, and reads the result from stdout.

## One-Shot Mode (Default)

In one-shot mode, the runtime spawns a new subprocess for each tool invocation. The process receives a single JSON object on stdin, writes a single JSON object to stdout, and exits.

### Input (stdin)

```json
{"args": {"text": "This product is amazing!"}}
```

### Output (stdout) -- success

```json
{"result": {"sentiment": "positive", "score": 0.85}}
```

### Output (stdout) -- error

```json
{"error": "model not loaded"}
```

One-shot mode is the simplest option. Use it when the tool has no persistent state and startup cost is acceptable.

## Server Mode

For tools that need persistent state, connection pooling, or expensive initialization, use `runtime: server`. The runtime starts the subprocess once and communicates over JSON-RPC 2.0 on stdio.

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

### Request (stdin)

```json
{"jsonrpc": "2.0", "id": 1, "method": "execute", "params": {"args": {"query": "SELECT count(*) FROM users"}}}
```

### Response (stdout)

```json
{"jsonrpc": "2.0", "id": 1, "result": {"count": 42}}
```

The subprocess must keep reading from stdin and writing responses to stdout until the runtime closes the pipe. Each request/response pair is a single line of JSON.

## Environment Variables

The `env` field lists environment variable names to forward to the subprocess. The runtime reads their values from the host process at invocation time.

```yaml
spec:
  tools:
    translate:
      exec:
        command: python3
        args: [./tools/translate.py]
        env: [DEEPL_API_KEY, TARGET_LANG]
```

Only the listed variable names are forwarded -- the subprocess does not inherit the full host environment.

## Timeouts

Set `timeout_ms` to limit how long the runtime waits for a response. If the subprocess exceeds the timeout, it is killed and the tool returns an error to the LLM.

```yaml
spec:
  tools:
    slow_analysis:
      exec:
        command: python3
        args: [./tools/analyze.py]
        timeout_ms: 30000  # 30 seconds
```

If omitted, the runtime uses a default timeout.

## Complete Example

### Tool script (`tools/sentiment-check.py`)

```python
#!/usr/bin/env python3
import json, sys

request = json.loads(sys.stdin.read())
args = request["args"]
text = args.get("text", "")

# Your logic here
score = 0.85 if "amazing" in text.lower() else 0.3
sentiment = "positive" if score > 0.5 else "negative"

result = {"sentiment": sentiment, "score": score}
print(json.dumps({"result": result}))
```

### RuntimeConfig (`runtimeconfig.yaml`)

```yaml
spec:
  tools:
    # One-shot mode (default)
    sentiment_check:
      exec:
        command: python3
        args: [./tools/sentiment-check.py]
        timeout_ms: 5000
        env: [NLTK_DATA]

    # Server mode — long-running subprocess
    db_query:
      exec:
        command: ./tools/db-query
        runtime: server
        timeout_ms: 10000
        env: [DATABASE_URL]
```

### Pack file (`assistant.pack.json`)

Define matching tool schemas so the LLM knows how to call them:

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

### SDK usage

```go
conv, err := sdk.Open("./assistant.pack.json", "assistant",
    sdk.WithRuntimeConfig("./runtimeconfig.yaml"),
)
if err != nil {
    log.Fatal(err)
}
defer conv.Close()

resp, err := conv.Send(ctx, "Is this review positive? 'This product is amazing!'")
fmt.Println(resp.Text())
```

The runtime handles subprocess lifecycle, timeout enforcement, and JSON marshalling automatically. Your Go code only needs to load the RuntimeConfig -- no `OnTool` registration is required for exec-bound tools.

## See Also

- [Register Tools](/sdk/how-to/register-tools/) -- programmatic tool registration in Go
- [HTTP Tools](/sdk/how-to/http-tools/) -- bind tools to HTTP APIs
- [Use RuntimeConfig](/sdk/how-to/use-runtime-config/) -- RuntimeConfig reference
- [Exec Hooks](/sdk/how-to/exec-hooks/) -- lifecycle hooks using external processes
