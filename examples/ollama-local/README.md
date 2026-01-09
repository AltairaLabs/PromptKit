# Ollama Local LLM Example

This example demonstrates how to use PromptArena with [Ollama](https://ollama.ai) for local LLM inference. No API keys required!

## Prerequisites

- Docker and Docker Compose installed
- PromptArena CLI (`arena`) installed

## Quick Start

### 1. Start Ollama with Docker Compose

```bash
cd examples/ollama-local
docker compose up -d
```

This will:
- Start the Ollama server on port 11434
- Automatically pull the `llama3.2:1b` model (small, ~1.3GB)

Wait for the model to download (check with `docker compose logs -f ollama-pull`).

### 2. Verify Ollama is Running

```bash
curl http://localhost:11434/api/tags
```

You should see the `llama3.2:1b` model listed.

### 3. Run the Arena Tests

```bash
arena run config.arena.yaml
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

### Using Different Models

To use a different model:

1. Pull the model:
   ```bash
   docker compose exec ollama ollama pull <model-name>
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
