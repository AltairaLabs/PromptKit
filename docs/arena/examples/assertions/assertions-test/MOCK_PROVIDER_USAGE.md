# Using Mock Providers with Arena

This guide demonstrates how to use mock providers to run Arena tests without making actual API calls.

## Quick Start

Run Arena with the `--mock-provider` flag:

```bash
promptarena run --mock-provider
```

## Using Mock Configuration

Run with custom mock responses:

```bash
promptarena run --mock-provider --mock-config mock-responses.yaml
```

## Example: Assertions Testing

Test assertion functionality with deterministic responses:

```bash
# Test all assertion scenarios
promptarena run --mock-provider --mock-config mock-responses.yaml --ci

# Test specific assertion type
promptarena run --mock-provider --scenario scripted-turns
```

## Benefits

- **No API keys required**: Test without credentials
- **Fast execution**: Instant responses for rapid iteration
- **Deterministic results**: Same input produces same output
- **Zero costs**: No API charges for testing

## See Also

- [Mock Configuration](mock-responses.yaml) - Mock responses for this example
- [Arena User Guide](../../docs/guides/arena-user-guide.md) - General Arena usage
- [Main Examples README](../README.md) - Overview of all examples
