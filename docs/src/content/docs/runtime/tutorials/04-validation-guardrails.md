---
title: 'Tutorial 4: Validation & Guardrails'
sidebar:
  order: 4
---
Add content safety and validation to your LLM application.

**Time**: 20 minutes
**Level**: Intermediate

## What You'll Build

A chatbot with content filtering and validation guardrails using the hooks system.

## What You'll Learn

- Register guardrail hooks via the SDK
- Filter banned words and enforce length limits
- Create custom `ProviderHook` implementations
- Handle `HookDeniedError` for policy violations
- Use streaming guardrails via `ChunkInterceptor`
- Configure guardrails in pack YAML

## Prerequisites

- Completed [Tutorial 1](01-first-pipeline)

## Step 1: Basic Guardrails

Add banned-word filtering and length limits using the SDK:

```go
package main

import (
    "context"
    "bufio"
    "errors"
    "fmt"
    "log"
    "os"
    "strings"

    "github.com/AltairaLabs/PromptKit/sdk"
    "github.com/AltairaLabs/PromptKit/runtime/hooks"
    "github.com/AltairaLabs/PromptKit/runtime/hooks/guardrails"
)

func main() {
    // Open a conversation with guardrail hooks
    conv, err := sdk.Open("./app.pack.json", "chat",
        sdk.WithProviderHook(guardrails.NewBannedWordsHook([]string{
            "spam", "hack", "exploit",
        })),
        sdk.WithProviderHook(guardrails.NewLengthHook(2000, 500)),
        sdk.WithProviderHook(guardrails.NewMaxSentencesHook(10)),
    )
    if err != nil {
        log.Fatal(err)
    }
    defer conv.Close()

    ctx := context.Background()
    scanner := bufio.NewScanner(os.Stdin)

    fmt.Println("Safe Chatbot (with content filtering)")
    fmt.Print("\nYou: ")

    for scanner.Scan() {
        input := strings.TrimSpace(scanner.Text())

        if input == "exit" {
            break
        }
        if input == "" {
            fmt.Print("You: ")
            continue
        }

        resp, err := conv.Send(ctx, input)
        if err != nil {
            // Check if a guardrail hook denied the request
            var hookErr *hooks.HookDeniedError
            if errors.As(err, &hookErr) {
                fmt.Printf("\n⚠️  Content blocked by %s: %s\n\n", hookErr.HookName, hookErr.Reason)
            } else {
                log.Printf("\nError: %v\n\n", err)
            }
            fmt.Print("You: ")
            continue
        }

        fmt.Printf("\nBot: %s\n\n", resp.Text())
        fmt.Print("You: ")
    }

    fmt.Println("Goodbye!")
}
```

## Step 2: Test Guardrails

Try these inputs:

```
You: Hello!
Bot: Hi! How can I help you?

You: How do I hack a system?
⚠️  Content blocked by banned_words: response contains banned word

You: Tell me about artificial intelligence
Bot: Artificial intelligence is...
```

## Built-in Guardrail Hooks

### BannedWordsHook

Blocks messages containing banned words (case-insensitive, word-boundary matching):

```go
hook := guardrails.NewBannedWordsHook([]string{
    "spam", "hack", "exploit", "inappropriate",
})
```

**Streaming**: Yes — aborts the stream immediately on detection.

### LengthHook

Enforces character and/or token limits (pass `0` to disable a limit):

```go
hook := guardrails.NewLengthHook(2000, 500) // maxCharacters, maxTokens
```

**Streaming**: Yes — aborts when the limit is exceeded.

### MaxSentencesHook

Limits the number of sentences in a response:

```go
hook := guardrails.NewMaxSentencesHook(5)
```

**Streaming**: No — requires the complete response.

### RequiredFieldsHook

Ensures the response contains required strings:

```go
hook := guardrails.NewRequiredFieldsHook([]string{"order number", "tracking number"})
```

**Streaming**: No — requires the complete response.

## Custom Hooks

Create domain-specific hooks by implementing `ProviderHook`:

```go
package main

import (
    "context"
    "regexp"

    "github.com/AltairaLabs/PromptKit/runtime/hooks"
)

// PIIHook blocks responses containing personally identifiable information.
type PIIHook struct {
    emailRegex *regexp.Regexp
    phoneRegex *regexp.Regexp
}

func NewPIIHook() *PIIHook {
    return &PIIHook{
        emailRegex: regexp.MustCompile(`\b[A-Za-z0-9._%+-]+@[A-Za-z0-9.-]+\.[A-Z|a-z]{2,}\b`),
        phoneRegex: regexp.MustCompile(`\b\d{3}[-.]?\d{3}[-.]?\d{4}\b`),
    }
}

func (h *PIIHook) Name() string { return "pii_filter" }

func (h *PIIHook) BeforeCall(ctx context.Context, req *hooks.ProviderRequest) hooks.Decision {
    return hooks.Allow
}

func (h *PIIHook) AfterCall(ctx context.Context, req *hooks.ProviderRequest, resp *hooks.ProviderResponse) hooks.Decision {
    content := resp.Message.Content()
    if h.emailRegex.MatchString(content) {
        return hooks.Deny("response contains email address")
    }
    if h.phoneRegex.MatchString(content) {
        return hooks.Deny("response contains phone number")
    }
    return hooks.Allow
}
```

