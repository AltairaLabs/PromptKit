---
title: Hooks & Guardrails
sidebar:
  order: 5
---
Extensible hook system for intercepting LLM calls, tool execution, and session lifecycle.

:::note[Migration]
The `runtime/validators` package has been removed. All validation is now handled through **hooks** in the `runtime/hooks` package, with built-in guardrails in `runtime/hooks/guardrails`. Pack YAML `validators:` sections are automatically converted to guardrail hooks at runtime.
:::

## Overview

Hooks provide interception points throughout the PromptKit pipeline:

- **ProviderHook** — intercept LLM calls (before/after), with optional streaming chunk interception
- **ToolHook** — intercept tool execution (before/after)
- **SessionHook** — track session lifecycle (start, update, end)
- **EvalHook** — observe (and optionally mutate) eval results as they are produced
- **Built-in guardrails** — content safety hooks (banned words, length, sentences, required fields)

## Core Interfaces

### ProviderHook

Intercepts LLM provider calls. This is the primary hook for content validation and guardrails.

```go
type ProviderHook interface {
    Name() string
    BeforeCall(ctx context.Context, req *ProviderRequest) Decision
    AfterCall(ctx context.Context, req *ProviderRequest, resp *ProviderResponse) Decision
}
```

### ChunkInterceptor

An opt-in streaming extension for `ProviderHook`. Hooks that also implement `ChunkInterceptor` can inspect each streaming chunk in real time:

```go
type ChunkInterceptor interface {
    OnChunk(ctx context.Context, chunk *providers.StreamChunk) Decision
}
```

### ToolHook

Intercepts LLM-initiated tool calls:

```go
type ToolHook interface {
    Name() string
    BeforeExecution(ctx context.Context, req ToolRequest) Decision
    AfterExecution(ctx context.Context, req ToolRequest, resp ToolResponse) Decision
}
```

### SessionHook

Tracks session lifecycle events:

```go
type SessionHook interface {
    Name() string
    OnSessionStart(ctx context.Context, event SessionEvent) error
    OnSessionUpdate(ctx context.Context, event SessionEvent) error
    OnSessionEnd(ctx context.Context, event SessionEvent) error
}
```

### EvalHook

Observes eval results as they are produced by the eval runner. Unlike the hooks above, `EvalHook` is purely **observational** — evals compute scores and do not gate execution, so there is no allow/deny semantics. Hooks fire once per executed eval, after the handler runs, and before the result is emitted as an event.

```go
type EvalHook interface {
    Name() string
    OnEvalResult(
        ctx context.Context,
        def *EvalDef,
        evalCtx *EvalContext,
        result *EvalResult,
    )
}
```

Hooks **may mutate `result` in place** — for example, to redact sensitive content from `Explanation`, annotate `Details`, or attach tracing metadata. The mutated result is what the runner returns and emits on the event bus.

Typical uses:
- Push results to external systems (metrics, tracing, logs)
- Redact or enrich result fields
- Fan out to a subprocess for custom scoring pipelines

Lives in `github.com/AltairaLabs/PromptKit/runtime/evals` alongside the eval types (`EvalDef`, `EvalContext`, `EvalResult`).

**Panic safety.** Each hook invocation is wrapped in `recover()` scoped to that hook. A panicking hook is logged and skipped; subsequent hooks still run and the eval result is still emitted.

## Decision Type

Provider, chunk, and tool hooks return a `Decision`:

```go
type Decision struct {
    Allow    bool
    Reason   string
    Metadata map[string]any
    Enforced bool           // Hook already applied enforcement
}
```

**Helpers**:

```go
hooks.Allow                          // Zero-cost approval
hooks.Deny("reason")                 // Denial with reason — pipeline stops with HookDeniedError
hooks.DenyWithMetadata("reason", m)  // Denial with reason + metadata
hooks.Enforced("reason", m)          // Enforcement applied — pipeline continues with modified content
```

