---
layout: default
title: Validators
parent: Runtime Reference
grand_parent: Runtime
nav_order: 5
---

# Validators Reference

Content validation for LLM responses and user inputs.

## Overview

Validators ensure conversation quality by checking content against policies:

- **Banned words**: Content policy enforcement
- **Length limits**: Control verbosity
- **Required fields**: Ensure necessary information
- **Streaming validation**: Real-time content checking

## Core Interfaces

### Validator

```go
type Validator interface {
    Validate(content string, params map[string]interface{}) ValidationResult
}

type ValidationResult struct {
    Passed  bool
    Details interface{}
}
```

### StreamingValidator

```go
type StreamingValidator interface {
    Validator
    ValidateChunk(chunk providers.StreamChunk, params ...map[string]interface{}) error
    SupportsStreaming() bool
}
```

## Built-in Validators

### BannedWordsValidator

Checks for prohibited words/phrases.

**Constructor**:
```go
func NewBannedWordsValidator(bannedWords []string) *BannedWordsValidator
```

**Example**:
```go
validator := validators.NewBannedWordsValidator([]string{
    "inappropriate",
    "offensive",
})

result := validator.Validate("This is inappropriate content", nil)
if !result.Passed {
    log.Println("Content policy violation")
}
```

**Streaming**:
```go
// Abort stream on banned word
err := validator.ValidateChunk(chunk)
if err != nil {
    // Stream will be interrupted
    return err
}
```

### LengthValidator

Enforces minimum and maximum length constraints.

**Constructor**:
```go
func NewLengthValidator(minLength, maxLength int) *LengthValidator
```

**Example**:
```go
validator := validators.NewLengthValidator(10, 500)

result := validator.Validate("Short", nil)
if !result.Passed {
    log.Println("Response too short")
}
```

### SentenceCountValidator

Validates number of sentences.

**Constructor**:
```go
func NewSentenceCountValidator(minSentences, maxSentences int) *SentenceCountValidator
```

**Example**:
```go
validator := validators.NewSentenceCountValidator(2, 10)

result := validator.Validate("One sentence.", nil)
if !result.Passed {
    log.Println("Not enough sentences")
}
```

### RoleIntegrityValidator

Prevents role confusion in responses.

**Constructor**:
```go
func NewRoleIntegrityValidator() *RoleIntegrityValidator
```

**Example**:
```go
validator := validators.NewRoleIntegrityValidator()

// Detects if response contains "User:" or "Assistant:" markers
result := validator.Validate("Assistant: I can help.", nil)
if !result.Passed {
    log.Println("Role marker detected in response")
}
```

## Usage with Pipeline

### Validator Middleware

```go
import (
    "github.com/AltairaLabs/PromptKit/runtime/pipeline/middleware"
    "github.com/AltairaLabs/PromptKit/runtime/validators"
)

// Create validators
validatorList := []validators.Validator{
    validators.NewBannedWordsValidator([]string{"banned"}),
    validators.NewLengthValidator(10, 1000),
    validators.NewSentenceCountValidator(1, 20),
}

// Add to pipeline
pipe := pipeline.NewPipeline(
    middleware.ProviderMiddleware(provider, nil, nil, config),
    middleware.ValidatorMiddleware(validatorList),
)

// Validation runs after provider execution
result, err := pipe.Execute(ctx, "user", "Hello")
if err != nil {
    log.Printf("Validation failed: %v", err)
}
```

### Streaming Validation

```go
// Streaming validators abort stream on violation
validatorList := []validators.Validator{
    validators.NewBannedWordsValidator([]string{"inappropriate"}),
}

pipe := pipeline.NewPipeline(
    middleware.ProviderMiddleware(provider, nil, nil, config),
    middleware.ValidatorMiddleware(validatorList),
)

// Stream will be interrupted if banned word detected
streamChan, _ := pipe.ExecuteStream(ctx, "user", "Hello")
for chunk := range streamChan {
    if chunk.Error != nil {
        log.Printf("Stream interrupted: %v", chunk.Error)
        break
    }
    fmt.Print(chunk.Delta)
}
```

## Custom Validators

### Synchronous Validator

```go
type CustomValidator struct{}

func (v *CustomValidator) Validate(
    content string,
    params map[string]interface{},
) validators.ValidationResult {
    // Custom validation logic
    if strings.Contains(content, "forbidden") {
        return validators.ValidationResult{
            Passed:  false,
            Details: "Contains forbidden term",
        }
    }
    return validators.ValidationResult{Passed: true}
}
```

### Streaming Validator

```go
type CustomStreamingValidator struct {
    buffer string
}

func (v *CustomStreamingValidator) ValidateChunk(
    chunk providers.StreamChunk,
    params ...map[string]interface{},
) error {
    v.buffer += chunk.Delta
    
    // Check accumulated content
    if strings.Contains(v.buffer, "stop-phrase") {
        return fmt.Errorf("stop phrase detected")
    }
    return nil
}

func (v *CustomStreamingValidator) SupportsStreaming() bool {
    return true
}

func (v *CustomStreamingValidator) Validate(
    content string,
    params map[string]interface{},
) validators.ValidationResult {
    // Fallback validation for non-streaming
    if strings.Contains(content, "stop-phrase") {
        return validators.ValidationResult{
            Passed:  false,
            Details: "Stop phrase detected",
        }
    }
    return validators.ValidationResult{Passed: true}
}
```

## Best Practices

### 1. Validation Order

```go
// Order matters - fast validators first
validatorList := []validators.Validator{
    validators.NewLengthValidator(1, 10000),      // Fast check
    validators.NewBannedWordsValidator(banned),   // Medium
    customExpensiveValidator,                      // Slow check last
}
```

### 2. Streaming Optimization

```go
// Use streaming validators for real-time enforcement
streamingValidators := []validators.Validator{
    validators.NewBannedWordsValidator(criticalBannedWords),
}

// Use regular validators for post-processing
postValidators := []validators.Validator{
    validators.NewLengthValidator(10, 500),
    validators.NewSentenceCountValidator(2, 10),
}
```

### 3. Error Handling

```go
result := validator.Validate(content, nil)
if !result.Passed {
    // Log details for debugging
    log.Printf("Validation failed: %v", result.Details)
    
    // Take appropriate action
    return fmt.Errorf("content validation failed")
}
```

## See Also

- [Pipeline Reference](pipeline.md) - Using validators in pipelines
- [Validator How-To](../how-to/implement-validators.md) - Custom validators
- [Validator Tutorial](../tutorials/04-content-validation.md) - Validation patterns
