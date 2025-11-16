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

## Example: MCP Memory Testing

Test MCP memory/knowledge graph operations with deterministic responses:

```bash
# Test memory operations
promptarena run --mock-provider --mock-config mock-responses.yaml --ci

# Test specific memory scenario
promptarena run --mock-provider --scenario mcp-memory-test
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