### Enforcement vs Denial

Built-in guardrail hooks return `Enforced` decisions instead of `Deny`. When a guardrail triggers:

1. The hook modifies content in-place (truncation for length validators, replacement for content blockers)
2. Returns `hooks.Enforced()` so the pipeline **continues** with the modified content
3. The violation is recorded in `message.Validations` and emitted as a `validation.failed` event

This means guardrails are **non-fatal** — they fix the content and let the pipeline proceed, rather than returning an error to the caller. Custom hooks can choose either behavior.

`SessionHook` returns plain Go `error` values (denial semantics via error, no enforcement mode). `EvalHook` returns nothing — it is observational; side effects and result mutation are the only outputs.

## Request & Response Types

### ProviderRequest

```go
type ProviderRequest struct {
    ProviderID   string
    Model        string
    Messages     []types.Message
    SystemPrompt string
    Round        int
    Metadata     map[string]any
}
```

### ProviderResponse

```go
type ProviderResponse struct {
    ProviderID string
    Model      string
    Message    types.Message
    Round      int
    LatencyMs  int64
}
```

### ToolRequest / ToolResponse

```go
type ToolRequest struct {
    Name   string
    Args   json.RawMessage
    CallID string
}

type ToolResponse struct {
    Name      string
    CallID    string
    Content   string
    Error     string
    LatencyMs int64
}
```

### SessionEvent

```go
type SessionEvent struct {
    SessionID      string
    ConversationID string
    Messages       []types.Message
    TurnIndex      int
    Metadata       map[string]any
}
```

## HookDeniedError

When a hook returns `Deny` (not `Enforced`), the runtime wraps the denial in a `HookDeniedError`:

```go
type HookDeniedError struct {
    HookName string
    HookType string // "provider_before", "provider_after", "chunk", "tool_before", "tool_after"
    Reason   string
    Metadata map[string]any
}
```

Check for hook denials in your error handling:

```go
var hookErr *hooks.HookDeniedError
if errors.As(err, &hookErr) {
    log.Printf("Denied by %s: %s", hookErr.HookName, hookErr.Reason)
}
```

:::note
Built-in guardrail hooks return `Enforced` decisions, not `Deny`. They modify content in-place and the pipeline continues — no `HookDeniedError` is returned. You only need to handle `HookDeniedError` for custom hooks that use `hooks.Deny()`.

`SessionHook` errors are not wrapped in `HookDeniedError` — they propagate as the plain Go error you returned. `EvalHook` never produces an error at all.
:::

## Registry

The `Registry` collects and executes provider, tool, and session hooks in order:

```go
reg := hooks.NewRegistry(
    hooks.WithProviderHook(myHook),
    hooks.WithToolHook(myToolHook),
    hooks.WithSessionHook(mySessionHook),
)
```

The registry automatically detects `ProviderHook` implementations that also satisfy `ChunkInterceptor` and routes streaming chunks to them.

**Execution methods**:

| Method | Description |
|--------|-------------|
| `RunBeforeProviderCall` | Run all provider hooks' `BeforeCall` |
| `RunAfterProviderCall` | Run all provider hooks' `AfterCall` |
| `RunOnChunk` | Run all chunk interceptors' `OnChunk` |
| `RunBeforeToolExecution` | Run all tool hooks' `BeforeExecution` |
| `RunAfterToolExecution` | Run all tool hooks' `AfterExecution` |
| `RunSessionStart` | Run all session hooks' `OnSessionStart` |
| `RunSessionUpdate` | Run all session hooks' `OnSessionUpdate` |
| `RunSessionEnd` | Run all session hooks' `OnSessionEnd` |

Multiple hooks execute in registration order. The first `Deny` short-circuits — subsequent hooks are not called.

`EvalHook` uses a separate registration path on the eval runner (not `hooks.Registry`), since eval hooks have different semantics (observational, no short-circuit):

