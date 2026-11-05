---
title: Exec Protocol
description: Wire protocol reference for subprocess tools, evals, and hooks
sidebar:
  order: 9
verified:
  commit: 004b9cbbd4d40f770a2e3c76053b600a55189355
  sources:
    - runtime/evals/exec_hook.go
    - runtime/evals/handlers/exec_handler.go
    - runtime/evals/handlers/external_eval.go
    - runtime/evals/handlers/tool_call_views.go
    - runtime/evals/runner.go
    - runtime/evals/types.go
    - runtime/evals/wrappers.go
    - runtime/hooks/exec_build.go
    - runtime/hooks/exec_hooks.go
    - runtime/hooks/execconfig/execconfig.go
    - runtime/hooks/guardrails/adapter.go
    - runtime/hooks/registry.go
    - runtime/hooks/sandbox/direct/direct.go
    - runtime/hooks/types.go
    - runtime/pipeline/stage/stages_provider.go
    - runtime/tools/exec_executor.go
    - runtime/tools/registry.go
    - runtime/tools/server_executor.go
    - sdk/exec_tools.go
---

The exec protocol is the JSON contract between the runtime and the subprocesses declared in a [RuntimeConfig](/sdk/reference/runtime-config/): exec tools, exec evals, and exec hooks. It is implemented in `runtime/tools` (`exec_executor.go`, `server_executor.go`), `runtime/evals` and `runtime/hooks`.

| Binding | Modes | stdin | stdout |
|---------|-------|-------|--------|
| Tool | one-shot (`runtime: exec`), long-running (`runtime: server`) | `{"args": ...}` or a JSON-RPC request | tool result, or a JSON-RPC response |
| Eval | one-shot | eval request | eval result |
| Provider, tool, session hook | one-shot | hook request | allow/deny decision (filter mode), discarded (observe mode) |
| Eval hook | one-shot | the eval result | discarded |

Only tool bindings support server mode.

## One-shot mode

```text
runtime → stdin:  JSON request (single object)
runtime ← stdout: JSON response (single object)
runtime ← stderr: captured for diagnostics
runtime ← exit:   0 = success, non-zero = error
```

The runtime spawns a new process for each invocation. It writes the request to stdin as one JSON object and reads the response from stdout as one JSON object.

The runtime does not log stderr. When the process fails, stderr is appended to the error message, as `(stderr: ...)`. Eval hooks log it at warn level.

## Server mode (JSON-RPC 2.0)

```text
runtime → stdin:  {"jsonrpc":"2.0","id":N,"method":"execute","params":{...}}
runtime ← stdout: {"jsonrpc":"2.0","id":N,"result":{...}}
```

Server mode applies to tools only. Messages are line-delimited (one JSON object per line, at most 10 MB per line). The process starts on the first call to the tool and stays alive across calls. Each tool has its own process. Calls to one process can be in flight concurrently, and the runtime matches responses to requests by `id`.

### JSON-RPC request

| Field | Type | Description |
|-------|------|-------------|
| `jsonrpc` | string | Always `"2.0"` |
| `id` | integer | Request ID, increasing per process |
| `method` | string | Always `"execute"` |
| `params` | object | Request parameters |
| `params.args` | object | Tool arguments from the LLM |

### JSON-RPC response

| Field | Type | Description |
|-------|------|-------------|
| `jsonrpc` | string | Always `"2.0"` |
| `id` | integer | Must match the request ID |
| `result` | any | Success result. A response with neither `result` nor `error` returns `null`. |
| `error` | object | Error (mutually exclusive with `result`) |
| `error.code` | integer | Error code |
| `error.message` | string | Error message |

A line that is not valid JSON fails every pending call. A response with an `id` that matches no pending call is dropped. A line over 10 MB kills the process.

## Environment

`env` on a binding lists host environment variable names to forward.

| Binding | Subprocess environment |
|---------|------------------------|
| Exec tool, `env` empty | The host environment |
| Exec tool, `env` set | Only the listed variables that are set on the host |
| Server tool, exec eval, hooks, eval hooks | The host environment, plus the listed variables |

## Timeouts

| Binding | Default | On timeout |
|---------|---------|------------|
| Tool | 30 seconds | The call fails with a tool timeout error |
| Eval | 30 seconds | The result is an error |
| Provider, tool, session hook | 10 seconds | A filter hook denies; an observe hook is ignored |
| Eval hook | 5 seconds | Logged and ignored |

`timeout_ms` on the binding overrides the default. The eval runner gives an exec eval its binding timeout plus a 5 second grace period, even when the runner's own per-eval timeout (`EvaluateOpts.Timeout`, `evals.WithTimeout`) is shorter. That holds when the eval is wrapped in `type: assertion` or `type: guardrail`.

## Tool protocol

### Request (stdin)

```json
{"args": {"city": "NYC", "units": "metric"}}
```

