# Ollama Local LLM Example

This example demonstrates how to use PromptArena with [Ollama](https://ollama.ai) for local LLM inference. No API keys required!

## Prerequisites

- Docker and Docker Compose installed (for local) OR Ollama running on Kubernetes
- PromptArena CLI (`arena`) installed

## Quick Start

### Option A: Local Docker Setup

#### 1. Start Ollama with Docker Compose

```bash
cd examples/ollama-local
docker compose up -d
```

This will:
- Start the Ollama server on port 11434
- Automatically pull the `llama3.2:1b` model (small, ~1.3GB)

Wait for the model to download (check with `docker compose logs -f ollama-pull`).

#### 2. Verify Ollama is Running

```bash
curl http://localhost:11434/api/tags
```

You should see the `llama3.2:1b` model listed.

#### 3. Run the Arena Tests

```bash
arena run config.arena.yaml
```

### Option B: Kubernetes Setup

If you have Ollama running on Kubernetes:

#### 1. Set the Ollama endpoint

```bash
export OLLAMA_BASE_URL=http://ollama.default.svc.cluster.local:11434
# Or use port-forward for local testing:
# kubectl port-forward svc/ollama 11434:11434
# export OLLAMA_BASE_URL=http://localhost:11434
```

#### 2. Run the Kubernetes config

```bash
arena run config.arena.k8s.yaml
```

## Configuration

### Provider Configuration

The Ollama provider is configured in `providers/ollama-llama.provider.yaml`:

```yaml
spec:
  type: ollama
  model: llama3.2:1b
  base_url: "http://localhost:11434"
  additional_config:
    keep_alive: "5m"  # Keep model loaded for 5 minutes
```

For Kubernetes, use `providers/ollama-k8s.provider.yaml` which supports environment variable substitution:

```yaml
spec:
  type: ollama
  model: llama3.2:1b
  base_url: "${OLLAMA_BASE_URL:-http://ollama.default.svc.cluster.local:11434}"
```

### Using Different Models

To use a different model:

1. Pull the model:
   ```bash
   # Local Docker
   docker compose exec ollama ollama pull <model-name>

   # Kubernetes
   kubectl exec -it deployment/ollama -- ollama pull <model-name>
   ```

2. Update the provider config:
   ```yaml
   model: <model-name>
   ```

Popular models:
- `llama3.2:1b` - Smallest, fastest (~1.3GB)
- `llama3.2:3b` - Good balance (~2GB)
- `llama3.1:8b` - Better quality (~4.7GB)
- `mistral:7b` - Strong performance (~4.1GB)
- `codellama:7b` - Optimized for code (~3.8GB)
- `llava:7b` - Vision + language (~4.5GB)

### GPU Acceleration

For NVIDIA GPU support, uncomment the GPU section in `docker-compose.yaml`:

```yaml
deploy:
  resources:
    reservations:
      devices:
        - driver: nvidia
          count: all
          capabilities: [gpu]
```

## Scenarios

### basic-chat
Simple conversation testing basic Q&A capabilities.

### code-generation
Tests code generation in Python and Go.

### streaming-verification
Validates that streaming responses work correctly:
- Tests long-form content generation with streaming
- Verifies chunk boundaries with multi-paragraph responses
- Tests code block streaming
- Validates short responses after long ones

### multimodal-vision (requires vision model)
Tests image analysis capabilities with vision models like LLaVA:
- Basic image description
- Context retention across turns
- Multi-image comparison
- Detailed structured analysis

To enable multimodal testing:

1. Pull a vision model:
   ```bash
   # Local
   docker compose exec ollama ollama pull llava:7b

   # Kubernetes
   kubectl exec -it deployment/ollama -- ollama pull llava:7b
   ```

2. Uncomment the vision provider and scenario in `config.arena.yaml`:
   ```yaml
   providers:
     - file: providers/ollama-llama.provider.yaml
     - file: providers/ollama-vision-local.provider.yaml  # Uncomment

   scenarios:
     - file: scenarios/basic-chat.scenario.yaml
     - file: scenarios/code-generation.scenario.yaml
     - file: scenarios/streaming-verification.scenario.yaml
     - file: scenarios/multimodal-vision.scenario.yaml    # Uncomment
   ```

3. Run with the vision provider:
   ```bash
   arena run config.arena.yaml --provider ollama-vision-local
   ```

## Running Specific Scenarios

Run individual scenarios:

```bash
# Just streaming tests
arena run config.arena.yaml --scenario streaming-verification

# Just multimodal tests (requires vision model)
arena run config.arena.yaml --scenario multimodal-vision --provider ollama-vision-local
```

## Cleanup

```bash
docker compose down -v  # -v removes the volume with downloaded models
```

## Troubleshooting

### Model not found
```bash
docker compose exec ollama ollama pull llama3.2:1b
```

### Slow responses
The first request loads the model into memory. Subsequent requests are faster.
Use `keep_alive` to keep the model loaded between requests.

### Out of memory
Try a smaller model like `llama3.2:1b` or `phi3:mini`.

### Streaming not working
Ensure the `streaming: true` setting is in your config defaults or scenario.
Check Ollama logs for any SSE-related errors:
```bash
# Local
docker compose logs ollama

# Kubernetes
kubectl logs deployment/ollama
```

### Multimodal/Vision errors
- Ensure you're using a vision model (`llava:7b`, `bakllava:7b`, `llama3.2-vision:11b`)
- Standard text models like `llama3.2:1b` do not support image inputs
- Check that test images exist at the referenced paths

### Pipeline timeouts (30s limit)
The arena pipeline has a 30-second timeout per turn. For cold starts (first request after model swap):
- Run with `-j 1` (single concurrency) to keep models warm
- Warm up the model before tests: `curl http://localhost:11434/api/generate -d '{"model": "llava:7b", "prompt": "Hi", "stream": false}'`
- Keep prompts concise to reduce response time

## Known Limitations

### Token/Cost Metrics Not Available
Ollama's streaming API does not return token usage information in streaming mode. This means:
- **Token counts** will show as 0 in the HTML report
- **Cost estimates** will show as $0.0000
- **Per-turn latency** is tracked correctly (measured at the pipeline level)

This is a limitation of the Ollama API, not PromptArena. For token-based metrics, consider using providers that return usage information (OpenAI, Anthropic, etc.) or use Ollama's non-streaming API (not recommended for arena testing).