```go
runner := evals.NewEvalRunner(reg,
    evals.WithEvalHook(myEvalHook),
    evals.WithEvalHook(myRedactor),
)
```

Or from the SDK:

```go
conv, _ := sdk.Open("./app.pack.json", "chat",
    sdk.WithEvalHook(myEvalHook),
)
```

Eval hooks execute in registration order; **every hook runs** for every eval result (no short-circuit), and a panicking hook does not block the rest.

## Built-in Guardrail Hooks

All guardrail hooks implement `ProviderHook`. Some also implement `ChunkInterceptor` for real-time streaming enforcement.

### BannedWordsHook

Rejects responses containing banned words. Case-insensitive with word-boundary matching.

**Streaming**: Yes (implements `ChunkInterceptor`)

```go
import "github.com/AltairaLabs/PromptKit/runtime/hooks/guardrails"

hook := guardrails.NewBannedWordsHook([]string{
    "guarantee", "promise", "definitely",
})
```

**SDK usage**:
```go
conv, _ := sdk.Open("./app.pack.json", "chat",
    sdk.WithProviderHook(guardrails.NewBannedWordsHook([]string{
        "guarantee", "promise",
    })),
)
```

### LengthHook

Rejects responses exceeding character and/or token limits. Pass `0` to disable a limit.

**Streaming**: Yes (implements `ChunkInterceptor`)

```go
hook := guardrails.NewLengthHook(1000, 250) // maxCharacters, maxTokens
```

Token estimation: uses `chunk.TokenCount` if available, otherwise approximates at 1 token ≈ 4 characters.

### MaxSentencesHook

Rejects responses exceeding a sentence count. Splits on `.`, `!`, `?`.

**Streaming**: No (requires complete response)

```go
hook := guardrails.NewMaxSentencesHook(5)
```

### RequiredFieldsHook

Rejects responses missing any of the specified field strings (case-insensitive substring match).

**Streaming**: No (requires complete response)

```go
hook := guardrails.NewRequiredFieldsHook([]string{
    "order number", "tracking number", "estimated delivery",
})
```

## Factory

The `guardrails.NewGuardrailHook` factory creates hooks from a type name and params map. This is used internally to convert pack YAML `validators:` sections to hooks:

```go
hook, err := guardrails.NewGuardrailHook("banned_words", map[string]any{
    "words": []string{"guarantee", "promise"},
})
```

Supported type names: `banned_words`, `length`, `max_length`, `max_sentences`, `required_fields`.

### Factory Options

```go
// Set a custom blocked message (replaces content when guardrail triggers)
hook, _ := guardrails.NewGuardrailHook("banned_words", params,
    guardrails.WithMessage("This response has been blocked by our content policy."),
)

// Monitor-only mode: evaluate but don't modify content
hook, _ := guardrails.NewGuardrailHook("banned_words", params,
    guardrails.WithMonitorOnly(),
)
```

| Option | Description |
|--------|-------------|
| `WithMessage(msg)` | Custom message shown when content is blocked (default: generic policy message) |
| `WithMonitorOnly()` | Evaluate and record results without modifying content |

### Monitor-Only Mode

Monitor-only guardrails evaluate content and emit events, but never modify the response. This is useful for:

- **Gradual rollout** — observe guardrail behavior before enforcing
- **Analytics** — track policy violations without impacting users
- **Shadow testing** — compare guardrail results against production traffic

The guardrail still returns an `Enforced` decision (so the pipeline continues), and violations are recorded in `message.Validations` and emitted as `validation.failed` events with `MonitorOnly: true`.

## Custom Hooks

### Custom ProviderHook

```go
type PIIHook struct{}

func (h *PIIHook) Name() string { return "pii_filter" }

func (h *PIIHook) BeforeCall(ctx context.Context, req *hooks.ProviderRequest) hooks.Decision {
    return hooks.Allow // No input filtering in this example
}

func (h *PIIHook) AfterCall(ctx context.Context, req *hooks.ProviderRequest, resp *hooks.ProviderResponse) hooks.Decision {
    content := resp.Message.Content()
    if containsSSN(content) {
        return hooks.Deny("response contains SSN")
    }
    return hooks.Allow
}
```

