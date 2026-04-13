---
title: Hooks
sidebar:
  order: 5
---
Reference for the four hook interfaces PromptKit exposes for intercepting LLM calls, tool execution, session lifecycle, and eval results — plus the built-in guardrails that ship on top of them.

For *how* to write a hook, see [Custom Hooks](/sdk/how-to/custom-hooks/) and [Exec Hooks](/sdk/how-to/exec-hooks/).
For *when* to reach for which hook type, see [The Hook System](/sdk/explanation/hooks/).

## Hook types

| Type | Fires | Decision shape | Built-in implementations |
|---|---|---|---|
| [`ProviderHook`](#providerhook) | Before/after each LLM call | `Decision` (allow / deny / enforced) | Guardrails: banned-words, length, sentences, required-fields |
| [`ChunkInterceptor`](#chunkinterceptor) | Each streaming chunk | `Decision` | Same guardrails when they implement streaming |
| [`ToolHook`](#toolhook) | Before/after each tool call | `Decision` | None — bring your own |
| [`SessionHook`](#sessionhook) | Session start, each turn, session end | `error` (nil = ok) | None |
| [`EvalHook`](#evalhook) | Each eval result, before emission | none — direct mutation only | None |

## ProviderHook

Intercepts LLM provider calls. This is the primary hook for content validation and guardrails.

```go
type ProviderHook interface {
    Name() string
    BeforeCall(ctx context.Context, req *ProviderRequest) Decision
    AfterCall(ctx context.Context, req *ProviderRequest, resp *ProviderResponse) Decision
}
```

## ChunkInterceptor

An opt-in streaming extension for `ProviderHook`. Hooks that also implement `ChunkInterceptor` can inspect each streaming chunk in real time:

```go
type ChunkInterceptor interface {
    OnChunk(ctx context.Context, chunk *providers.StreamChunk) Decision
}
```

## ToolHook

Intercepts LLM-initiated tool calls:

```go
type ToolHook interface {
    Name() string
    BeforeExecution(ctx context.Context, req ToolRequest) Decision
    AfterExecution(ctx context.Context, req ToolRequest, resp ToolResponse) Decision
}
```

## SessionHook

Tracks session lifecycle events:

```go
type SessionHook interface {
    Name() string
    OnSessionStart(ctx context.Context, event SessionEvent) error
    OnSessionUpdate(ctx context.Context, event SessionEvent) error
    OnSessionEnd(ctx context.Context, event SessionEvent) error
}
```

## EvalHook

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

## Execution Order

1. **BeforeCall** hooks run before the LLM request (first deny aborts the call)
2. **OnChunk** interceptors run on each streaming chunk (first deny aborts the stream)
3. **AfterCall** hooks run after the LLM response (first deny rejects the response)
4. **BeforeExecution** tool hooks run before each tool call
5. **AfterExecution** tool hooks run after each tool call
6. **Session hooks** run at session boundaries (start, after each turn, end)
7. **EvalHooks** run after each eval result is computed, before emission on the event bus

## Package import

```go
import (
    "github.com/AltairaLabs/PromptKit/runtime/hooks"
    "github.com/AltairaLabs/PromptKit/runtime/hooks/guardrails"
    "github.com/AltairaLabs/PromptKit/runtime/evals" // for EvalHook
)
```

## Exec adapters

External-subprocess implementations of each hook type, configured via RuntimeConfig YAML. See [Exec Hooks](/sdk/how-to/exec-hooks/) for the full how-to and [Exec Protocol](/sdk/reference/exec-protocol/) for the wire format.

| Adapter | Implements | Modes |
|---|---|---|
| `ExecProviderHook` | `ProviderHook` | `filter`, `observe` |
| `ExecToolHook` | `ToolHook` | `filter`, `observe` |
| `ExecSessionHook` | `SessionHook` | `filter` only (observe is a no-op) |
| `ExecEvalHook` | `EvalHook` | always fire-and-forget — `mode`/`phases` ignored |

## See also

- [Custom Hooks](/sdk/how-to/custom-hooks/) — write a Go hook of any type
- [Exec Hooks](/sdk/how-to/exec-hooks/) — write a hook as a subprocess in any language
- [The Hook System](/sdk/explanation/hooks/) — mental model and design rationale
- [Exec Protocol](/sdk/reference/exec-protocol/) — wire format for exec adapters
- [Checks Reference](/reference/checks/) — guardrail/assertion/eval check types
- [Unified Check Model](/concepts/validation/) — how guardrails, assertions, and evals relate
- [Pipeline Reference](/runtime/reference/pipeline/) — stage and pipeline interfaces
