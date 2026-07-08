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

**Pending (schema only — not currently acted on):**

```json
{"pending": {"reason": "requires_approval", "message": "Refund of $500 requires manager approval"}}
```

`ExecExecutor.Execute` (`runtime/tools/exec_executor.go`) unmarshals this field but never reads it: it checks `error`, then `result`, and otherwise falls back to returning the raw stdout bytes as the tool result. There is currently **no subprocess-level pause/resume mechanism** for exec tools — a `pending`-only response does not suspend the pipeline. The working human-in-the-loop path is the in-process `sdk.OnToolAsync` / `PendingResult` (`sdk/tools/pending.go`), which is unrelated to this wire protocol. Do not rely on `pending` from an exec subprocess to suspend execution.

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
| `context.session_id` | string | Session identifier (omitted when empty) |
| `context.prompt_id` | string | Prompt identifier (omitted when empty) |

`context.tool_calls` items are a `toolCallView` (`runtime/evals/handlers/tool_call_views.go`), which has **no `json` tags** — populated entries serialize with PascalCase keys (`Name`, `Args`, `Result`, `Error`, `Index`), not snake_case, e.g. `{"Name": "db_query", "Args": {...}, "Result": "...", "Error": "", "Index": 0}`.

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

- **Assertions**: `AssertionEvalHandler` checks min/max score thresholds to determine pass/fail.
- **Guardrails**: `GuardrailHookAdapter` derives pass/fail directly from `Score` (`Score == nil || *Score < 1.0` → fail) to enforce or monitor. There is no `IsPassed()` method — the check is inlined (`runtime/hooks/guardrails/adapter.go`).
- **Standalone evals**: Score is recorded as a metric.

## Hook Protocol

Hooks receive a JSON object describing the hook type, phase, and event-specific payload.

:::caution
The envelope fields (`hook`, `phase`, `request`, `response`, `event`) are tagged on `execHookRequest` (`runtime/hooks/exec_hooks.go`) and always serialize as shown below. The **nested payload structs** — `ProviderRequest`, `ProviderResponse`, `ToolRequest`, `ToolResponse`, `SessionEvent` (`runtime/hooks/types.go`) — have **no `json` tags**, so `encoding/json` marshals them using the Go field names verbatim (PascalCase), not snake_case. A subprocess coded to snake_case keys inside `request`/`response`/`event` gets all-zero values. The examples below show the actual wire casing.
:::

### Provider Hook

**`before_call` phase:**

```json
{
  "hook": "provider",
  "phase": "before_call",
  "request": {
    "ProviderID": "anthropic-main",
    "Model": "claude-sonnet-4-20250514",
    "Messages": [],
    "SystemPrompt": "You are a helpful assistant.",
    "Round": 1,
    "Metadata": null
  }
}
```

**`after_call` phase** (includes a `response` field):

```json
{
  "hook": "provider",
  "phase": "after_call",
  "request": {
    "ProviderID": "anthropic-main",
    "Model": "claude-sonnet-4-20250514",
    "Messages": [],
    "SystemPrompt": "You are a helpful assistant.",
    "Round": 1,
    "Metadata": null
  },
  "response": {
    "ProviderID": "anthropic-main",
    "Model": "claude-sonnet-4-20250514",
    "Message": {},
    "Round": 1,
    "LatencyMs": 450
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
    "Name": "db_query",
    "Args": {"query": "SELECT ..."},
    "CallID": "call_abc123"
  }
}
```

**`after_execution` phase** (includes a `response` field):

```json
{
  "hook": "tool",
  "phase": "after_execution",
  "request": {
    "Name": "db_query",
    "Args": {"query": "SELECT ..."},
    "CallID": "call_abc123"
  },
  "response": {
    "Name": "db_query",
    "CallID": "call_abc123",
    "Content": "{\"rows\": []}",
    "Error": "",
    "LatencyMs": 120
  }
}
```

### Session Hook

```json
{
  "hook": "session",
  "phase": "session_start",
  "event": {
    "SessionID": "sess_123",
    "ConversationID": "conv_456",
    "Messages": [],
    "TurnIndex": 0,
    "Metadata": null
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

- [Exec Tools](/sdk/how-to/tools/exec-tools/) — how-to guide for subprocess tool bindings
- [Exec Hooks](/sdk/how-to/hooks/exec-hooks/) — how-to guide for external process hooks
- [RuntimeConfig](/sdk/reference/runtime-config/) — YAML config reference