### Custom ProviderHook with ChunkInterceptor

```go
type StreamingPIIHook struct {
    buffer strings.Builder
}

func (h *StreamingPIIHook) Name() string { return "streaming_pii" }

func (h *StreamingPIIHook) BeforeCall(ctx context.Context, req *hooks.ProviderRequest) hooks.Decision {
    h.buffer.Reset()
    return hooks.Allow
}

func (h *StreamingPIIHook) AfterCall(ctx context.Context, req *hooks.ProviderRequest, resp *hooks.ProviderResponse) hooks.Decision {
    return hooks.Allow
}

// Implement ChunkInterceptor for streaming checks
func (h *StreamingPIIHook) OnChunk(ctx context.Context, chunk *providers.StreamChunk) hooks.Decision {
    h.buffer.WriteString(chunk.Content)
    if containsSSN(h.buffer.String()) {
        return hooks.Deny("streaming content contains SSN")
    }
    return hooks.Allow
}
```

### Custom ToolHook

```go
type AuditToolHook struct {
    logger *slog.Logger
}

func (h *AuditToolHook) Name() string { return "audit_tools" }

func (h *AuditToolHook) BeforeExecution(ctx context.Context, req hooks.ToolRequest) hooks.Decision {
    h.logger.Info("tool called", "name", req.Name, "callID", req.CallID)
    return hooks.Allow
}

func (h *AuditToolHook) AfterExecution(ctx context.Context, req hooks.ToolRequest, resp hooks.ToolResponse) hooks.Decision {
    if resp.Error != "" {
        h.logger.Error("tool failed", "name", req.Name, "error", resp.Error)
    }
    return hooks.Allow
}
```

### Custom SessionHook

```go
type SessionLogger struct {
    logger *slog.Logger
}

func (h *SessionLogger) Name() string { return "session_logger" }

func (h *SessionLogger) OnSessionStart(ctx context.Context, e hooks.SessionEvent) error {
    h.logger.Info("session started", "session_id", e.SessionID, "conv_id", e.ConversationID)
    return nil
}

func (h *SessionLogger) OnSessionUpdate(ctx context.Context, e hooks.SessionEvent) error {
    h.logger.Info("turn complete", "session_id", e.SessionID, "turn", e.TurnIndex)
    return nil
}

func (h *SessionLogger) OnSessionEnd(ctx context.Context, e hooks.SessionEvent) error {
    h.logger.Info("session ended", "session_id", e.SessionID, "turns", e.TurnIndex+1)
    return nil
}
```

Registered via `sdk.WithSessionHook(&SessionLogger{logger: slog.Default()})`. Returning a non-nil error causes the runtime to surface it to the caller; hooks that only observe should always return `nil`.

### Custom EvalHook

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

An eval hook that mutates the result (e.g. redacting PII from `Explanation`):

```go
type RedactingEvalHook struct{}

func (h *RedactingEvalHook) Name() string { return "redact_explanations" }

func (h *RedactingEvalHook) OnEvalResult(
    _ context.Context, _ *evals.EvalDef, _ *evals.EvalContext, result *evals.EvalResult,
) {
    result.Explanation = ssnPattern.ReplaceAllString(result.Explanation, "[REDACTED]")
}
```

Registered via `sdk.WithEvalHook(&MetricsEvalHook{exporter: exp})`.

## Execution Order

1. **BeforeCall** hooks run before the LLM request (first deny aborts the call)
2. **OnChunk** interceptors run on each streaming chunk (first deny aborts the stream)
3. **AfterCall** hooks run after the LLM response (first deny rejects the response)
4. **BeforeExecution** tool hooks run before each tool call
5. **AfterExecution** tool hooks run after each tool call
6. **Session hooks** run at session boundaries (start, after each turn, end)
7. **EvalHooks** run after each eval result is computed, before emission on the event bus

