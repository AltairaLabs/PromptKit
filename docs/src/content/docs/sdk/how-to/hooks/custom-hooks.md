---
title: Write Custom Hooks
description: Implement each PromptKit hook type in Go and register it with an SDK conversation.
sidebar:
  order: 8
verified:
  commit: ffbed7c4b0fff266953352be5e73d4e308a97acf
  sources:
    - runtime/evals/hooks.go
    - runtime/evals/runner.go
    - runtime/evals/types.go
    - runtime/hooks/errors.go
    - runtime/hooks/hooks.go
    - runtime/hooks/registry.go
    - runtime/hooks/types.go
    - runtime/pipeline/stage/stages_provider.go
    - runtime/providers/mock/mock.go
    - sdk/conversation.go
    - sdk/options.go
    - sdk/sdk.go
    - sdk/session_hooks.go
---

Implement a PromptKit hook in Go and register it with an SDK conversation. For subprocess-backed hooks in any language, see [Exec Hooks](/sdk/how-to/hooks/exec-hooks/).

## Prerequisites

- An SDK conversation to register the hook on (`sdk.Open`).
- The hooks package, imported as `github.com/AltairaLabs/PromptKit/runtime/v2/hooks`.

## Pick a hook type

| Goal | Hook type |
|---|---|
| Inspect or block an LLM request/response | [`ProviderHook`](#providerhook) |
| Same as above, but abort mid-stream on bad chunks | [`ProviderHook` + `ChunkInterceptor`](#providerhook--chunkinterceptor) |
| Inspect or block a tool call | [`ToolHook`](#toolhook) |
| Rewrite a tool's result before the model sees it | [`ToolHook` + `ToolResultRewriter`](#toolhook--toolresultrewriter) |
| Observe session start/turn/end | [`SessionHook`](#sessionhook) |
| Push or mutate eval results | [`EvalHook`](#evalhook) |

## ProviderHook

Implement `Name`, `BeforeCall`, and `AfterCall`. Return `hooks.Allow` to continue, `hooks.Deny(reason)` to abort with a `*hooks.HookDeniedError`, or `hooks.Enforced(reason, metadata)` when the hook has already enforced the result. In `AfterCall`, mutate `resp.Message` in place and return `Enforced`: the pipeline continues with the modified response. In `BeforeCall`, `Enforced` skips the provider call and the reply is `req.Replacement`, or the default blocked message when it is empty.

```go
import "github.com/AltairaLabs/PromptKit/runtime/v2/hooks"

type PIIHook struct{}

func (h *PIIHook) Name() string { return "pii_filter" }

func (h *PIIHook) BeforeCall(ctx context.Context, req *hooks.ProviderRequest) hooks.Decision {
    return hooks.Allow
}

func (h *PIIHook) AfterCall(ctx context.Context, req *hooks.ProviderRequest, resp *hooks.ProviderResponse) hooks.Decision {
    if containsSSN(resp.Message.Content) {
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

If your hook also implements `ChunkInterceptor`, the registry routes streaming chunks to it, so you can abort a response mid-stream.

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

A streaming denial surfaces as `*providers.ValidationAbortError` rather than `*hooks.HookDeniedError`, because the chunk loop has already started by the time it fires. Handle both in your error path if you care which one tripped.

## ToolHook

Same shape as `ProviderHook` but firing around tool execution. Use `BeforeExecution` to gate calls (for example, an allowlist) and `AfterExecution` to observe results. A denied call does not make `Send` return an error: the model receives an error tool result naming the hook's reason. `AfterExecution` runs after the result is built, so it can't change it, and the `Decision` it returns is ignored. To change a result, also implement [`ToolResultRewriter`](#toolhook--toolresultrewriter).

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

## ToolHook + ToolResultRewriter

If your tool hook also implements `ToolResultRewriter`, the registry passes the text of each successful tool result through `RewriteResult` before the model sees it. Use it to redact a result.

```go
type RedactHook struct{ AuditToolHook }

func (h *RedactHook) RewriteResult(_ context.Context, req hooks.ToolRequest, content string) (string, error) {
    if req.Name != "lookup_customer" {
        return content, nil
    }
    return ssnPattern.ReplaceAllString(content, "[redacted]"), nil
}
```

- Rewriters run in registration order, each on the previous one's output.
- The whole text of a plain result, before the result size limit applies; the limit then applies to what they return. A multimodal result is rewritten one text part at a time, with media parts left where they are; a result with no text isn't rewritten.
- The model, the `tool.call.completed` event and `AfterExecution` all see the rewritten result.
- A failed call isn't rewritten. A rewriter that returns an error fails the call: the model gets an error result, `tool.call.failed` is emitted, and the original text is not passed on.
- Parallel tool calls are rewritten concurrently, so `RewriteResult` must be safe for concurrent use.
- Only tools the pipeline runs are rewritten. Client tools, calls resumed after approval, and tools in duplex sessions are not.

## SessionHook

Returns plain Go `error`, with no decision/enforcement distinction. The SDK discards a returned error, so a failing session hook never blocks `Open`, `Send` or `Close`. The registry logs each failure at WARN with the hook's name and still runs the remaining session hooks for that event. `TurnIndex` is the number of completed turns.

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
    h.logger.Info("session ended", "session_id", e.SessionID, "turns", e.TurnIndex)
    return nil
}
```

Register via `sdk.WithSessionHook(&SessionLogger{logger: slog.Default()})`.

## EvalHook

Observational. The runner hands you a pointer to the result. Mutate it in place and the change propagates to the caller and the event bus. There is no allow/deny decision: every registered hook runs for every result.

```go
import "github.com/AltairaLabs/PromptKit/runtime/v2/evals"

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

To redact instead, assign to the result in place, for example `result.Explanation = ssnPattern.ReplaceAllString(result.Explanation, "[REDACTED]")`.

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

`HookType` is `"provider_before"` or `"provider_after"`, which tells you which phase fired. Only provider hooks produce a `HookDeniedError`: streaming denials return `*providers.ValidationAbortError`, and tool hook denials reach the model as an error tool result.

## Verify

Register a `ProviderHook` that denies, send a message, and check the error:

```go
type DenyHook struct{}

func (h *DenyHook) Name() string { return "deny_all" }

func (h *DenyHook) BeforeCall(_ context.Context, _ *hooks.ProviderRequest) hooks.Decision {
    return hooks.Deny("blocked for the check")
}

func (h *DenyHook) AfterCall(_ context.Context, _ *hooks.ProviderRequest, _ *hooks.ProviderResponse) hooks.Decision {
    return hooks.Allow
}

conv, err := sdk.Open("./app.pack.json", "chat",
    sdk.WithProvider(mock.NewProvider("mock", "mock-model", false)),
    sdk.WithProviderHook(&DenyHook{}),
)
if err != nil {
    log.Fatal(err)
}
defer conv.Close()

_, err = conv.Send(ctx, "Hello")
var hookErr *hooks.HookDeniedError
fmt.Println(errors.As(err, &hookErr), hookErr.HookType, hookErr.Reason)
```

It prints `true provider_before blocked for the check`. `HookName` is the fixed string `provider_hook`, not the hook's own `Name()`.

## Related

- [Hooks Reference](/runtime/reference/hooks/): full interface signatures, types, registry
- [Exec Hooks](/sdk/how-to/hooks/exec-hooks/): implement hooks as subprocesses in any language
- [The Hook System](/sdk/explanation/hooks/): when to use which hook type, design rationale
- [Run Evals](/sdk/how-to/observability/run-evals/): registering eval hooks via `sdk.WithEvalHook`
