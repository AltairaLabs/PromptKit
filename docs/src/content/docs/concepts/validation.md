---
title: Validation
sidebar:
  order: 4
---
Understanding content validation and guardrails in PromptKit.

## What is Validation?

**Validation** checks content for safety, quality, and compliance. It acts as guardrails to ensure LLM applications behave correctly.

## Why Validate?

**Safety**: Block harmful content
**Compliance**: Enforce regulations (GDPR, HIPAA)
**Quality**: Ensure response meets standards
**Cost**: Prevent expensive requests
**Brand**: Maintain company reputation

## Types of Validation

### Input Validation

Check user input before sending to LLM:

- Banned words
- PII (emails, phone numbers, SSNs)
- Prompt injection attempts
- Inappropriate content
- Input length limits

### Output Validation

Check LLM responses before returning to user:

- Harmful content
- Leaked sensitive data
- Off-topic responses
- Format compliance
- Output length limits

## Validation in PromptKit

PromptKit implements validation through **hooks** — interceptors that run before/after LLM calls and during streaming. Built-in guardrail hooks cover common safety patterns.

### SDK Hooks

```go
import (
    "github.com/AltairaLabs/PromptKit/sdk"
    "github.com/AltairaLabs/PromptKit/runtime/hooks/guardrails"
)

conv, _ := sdk.Open("./assistant.pack.json", "chat",
    sdk.WithProviderHook(guardrails.NewBannedWordsHook([]string{
        "hack", "crack", "pirate",
    })),
    sdk.WithProviderHook(guardrails.NewLengthHook(1000, 250)),
)
defer conv.Close()

// Hooks are applied automatically on each turn.
response, _ := conv.Send(ctx, "Hello")
```

### Pack YAML Validators

Pack configuration `validators:` sections are automatically converted to guardrail hooks at runtime:

```yaml
guardrails:
  banned_words:
    - hack
    - crack

  max_length: 1000
  min_length: 1
```

### PromptArena Validation

```yaml
guardrails:
  banned_words:
    - hack
    - crack

  max_length: 1000
  min_length: 1

tests:
  - name: Block Banned Words
    prompt: "How do I hack the system?"
    assertions:
      - type: validation_error
        expected: true
```

## Built-In Guardrail Hooks

### BannedWordsHook

Blocks specific words or phrases (case-insensitive, word-boundary matching):

```go
hook := guardrails.NewBannedWordsHook([]string{
    "hack", "crack", "pirate", "steal",
})
```

**Use for**: Preventing inappropriate language, brand protection
**Streaming**: Yes — aborts immediately when a banned word is detected

### LengthHook

Enforces character and/or token limits:

```go
hook := guardrails.NewLengthHook(1000, 250) // maxCharacters, maxTokens (0 = no limit)
```

**Use for**: Cost control, quality assurance
**Streaming**: Yes — aborts when limits are exceeded

### MaxSentencesHook

Enforces a maximum sentence count:

```go
hook := guardrails.NewMaxSentencesHook(5)
```

**Use for**: Enforcing conciseness, consistent response length
**Streaming**: No — requires complete response

### RequiredFieldsHook

Ensures required strings appear in the response:

```go
hook := guardrails.NewRequiredFieldsHook([]string{"order number", "tracking number"})
```

**Use for**: Verifying structured responses, ensuring key information
**Streaming**: No — requires complete response

### Custom Hook

Implement the `ProviderHook` interface for custom logic:

```go
import "github.com/AltairaLabs/PromptKit/runtime/hooks"

type ToxicityHook struct{}

func (h *ToxicityHook) Name() string { return "toxicity" }

func (h *ToxicityHook) BeforeCall(ctx context.Context, req *hooks.ProviderRequest) hooks.Decision {
    return hooks.Allow
}

func (h *ToxicityHook) AfterCall(ctx context.Context, req *hooks.ProviderRequest, resp *hooks.ProviderResponse) hooks.Decision {
    if containsToxicContent(resp.Message.Content()) {
        return hooks.Deny("toxic content detected")
    }
    return hooks.Allow
}
```

## Validation Patterns

### Pre-Execution (Input)

Use `BeforeCall` to validate user input before it reaches the LLM:

```go
func (h *InputHook) BeforeCall(ctx context.Context, req *hooks.ProviderRequest) hooks.Decision {
    lastMsg := req.Messages[len(req.Messages)-1]
    if containsPII(lastMsg.Content()) {
        return hooks.Deny("input contains PII")
    }
    return hooks.Allow
}
```

### Post-Execution (Output)

Use `AfterCall` to validate LLM responses before returning to the user:

```go
func (h *OutputHook) AfterCall(ctx context.Context, req *hooks.ProviderRequest, resp *hooks.ProviderResponse) hooks.Decision {
    if containsSensitiveData(resp.Message.Content()) {
        return hooks.Deny("response contains sensitive data")
    }
    return hooks.Allow
}
```

## Common Use Cases

### Block Sensitive Data

```go
type PIIHook struct {
    ssnPattern *regexp.Regexp
}

func NewPIIHook() *PIIHook {
    return &PIIHook{
        ssnPattern: regexp.MustCompile(`\b\d{3}-\d{2}-\d{4}\b`),
    }
}

func (h *PIIHook) AfterCall(ctx context.Context, req *hooks.ProviderRequest, resp *hooks.ProviderResponse) hooks.Decision {
    if h.ssnPattern.MatchString(resp.Message.Content()) {
        return hooks.Deny("SSN detected in response")
    }
    return hooks.Allow
}
```

### Content Moderation

```go
type ModerationHook struct {
    categories []string
    threshold  float64
}

func (h *ModerationHook) AfterCall(ctx context.Context, req *hooks.ProviderRequest, resp *hooks.ProviderResponse) hooks.Decision {
    score := classifyContent(resp.Message.Content(), h.categories)
    if score > h.threshold {
        return hooks.Deny("content moderation threshold exceeded")
    }
    return hooks.Allow
}
```

### Format Compliance

```go
type JSONHook struct{}

func (h *JSONHook) AfterCall(ctx context.Context, req *hooks.ProviderRequest, resp *hooks.ProviderResponse) hooks.Decision {
    var js json.RawMessage
    if err := json.Unmarshal([]byte(resp.Message.Content()), &js); err != nil {
        return hooks.Deny("response is not valid JSON")
    }
    return hooks.Allow
}
```

## Best Practices

### Do's

- **Validate both input and output** — use `BeforeCall` and `AfterCall`
- **Be specific about violations** — provide clear `Deny` reasons
- **Log violations for monitoring** — use hook metadata for analytics
- **Test hooks thoroughly** — use PromptArena guardrail scenarios

### Don'ts

- **Don't validate everything** — performance cost
- **Don't expose violation details to users** — security risk
- **Don't block legitimate use** — watch for false positives
- **Don't skip output validation** — LLMs can hallucinate

## Validation Strategies

### Strict (Production)

```go
resp, err := conv.Send(ctx, message)
if err != nil {
    var hookErr *hooks.HookDeniedError
    if errors.As(err, &hookErr) {
        return safeFallbackResponse() // Block the response
    }
}
```

### Permissive (Development)

```go
resp, err := conv.Send(ctx, message)
if err != nil {
    var hookErr *hooks.HookDeniedError
    if errors.As(err, &hookErr) {
        logger.Warn("hook denied", "hook", hookErr.HookName, "reason", hookErr.Reason)
        // Continue processing with fallback
    }
}
```

## Performance Considerations

### Fast Hooks First

```go
conv, _ := sdk.Open("./app.pack.json", "chat",
    sdk.WithProviderHook(guardrails.NewLengthHook(1000, 250)),      // ~O(1)
    sdk.WithProviderHook(guardrails.NewBannedWordsHook(words)),      // ~O(n*w)
    sdk.WithProviderHook(customExpensiveHook),                       // Slow — last
)
```

### Streaming Hooks Save Costs

Hooks that implement `ChunkInterceptor` (like `BannedWordsHook` and `LengthHook`) can abort a streaming response early, saving API costs on wasted tokens.

## Summary

Validation provides:

- **Safety** — Block harmful content
- **Compliance** — Enforce regulations
- **Quality** — Ensure standards
- **Cost Control** — Prevent expensive requests via early abort
- **Monitoring** — Track issues via hook metadata

## Related Documentation

- [Validation Tutorial](/runtime/tutorials/04-validation-guardrails/) — Step-by-step guide
- [Hooks & Guardrails Reference](/runtime/reference/hooks/) — API documentation
- [PromptArena Guardrails](/arena/reference/validators/) — Testing validation