## Best Practices

### 1. Put Fast Hooks First

```go
conv, _ := sdk.Open("./app.pack.json", "chat",
    sdk.WithProviderHook(guardrails.NewLengthHook(1000, 250)),      // Fast O(1)
    sdk.WithProviderHook(guardrails.NewBannedWordsHook(banned)),     // O(n*w)
    sdk.WithProviderHook(customExpensiveHook),                       // Slow
)
```

### 2. Use Streaming Hooks for Early Abort

Hooks that implement `ChunkInterceptor` can abort a streaming response mid-flight, saving API costs:

```go
// BannedWordsHook and LengthHook both support streaming
// MaxSentencesHook and RequiredFieldsHook require the full response
```

### 3. Handle HookDeniedError

```go
resp, err := conv.Send(ctx, "Hello")
if err != nil {
    var hookErr *hooks.HookDeniedError
    if errors.As(err, &hookErr) {
        log.Printf("Policy violation: %s", hookErr.Reason)
        // Return a safe fallback response to the user
    }
}
```

### 4. Keep Hooks Stateless When Possible

Stateless hooks are safe for concurrent use. If your hook must maintain state (e.g., a streaming buffer), ensure it is scoped to a single conversation or protected by synchronization.

## Package Import

```go
import (
    "github.com/AltairaLabs/PromptKit/runtime/hooks"
    "github.com/AltairaLabs/PromptKit/runtime/hooks/guardrails"
)
```

## External Exec Hooks

Hooks can be implemented as external subprocesses in any language using the exec protocol. Configure them in RuntimeConfig:

```yaml
spec:
  hooks:
    pii_redactor:
      command: ./hooks/pii-redactor
      hook: provider
      phases: [before_call, after_call]
      mode: filter
      timeout_ms: 3000

    audit_logger:
      command: ./hooks/audit-logger
      hook: session
      phases: [session_start, session_update, session_end]
      mode: observe

    eval_metrics:
      command: ./hooks/eval-metrics
      hook: eval
      timeout_ms: 5000
```

Four adapters bridge external processes to the hook interfaces:

| Adapter | Implements | Description |
|---------|-----------|-------------|
| `ExecProviderHook` | `ProviderHook` | External provider interception |
| `ExecToolHook` | `ToolHook` | External tool interception |
| `ExecSessionHook` | `SessionHook` | External session tracking |
| `ExecEvalHook` | `EvalHook` | External eval-result processing (fire-and-forget) |

**Modes** (provider, tool, session only):
- **filter** — Fail-closed. Process failure = deny. Can block the pipeline.
- **observe** — Fire-and-forget. Process failure is swallowed. Pipeline always continues.

**Eval exec hooks** are always fire-and-forget — `mode` and `phases` are ignored for `hook: eval`, since evals have no allow/deny semantics. Subprocess errors and timeouts are logged and discarded; the eval pipeline continues regardless.

See [Exec Hooks](/sdk/how-to/exec-hooks/) for the full how-to guide and [Exec Protocol](/sdk/reference/exec-protocol/) for the wire format.

## See Also

- [Checks Reference](/reference/checks/) -- All check types, parameters, and extensibility details
- [Unified Check Model](/concepts/validation/) -- How guardrails, assertions, and evals relate
- [Guardrails Reference](/arena/reference/validators/) -- Guardrail configuration and behavior
- [Pipeline Reference](/runtime/reference/pipeline/) -- Stage and pipeline interfaces
- [Validation Tutorial](/runtime/tutorials/04-validation-guardrails/) -- Step-by-step guide
- [Exec Hooks](/sdk/how-to/exec-hooks/) -- External hooks in any language
- [Exec Protocol](/sdk/reference/exec-protocol/) -- Wire protocol reference
