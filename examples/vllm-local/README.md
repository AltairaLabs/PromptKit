# vLLM Local Inference Example

This example demonstrates how to use PromptArena with [vLLM](https://docs.vllm.ai/) for high-performance local LLM inference. vLLM is a fast and memory-efficient inference engine that provides OpenAI-compatible API endpoints.

## Prerequisites

- Docker and Docker Compose installed (for local setup) OR vLLM running on Kubernetes
- PromptArena CLI (`arena`) installed
- NVIDIA GPU with CUDA support (recommended for performance, CPU mode available but slower)

## Quick Start

### Option A: Local Docker Setup

#### 1. Start vLLM with Docker Compose

```bash
cd examples/vllm-local
docker compose up -d
```

This will:
- Start the vLLM server on port 8000
- Automatically download and serve the `facebook/opt-125m` model (small, ~250MB for testing)
- Expose an OpenAI-compatible API endpoint

**Note**: The first startup will take some time to download the model. You can monitor progress with:
```bash
docker compose logs -f vllm
```

#### 2. Verify vLLM is Running

```bash
curl http://localhost:8000/v1/models
```

You should see the model listed in the response.

#### 3. Run the Arena Tests

```bash
arena run config.arena.yaml
```

This will:
- Execute test scenarios against the local vLLM instance
- Compare responses from the model
- Generate test results in the `out/` directory

### Option B: Kubernetes Setup

If you have vLLM running on Kubernetes:

#### 1. Set the vLLM endpoint

```bash
export VLLM_BASE_URL=http://vllm.default.svc.cluster.local:8000
# Or use port-forward for local testing:
# kubectl port-forward svc/vllm 8000:8000
```

#### 2. Run with Kubernetes config

```bash
arena run config.arena.k8s.yaml
```

## Configuration Files

### Providers

- `providers/vllm-local.provider.yaml` - Basic vLLM provider for local Docker setup
- `providers/vllm-k8s.provider.yaml` - vLLM provider for Kubernetes deployment
- `providers/vllm-guided-decoding.provider.yaml` - Example with guided decoding for structured output

### Prompts

- `prompts/assistant.prompt.yaml` - General assistant prompt
- `prompts/code-helper.prompt.yaml` - Code generation assistant

### Scenarios

- `scenarios/basic-chat.scenario.yaml` - Simple conversation test
- `scenarios/code-generation.scenario.yaml` - Code generation scenarios
- `scenarios/guided-output.scenario.yaml` - Structured output with guided decoding

## Using Different Models

### Small Models (Good for Testing)

```yaml
# In providers/vllm-local.provider.yaml
spec:
  model: facebook/opt-125m  # ~250MB
  # or
  model: facebook/opt-1.3b  # ~2.5GB
```

### Production Models

```yaml
spec:
  model: meta-llama/Llama-2-7b-chat-hf     # ~13GB
  # or
  model: mistralai/Mistral-7B-Instruct-v0.1  # ~14GB
  # or
  model: meta-llama/Llama-2-13b-chat-hf    # ~26GB (requires more GPU memory)
```

**Note**: Larger models require more GPU memory. Ensure your GPU has sufficient VRAM:
- 7B models: ~14GB VRAM
- 13B models: ~26GB VRAM
- 70B models: Multiple GPUs with tensor parallelism

### Update the docker-compose.yaml

```yaml
services:
  vllm:
    command:
      - --model
      - meta-llama/Llama-2-7b-chat-hf  # Change this
      - --dtype
      - auto
```

## Advanced Features

### Guided Decoding for Structured Output

vLLM supports guided decoding to ensure outputs match specific formats (JSON Schema, regex, grammar, or choices):

```yaml
# providers/vllm-guided-decoding.provider.yaml
spec:
  additional_config:
    guided_json:
      type: object
      properties:
        sentiment:
          type: string
          enum: ["positive", "negative", "neutral"]
        confidence:
          type: number
          minimum: 0
          maximum: 1
      required: ["sentiment", "confidence"]
```

See `scenarios/guided-output.scenario.yaml` for a complete example.

### Beam Search

Enable beam search for potentially better quality outputs:

```yaml
spec:
  additional_config:
    use_beam_search: true
    best_of: 3
```

### Authentication (Optional)

If your vLLM instance requires authentication:

```yaml
spec:
  additional_config:
    api_key: "${VLLM_API_KEY}"
```

Then set the environment variable:
```bash
export VLLM_API_KEY="your-api-key"
```

## Performance Optimization

### GPU Configuration

For better performance with larger models, you can configure vLLM's GPU settings in `docker-compose.yaml`:

```yaml
services:
  vllm:
    command:
      - --model
      - meta-llama/Llama-2-7b-chat-hf
      - --tensor-parallel-size
      - "2"  # Use 2 GPUs
      - --gpu-memory-utilization
      - "0.9"  # Use 90% of GPU memory
    deploy:
      resources:
        reservations:
          devices:
            - driver: nvidia
              count: 2  # Number of GPUs
              capabilities: [gpu]
```

### Connection Pooling

For high-throughput scenarios, PromptKit automatically uses connection pooling. You can run parallel tests:

```bash
arena run config.arena.yaml --parallel 10
```

## Cost Tracking

vLLM is self-hosted, so costs are $0 by default. However, you can configure custom costs for internal accounting:

```yaml
spec:
  pricing:
    input_cost_per_1k: 0.001   # $0.001 per 1K input tokens
    output_cost_per_1k: 0.002  # $0.002 per 1K output tokens
```

## Troubleshooting

### vLLM Not Starting

Check GPU availability:
```bash
docker run --rm --gpus all nvidia/cuda:11.8.0-base-ubuntu22.04 nvidia-smi
```

### Model Download Issues

vLLM downloads models from HuggingFace. If you have authentication issues:

```bash
export HUGGING_FACE_HUB_TOKEN="your-token"
```

And update `docker-compose.yaml`:
```yaml
services:
  vllm:
    environment:
      - HUGGING_FACE_HUB_TOKEN=${HUGGING_FACE_HUB_TOKEN}
```

### Out of Memory

Reduce model size or GPU memory utilization:
```yaml
command:
  - --gpu-memory-utilization
  - "0.7"  # Reduce from default 0.9
```

### Connection Refused

Ensure vLLM is fully started:
```bash
docker compose logs vllm
# Wait for: "Application startup complete"
```

## Resources

- [vLLM Documentation](https://docs.vllm.ai/)
- [vLLM OpenAI-Compatible Server](https://docs.vllm.ai/en/latest/serving/openai_compatible_server.html)
- [Guided Decoding](https://docs.vllm.ai/en/latest/serving/guided_decoding.html)
- [Supported Models](https://docs.vllm.ai/en/latest/models/supported_models.html)

## Next Steps

- Try different models from HuggingFace
- Implement custom scenarios for your use cases
- Compare vLLM performance with cloud providers using Arena
- Set up vLLM on Kubernetes for production workloads
