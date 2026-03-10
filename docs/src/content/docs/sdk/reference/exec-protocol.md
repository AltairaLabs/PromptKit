---
title: Exec Protocol
description: Wire protocol reference for subprocess tools, evals, and hooks
sidebar:
  order: 9
---

The exec protocol defines how PromptKit communicates with external tool, eval, and hook subprocesses. Two modes are supported:

- **One-shot (exec)**: Process spawned per invocation, JSON over stdin/stdout
- **Long-running (server)**: Process spawned once, JSON-RPC 2.0 over stdin/stdout

## One-Shot Mode

```
runtime → stdin:  JSON request (single object)
runtime ← stdout: JSON response (single object)
runtime ← stderr: diagnostic logging (forwarded to runtime logger)
runtime ← exit:   0 = success, non-zero = error
```

The runtime spawns a new process for each invocation. The request is written to stdin as a single JSON object, and the response is read from stdout as a single JSON object.

## Server Mode (JSON-RPC 2.0)

```
runtime → stdin:  {"jsonrpc":"2.0","id":N,"method":"execute","params":{...}}
runtime ← stdout: {"jsonrpc":"2.0","id":N,"result":{...}}
```

Line-delimited (one JSON object per line). The process stays alive across invocations.

### JSON-RPC Request

| Field | Type | Description |
|-------|------|-------------|
| `jsonrpc` | string | Always `"2.0"` |
| `id` | integer | Request ID (monotonically increasing) |
| `method` | string | Always `"execute"` |
| `params` | object | Request parameters |
| `params.args` | object | Tool arguments from the LLM |

### JSON-RPC Response

| Field | Type | Description |
|-------|------|-------------|
| `jsonrpc` | string | Always `"2.0"` |
| `id` | integer | Must match request ID |
| `result` | any | Success result (mutually exclusive with `error`) |
| `error` | object | Error (mutually exclusive with `result`) |
| `error.code` | integer | Error code |
| `error.message` | string | Error message |

## Tool Protocol

### Request (stdin)

```json
{"args": {"city": "NYC", "units": "metric"}}
```

The `args` object contains the tool arguments as provided by the LLM, matching the tool's input schema.

### Response (stdout)

**Success:**

```json
{"result": {"temp": 22, "condition": "cloudy"}}
```

**Error:**

```json
{"error": "city not found"}
```

**Pending (human-in-the-loop):**

```json
{"pending": {"reason": "requires_approval", "message": "Refund of $500 requires manager approval"}}
```

A pending response pauses the pipeline until the operation is resolved externally.

## Eval Protocol

### Request (stdin)

```json
{
  "type": "sentiment_check",
  "params": {"language": "en"},
  "content": "I'd be happy to help you with that!",
  "context": {
    "messages": [],
    "turn_index": 1,
    "tool_calls": [],
    "variables": {},
    "metadata": {}
  }
}
```

| Field | Type | Description |
|-------|------|-------------|
| `type` | string | Eval type name |
| `params` | object | Eval-specific parameters from config |
| `content` | string | Current assistant output to evaluate |
| `context` | object | Conversation context |
| `context.messages` | array | Conversation history |
| `context.turn_index` | integer | Current turn index |
| `context.tool_calls` | array | Tool calls made during this turn |
| `context.variables` | object | Template variables in scope |
| `context.metadata` | object | Arbitrary metadata from the pipeline |

### Response (stdout)

```json
{
  "score": 0.85,
  "detail": "Sentiment polarity: 0.85",
  "data": {"polarity": 0.85, "subjectivity": 0.6}
}
```

| Field | Type | Description |
|-------|------|-------------|
| `score` | float | Numeric score (0.0--1.0) |
| `detail` | string | Human-readable explanation |
| `data` | object | Arbitrary structured data |

### How Callers Use the Result

- **Assertions**: `Threshold.Apply()` checks min/max score to determine pass/fail.
- **Guardrails**: `GuardrailHookAdapter` calls `IsPassed()` (score < 1.0 = fail) to enforce or monitor.
- **Standalone evals**: Score is recorded as a metric.

## Hook Protocol

Hooks receive a JSON object describing the hook type, phase, and event-specific payload.

### Provider Hook

**`before_call` phase:**

```json
{
  "hook": "provider",
  "phase": "before_call",
  "request": {
    "provider_id": "anthropic-main",
    "model": "claude-sonnet-4-20250514",
    "messages": [],
    "system_prompt": "You are a helpful assistant.",
    "round": 1
  }
}
```

**`after_call` phase** (includes a `response` field):

```json
{
  "hook": "provider",
  "phase": "after_call",
  "request": {
    "provider_id": "anthropic-main",
    "model": "claude-sonnet-4-20250514",
    "messages": [],
    "system_prompt": "You are a helpful assistant.",
    "round": 1
  },
  "response": {
    "provider_id": "anthropic-main",
    "model": "claude-sonnet-4-20250514",
    "message": {},
    "latency_ms": 450
  }
}
```

### Tool Hook

**`before_execution` phase:**

```json
{
  "hook": "tool",
  "phase": "before_execution",
  "request": {
    "name": "db_query",
    "args": {"query": "SELECT ..."},
    "call_id": "call_abc123"
  }
}
```

**`after_execution` phase** (includes a `response` field):

```json
{
  "hook": "tool",
  "phase": "after_execution",
  "request": {
    "name": "db_query",
    "args": {"query": "SELECT ..."},
    "call_id": "call_abc123"
  },
  "response": {
    "name": "db_query",
    "call_id": "call_abc123",
    "content": "{\"rows\": []}",
    "latency_ms": 120
  }
}
```

### Session Hook

```json
{
  "hook": "session",
  "phase": "session_start",
  "event": {
    "session_id": "sess_123",
    "conversation_id": "conv_456",
    "messages": [],
    "turn_index": 0
  }
}
```

### Hook Response

**Filter mode** (provider and tool hooks):

```json
{"allow": true}
```

```json
{"allow": false, "reason": "PII detected in input"}
```

```json
{"allow": false, "enforced": true, "reason": "PII redacted", "metadata": {"field": "ssn"}}
```

| Field | Type | Description |
|-------|------|-------------|
| `allow` | bool | Whether to allow the operation |
| `reason` | string | Denial reason (when `allow` is false) |
| `enforced` | bool | Whether the hook already applied enforcement |
| `metadata` | object | Arbitrary metadata passed to the pipeline |

**Observe mode** (session hooks):

```json
{"ack": true}
```

Session hooks are observe-only and do not influence the pipeline.

## Error Handling

- **One-shot**: Non-zero exit code indicates an error. Stderr is captured for diagnostics.
- **Server**: JSON-RPC error response. The process is restarted on unexpected termination.
- **Filter hooks**: Process failure results in deny (fail-closed safety).
- **Observe hooks**: Process failure is swallowed. The pipeline always continues.

## See Also

- Exec Tools (how-to)
- Exec Hooks (how-to)
- RuntimeConfig (reference)
