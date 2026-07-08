---
title: Redact Secrets from Logs
description: Understand and use PromptKit's automatic API-key redaction in debug logs
---

PromptKit's structured logger automatically redacts common secret patterns before writing debug
logs, so provider API keys and bearer tokens don't end up in log output even when full request/
response bodies are logged for debugging.

## Automatic Redaction

Debug-level logging helpers such as `logger.APIRequest` run their input through
`runtime/logger.RedactSensitiveData` before writing:

```go
// API keys are redacted automatically
logger.APIRequest("openai", "POST", url, headers, body)
// Output: sk-1234...[REDACTED] instead of the full key
```

### Supported Patterns

| Pattern | Example | Redacted form |
|---------|---------|----------------|
| OpenAI keys | `sk-abc123...` | first 4 chars + `...[REDACTED]` |
| Google keys | `AIzaXYZ...` | first 4 chars + `...[REDACTED]` |
| Bearer tokens | `Bearer xyz...` | `Bearer [REDACTED]` |

## Manual Redaction

Call `RedactSensitiveData` directly to sanitize a string before logging it yourself (e.g. in a
custom hook or middleware):

```go
redacted := logger.RedactSensitiveData(sensitiveString)
```

## See Also

- [Logging Reference](/runtime/reference/logging/) — complete `logger` package API
