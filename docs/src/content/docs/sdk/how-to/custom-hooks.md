---
title: Write Custom Hooks
sidebar:
  order: 8
---

How to implement each PromptKit hook type in Go and register it with an SDK conversation. For subprocess-backed hooks in any language, see [Exec Hooks](/sdk/how-to/exec-hooks/).

## Pick a hook type

| Goal | Hook type |
|---|---|
| Inspect or block an LLM request/response | [`ProviderHook`](#providerhook) |
| Same as above, but abort mid-stream on bad chunks | [`ProviderHook` + `ChunkInterceptor`](#providerhook--chunkinterceptor) |
| Inspect or block a tool call | [`ToolHook`](#toolhook) |
| Observe session start/turn/end | [`SessionHook`](#sessionhook) |
| Push or mutate eval results | [`EvalHook`](#evalhook) |

For the conceptual difference between Decision-based and observational hooks, see [The Hook System](/sdk/explanation/hooks/).

## ProviderHook

Implement `Name`, `BeforeCall`, and `AfterCall`. Return `hooks.Allow` to continue, `hooks.Deny(reason)` to abort with a `*hooks.HookDeniedError`, or `hooks.Enforced(reason, metadata)` if you mutated the request/response in place and want the pipeline to continue with the modified content.

```go
import "github.com/AltairaLabs/PromptKit/runtime/hooks"

type PIIHook struct{}

func (h *PIIHook) Name() string { return "pii_filter" }

func (h *PIIHook) BeforeCall(ctx context.Context, req *hooks.ProviderRequest) hooks.Decision {
    return hooks.Allow
}

func (h *PIIHook) AfterCall(ctx context.Context, req *hooks.ProviderRequest, resp *hooks.ProviderResponse) hooks.Decision {
    if containsSSN(resp.Message.Content()) {
        return hooks.Deny("response contains SSN")
    }
    return hooks.Allow
}
```

Register it:

```go
conv, _ := sdk.Open("./app.pack.json", "chat",
    sdk.WithProviderHook(&PIIHook{}),
)
```

## ProviderHook + ChunkInterceptor

If your hook also implements `ChunkInterceptor`, the registry routes streaming chunks to it. This lets you abort a streaming response mid-flight, saving API costs when the model starts producing something you don't want to ship.

```go
type StreamingPIIHook struct {
    buffer strings.Builder
}

func (h *StreamingPIIHook) Name() string { return "streaming_pii" }

func (h *StreamingPIIHook) BeforeCall(_ context.Context, _ *hooks.ProviderRequest) hooks.Decision {
    h.buffer.Reset()
    return hooks.Allow
}

func (h *StreamingPIIHook) AfterCall(_ context.Context, _ *hooks.ProviderRequest, _ *hooks.ProviderResponse) hooks.Decision {
    return hooks.Allow
}

// Implement ChunkInterceptor for streaming checks
func (h *StreamingPIIHook) OnChunk(_ context.Context, chunk *providers.StreamChunk) hooks.Decision {
    h.buffer.WriteString(chunk.Content)
    if containsSSN(h.buffer.String()) {
        return hooks.Deny("streaming content contains SSN")
    }
    return hooks.Allow
}
```

A streaming denial surfaces as `*providers.ValidationAbortError` rather than `*hooks.HookDeniedError` — the chunk loop has already started by the time it fires. Handle both in your error path if you care which one tripped.

## ToolHook

Same shape as `ProviderHook` but firing around tool execution. Use `BeforeExecution` to gate calls (e.g. allowlist) and `AfterExecution` to observe or sanitise results.

```go
type AuditToolHook struct {
    logger *slog.Logger
}

func (h *AuditToolHook) Name() string { return "audit_tools" }

func (h *AuditToolHook) BeforeExecution(_ context.Context, req hooks.ToolRequest) hooks.Decision {
    h.logger.Info("tool called", "name", req.Name, "callID", req.CallID)
    return hooks.Allow
}

func (h *AuditToolHook) AfterExecution(_ context.Context, req hooks.ToolRequest, resp hooks.ToolResponse) hooks.Decision {
    if resp.Error != "" {
        h.logger.Error("tool failed", "name", req.Name, "error", resp.Error)
    }
    return hooks.Allow
}
```

Register via `sdk.WithToolHook(&AuditToolHook{logger: slog.Default()})`.

## SessionHook

Returns plain Go `error` — there's no decision/enforcement distinction. Non-nil errors propagate to the caller. Hooks that only observe should always return `nil`.

```go
type SessionLogger struct {
    logger *slog.Logger
}

func (h *SessionLogger) Name() string { return "session_logger" }

func (h *SessionLogger) OnSessionStart(_ context.Context, e hooks.SessionEvent) error {
    h.logger.Info("session started", "session_id", e.SessionID, "conv_id", e.ConversationID)
    return nil
}

func (h *SessionLogger) OnSessionUpdate(_ context.Context, e hooks.SessionEvent) error {
    h.logger.Info("turn complete", "session_id", e.SessionID, "turn", e.TurnIndex)
    return nil
}

func (h *SessionLogger) OnSessionEnd(_ context.Context, e hooks.SessionEvent) error {
    h.logger.Info("session ended", "session_id", e.SessionID, "turns", e.TurnIndex+1)
    return nil
}
```

Register via `sdk.WithSessionHook(&SessionLogger{logger: slog.Default()})`.

## EvalHook

Observational. The runner hands you a pointer to the result; you can mutate it in place (redact, enrich, attach metadata) and the mutated result is what propagates to the caller and the event bus. There is no allow/deny decision — every registered hook always runs for every result.

```go
import "github.com/AltairaLabs/PromptKit/runtime/evals"

type MetricsEvalHook struct {
    exporter MetricExporter
}

func (h *MetricsEvalHook) Name() string { return "metrics_exporter" }

func (h *MetricsEvalHook) OnEvalResult(
    ctx context.Context,
    def *evals.EvalDef,
    _ *evals.EvalContext,
    result *evals.EvalResult,
) {
    h.exporter.Record(ctx, def.ID, result.Score, result.DurationMs)
}
```

A redacting hook that mutates the result before it leaves the runner:

```go
type RedactingEvalHook struct{}

func (h *RedactingEvalHook) Name() string { return "redact_explanations" }

func (h *RedactingEvalHook) OnEvalResult(
    _ context.Context, _ *evals.EvalDef, _ *evals.EvalContext, result *evals.EvalResult,
) {
    result.Explanation = ssnPattern.ReplaceAllString(result.Explanation, "[REDACTED]")
}
```

Register via `sdk.WithEvalHook(&MetricsEvalHook{exporter: exp})`. A panic inside an eval hook is caught and logged; subsequent hooks still run.

## Handle HookDeniedError

When a Decision-based hook returns `Deny`, the runtime wraps the denial in `*hooks.HookDeniedError`. Detect it with `errors.As`:

```go
resp, err := conv.Send(ctx, "Hello")
if err != nil {
    var hookErr *hooks.HookDeniedError
    if errors.As(err, &hookErr) {
        log.Printf("denied by %s (%s): %s",
            hookErr.HookName, hookErr.HookType, hookErr.Reason)
        // return a safe fallback to the user
        return
    }
    // some other error
}
```

`HookType` is one of `"provider_before"`, `"provider_after"`, `"chunk"`, `"tool_before"`, `"tool_after"` — useful when you have multiple hooks and want to know which phase fired.

## See also

- [Hooks Reference](/runtime/reference/hooks/) — full interface signatures, types, registry
- [Exec Hooks](/sdk/how-to/exec-hooks/) — implement hooks as subprocesses in any language
- [The Hook System](/sdk/explanation/hooks/) — when to use which hook type, design rationale
- [Run Evals](/sdk/how-to/run-evals/) — registering eval hooks via `sdk.WithEvalHook`
