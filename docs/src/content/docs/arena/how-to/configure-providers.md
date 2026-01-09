---
title: Configure LLM Providers
sidebar:
  order: 3
---
Learn how to configure and manage LLM providers for testing.

## Overview

Providers define how PromptArena connects to different LLM services (OpenAI, Anthropic, Google, etc.). Each provider configuration specifies authentication, model selection, and default parameters.

## Quick Start with Templates

The easiest way to set up providers is using the project generator:

```bash
# Create project with OpenAI
promptarena init my-test --quick --provider openai

# Or choose during interactive setup
promptarena init my-test
# Select provider when prompted: openai, anthropic, google, or mock
```

This automatically creates a working provider configuration with:

- Correct API version and schema
- Recommended model defaults
- Environment variable setup (.env file)
- Ready-to-use configuration

## Manual Provider Configuration

For custom setups or advanced configurations, create provider files in `providers/` directory:

```yaml
# providers/openai.yaml
apiVersion: promptkit.altairalabs.ai/v1alpha1
kind: Provider
metadata:
  name: openai-gpt4o-mini
  labels:
    provider: openai

spec:
  type: openai
  model: gpt-4o-mini
  
  defaults:
    temperature: 0.6
    max_tokens: 2000
    top_p: 1.0
```

Authentication uses the `OPENAI_API_KEY` environment variable automatically.

## Supported Providers

### OpenAI

```yaml
# providers/openai-gpt4.yaml
apiVersion: promptkit.altairalabs.ai/v1alpha1
kind: Provider
metadata:
  name: openai-gpt4o
  labels:
    provider: openai

spec:
  type: openai
  model: gpt-4o
  
  defaults:
    temperature: 0.7
    max_tokens: 4000
```

**Available Models**: `gpt-4o`, `gpt-4o-mini`, `gpt-4-turbo`, `gpt-3.5-turbo`

### Anthropic Claude

```yaml
# providers/claude.yaml
apiVersion: promptkit.altairalabs.ai/v1alpha1
kind: Provider
metadata:
  name: claude-sonnet
  labels:
    provider: anthropic

spec:
  type: anthropic
  model: claude-3-5-sonnet-20241022
  
  defaults:
    temperature: 0.6
    max_tokens: 4000
```

Authentication uses the `ANTHROPIC_API_KEY` environment variable automatically.

**Available Models**: `claude-3-5-sonnet-20241022`, `claude-3-5-haiku-20241022`, `claude-3-opus-20240229`

### Google Gemini

```yaml
# providers/gemini.yaml
apiVersion: promptkit.altairalabs.ai/v1alpha1
kind: Provider
metadata:
  name: gemini-flash
  labels:
    provider: google

spec:
  type: gemini
  model: gemini-1.5-flash
  
  defaults:
    temperature: 0.7
    max_tokens: 2000
```

Authentication uses the `GOOGLE_API_KEY` environment variable automatically.

**Available Models**: `gemini-1.5-pro`, `gemini-1.5-flash`, `gemini-2.0-flash-exp`

### Azure OpenAI

```yaml
# providers/azure-openai.yaml
apiVersion: promptkit.altairalabs.ai/v1alpha1
kind: Provider
metadata:
  name: azure-openai-gpt4o
  labels:
    provider: azure-openai

spec:
  type: azure-openai
  model: gpt-4o

  base_url: https://your-resource.openai.azure.com

  defaults:
    temperature: 0.6
    max_tokens: 2000
```

Authentication uses the `AZURE_OPENAI_API_KEY` environment variable automatically.

### Ollama (Local)

```yaml
# providers/ollama.yaml
apiVersion: promptkit.altairalabs.ai/v1alpha1
kind: Provider
metadata:
  name: ollama-llama
  labels:
    provider: ollama

spec:
  type: ollama
  model: llama3.2:1b
  base_url: http://localhost:11434

  additional_config:
    keep_alive: "5m"  # Keep model loaded for 5 minutes

  defaults:
    temperature: 0.7
    max_tokens: 2048
```

No API key required - Ollama runs locally. Start Ollama with:

```bash
# Install Ollama
brew install ollama  # macOS
# or visit https://ollama.ai for other platforms

# Start Ollama server
ollama serve

# Pull a model
ollama pull llama3.2:1b
```

Or use Docker:

```bash
docker run -d -p 11434:11434 -v ollama:/root/.ollama ollama/ollama
docker exec -it <container> ollama pull llama3.2:1b
```

**Available Models**: Any model from `ollama list` - `llama3.2:1b`, `llama3.2:3b`, `mistral`, `llava`, `deepseek-r1:8b`, etc.

## Arena Configuration

Reference providers in your `arena.yaml`:

```yaml
apiVersion: promptkit.altairalabs.ai/v1alpha1
kind: Arena
metadata:
  name: multi-provider-arena

spec:
  prompt_configs:
    - id: support
      file: prompts/support.yaml
  
  providers:
    - file: providers/openai.yaml
    - file: providers/claude.yaml
    - file: providers/gemini.yaml
  
  scenarios:
    - file: scenarios/customer-support.yaml
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
# providers/openai-gpt4.yaml
apiVersion: promptkit.altairalabs.ai/v1alpha1
kind: Provider
metadata:
  name: openai-gpt4
  labels:
    provider: openai
    tier: premium

spec:
  type: openai
  model: gpt-4o
  defaults:
    temperature: 0.6

---
# providers/openai-mini.yaml
apiVersion: promptkit.altairalabs.ai/v1alpha1
kind: Provider
metadata:
  name: openai-mini
  labels:
    provider: openai
    tier: cost-effective

spec:
  type: openai
  model: gpt-4o-mini
  defaults:
    temperature: 0.6
```

### Temperature Variations

```yaml
# providers/openai-creative.yaml
apiVersion: promptkit.altairalabs.ai/v1alpha1
kind: Provider
metadata:
  name: openai-creative
  labels:
    mode: creative

spec:
  type: openai
  model: gpt-4o
  defaults:
    temperature: 0.9  # More creative/random

---
# providers/openai-precise.yaml
apiVersion: promptkit.altairalabs.ai/v1alpha1
kind: Provider
metadata:
  name: openai-precise
  labels:
    mode: deterministic

spec:
  type: openai
  model: gpt-4o
  defaults:
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

Use labels to specify provider constraints:

```yaml
# scenarios/openai-only.yaml
apiVersion: promptkit.altairalabs.ai/v1alpha1
kind: Scenario
metadata:
  name: openai-specific-test
  labels:
    provider-specific: openai

spec:
  task_type: support
  
  turns:
    - role: user
      content: "Test message"
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
- `examples/ollama-local/providers/` - Local Ollama setup with Docker