The `args` object contains the tool arguments as provided by the LLM, matching the tool's input schema.

### Response (stdout)

Success:

```json
{"result": {"temp": 22, "condition": "cloudy"}}
```

Error:

```json
{"error": "city not found"}
```

| Field | Type | Description |
|-------|------|-------------|
| `result` | any | The tool result passed to the model |
| `error` | string | A non-empty value fails the call with this message |

The runtime checks `error` first, then `result`. A response with neither is returned to the model as the raw stdout bytes.

## Eval protocol

### Request (stdin)

```json
{
  "type": "sentiment_check",
  "params": {"language": "en"},
  "content": "I'd be happy to help you with that!",
  "context": {
    "messages": [{"role": "user", "content": "Can you help?"}],
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
| `params` | object | Eval-specific parameters from config. Omitted when empty. |
| `content` | string | Current assistant output to evaluate |
| `context` | object | Conversation context |
| `context.messages` | array | Conversation history, as `{"role", "content"}` objects. Omitted when empty. |
| `context.turn_index` | integer | Current turn index |
| `context.tool_calls` | array | Tool calls made during this turn. Omitted when empty. |
| `context.variables` | object | Template variables in scope. Omitted when empty. |
| `context.metadata` | object | Metadata the host attached to the eval context. Entries that cannot be encoded as JSON (live objects such as a tool registry) are dropped; the rest pass through unchanged. Omitted when empty. |
| `context.session_id` | string | Session identifier (omitted when empty) |
| `context.prompt_id` | string | Prompt identifier (omitted when empty) |

`context.tool_calls` items are a `toolCallView` (`runtime/evals/handlers/tool_call_views.go`), which has no `json` tags. Populated entries serialize with PascalCase keys (`Name`, `Args`, `Result`, `Error`, `Index`), not snake_case, for example `{"Name": "db_query", "Args": {...}, "Result": "...", "Error": "", "Index": 0}`. `Index` is the turn index of the call.

### Response (stdout)

```json
{
  "score": 0.85,
  "detail": "Sentiment polarity: 0.85",
  "data": {"polarity": 0.85, "subjectivity": 0.6},
  "metrics": {"polarity": 0.85, "subjectivity": 0.6}
}
```

| Field | Type | Description |
|-------|------|-------------|
| `score` | float | The eval's score. Reported as both `Score` and `MetricValue`. |
| `detail` | string | Human-readable explanation |
| `data` | object | Arbitrary structured data, reported in `Value` and `Details` |
| `metrics` | object | Named numeric sub-scores, reported as the result's `Metrics`. Every value must be a number. |
| `error` | string | Set when the program could not take its measurement. The result is an error, and `data` and `metrics` are still reported. |

The result is an error, not a score of 0, when the process exits non-zero, runs past `timeout_ms`, writes output that is not this JSON, or sets `error`. An assertion fails on an error whatever its `min_score` / `max_score`. A score of 0 would pass a `max_score` gate, so an eval that failed to run would read as "no findings".

### How callers use the result

- Assertions: `AssertionEvalHandler` (`runtime/evals/wrappers.go`) fails a result that carries an error, then checks `min_score` / `max_score`. With neither set, the score must be at least 1.0.
- Guardrails: `GuardrailHookAdapter` triggers on a result with no score, a score below the minimum (default 1.0), or a score above the maximum (`ScoreThresholds.Triggered`, `runtime/evals/wrappers.go`), then enforces or monitors.
- Standalone evals: the score is recorded as a metric.

## Hook protocol

A hook subprocess receives one JSON object that names the hook type and phase and carries the event payload.

The envelope fields (`hook`, `phase`, `request`, `response`, `event`) are tagged on `execHookRequest` (`runtime/hooks/exec_hooks.go`) and serialize as shown below. The payload structs inside them (`ProviderRequest`, `ProviderResponse`, `ToolRequest`, `ToolResponse`, `SessionEvent` in `runtime/hooks/types.go`) have no `json` tags, so `encoding/json` writes their Go field names verbatim (PascalCase). A subprocess that reads snake_case keys inside `request`, `response` or `event` sees zero values. Message objects inside `Messages` and `Message` use the `types.Message` tags (`role`, `content`).

A hook runs only in the phases listed under `phases` on its binding. A hook with no `phases` never runs.

### Provider hook

Phases: `before_call`, `after_call`.

`before_call`:

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
    "Metadata": null,
    "TurnIndex": 0,
    "Prompt": null,
    "Replacement": ""
  }
}
```

`after_call` adds a `response` field:

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
    "Metadata": null,
    "TurnIndex": 0,
    "Prompt": null,
    "Replacement": ""
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

