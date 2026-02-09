---
title: 'Tutorial 4: Validation & Guardrails'
sidebar:
  order: 4
---
Add content safety and validation to your LLM application.

**Time**: 20 minutes  
**Level**: Intermediate

## What You'll Build

A chatbot with content filtering and validation guardrails.

## What You'll Learn

- Implement content validators
- Filter banned words
- Enforce length limits
- Create custom validators
- Handle validation errors

## Prerequisites

- Completed [Tutorial 1](01-first-pipeline)

## Step 1: Basic Validation

Add banned word filtering:

```go
package main

import (
    "bufio"
    "context"
    "fmt"
    "log"
    "os"
    "strings"
    
    "github.com/AltairaLabs/PromptKit/runtime/pipeline"
    "github.com/AltairaLabs/PromptKit/runtime/pipeline/middleware"
    "github.com/AltairaLabs/PromptKit/runtime/providers/openai"
    "github.com/AltairaLabs/PromptKit/runtime/validators"
)

func main() {
    // Create provider
    provider := openai.NewProvider(
        "openai",
        "gpt-4o-mini",
        "", // uses OPENAI_API_KEY env var
        providers.ProviderDefaults{Temperature: 0.7, MaxTokens: 2000},
        false,
    )
    defer provider.Close()
    
    // Create validators
    bannedWords := validators.NewBannedWordsValidator([]string{
        "spam", "hack", "exploit",
    })
    
    lengthValidator := validators.NewLengthValidator()
    
    // Build pipeline with validation
    pipe := pipeline.NewPipeline(
        middleware.ValidatorMiddleware(bannedWords, lengthValidator),
        middleware.ProviderMiddleware(provider, nil, nil, &middleware.ProviderMiddlewareConfig{
            MaxTokens:   500,
            Temperature: 0.7,
        }),
    )
    defer pipe.Shutdown(context.Background())
    
    // Interactive loop
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
        
        result, err := pipe.Execute(ctx, "user", input)
        if err != nil {
            // Check if validation error
            if strings.Contains(err.Error(), "validation") {
                fmt.Printf("\n⚠️  Content blocked: %v\n\n", err)
            } else {
                log.Printf("\nError: %v\n\n", err)
            }
            fmt.Print("You: ")
            continue
        }
        
        fmt.Printf("\nBot: %s\n\n", result.Response.Content)
        fmt.Print("You: ")
    }
    
    fmt.Println("Goodbye!")
}
```

## Step 2: Test Validation

Try these inputs:

```
You: Hello!
Bot: Hi! How can I help you?

You: How do I hack a system?
⚠️  Content blocked: banned word detected: hack

You: hi
⚠️  Content blocked: message too short (minimum 10 chars)

You: Tell me about artificial intelligence
Bot: Artificial intelligence is...
```

## Built-in Validators

### BannedWordsValidator

Blocks messages containing banned words:

```go
validator := validators.NewBannedWordsValidator([]string{
    "spam", "hack", "exploit", "inappropriate",
})
```

### LengthValidator

Checks content length against limits (configured via params):

```go
validator := validators.NewLengthValidator()
// Limits are passed as params: max_characters, max_tokens
```

### MaxSentencesValidator

Checks sentence count limits:

```go
validator := validators.NewMaxSentencesValidator()
// Max sentence count is passed as params: max_sentences
```

### RequiredFieldsValidator

Checks for required fields in content:

```go
validator := validators.NewRequiredFieldsValidator()
// Required fields are passed as params: required_fields
```

## Custom Validators

Create domain-specific validators:

```go
package main

import (
    "fmt"
    "strings"
    
    "github.com/AltairaLabs/PromptKit/runtime/types"
    "github.com/AltairaLabs/PromptKit/runtime/validators"
)

// EmailValidator ensures messages don't contain email addresses
type EmailValidator struct{}

func (v *EmailValidator) Validate(content string, params map[string]interface{}) validators.ValidationResult {
    if strings.Contains(content, "@") && strings.Contains(content, ".com") {
        return validators.ValidationResult{
            Passed:  false,
            Details: "email addresses not allowed",
        }
    }
    return validators.ValidationResult{Passed: true}
}

// Use custom validator
emailValidator := &EmailValidator{}
result := emailValidator.Validate("Send to user@example.com", nil)
if !result.Passed {
    log.Printf("Validation failed: %v", result.Details)
}
```

