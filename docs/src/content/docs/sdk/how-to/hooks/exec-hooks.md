---
title: Run exec hooks
description: Intercept provider calls, tool executions, and session events with external programs written in any language
sidebar:
  order: 17
verified:
  commit: 8489bf3d95c7498f9de0f024769db9399c24e884
  sources:
    - pkg/config/runtime_config.go
    - runtime/evals/exec_hook.go
    - runtime/evals/runner.go
    - runtime/hooks/exec_build.go
    - runtime/hooks/exec_hooks.go
    - runtime/hooks/execconfig/execconfig.go
    - runtime/hooks/sandbox/direct/direct.go
    - runtime/hooks/types.go
---

Run an external program as a hook. The runtime writes a JSON payload to the program's stdin and, for filter hooks, reads a JSON verdict from its stdout.

## Prerequisites

- An SDK application that loads a `RuntimeConfig` with `sdk.WithRuntimeConfig`. See [Use RuntimeConfig](/sdk/how-to/conversations/use-runtime-config/).
- An executable hook program, or an interpreter such as `python3` on the host that runs the hook.

## Steps

### 1. Write the hook program

This provider hook denies any request that contains a US Social Security number. Save it as `./hooks/pii-redactor` and make it executable.

```python
#!/usr/bin/env python3
import json
import re
import sys

payload = json.load(sys.stdin)
messages = payload["request"].get("Messages") or []

SSN = re.compile(r"\b\d{3}-\d{2}-\d{4}\b")

for msg in messages:
    content = msg.get("content", "")
    if isinstance(content, str) and SSN.search(content):
        json.dump({"allow": False, "reason": "PII detected in input"}, sys.stdout)
        sys.exit(0)

json.dump({"allow": True}, sys.stdout)
```

The payload keys inside `request`, `response` and `event` use Go field names (`Messages`, `ProviderID`, `Name`, `Args`, `SessionID`). The envelope keys (`hook`, `phase`, `request`, `response`, `event`) and `types.Message` fields (`role`, `content`) are lowercase. See the [exec protocol reference](/sdk/reference/exec-protocol/) for every payload.

### 2. Declare the hook in RuntimeConfig

```yaml
apiVersion: promptkit.altairalabs.ai/v1alpha1
kind: RuntimeConfig
spec:
  hooks:
    pii_redactor:
      command: ./hooks/pii-redactor
      hook: provider
      phases: [before_call]
      mode: filter
      timeout_ms: 3000
```

| Field | Description |
|-------|-------------|
| `command` | Required. Executable path, or an interpreter. A relative path resolves against the host process's working directory. |
| `args` | Arguments passed to `command`. |
| `env` | Environment entries added to the host environment, which the subprocess also inherits: a bare name forwards the host value, `KEY=value` sets it literally. |
| `hook` | Required. `provider`, `tool`, `session` or `eval`. |
| `phases` | Phases the hook fires on. A hook with no phases never fires. Ignored for `eval`. |
| `mode` | `filter` (default) or `observe`. Ignored for `eval`. |
| `timeout_ms` | Subprocess timeout. Defaults to 10000 (5000 for `eval`). |
| `sandbox` | Name of a sandbox declared under `spec.sandboxes`. See [Sandbox exec hooks](/sdk/how-to/hooks/sandbox-hooks/). |

Phases per hook type:

| Hook | Phases |
|------|--------|
| `provider` | `before_call`, `after_call` |
| `tool` | `before_execution`, `after_execution` |
| `session` | `session_start`, `session_update`, `session_end` |
| `eval` | none: runs once per eval result |

### 3. Load the config

```go
conv, err := sdk.Open("./pack.json", "chat",
	sdk.WithRuntimeConfig("./runtime.yaml"),
)
```

### 4. Choose filter or observe

In `filter` mode the hook decides whether the operation proceeds. A response with `"allow": false`, a non-zero exit, a timeout or invalid JSON denies the operation.

In `observe` mode the hook receives the event and cannot block it. Failures are discarded and the operation continues.

A filter response takes these fields:

| Field | Description |
|-------|-------------|
| `allow` | `true` lets the operation proceed. |
| `reason` | Denial reason. |
| `enforced` | `true` reports that the hook already applied enforcement (for example, replaced content). The pipeline records the result and continues. |
| `metadata` | Object attached to the decision. |

Session hooks have no verdict. A filter-mode session hook returns an error to the runtime when the process fails, and its stdout is ignored.

### 5. Add other hook types

A tool hook that permits only approved SQL. The tool arguments arrive as a JSON object under `request.Args`.

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
import json
import sys

ALLOWED = {
    "SELECT * FROM products WHERE category = ?",
    "SELECT COUNT(*) FROM orders WHERE status = ?",
}

request = json.load(sys.stdin)["request"]

if request["Name"] != "db_query":
    json.dump({"allow": True}, sys.stdout)
    sys.exit(0)

sql = (request.get("Args") or {}).get("sql", "")
if sql in ALLOWED:
    json.dump({"allow": True}, sys.stdout)
else:
    json.dump({"allow": False, "reason": "Query not in allowlist"}, sys.stdout)
```

A session hook that logs lifecycle events without blocking:

```yaml
spec:
  hooks:
    audit_logger:
      command: ./hooks/audit-logger
      hook: session
      phases: [session_start, session_update, session_end]
      mode: observe
```

An eval hook receives each eval result as the JSON of `evals.EvalResult` (`eval_id`, `type`, `score`, `passed`, `duration_ms`, `explanation`, `value`, ...). The runner calls it after the handler runs and before the result is emitted. Stdout is discarded, and errors and timeouts are logged and never reach the eval pipeline.

```yaml
spec:
  hooks:
    eval_metrics:
      command: python3
      args: [./hooks/eval-metrics.py]
      hook: eval
      timeout_ms: 5000
```

## Verify

Send a message that contains `123-45-6789`. The `before_call` hook denies the request and the provider is never called. Send a message without it and the call proceeds.

To test a hook in isolation, build it from the config and call it directly:

```go
rc, err := config.LoadRuntimeConfig("./runtime.yaml")
if err != nil {
	return err
}
provider, _, _, err := hooks.BuildExecHooks(rc.Spec.Hooks, nil)
if err != nil {
	return err
}
d := provider[0].BeforeCall(ctx, &hooks.ProviderRequest{
	Messages: []types.Message{{Role: "user", Content: "my ssn is 123-45-6789"}},
})
fmt.Println(d.Allow, d.Reason) // false PII detected in input
```

## Troubleshoot

- A filter hook denies every call: run the program by hand with a sample payload on stdin. The denial reason includes the process error and its stderr.
- A hook reads empty values from `request`: the nested keys are PascalCase, not snake_case.
- A hook never runs: check that `phases` lists the phase and spells it exactly as in the table above.
- A hook exceeds its timeout under load: raise `timeout_ms`.

## Related

- [Custom hooks](/sdk/how-to/hooks/custom-hooks/): the same hook types in Go
- [The hook system](/sdk/explanation/hooks/): when to choose exec or Go hooks
- [Hooks reference](/runtime/reference/hooks/): the runtime hook API
- [Exec protocol reference](/sdk/reference/exec-protocol/): every payload and response
- [Sandbox exec hooks](/sdk/how-to/hooks/sandbox-hooks/): run hook subprocesses in a container or sidecar