| `request` field | Type | Description |
|-----------------|------|-------------|
| `ProviderID` | string | Provider ID |
| `Model` | string | Model name |
| `Messages` | array | Messages sent to the provider |
| `SystemPrompt` | string | System prompt |
| `Round` | integer | Tool-loop round within the turn |
| `Metadata` | object | Call metadata |
| `TurnIndex` | integer | 0-based turn index |
| `Prompt` | object | The pack's definition of the prompt, or `null` when undeclared |
| `Replacement` | string | Empty unless an earlier in-process hook set it. An exec hook supplies replacement text through `metadata.replacement` in an enforced response. |

| `response` field | Type | Description |
|------------------|------|-------------|
| `ProviderID`, `Model`, `Round` | | As in `request` |
| `Message` | object | The assistant message |
| `LatencyMs` | integer | Call latency in milliseconds |

### Tool hook

Phases: `before_execution`, `after_execution`.

`before_execution`:

```json
{
  "hook": "tool",
  "phase": "before_execution",
  "request": {
    "Name": "db_query",
    "Args": {"query": "SELECT ..."},
    "CallID": "call_abc123",
    "Declaration": null
  }
}
```

`after_execution` adds a `response` field:

```json
{
  "hook": "tool",
  "phase": "after_execution",
  "request": {
    "Name": "db_query",
    "Args": {"query": "SELECT ..."},
    "CallID": "call_abc123",
    "Declaration": null
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

| `request` field | Type | Description |
|-----------------|------|-------------|
| `Name` | string | Tool name |
| `Args` | object | Arguments the model supplied |
| `CallID` | string | Tool call ID |
| `Declaration` | object | The pack's definition of the tool, or `null` when the pack does not declare it |

| `response` field | Type | Description |
|------------------|------|-------------|
| `Name`, `CallID` | string | As in `request` |
| `Content` | string | Tool result text |
| `Error` | string | Error message, empty on success |
| `LatencyMs` | integer | Execution latency in milliseconds |

### Session hook

Phases: `session_start`, `session_update`, `session_end`.

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

| `event` field | Type | Description |
|---------------|------|-------------|
| `SessionID` | string | Session ID |
| `ConversationID` | string | Conversation ID |
| `Messages` | array | Conversation history |
| `TurnIndex` | integer | Current turn index |
| `Metadata` | object | Session metadata |

### Hook modes

`mode` on the binding is `filter` or `observe`. It is optional and defaults to `filter`.

| Mode | Provider and tool hooks | Session hooks |
|------|-------------------------|---------------|
| `filter` | The runtime waits for the process and reads its decision from stdout. | The runtime waits for the process. It does not read stdout, and a failed process returns an error. |
| `observe` | The runtime waits for the process, discards stdout, and always continues. | Same as provider and tool hooks. |

### Hook response

Filter-mode provider and tool hooks write a decision to stdout:

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
| `allow` | bool | Whether to allow the operation. Missing counts as `false`. |
| `reason` | string | Denial reason (when `allow` is false) |
| `enforced` | bool | Provider hooks only. On `before_call`, the runtime skips the provider call and returns a canned assistant turn. On `after_call`, it replaces the response. Either way the pipeline continues instead of returning an error. |
| `metadata` | object | Arbitrary metadata passed to the pipeline. On an enforced `before_call`, `metadata.replacement` is the assistant text returned in place of the blocked call. |

The runtime does not read `ack` or any other field. Session hooks and observe-mode hooks produce no decision, so their stdout has no required shape.

## Eval hook protocol

An eval hook runs once per eval result. The runtime writes the JSON-encoded `EvalResult` to stdin and discards stdout. `phases` and `mode` are ignored.

```json
{
  "eval_id": "sentiment",
  "type": "sentiment_check",
  "score": 0.85,
  "metric_value": 0.85,
  "explanation": "Sentiment polarity: 0.85",
  "duration_ms": 412
}
```

`EvalResult` (`runtime/evals/types.go`) tags every field, so keys are snake_case. The fields beyond the example are `kind`, `passed`, `value`, `metrics`, `error`, `message`, `details`, `violations`, `skipped`, `skip_reason`, `session_id` and `turn_index`; all except `eval_id`, `type` and `duration_ms` are omitted when empty.

## Failure handling

| Binding | Process failure, timeout or invalid output |
|---------|--------------------------------------------|
| Tool, one-shot | The call fails with an error that includes stderr |
| Tool, server | The call fails. The next call starts a new process. A call in flight when the process exits fails with the exit status and stderr. |
| Eval | The result is an error |
| Filter hook (provider, tool) | Deny, with a reason that names the hook and the failure |
| Filter hook (session) | The hook returns an error |
| Observe hook | Ignored. The pipeline continues. |
| Eval hook | Logged and ignored |

## See also

- [Exec Tools](/sdk/how-to/tools/exec-tools/): how-to guide for subprocess tool bindings
- [Exec Hooks](/sdk/how-to/hooks/exec-hooks/): how-to guide for external process hooks
- [RuntimeConfig](/sdk/reference/runtime-config/): YAML config reference
