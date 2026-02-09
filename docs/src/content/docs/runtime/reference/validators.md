---
title: Validators
sidebar:
  order: 5
---
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

Enforces length constraints.

**Constructor**:
```go
func NewLengthValidator() *LengthValidator
```

**Example**:
```go
validator := validators.NewLengthValidator()

result := validator.Validate("Short", nil)
if !result.Passed {
    log.Println("Response length violation")
}
```

### MaxSentencesValidator

Validates number of sentences.

**Constructor**:
```go
func NewMaxSentencesValidator() *MaxSentencesValidator
```

**Example**:
```go
validator := validators.NewMaxSentencesValidator()

result := validator.Validate("One sentence.", nil)
if !result.Passed {
    log.Println("Sentence count violation")
}
```

### RequiredFieldsValidator

Checks for required fields in content.

**Constructor**:
```go
func NewRequiredFieldsValidator() *RequiredFieldsValidator
```

**Example**:
```go
validator := validators.NewRequiredFieldsValidator()

result := validator.Validate("Some content", map[string]interface{}{
    "required_fields": []string{"name", "email"},
})
if !result.Passed {
    log.Println("Missing required fields")
}
```

## Usage with Pipeline

### Validator Middleware

```go
import "github.com/AltairaLabs/PromptKit/runtime/validators"

// Create validators
validatorList := []validators.Validator{
    validators.NewBannedWordsValidator([]string{"banned"}),
    validators.NewLengthValidator(),
    validators.NewMaxSentencesValidator(),
}

// Validate content
for _, v := range validatorList {
    result := v.Validate(content, nil)
    if !result.Passed {
        log.Printf("Validation failed: %v", result.Details)
    }
}
```

### Streaming Validation

```go
// Streaming validators can check chunks in real-time
validator := validators.NewBannedWordsValidator([]string{"inappropriate"})

// Check each chunk during streaming
err := validator.ValidateChunk(chunk)
if err != nil {
    log.Printf("Stream interrupted: %v", err)
    // Abort stream
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
    validators.NewLengthValidator(),              // Fast check
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
    validators.NewLengthValidator(),
    validators.NewMaxSentencesValidator(),
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

- [Pipeline Reference](pipeline) - Using validators in pipelines
- [Validator How-To](../how-to/implement-validators) - Custom validators
- [Validator Tutorial](../tutorials/04-content-validation) - Validation patterns
