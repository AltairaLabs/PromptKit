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

### Mock Configuration Format

```yaml
# Global default response (used when no specific match found)
defaultResponse: "This is a mock response"

# Scenario-specific configurations
scenarios:
  my-scenario-id:
    # Default for this scenario (overrides global)
    defaultResponse: "Scenario-specific default"
    
    # Turn-by-turn responses
    turns:
      1: "Response for turn 1"
      2: "Response for turn 2"
      3: "Response for turn 3"
```

### Response Priority

The mock provider uses this priority order for responses:

1. **Scenario + Turn specific** (`scenarios.my-scenario.turns.1`)
2. **Scenario default** (`scenarios.my-scenario.defaultResponse`)
3. **Global default** (`defaultResponse`)
4. **Built-in fallback** (generated from provider/model names)

## Example: Customer Support

This example demonstrates running the customer-support scenario with mock responses:

```bash
# Run with mock provider and configuration
promptarena run \
  --mock-provider \
  --mock-config mock-responses.yaml \
  --scenario customer-support-scenarios \
  --provider openai-gpt-4o-mini
```

The mock configuration provides appropriate responses for each turn of the conversation:

```yaml
scenarios:
  customer-support-scenarios:
    turns:
      1: "Thank you for reaching out! I can help you track your order #12345..."
      2: "I understand your concern about not receiving your order yet..."
      3: "I'd be happy to help you process a refund..."
```

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

## Advanced Usage

### Programmatic Configuration (SDK)

For SDK usage or unit tests, use the in-memory repository:

```go
import "github.com/AltairaLabs/PromptKit/runtime/providers"

// Create in-memory repository
repo := providers.NewInMemoryMockRepository("default response")

// Set scenario-specific responses
repo.SetResponse("scenario1", 1, "First turn response")
repo.SetResponse("scenario1", 2, "Second turn response")

// Create mock provider with repository
mockProvider := providers.NewMockProviderWithRepository(
    "my-provider",
    "gpt-4",
    false,
    repo,
)
```

### File-Based Configuration (Flexible)

Load responses from YAML files:

```go
repo, err := providers.NewFileMockRepository("mock-config.yaml")
if err != nil {
    log.Fatal(err)
}

mockProvider := providers.NewMockProviderWithRepository(
    "my-provider",
    "gpt-4",
    false,
    repo,
)
```

## CI/CD Integration

### GitHub Actions

```yaml
- name: Run Arena Tests (Mock Provider)
  run: |
    promptarena run \
      --mock-provider \
      --mock-config .github/mock-responses.yaml \
      --ci
```

### GitLab CI

```yaml
test:arena:
  script:
    - promptarena run --mock-provider --ci
```

## Limitations

Currently, the mock provider:

- ✅ Returns deterministic responses
- ✅ Simulates costs and tokens
- ✅ Works with all Arena features (validators, tools, etc.)
- ❌ Does not support tool call simulation (planned)
- ⚠️ Turn-specific responses require scenario context (future enhancement)

## See Also

- [Mock Configuration Example](mock-config-example.yaml) - Full configuration format
- [Arena User Guide](../../docs/guides/arena-user-guide.md) - General Arena usage
- [GitHub Issue #27](https://github.com/AltairaLabs/PromptKit/issues/27) - Mock Provider enhancement tracking

