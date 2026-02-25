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

## Decision Type

All hook methods return a `Decision`:

```go
type Decision struct {
    Allow    bool
    Reason   string
    Metadata map[string]any
}
```

**Helpers**:

```go
hooks.Allow                          // Zero-cost approval
hooks.Deny("reason")                 // Denial with reason
hooks.DenyWithMetadata("reason", m)  // Denial with reason + metadata
```

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

When a hook denies a request, the runtime wraps the denial in a `HookDeniedError`:

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

## Registry

The `Registry` collects and executes hooks in order:

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

## Execution Order

1. **BeforeCall** hooks run before the LLM request (first deny aborts the call)
2. **OnChunk** interceptors run on each streaming chunk (first deny aborts the stream)
3. **AfterCall** hooks run after the LLM response (first deny rejects the response)
4. **BeforeExecution** tool hooks run before each tool call
5. **AfterExecution** tool hooks run after each tool call
6. **Session hooks** run at session boundaries (start, after each turn, end)

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

## See Also

- [Pipeline Reference](pipeline) — Stage and pipeline interfaces
- [Validation Concepts](/concepts/validation/) — Why and when to validate
- [Validation Tutorial](../tutorials/04-validation-guardrails) — Step-by-step guide
