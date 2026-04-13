---
title: Exec Hooks
description: Add external hooks in any language via subprocesses
sidebar:
  order: 17
---

Run external programs as hooks to intercept provider calls, tool executions, and session events. Hooks can be written in any language -- the runtime communicates over stdin/stdout using JSON.

---

## Quick Start

Add a provider hook to your `RuntimeConfig` YAML:

```yaml
spec:
  hooks:
    pii_redactor:
      command: ./hooks/pii-redactor
      hook: provider
      phases: [before_call]
      mode: filter
      timeout_ms: 3000
```

The runtime starts `./hooks/pii-redactor` before each provider call, sends the request as JSON on stdin, and reads the verdict from stdout.

---

## Hook Types

There are four hook types, each receiving a different payload on stdin.

### Provider Hooks

Intercept LLM provider requests and responses.

| Phase | When it fires |
|-------|---------------|
| `before_call` | Before the request is sent to the provider |
| `after_call` | After the provider returns a response |

### Tool Hooks

Intercept tool executions.

| Phase | When it fires |
|-------|---------------|
| `before_execution` | Before a tool handler runs |
| `after_execution` | After a tool handler returns |

### Session Hooks

Observe session lifecycle events.

| Phase | When it fires |
|-------|---------------|
| `session_start` | A new session begins |
| `session_update` | The session state changes |
| `session_end` | The session ends |

### Eval Hooks

Observe eval results as they are produced by the runner. Eval hooks are **always fire-and-forget** — `mode` and `phases` are ignored; the subprocess never gates execution.

| Phase | When it fires |
|-------|---------------|
| (implicit) | Once per executed eval, after the handler runs, before the result is emitted |

---

## Modes

Each hook runs in one of two modes.

### Filter (fail-closed)

The hook decides whether the operation proceeds. If the subprocess returns `{"allow": false}`, crashes, or exceeds the timeout, the operation is **denied**.

```yaml
pii_redactor:
  command: ./hooks/pii-redactor
  hook: provider
  phases: [before_call, after_call]
  mode: filter
  timeout_ms: 3000
```

### Observe (fire-and-forget)

The hook receives the event but cannot block the pipeline. Subprocess failures are swallowed -- the operation always continues.

```yaml
audit_logger:
  command: ./hooks/audit-logger
  hook: session
  phases: [session_start, session_update, session_end]
  mode: observe
```

---

## Phase Gating

Hooks only run for the phases listed in `phases`. A provider hook configured with `phases: [before_call]` will not fire after the provider responds.

```yaml
spec:
  hooks:
    input_guard:
      command: ./hooks/input-guard
      hook: provider
      phases: [before_call]   # runs before the call only
      mode: filter

    response_logger:
      command: ./hooks/response-logger
      hook: provider
      phases: [after_call]    # runs after the call only
      mode: observe
```

---

## Hook Protocol

The runtime starts the subprocess, writes a JSON object to stdin, and reads a JSON object from stdout.

### Provider Hook

**stdin:**

```json
{
  "hook": "provider",
  "phase": "before_call",
  "request": {
    "messages": [{"role": "user", "content": "..."}],
    "model": "gpt-4o"
  }
}
```

**stdout -- allow:**

```json
{"allow": true}
```

**stdout -- deny:**

```json
{"allow": false, "reason": "PII detected in input"}
```

**stdout -- deny with enforcement detail:**

```json
{"allow": false, "enforced": true, "reason": "PII redacted"}
```

### Tool Hook

**stdin:**

```json
{
  "hook": "tool",
  "phase": "before_execution",
  "request": {
    "name": "db_query",
    "args": {"sql": "SELECT * FROM users"}
  }
}
```

**stdout -- allow:**

```json
{"allow": true}
```

**stdout -- deny:**

```json
{"allow": false, "reason": "Query not in allowlist"}
```

### Session Hook

**stdin:**

```json
{
  "hook": "session",
  "phase": "session_start",
  "event": {
    "session_id": "abc-123",
    "messages": []
  }
}
```

**stdout:**

```json
{"ack": true}
```

### Eval Hook

The eval runner writes the raw `EvalResult` JSON to the subprocess's stdin. Stdout is ignored — this is strictly fire-and-forget.

**stdin:**

```json
{
  "eval_id": "assertion_1_tool_called",
  "type": "assertion",
  "score": 1.0,
  "passed": true,
  "duration_ms": 3,
  "explanation": "tool 'lookup_order' was called",
  "details": {"tool_name": "lookup_order"}
}
```

**stdout:** discarded.

Errors, non-zero exits, and timeouts are logged via the runtime logger but never propagate to the eval pipeline. Missing stdout, empty stdout, or any other I/O anomaly is not an error.

---

## Examples

### PII Redactor (Provider / Filter)

Block requests containing personally identifiable information before they reach the provider.

