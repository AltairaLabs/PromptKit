---
layout: default
title: guardrails-test
parent: PromptArena Examples
grand_parent: PromptArena
---


# Guardrails Test Example

This example demonstrates the **guardrail assertion** feature, which allows you to test whether validators (guardrails) trigger as expected in your prompt configurations.

## Overview

The `guardrail_triggered` assertion type enables you to:

- **Verify guardrails trigger when they should** - Test that your validators catch problematic inputs
- **Verify guardrails don't trigger when they shouldn't** - Ensure clean inputs pass through without false positives
- **Test in non-production mode** - Use `suppress_validation_exceptions: true` to allow execution to continue after validation failures, so you can assert on the guardrail behavior

## Key Concept: SuppressValidationExceptions

By default, when a validator fails, it throws a `ValidationError` and halts execution. This is the correct behavior for production.

For **testing purposes**, Arena automatically enables `SuppressValidationExceptions` mode in the validator middleware. This allows:

1. The validator to run and record its result (pass/fail)
2. Execution to continue even if validation fails
3. Assertions to inspect whether the guardrail triggered

**Important**: This suppression behavior is built into Arena's pipeline construction, not configured in the PromptConfig. Your production prompt configurations remain unchanged - they use the same validator definitions for both production and testing.


## Example Structure

```text
guardrails-test/
├── arena.yaml              # Test scenarios with guardrail_triggered assertions
├── prompts/
│   └── content-filter.yaml # Prompt with banned_words validator
└── providers/
    └── openai.yaml         # OpenAI provider configuration
```


## Configuration Details

### Prompt Configuration (`prompts/content-filter.yaml`)

The prompt includes a `banned_words` validator - the same configuration used in production:

```yaml
validators:
  - type: banned_words
    params:
      words:
        - damn
        - crap
        - hell
      case_sensitive: false
```

**Note**: No special test-only flags are needed in the PromptConfig. Arena's test framework automatically enables suppression mode when running validators.

### Test Scenarios (`arena.yaml`)

Four test scenarios demonstrate different assertion patterns:

1. **`guardrail-should-trigger`**: Input contains banned words → expect validator to trigger
2. **`guardrail-should-not-trigger`**: Clean input → expect validator not to trigger
3. **`multiple-violations`**: Multiple banned words → expect validator to trigger
4. **`streaming-guardrail-trigger`**: Tests guardrail in streaming mode → expect validator to trigger and interrupt stream

Each scenario uses the `guardrail_triggered` assertion:

```yaml
assertions:
  - type: guardrail_triggered
    validator: banned_words        # Name of the validator to check
    should_trigger: true           # Expected behavior (true = should fail, false = should pass)
    message: "Descriptive message for test output"
```

## Running the Tests

1. Set up your OpenAI API key:

   ```bash
   export OPENAI_API_KEY="your-api-key-here"
   ```

2. Run the Arena tests:

   ```bash
   promptarena run examples/guardrails-test/arena.yaml
   ```


## Expected Results

- ✅ **guardrail-should-trigger**: PASS (validator triggered as expected)
- ✅ **guardrail-should-not-trigger**: PASS (validator did not trigger as expected)
- ✅ **multiple-violations**: PASS (validator triggered on multiple violations as expected)
- ✅ **streaming-guardrail-trigger**: PASS (validator triggered in streaming mode and interrupted stream as expected)

## Streaming Mode Support

The `streaming-guardrail-trigger` scenario demonstrates how guardrails work with streaming responses:

- **Real-time validation**: Validators process each chunk as it arrives
- **Stream interruption**: When a validation fails, the stream is immediately interrupted
- **Suppression behavior**: With suppression enabled (Arena test mode), the stream interrupts but no error is thrown
- **Recorded results**: Validation failures are still recorded in metadata for assertions to inspect

This ensures that guardrails work correctly in both regular and streaming execution modes.

## How It Works

1. **Execution Phase**:
   - User input is processed through the prompt
   - Validators run (Arena automatically enables suppression mode)
   - Validation results are recorded in execution context, but errors are suppressed
   - LLM generates a response

2. **Assertion Phase**:
   - The `guardrail_triggered` assertion inspects the execution context
   - It finds the last assistant message and its validation results
   - It checks if the specified validator passed or failed
   - It compares the actual result against the `should_trigger` expectation

3. **Test Outcome**:
   - If actual behavior matches expectation → Test PASS
   - If actual behavior differs from expectation → Test FAIL with descriptive error

## Use Cases

This pattern is valuable for:

- **Regression testing**: Ensure guardrails continue to work as expected over time
- **Configuration validation**: Verify validator configs (banned word lists, patterns, etc.) are correct
- **Coverage testing**: Confirm edge cases are properly handled by your guardrails
- **CI/CD integration**: Automated testing of prompt safety measures


## Production vs Test Mode

**Production Mode** (SDK/Conversation API - default):

```go
// Production code uses DynamicValidatorMiddleware with default behavior
middleware.DynamicValidatorMiddleware(registry)
```

- Validation failures throw errors immediately
- Execution halts on first validation failure
- Appropriate for live user-facing systems

**Test Mode** (Arena test framework):

```go
// Arena automatically uses suppression mode
middleware.DynamicValidatorMiddlewareWithSuppression(registry, true)
```

- Validation failures are logged but don't throw errors
- Execution continues so assertions can inspect results
- Appropriate for automated testing and development

**The same PromptConfig works in both modes** - no test-specific configuration needed!


## Related Documentation

- [GUARDRAIL_ASSERTION_PROPOSAL.md](../../docs/local-backlog/GUARDRAIL_ASSERTION_PROPOSAL.md) - Original proposal
- [GitHub Issue #25](https://github.com/altairalabs/promptkit-public/issues/25) - Implementation tracking
- [Arena User Guide](../../docs/guides/arena-user-guide.md) - General Arena testing documentation
