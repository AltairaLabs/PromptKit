# Using Mock Providers with Arena

This guide demonstrates how to use mock providers to run Arena tests without making actual API calls. This is useful for:

- **CI/CD pipelines**: Fast, deterministic tests without API costs
- **Local development**: Test scenarios without API keys
- **Reproducible results**: Consistent responses for regression testing

## Quick Start

Run Arena with the `--mock-provider` flag to replace all configured providers with MockProvider:

```bash
promptarena run --mock-provider
```

## Using Mock Configuration Files

For more control over responses, create a YAML configuration file with scenario and turn-specific responses:

```bash
promptarena run --mock-provider --mock-config mock-responses.yaml
```

## Example: Context Management

This example demonstrates running context management scenarios with mock responses:

```bash
# Run with mock provider and configuration
promptarena run \
  --mock-provider \
  --mock-config mock-responses.yaml \
  --scenario context-unlimited \
  --provider openai-gpt4
```

The mock configuration provides appropriate responses for each turn of the conversation, testing context retention and management without requiring real API calls.

## Benefits

### No API Keys Required

Run tests without setting up API credentials:

```bash
# No .env file or API keys needed!
promptarena run --mock-provider --ci
```

### Fast Execution

Mock providers return responses instantly:

- **Real API**: 1-3 seconds per request
- **Mock Provider**: < 1ms per request

### Deterministic Testing

Same input always produces same output:

- Perfect for regression tests
- Reproducible CI/CD results
- No flaky tests due to API variance

### Cost Savings

- **Zero API costs** in CI/CD
- Test multiple scenarios without budget concerns
- Rapid iteration during development

## See Also

- [Mock Configuration](mock-responses.yaml) - Example configuration for this example
- [Arena User Guide](../../docs/guides/arena-user-guide.md) - General Arena usage
- [Main Examples README](../README.md) - Overview of all examples