```yaml
spec:
  hooks:
    pii_redactor:
      command: ./hooks/pii-redactor
      hook: provider
      phases: [before_call, after_call]
      mode: filter
      timeout_ms: 3000
```

A minimal implementation in Python:

```python
#!/usr/bin/env python3
import json, sys, re

payload = json.load(sys.stdin)
messages = payload.get("request", {}).get("messages", [])

PII_PATTERN = re.compile(r"\b\d{3}-\d{2}-\d{4}\b")  # SSN pattern

for msg in messages:
    content = msg.get("content", "")
    if isinstance(content, str) and PII_PATTERN.search(content):
        json.dump({"allow": False, "reason": "PII detected in input"}, sys.stdout)
        sys.exit(0)

json.dump({"allow": True}, sys.stdout)
```

### Audit Logger (Session / Observe)

Log session events to an external system without blocking the pipeline.

```yaml
spec:
  hooks:
    audit_logger:
      command: ./hooks/audit-logger
      hook: session
      phases: [session_start, session_update, session_end]
      mode: observe
```

```python
#!/usr/bin/env python3
import json, sys, datetime

payload = json.load(sys.stdin)
entry = {
    "timestamp": datetime.datetime.utcnow().isoformat(),
    "phase": payload["phase"],
    "session_id": payload.get("event", {}).get("session_id"),
}

with open("/var/log/promptkit-audit.jsonl", "a") as f:
    f.write(json.dumps(entry) + "\n")

json.dump({"ack": True}, sys.stdout)
```

### Metrics Exporter (Eval / Fire-and-forget)

Push every eval result to an external metrics backend without blocking the eval pipeline.

```yaml
spec:
  hooks:
    eval_metrics:
      command: python3
      args: [./hooks/eval-metrics.py]
      hook: eval
      timeout_ms: 5000
```

```python
#!/usr/bin/env python3
import json, sys, urllib.request

result = json.load(sys.stdin)
payload = {
    "eval_id": result["eval_id"],
    "score": result.get("score", 0.0),
    "passed": result.get("passed", False),
    "duration_ms": result.get("duration_ms", 0),
}

req = urllib.request.Request(
    "https://metrics.internal/evals",
    data=json.dumps(payload).encode(),
    headers={"Content-Type": "application/json"},
)
urllib.request.urlopen(req, timeout=2)  # stdout is ignored
```

### Query Allowlist (Tool / Filter)

Only permit pre-approved SQL queries to run.

```yaml
spec:
  hooks:
    query_allowlist:
      command: python3
      args: [./hooks/query-allowlist.py]
      hook: tool
      phases: [before_execution]
      mode: filter
```

```python
#!/usr/bin/env python3
import json, sys

ALLOWED_QUERIES = {
    "SELECT * FROM products WHERE category = ?",
    "SELECT COUNT(*) FROM orders WHERE status = ?",
}

payload = json.load(sys.stdin)
request = payload.get("request", {})

if request.get("name") != "db_query":
    json.dump({"allow": True}, sys.stdout)
    sys.exit(0)

sql = request.get("args", {}).get("sql", "")
if sql in ALLOWED_QUERIES:
    json.dump({"allow": True}, sys.stdout)
else:
    json.dump({"allow": False, "reason": "Query not in allowlist"}, sys.stdout)
```

---

## Full Configuration Reference

```yaml
spec:
  hooks:
    pii_redactor:
      command: ./hooks/pii-redactor
      hook: provider
      phases: [before_call, after_call]
      mode: filter
      timeout_ms: 3000

    query_allowlist:
      command: python3
      args: [./hooks/query-allowlist.py]
      hook: tool
      phases: [before_execution]
      mode: filter

    audit_logger:
      command: ./hooks/audit-logger
      hook: session
      phases: [session_start, session_update, session_end]
      mode: observe

    eval_metrics:
      command: python3
      args: [./hooks/eval-metrics.py]
      hook: eval
      timeout_ms: 5000
```

| Field | Required | Description |
|-------|----------|-------------|
| `command` | Yes | Path to the executable or interpreter |
| `args` | No | Additional arguments passed to the command |
| `hook` | Yes | Hook type: `provider`, `tool`, `session`, or `eval` |
| `phases` | Yes (ignored for `eval`) | List of phases this hook fires on |
| `mode` | Yes (ignored for `eval`) | `filter` (fail-closed) or `observe` (fire-and-forget) |
| `timeout_ms` | No | Subprocess timeout in milliseconds |

---

## See Also

- [Use RuntimeConfig](/sdk/how-to/use-runtime-config/) -- configure the runtime via YAML
- [Hooks Reference](/runtime/reference/hooks/) -- full runtime hook API
- [Validation Concepts](/concepts/validation/) -- how hooks fit into the validation pipeline
- [Exec Protocol Reference](/sdk/reference/exec-protocol/) -- detailed subprocess protocol specification
