# vLLM Provider for PromptKit

This provider enables PromptKit to use [vLLM](https://github.com/vllm-project/vllm) as a high-performance inference backend. vLLM is a fast and easy-to-use library for LLM inference and serving, providing OpenAI-compatible APIs.

## Features

### Core Functionality
- ✅ Text generation via `/v1/chat/completions` endpoint
- ✅ Streaming support using Server-Sent Events (SSE)
- ✅ Optional API key authentication
- ✅ Cost tracking (free by default, customizable pricing)
- ✅ OpenAI-compatible message format

### vLLM-Specific Parameters
- `use_beam_search` - Enable beam search for better quality (slower)
- `best_of` - Number of candidate completions to generate
- `ignore_eos` - Ignore end-of-sequence tokens
- `guided_json` - Constrain output to valid JSON schema
- `guided_regex` - Constrain output to match regex pattern
- `guided_choice` - Constrain output to one of provided choices
- `guided_grammar` - Constrain output to match grammar
- `guided_decoding_backend` - Backend for guided decoding
- `guided_whitespace_pattern` - Whitespace handling for guided decoding

### Multimodal Support
- ✅ Image inputs (JPEG, PNG, GIF, WebP)
- ✅ Maximum 20MB per image
- ✅ Both URL and base64 data URL formats
- ✅ Detail level control ("low", "high", "auto")

## Usage

### Basic Configuration

```yaml
provider_id: vllm-local
provider_name: vllm
model: meta-llama/Llama-3.1-8B-Instruct
base_url: http://localhost:8000
defaults:
  temperature: 0.7
  top_p: 0.9
  max_tokens: 2048
```

### With API Key Authentication

```yaml
provider_id: vllm-remote
provider_name: vllm
model: meta-llama/Llama-3.1-70B-Instruct
base_url: https://my-vllm-server.com
api_key_env_var: VLLM_API_KEY
```

### With Custom Pricing

```yaml
provider_id: vllm-priced
provider_name: vllm
model: meta-llama/Llama-3.1-8B-Instruct
base_url: http://localhost:8000
defaults:
  pricing:
    input_cost_per_1k: 0.0001
    output_cost_per_1k: 0.0002
```

### Programmatic Usage

```go
import (
    "github.com/AltairaLabs/PromptKit/runtime/providers"
    _ "github.com/AltairaLabs/PromptKit/runtime/providers/all"
)

// Create provider
spec := providers.ProviderSpec{
    ID:       "vllm-local",
    Name:     "vllm",
    Model:    "meta-llama/Llama-3.1-8B-Instruct",
    BaseURL:  "http://localhost:8000",
    Defaults: providers.ProviderDefaults{
        Temperature: 0.7,
        TopP:        0.9,
        MaxTokens:   2048,
    },
}
provider, err := providers.CreateProviderFromSpec(spec)

// Simple prediction
req := providers.PredictionRequest{
    Messages: []types.Message{
        {Role: "user", Content: "What is vLLM?"},
    },
}
resp, err := provider.Predict(ctx, req)

// Streaming prediction
streamChan, err := provider.PredictStream(ctx, req)
for chunk := range streamChan {
    fmt.Print(chunk.Content)
}
```

### Using vLLM-Specific Parameters

```go
req := providers.PredictionRequest{
    Messages: []types.Message{
        {Role: "user", Content: "Generate a JSON object"},
    },
    ProviderParams: map[string]interface{}{
        "guided_json": map[string]interface{}{
            "type": "object",
            "properties": map[string]interface{}{
                "name": map[string]interface{}{"type": "string"},
                "age":  map[string]interface{}{"type": "number"},
            },
            "required": []string{"name", "age"},
        },
    },
}
```

### Multimodal Usage

```go
// Create message with image
msg := types.Message{Role: "user"}
msg.AddTextPart("What's in this image?")
msg.AddImagePartFromURL("https://example.com/image.jpg", nil)

req := providers.PredictionRequest{
    Messages: []types.Message{msg},
}

// Use multimodal prediction
multiProvider := provider.(providers.MultimodalSupport)
resp, err := multiProvider.PredictMultimodal(ctx, req)
```

## Running vLLM Server

### Using Docker

```bash
docker run --gpus all \
    -v ~/.cache/huggingface:/root/.cache/huggingface \
    -p 8000:8000 \
    --ipc=host \
    vllm/vllm-openai:latest \
    --model meta-llama/Llama-3.1-8B-Instruct
```

### Using Docker Compose

See [examples/vllm-local/docker-compose.yml](../../../examples/vllm-local/docker-compose.yml)

### Using Python

```bash
pip install vllm
vllm serve meta-llama/Llama-3.1-8B-Instruct
```

## Test Coverage

The vLLM provider has **86.8% test coverage**, exceeding the required 80% threshold.

### Test Suite (40 tests)

#### Core Functionality (8 tests)
- Provider creation with/without API key
- Cost calculation (free and custom pricing)
- Basic prediction
- Streaming support
- Close method

#### Error Handling (4 tests)
- Empty choices in response
- HTTP errors (500, etc.)
- Invalid JSON responses
- vLLM API error messages

#### vLLM Features (6 tests)
- System message handling
- vLLM-specific parameters (beam search, guided JSON, etc.)
- Seed parameter
- Raw output inclusion
- Streaming with SSE
- Context cancellation

#### Message Processing (4 tests)
- Message preparation with/without system
- Multiple message handling
- Default parameter application
- Content extraction

#### Multimodal Support (13 tests)
- Capabilities reporting
- Image prediction (URL and base64)
- Text-only multimodal requests
- Multimodal streaming
- Content building with text and images
- Detail level handling
- Unsupported media types
- URL conversion (URL, base64, file path)
- System message with multimodal

#### Utilities (5 tests)
- Model name retrieval
- Content string extraction
- Helper functions

## Architecture

The provider consists of four main files:

1. **vllm.go** (513 lines) - Core provider implementation
   - Message preparation and request building
   - HTTP client for vLLM API
   - Streaming response handling
   - Cost calculation

2. **vllm_multimodal.go** (168 lines) - Multimodal support
   - Image capabilities
   - Multimodal message conversion
   - Media URL conversion

3. **factory.go** (18 lines) - Provider registration
   - Factory function for provider creation
   - Automatic registration with PromptKit

4. **vllm_test.go** + **vllm_multimodal_test.go** (1000+ lines combined)
   - Comprehensive test suite
   - Mock HTTP servers for testing
   - Edge case coverage

## API Compatibility

vLLM uses OpenAI-compatible APIs but with some differences:

### Supported
- `/v1/chat/completions` endpoint
- Messages with `role` and `content`
- System messages
- Temperature, top_p, max_tokens
- Streaming via SSE
- Multimodal inputs (vision models)

### vLLM Extensions
- Guided decoding (JSON, regex, grammar)
- Beam search parameters
- EOS token control

### Not Supported (vs OpenAI)
- Function/tool calling (planned for phase 2)
- Response format enforcement (use guided_json instead)
- logprobs (vLLM has different format)

## Linting Status

The vLLM provider passes all critical linting checks. Some style warnings match the existing provider patterns:

- `gochecknoinits` - Standard Go pattern for package registration
- `gocognit` - Acceptable complexity for build/conversion functions
- `gocritic` - 80-byte params consistent with other providers
- `mnd` - Reasonable default values (20MB image limit, 1000 for cost calculation)

These are identical to warnings in the Ollama provider and don't represent actual code issues.

## Implementation Status

### Phase 1 (Complete) ✅
- [x] Core provider implementation
- [x] Streaming support
- [x] Multimodal image support
- [x] vLLM-specific parameters
- [x] Cost tracking
- [x] 86.8% test coverage
- [x] Integration with registry
- [x] Example project
- [x] Documentation

### Phase 2 (Planned)
- [ ] Tool/function calling support
- [ ] Tool support tests
- [ ] Advanced guided decoding examples

## Related Files

- Provider: [vllm.go](vllm.go), [vllm_multimodal.go](vllm_multimodal.go)
- Factory: [factory.go](factory.go)
- Tests: [vllm_test.go](vllm_test.go), [vllm_multimodal_test.go](vllm_multimodal_test.go)
- Example: [examples/vllm-local/](../../../examples/vllm-local/)
- Registry: [../registry.go](../registry.go), [../all/all.go](../all/all.go)

## References

- [vLLM Documentation](https://docs.vllm.ai/)
- [vLLM GitHub](https://github.com/vllm-project/vllm)
- [OpenAI API Compatibility](https://docs.vllm.ai/en/latest/serving/openai_compatible_server.html)
- [GitHub Issue #212](https://github.com/AltairaLabs/PromptKit/issues/212)
