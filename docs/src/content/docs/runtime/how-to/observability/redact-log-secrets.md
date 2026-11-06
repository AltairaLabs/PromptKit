---
title: Redact secrets from logs
description: Keep provider API keys and bearer tokens out of debug logs
verified:
  commit: 8489bf3d95c7498f9de0f024769db9399c24e884
  sources:
    - runtime/logger/logger.go
---

Strip API keys and bearer tokens from strings before they reach your logs, using `logger.RedactSensitiveData`.

## Prerequisites

- The runtime logger package: `github.com/AltairaLabs/PromptKit/runtime/v2/logger`.

## Steps

1. Use the API logging helpers for provider traffic. `logger.APIRequest` and `logger.APIResponse` write at debug level and redact the URL, header values and bodies before writing. Both return immediately when debug logging is off.

   ```go
   logger.APIRequest("openai", "POST", url, headers, body)
   logger.APIResponse("openai", resp.StatusCode, string(respBody), nil)
   ```

2. Redact any other string you log yourself, such as in a custom hook or stage.

   ```go
   logger.Debug("calling provider", "endpoint", logger.RedactSensitiveData(endpoint))
   ```

## Verify

Redact a sample string and print it.

```go
fmt.Println(logger.RedactSensitiveData("Authorization: Bearer abc123"))
// Authorization: Bearer [REDACTED]
```

`RedactSensitiveData` replaces three patterns:

| Pattern | Matches | Result |
|---|---|---|
| OpenAI keys | `sk-` followed by 32 or more letters or digits | first 4 characters + `...[REDACTED]` |
| Google keys | `AIza` followed by 35 letters, digits, `_` or `-` | first 4 characters + `...[REDACTED]` |
| Bearer tokens | `Bearer`, whitespace, then an opaque, base64 or dotted JWT token | `Bearer [REDACTED]` |

## Troubleshoot

- A secret still appears in a log line: the function matches only the patterns above. Keys from other providers and values logged through `logger.Debug` or `logger.Info` are not redacted unless you pass them through `RedactSensitiveData`.

## Related

- [Logging reference](/runtime/reference/logging/)
