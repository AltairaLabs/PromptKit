---
title: Configure LLM Providers
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

  capabilities:
    - text
    - streaming
    - vision
    - tools
    - json

  defaults:
    temperature: 0.6
    max_tokens: 2000
    top_p: 1.0
```

Authentication uses the `OPENAI_API_KEY` environment variable automatically.

## Provider Capabilities

The `capabilities` field declares what features a provider supports. Scenarios can then use `required_capabilities` to only run against providers with matching capabilities.

**Available Capabilities**:

| Capability | Description |
|------------|-------------|
| `text` | Basic text completion |
| `streaming` | Streaming responses |
| `vision` | Image understanding |
| `tools` | Function/tool calling |
| `json` | JSON mode output |
| `audio` | Audio input understanding |
| `video` | Video input understanding |
| `documents` | PDF/document upload support |
| `duplex` | Real-time bidirectional audio |

**Example - Vision-capable provider**:
```yaml
spec:
  type: openai
  model: gpt-4o
  capabilities:
    - text
    - streaming
    - vision
    - tools
    - json
```

**Example - Audio-only provider**:
```yaml
spec:
  type: openai
  model: gpt-4o-audio-preview
  capabilities:
    - audio
    - duplex
```

**Example - Local model with limited capabilities**:
```yaml
spec:
  type: ollama
  model: llama3.2:3b
  capabilities:
    - text
    - streaming
    - tools
    - json
  # Note: llama3.2:3b does NOT support vision (only 11B/90B models do)
```

When a scenario specifies `required_capabilities`, only providers with ALL listed capabilities will run that scenario. See [Write Scenarios](/arena/how-to/write-scenarios/) for details.

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

### vLLM (High-Performance)

```yaml
# providers/vllm.yaml
apiVersion: promptkit.altairalabs.ai/v1alpha1
kind: Provider
metadata:
  name: vllm-llama
  labels:
    provider: vllm

spec:
  type: vllm
  model: meta-llama/Llama-3.2-3B-Instruct
  base_url: http://localhost:8000

  additional_config:
    use_beam_search: false
    best_of: 1
    # Guided decoding for structured output
    # guided_json: '{"type": "object", "properties": {...}}'

  defaults:
    temperature: 0.7
    max_tokens: 2048
```

No API key required - vLLM runs as a self-hosted service. Start vLLM with Docker:

```bash
# GPU-accelerated (recommended)
docker run --rm --gpus all \
  -p 8000:8000 \
  vllm/vllm-openai:latest \
  --model meta-llama/Llama-3.2-3B-Instruct \
  --dtype half \
  --max-model-len 4096

# CPU-only (for testing, slow)
docker run --rm \
  -p 8000:8000 \
  vllm/vllm-openai:latest \
  --model meta-llama/Llama-3.2-1B-Instruct \
  --max-model-len 2048
```

Or use Docker Compose:

```yaml
services:
  vllm:
    image: vllm/vllm-openai:latest
    ports:
      - "8000:8000"
    volumes:
      - vllm_cache:/root/.cache/huggingface
    command:
      - --model
      - meta-llama/Llama-3.2-3B-Instruct
      - --dtype
      - half
      - --max-model-len
      - "4096"
    deploy:
      resources:
        reservations:
          devices:
            - driver: nvidia
              count: all
              capabilities: [gpu]

volumes:
  vllm_cache:
```

**Available Models**: Any HuggingFace model supported by vLLM - Llama 3.x, Mistral, Qwen, Phi, LLaVA for vision, etc. See [vLLM docs](https://docs.vllm.ai/en/latest/models/supported_models.html).

**Advanced Features**:

```yaml
# Guided JSON output
spec:
  additional_config:
    guided_json: |
      {
        "type": "object",
        "properties": {
          "sentiment": {"type": "string", "enum": ["positive", "negative", "neutral"]},
          "confidence": {"type": "number", "minimum": 0, "maximum": 1}
        },
        "required": ["sentiment", "confidence"]
      }

# Regex-constrained output
spec:
  additional_config:
    guided_regex: "^[0-9]{3}-[0-9]{3}-[0-9]{4}$"  # Phone number format

# Choice selection
spec:
  additional_config:
    guided_choice: ["yes", "no", "maybe"]
```

## OpenAI-Compatible Gateways

Many third-party services expose an OpenAI-compatible API: **OpenRouter**, **Groq**, **Together AI**, **Fireworks AI**, **LiteLLM**, and self-hosted proxies. You can point the built-in `openai` provider at any of them with `base_url` and (optionally) `headers`.

The `headers` field injects custom HTTP headers into every request this provider sends. It's a top-level field on the provider spec. Header values are plain strings — use the `credential` field for secrets. If a custom header collides with a built-in header the provider sets itself (e.g. `Authorization`, `Content-Type`), the request fails fast with an error.

### OpenRouter

OpenRouter routes requests across hundreds of models through a single OpenAI-compatible endpoint. Its documentation recommends setting `HTTP-Referer` and `X-Title` for app attribution and leaderboard ranking:

```yaml
# providers/openrouter.yaml
apiVersion: promptkit.altairalabs.ai/v1alpha1
kind: Provider
metadata:
  name: openrouter-claude
  labels:
    gateway: openrouter