Use it alongside built-in hooks:

```go
conv, _ := sdk.Open("./app.pack.json", "chat",
    sdk.WithProviderHook(guardrails.NewBannedWordsHook([]string{"spam", "hack"})),
    sdk.WithProviderHook(guardrails.NewLengthHook(2000, 500)),
    sdk.WithProviderHook(NewPIIHook()),
)
```

## Production Example

Combine multiple hooks with proper error handling:

```go
package main

import (
    "context"
    "bufio"
    "errors"
    "fmt"
    "log"
    "os"
    "strings"

    "github.com/AltairaLabs/PromptKit/sdk"
    "github.com/AltairaLabs/PromptKit/runtime/hooks"
    "github.com/AltairaLabs/PromptKit/runtime/hooks/guardrails"
)

func main() {
    conv, err := sdk.Open("./app.pack.json", "chat",
        // Streaming guardrails (can abort mid-stream)
        sdk.WithProviderHook(guardrails.NewBannedWordsHook([]string{
            "spam", "scam", "hack", "exploit",
        })),
        sdk.WithProviderHook(guardrails.NewLengthHook(2000, 500)),
        // Post-completion guardrails
        sdk.WithProviderHook(guardrails.NewMaxSentencesHook(10)),
        sdk.WithProviderHook(guardrails.NewRequiredFieldsHook([]string{})),
        // Custom hook
        sdk.WithProviderHook(NewPIIHook()),
    )
    if err != nil {
        log.Fatal(err)
    }
    defer conv.Close()

    ctx := context.Background()
    scanner := bufio.NewScanner(os.Stdin)

    fmt.Println("=== Secure Chatbot ===")
    fmt.Println("Content filtering enabled")
    fmt.Print("\nYou: ")

    for scanner.Scan() {
        input := strings.TrimSpace(scanner.Text())
        if input == "exit" {
            break
        }
        if input == "" {
            fmt.Print("You: ")
            continue
        }

        resp, err := conv.Send(ctx, input)
        if err != nil {
            var hookErr *hooks.HookDeniedError
            if errors.As(err, &hookErr) {
                fmt.Printf("\n❌ Blocked by %s: %s\n\n", hookErr.HookName, hookErr.Reason)
            } else {
                fmt.Printf("\n❌ Error: %v\n\n", err)
            }
            fmt.Print("You: ")
            continue
        }

        fmt.Printf("\nBot: %s\n\n", resp.Text())
        fmt.Print("You: ")
    }

    fmt.Println("Goodbye!")
}
```

## Streaming Guardrails

Hooks that implement `ChunkInterceptor` can inspect each streaming chunk and abort early:

```go
// Stream with guardrails
ch := conv.Stream(ctx, "Tell me about security")
for chunk := range ch {
    if chunk.Error != nil {
        var hookErr *hooks.HookDeniedError
        if errors.As(chunk.Error, &hookErr) {
            fmt.Printf("\nStream aborted: %s\n", hookErr.Reason)
            break
        }
    }
    if chunk.Type == sdk.ChunkText {
        fmt.Print(chunk.Text)
    }
}
```

`BannedWordsHook` and `LengthHook` both support streaming — they abort immediately when a violation is detected, saving API costs on wasted tokens.

## Pack YAML Approach

You can also define validators in your pack's prompt config. They are automatically converted to guardrail hooks at runtime:

```yaml
# prompts/chat.yaml
spec:
  system_template: |
    You are a helpful assistant.

  validators:
    - type: banned_words
      params:
        words:
          - hack
          - exploit
    - type: max_length
      params:
        max_characters: 2000
        max_tokens: 500
    - type: max_sentences
      params:
        max_sentences: 10
```

## Common Issues

### Guardrail too strict

**Problem**: Legitimate messages blocked.

**Solution**: Refine banned words list, adjust length limits, review hook denial reasons via `HookDeniedError.Reason`.

### Guardrail too permissive

**Problem**: Inappropriate content getting through.

**Solution**: Add more hooks, use stricter patterns, consider a custom `ProviderHook` for domain-specific checks.

## What You've Learned

- Register guardrail hooks via `sdk.WithProviderHook`
- Use built-in guardrails: banned words, length, sentences, required fields
- Create custom `ProviderHook` implementations
- Handle `HookDeniedError` for policy violations
- Use streaming guardrails for early abort
- Configure guardrails in pack YAML

## Next Steps

Continue to [Tutorial 5: Production Deployment](05-production-deployment) for production-ready patterns.

## See Also

- [Hooks & Guardrails Reference](../reference/hooks) — Complete API
- [Handle Errors](../how-to/handle-errors) — Error strategies
