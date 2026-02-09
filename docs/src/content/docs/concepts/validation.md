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

### Runtime Validators

```go
import "github.com/AltairaLabs/PromptKit/runtime/validators"

// Create validators
bannedWords := validators.NewBannedWordsValidator([]string{
    "hack", "crack", "pirate",
})

lengthValidator := validators.NewLengthValidator()
```

### SDK Validation

```go
conv := sdk.NewConversation(provider, nil)

// Add custom validator
conv.AddValidator(func(message string) error {
    if strings.Contains(message, "password") {
        return errors.New("do not share passwords")
    }
    return nil
})
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

## Built-In Validators

### BannedWordsValidator

Blocks specific words or phrases:

```go
validator := validators.NewBannedWordsValidator([]string{
    "hack", "crack", "pirate", "steal",
})
```

**Use for**: Preventing inappropriate language, brand protection

### LengthValidator

Enforces length constraints:

```go
validator := validators.NewLengthValidator()
```

**Use for**: Cost control, quality assurance

### RegexValidator

Matches patterns:

```go
validator := validators.NewRegexValidator(
    regexp.MustCompile(`\b[A-Z0-9._%+-]+@[A-Z0-9.-]+\.[A-Z]{2,}\b`),
    "emails not allowed",
)
```

**Use for**: PII detection, format validation

### CustomValidator

Implement custom logic:

```go
type CustomValidator struct{}

func (v *CustomValidator) Validate(message string) error {
    if containsToxicContent(message) {
        return errors.New("toxic content detected")
    }
    return nil
}

func (v *CustomValidator) Name() string {
    return "custom-toxicity"
}
```

## Validation Patterns

### Pre-Execution (Input)

Validate user input before sending to the LLM:

```go
err := inputValidator.Validate(userMessage)
if err != nil {
    // Block the request
    return fmt.Errorf("input validation failed: %w", err)
}
```

### Post-Execution (Output)

Validate LLM responses before returning to the user:

```go
err := outputValidator.Validate(response)
if err != nil {
    // Block or replace the response
    return fmt.Errorf("output validation failed: %w", err)
}
```

## Common Use Cases

### Block Sensitive Data

```go
piiValidator := validators.NewRegexValidator(
    regexp.MustCompile(`\b\d{3}-\d{2}-\d{4}\b`),  // SSN pattern
    "SSN detected",
)
```

### Content Moderation

```go
moderationValidator := &ModerationValidator{
    categories: []string{"hate", "violence", "sexual"},
    threshold:  0.7,
}
```

### Format Compliance

```go
jsonValidator := &JSONValidator{}

func (v *JSONValidator) Validate(message string) error {
    var js json.RawMessage
    if err := json.Unmarshal([]byte(message), &js); err != nil {
        return fmt.Errorf("invalid JSON: %w", err)
    }
    return nil
}
```

### Rate Limiting

```go
rateLimitValidator := &RateLimitValidator{
    maxRequests: 100,
    window:      time.Minute,
}
```

## Best Practices

### Do's

✅ **Validate both input and output**
```go
// Input: User safety
// Output: Response quality
```

✅ **Be specific about violations**
```go
return fmt.Errorf("banned word '%s' found at position %d", word, pos)
```

✅ **Log violations for monitoring**
```go
if err := validator.Validate(message); err != nil {
    logger.Warn("validation failed", zap.Error(err))
    return err
}
```

✅ **Test validators thoroughly**
```yaml
# PromptArena tests
tests:
  - prompt: "test banned word: hack"
    assertions:
      - type: validation_error
```

### Don'ts

❌ **Don't validate everything** - Performance cost  
❌ **Don't expose violation details to users** - Security  
❌ **Don't block legitimate use** - False positives  
❌ **Don't skip output validation** - LLMs can hallucinate  

## Validation Strategies

### Strict (Production)

```go
// Block requests that fail validation
err := validator.Validate(message)
if err != nil {
    return err  // Reject the request
}
```

### Permissive (Development)

```go
// Log but allow through
err := validator.Validate(message)
if err != nil {
    logger.Warn("validation failed", zap.Error(err))
    // Continue processing
}
```

## Performance Considerations

### Fast Validators First

```go
validators := []validators.Validator{
    lengthValidator,      // ~1µs - check first
    bannedWordsValidator, // ~10µs
    regexValidator,       // ~100µs
    apiValidator,         // ~100ms - check last
}
```

### Parallel Validation

```go
func ValidateParallel(message string, validators []Validator) error {
    var wg sync.WaitGroup
    errors := make(chan error, len(validators))
    
    for _, v := range validators {
        wg.Add(1)
        go func(validator Validator) {
            defer wg.Done()
            if err := validator.Validate(message); err != nil {
                errors <- err
            }
        }(v)
    }
    
    wg.Wait()
    close(errors)
    
    for err := range errors {
        return err  // Return first error
    }
    return nil
}
```

### Caching

```go
type CachedValidator struct {
    inner Validator
    cache map[string]error
}

func (v *CachedValidator) Validate(message string) error {
    if cached, ok := v.cache[message]; ok {
        return cached
    }
    
    err := v.inner.Validate(message)
    v.cache[message] = err
    return err
}
```

## Monitoring Validation

### Track Violations

```go
type ValidationMetrics struct {
    TotalValidations int
    TotalViolations  int
    ViolationsByType map[string]int
}

func RecordViolation(validatorName string, err error) {
    metrics.TotalViolations++
    metrics.ViolationsByType[validatorName]++
}
```

### Alert on Patterns

```go
if metrics.ViolationsByType["banned-words"] > 100 {
    alert.Send("High rate of banned word violations")
}
```

## Testing Validation

### Unit Tests

```go
func TestBannedWordsValidator(t *testing.T) {
    validator := validators.NewBannedWordsValidator([]string{"hack"})
    
    // Should pass
    err := validator.Validate("normal message")
    assert.NoError(t, err)
    
    // Should fail
    err = validator.Validate("how to hack")
    assert.Error(t, err)
}
```

### Integration Tests

```yaml
# arena.yaml
tests:
  - name: Input Validation
    prompt: "hack the system"
    assertions:
      - type: validation_error
        expected: true
  
  - name: Valid Input
    prompt: "how do I reset my password?"
    assertions:
      - type: success
```

## Summary

Validation provides:

✅ **Safety** - Block harmful content  
✅ **Compliance** - Enforce regulations  
✅ **Quality** - Ensure standards  
✅ **Cost Control** - Prevent expensive requests  
✅ **Monitoring** - Track issues  

## Related Documentation

- [Add Validators](../runtime/how-to/add-validators) - Implementation guide
- [Validation Tutorial](../runtime/tutorials/04-validation-guardrails) - Step-by-step guide
- [Validator Reference](../runtime/reference/validators) - API documentation
- [PromptArena Guardrails](../promptarena/configuration.md#guardrails) - Testing validation