spec:
  type: openai
  model: anthropic/claude-sonnet-4-20250514
  base_url: https://openrouter.ai/api/v1

  headers:
    HTTP-Referer: https://myapp.com
    X-Title: My App

  credential:
    credential_env: OPENROUTER_API_KEY

  defaults:
    temperature: 0.6
    max_tokens: 2000
```

Then export your key:

```bash
export OPENROUTER_API_KEY="sk-or-v1-..."
```

OpenRouter accepts any model identifier from its catalog: `openai/gpt-4o-mini`, `anthropic/claude-sonnet-4-20250514`, `meta-llama/llama-3.3-70b-instruct`, etc.

### Groq

Groq offers ultra-fast inference for open-source models:

```yaml
# providers/groq.yaml
apiVersion: promptkit.altairalabs.ai/v1alpha1
kind: Provider
metadata:
  name: groq-llama
spec:
  type: openai
  model: llama-3.3-70b-versatile
  base_url: https://api.groq.com/openai/v1
  credential:
    credential_env: GROQ_API_KEY
```

No custom headers needed — Groq's OpenAI-compatible endpoint works with `base_url` alone.

### Together AI

```yaml
# providers/together.yaml
apiVersion: promptkit.altairalabs.ai/v1alpha1
kind: Provider
metadata:
  name: together-llama
spec:
  type: openai
  model: meta-llama/Llama-3.3-70B-Instruct-Turbo
  base_url: https://api.together.xyz/v1
  credential:
    credential_env: TOGETHER_API_KEY
```

### Self-Hosted LiteLLM Proxy

If you run [LiteLLM](https://github.com/BerriAI/litellm) as an on-prem gateway, point PromptArena at it and use a custom auth header if your proxy is secured internally:

```yaml
# providers/litellm.yaml
apiVersion: promptkit.altairalabs.ai/v1alpha1
kind: Provider
metadata:
  name: litellm-internal
spec:
  type: openai
  model: gpt-4o-mini      # whatever LiteLLM routes this name to
  base_url: http://litellm.internal:4000
  headers:
    X-Internal-Auth: shared-gateway-token
```

### How It Works

Any provider can declare `headers`, not just OpenAI-compatible gateways. The headers are applied after the provider sets its own built-in headers (auth, content type, etc.), so collision detection kicks in before any bytes leave the client. If you need to override a built-in header — don't. Use the `credential` field for auth instead.

## Streaming and Reliability

Provider configs support streaming retry, concurrency limits, and connection pool tuning. These are all optional and default to safe values.

### Streaming Retry

Recover from transient HTTP/2 stream resets without failing the test:

```yaml
spec:
  type: openai
  model: gpt-5-pro
  stream_retry:
    enabled: true
    max_attempts: 2           # initial + 1 retry
    retry_window: pre_first_chunk  # safe default
    budget:
      rate_per_sec: 5
      burst: 10
```

Set `retry_window: always` to also retry mid-stream failures (the response is discarded and re-requested from scratch, costing additional tokens):

```yaml
  stream_retry:
    enabled: true
    retry_window: always      # costs tokens on mid-stream retry
```

### Concurrency and Timeouts

```yaml
spec:
  type: openai
  model: gpt-4o
  request_timeout: "60s"          # non-streaming call timeout
  stream_idle_timeout: "30s"      # abort if stream goes silent
  stream_max_concurrent: 50       # max parallel streams (0 = unlimited)
```

### Connection Pool

Raise `max_conns_per_host` when running high-concurrency test suites against a single provider:

```yaml
spec:
  type: openai
  model: gpt-4o
  http_transport:
    max_conns_per_host: 250       # default: 100
    max_idle_conns_per_host: 250
    idle_conn_timeout: "90s"
```

The effective concurrent-stream ceiling is `max_conns_per_host` multiplied by the upstream's HTTP/2 max concurrent streams setting (typically 100-256). The `promptkit_http_conns_in_use` Prometheus gauge tracks pool pressure.

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

- **[Use Mock Providers](/arena/how-to/use-mock-providers/)** - Test without API calls
- **[Validate Outputs](/arena/how-to/validate-outputs/)** - Add assertions
- **[Integrate CI/CD](/arena/how-to/integrate-ci-cd/)** - Automate testing
- **[Config Reference](/arena/reference/config-schema/)** - Complete configuration options

## Examples

See working provider configurations in:

- `examples/customer-support/providers/`
- `examples/mcp-chatbot/providers/`
- `examples/ollama-local/providers/` - Local Ollama setup with Docker