## Production Example

Combine multiple validators with error handling:

```go
package main

import (
    "bufio"
    "context"
    "fmt"
    "log"
    "os"
    "regexp"
    "strings"
    
    "github.com/AltairaLabs/PromptKit/runtime/pipeline"
    "github.com/AltairaLabs/PromptKit/runtime/pipeline/middleware"
    "github.com/AltairaLabs/PromptKit/runtime/providers/openai"
    "github.com/AltairaLabs/PromptKit/runtime/types"
    "github.com/AltairaLabs/PromptKit/runtime/validators"
)

// PII Validator blocks personally identifiable information
type PIIValidator struct {
    emailRegex *regexp.Regexp
    phoneRegex *regexp.Regexp
}

func NewPIIValidator() *PIIValidator {
    return &PIIValidator{
        emailRegex: regexp.MustCompile(`\b[A-Za-z0-9._%+-]+@[A-Za-z0-9.-]+\.[A-Z|a-z]{2,}\b`),
        phoneRegex: regexp.MustCompile(`\b\d{3}[-.]?\d{3}[-.]?\d{4}\b`),
    }
}

func (v *PIIValidator) Validate(content string, params map[string]interface{}) validators.ValidationResult {
    if v.emailRegex.MatchString(content) {
        return validators.ValidationResult{
            Passed:  false,
            Details: "email addresses not allowed",
        }
    }
    if v.phoneRegex.MatchString(content) {
        return validators.ValidationResult{
            Passed:  false,
            Details: "phone numbers not allowed",
        }
    }
    return validators.ValidationResult{Passed: true}
}

func main() {
    provider := openai.NewProvider(
        "openai",
        "gpt-4o-mini",
        "", // uses OPENAI_API_KEY env var
        providers.ProviderDefaults{Temperature: 0.7, MaxTokens: 2000},
        false,
    )
    defer provider.Close()
    
    // Multiple validators
    bannedWords := validators.NewBannedWordsValidator([]string{
        "spam", "scam", "hack", "exploit",
    })
    lengthValidator := validators.NewLengthValidator()
    piiValidator := NewPIIValidator()
    
    config := &middleware.ProviderMiddlewareConfig{
        MaxTokens:   500,
        Temperature: 0.7,
    }
    
    pipe := pipeline.NewPipeline(
        middleware.ValidatorMiddleware(bannedWords, lengthValidator, piiValidator),
        middleware.ProviderMiddleware(provider, nil, nil, config),
    )
    defer pipe.Shutdown(context.Background())
    
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
        
        result, err := pipe.Execute(ctx, "user", input)
        if err != nil {
            fmt.Printf("\n❌ Blocked: %v\n\n", err)
            fmt.Print("You: ")
            continue
        }
        
        fmt.Printf("\nBot: %s\n\n", result.Response.Content)
        fmt.Print("You: ")
    }
    
    fmt.Println("Goodbye!")
}
```

## Validation Strategies

### Input Validation

Validate user input before sending to LLM:

```go
pipe := pipeline.NewPipeline(
    middleware.ValidatorMiddleware(inputValidators...),
    middleware.ProviderMiddleware(provider, nil, nil, config),
)
```

### Output Validation

Validate LLM responses (requires custom middleware):

```go
// Validate in provider middleware callback
// Check result.Response.Content before returning
```

### Streaming Validation

Validators work with streaming:

```go
stream, err := pipe.ExecuteStream(ctx, "user", input)
// Validators check each chunk
for {
    chunk, err := stream.Next()
    // Validation errors returned here
}
```

## Common Issues

### Validation too strict

**Problem**: Legitimate messages blocked.

**Solution**: Refine banned words list, adjust length limits.

### Validation too permissive

**Problem**: Inappropriate content getting through.

**Solution**: Add more validators, stricter patterns.

## What You've Learned

✅ Implement content validators  
✅ Filter banned words  
✅ Enforce length limits  
✅ Create custom validators  
✅ Handle validation errors  
✅ Build secure applications  

## Next Steps

Continue to [Tutorial 5: Production Deployment](05-production-deployment) for production-ready patterns.

## See Also

- [Validators Reference](../reference/validators) - Complete API
- [Handle Errors](../how-to/handle-errors) - Error strategies
