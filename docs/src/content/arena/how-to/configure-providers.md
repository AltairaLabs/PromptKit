---
title: Configure LLM Providers
docType: how-to
order: 3
---
# Configure LLM Providers

Learn how to configure and manage LLM providers for testing.

## Overview

Providers define how PromptArena connects to different LLM services (OpenAI, Anthropic, Google, etc.). Each provider configuration specifies authentication, model selection, and default parameters.

## Provider File Structure

Create provider configurations in `providers/` directory:

```yaml
# providers/openai.yaml
version: "1.0"
type: openai
model: gpt-4o-mini
region: us

parameters:
  temperature: 0.6
  max_tokens: 2000
  top_p: 1.0

auth:
  api_key_env: OPENAI_API_KEY  # Read from environment variable
```

## Supported Providers

### OpenAI

```yaml
# providers/openai-gpt4.yaml
version: "1.0"
type: openai
model: gpt-4o
region: us

parameters:
  temperature: 0.7
  max_tokens: 4000

auth:
  api_key_env: OPENAI_API_KEY
```

**Available Models**: `gpt-4o`, `gpt-4o-mini`, `gpt-4-turbo`, `gpt-3.5-turbo`

### Anthropic Claude

```yaml
# providers/claude.yaml
version: "1.0"
type: anthropic
model: claude-3-5-sonnet-20241022
region: us

parameters:
  temperature: 0.6
  max_tokens: 4000

auth:
  api_key_env: ANTHROPIC_API_KEY
```

**Available Models**: `claude-3-5-sonnet-20241022`, `claude-3-5-haiku-20241022`, `claude-3-opus-20240229`

### Google Gemini

```yaml
# providers/gemini.yaml
version: "1.0"
type: google
model: gemini-1.5-flash
region: us

parameters:
  temperature: 0.7
  max_tokens: 2000

auth:
  api_key_env: GOOGLE_API_KEY
```

**Available Models**: `gemini-1.5-pro`, `gemini-1.5-flash`, `gemini-2.0-flash-exp`

### Azure OpenAI

```yaml
# providers/azure-openai.yaml
version: "1.0"
type: azure-openai
model: gpt-4o
region: eastus

azure:
  endpoint: https://your-resource.openai.azure.com
  deployment: gpt-4o-deployment
  api_version: "2024-02-15-preview"

parameters:
  temperature: 0.6
  max_tokens: 2000

auth:
  api_key_env: AZURE_OPENAI_API_KEY
```

## Arena Configuration

Reference providers in your `arena.yaml`:

```yaml
version: "1.0"

providers:
  - path: ./providers/openai.yaml
  - path: ./providers/claude.yaml
  - path: ./providers/gemini.yaml

# Provider selection
default_providers: [openai-gpt4, claude-sonnet]

# Or in scenarios, specify which providers to use
scenarios:
  - path: ./scenarios/customer-support.yaml
    providers: [openai-gpt4, claude-sonnet]  # Test with these providers
```

## Authentication Setup

### Environment Variables

Set API keys as environment variables:

```bash
# Add to ~/.zshrc or ~/.bashrc
export OPENAI_API_KEY="sk-..."
export ANTHROPIC_API_KEY="sk-ant-..."
export GOOGLE_API_KEY="..."

# Reload shell configuration
source ~/.zshrc
```

### .env File (Local Development)

Create a `.env` file (never commit this):

```bash
# .env
OPENAI_API_KEY=sk-...
ANTHROPIC_API_KEY=sk-ant-...
GOOGLE_API_KEY=...
```

Load environment variables before running:

```bash
# Load .env and run tests
export $(cat .env | xargs) && promptarena run
```

### CI/CD Secrets

For GitHub Actions, GitLab CI, or other platforms:

```yaml
# .github/workflows/test.yml
env:
  OPENAI_API_KEY: $
  ANTHROPIC_API_KEY: $
```

## Common Configurations

### Multiple Model Variants

Test across different model sizes/versions:

```yaml
# providers/openai-variants.yaml
---
version: "1.0"
type: openai
model: gpt-4o
alias: openai-gpt4
parameters:
  temperature: 0.6
---
version: "1.0"
type: openai
model: gpt-4o-mini
alias: openai-mini
parameters:
  temperature: 0.6
```

### Regional Variants

```yaml
# providers/claude-us.yaml
version: "1.0"
type: anthropic
model: claude-3-5-sonnet-20241022
region: us
alias: claude-us

# providers/claude-eu.yaml
version: "1.0"
type: anthropic
model: claude-3-5-sonnet-20241022
region: eu
alias: claude-eu
```

### Temperature Variations

```yaml
# providers/openai-creative.yaml
version: "1.0"
type: openai
model: gpt-4o
alias: openai-creative
parameters:
  temperature: 0.9  # More creative/random

# providers/openai-precise.yaml
version: "1.0"
type: openai
model: gpt-4o
alias: openai-precise
parameters:
  temperature: 0.1  # More deterministic
```

## Provider Selection

### Run Specific Providers

```bash
# Test with only OpenAI
promptarena run --provider openai-gpt4

# Test with multiple providers
promptarena run --provider openai-gpt4,claude-sonnet

# Test all configured providers (default)
promptarena run
```

### Scenario-specific Providers

```yaml
# scenarios/openai-only.yaml
version: "1.0"
task_type: support
providers: [openai-gpt4]  # Only test with this provider

test_cases:
  - name: "OpenAI-specific feature test"
    turns:
      - user: "Test message"
```

## Parameter Overrides

Override provider parameters at runtime:

```bash
# Override temperature for all providers
promptarena run --temperature 0.8

# Override max tokens
promptarena run --max-tokens 1000

# Combined overrides
promptarena run --temperature 0.9 --max-tokens 4000
```

## Validation

Verify provider configuration:

```bash
# Inspect loaded providers
promptarena config-inspect

# Should show:
# Providers:
#   ✓ openai-gpt4 (providers/openai.yaml)
#   ✓ claude-sonnet (providers/claude.yaml)
```

## Troubleshooting

### Authentication Errors

```bash
# Verify API key is set
echo $OPENAI_API_KEY
# Should display: sk-...

# Test with verbose logging
promptarena run --provider openai-gpt4 --verbose
```

### Provider Not Found

```bash
# Check provider configuration
promptarena config-inspect --verbose

# Verify file path in arena.yaml matches actual file location
```

### Rate Limiting

Configure concurrency to avoid rate limits:

```bash
# Reduce concurrent requests
promptarena run --concurrency 2

# For large test suites
promptarena run --concurrency 1  # Sequential execution
```

## Next Steps

- **[Use Mock Providers](use-mock-providers)** - Test without API calls
- **[Validate Outputs](validate-outputs)** - Add assertions
- **[Integrate CI/CD](integrate-ci-cd)** - Automate testing
- **[Config Reference](../reference/config-schema)** - Complete configuration options

## Examples

See working provider configurations in:
- `examples/customer-support/providers/`
- `examples/mcp-chatbot/providers/`
