---
title: Providers
sidebar:
  order: 3
---
Understanding LLM providers in PromptKit.

## What is a Provider?

A **provider** is an LLM service (OpenAI, Anthropic, Google) that generates text responses. PromptKit abstracts providers behind a common interface.

## Supported Providers

### OpenAI
- **Models**: GPT-4o, GPT-4o-mini, GPT-4-turbo, GPT-3.5-turbo
- **Features**: Function calling, JSON mode, vision, streaming
- **Pricing**: Pay per token, varies by model

### Anthropic (Claude)
- **Models**: Claude 3.5 Sonnet, Claude 3.5 Haiku, Claude 3 Opus
- **Features**: Function calling, vision, 200K context, streaming
- **Pricing**: Pay per token, varies by model

### Google (Gemini)
- **Models**: Gemini 1.5 Pro, Gemini 1.5 Flash
- **Features**: Function calling, multimodal, 1M+ context, streaming
- **Pricing**: Pay per token, free tier available

### Ollama (Local)
- **Models**: Llama 3.2, Mistral, LLaVA, DeepSeek, Phi, and more
- **Features**: Function calling, vision (LLaVA), streaming, OpenAI-compatible API
- **Pricing**: Free (local inference, no API costs)

### vLLM (High-Performance Inference)
- **Models**: Any HuggingFace model, Llama 3.x, Mistral, Qwen, Phi, LLaVA, and more
- **Features**: Function calling, vision, streaming, guided decoding, beam search, GPU-accelerated high-throughput
- **Pricing**: Free (self-hosted, no API costs)

## Platform Support

PromptKit supports running models on cloud hyperscaler platforms in addition to direct API access:

### AWS Bedrock
- **Authentication**: Uses AWS SDK credential chain (IRSA, IAM roles, env vars)
- **Models**: Claude models via Anthropic partnership
- **Benefits**: Enterprise security, VPC integration, no API key management

### Google Cloud Vertex AI
- **Authentication**: Uses GCP Application Default Credentials (Workload Identity, service accounts)
- **Models**: Claude and Gemini models
- **Benefits**: GCP integration, enterprise compliance, unified billing

### Azure AI Foundry
- **Authentication**: Uses Azure AD tokens (Managed Identity, service principals)
- **Models**: OpenAI models via Azure partnership
- **Benefits**: Azure integration, enterprise security, compliance

## Why Provider Abstraction?

**Problem**: Each provider has different APIs

```go
// OpenAI specific
openai.CreateChatCompletion(...)

// Claude specific
anthropic.Messages(...)

// Gemini specific
genai.GenerateContent(...)
```

**Solution**: Common interface

```go
// Works with any provider
var provider providers.Provider
response, err := provider.Predict(ctx, req)
```

## Provider Interface

```go
type Provider interface {
    ID() string
    Model() string
    Predict(ctx context.Context, req PredictionRequest) (PredictionResponse, error)
    PredictStream(ctx context.Context, req PredictionRequest) (<-chan StreamChunk, error)
    SupportsStreaming() bool
    ShouldIncludeRawOutput() bool
    Close() error
    CalculateCost(inputTokens, outputTokens, cachedTokens int) types.CostInfo
}
```

## Using Providers

### Basic Usage

```go
// Create provider
provider := openai.NewProvider("my-openai", "gpt-4o-mini", "", providers.ProviderDefaults{}, false)
defer provider.Close()

// Send request
req := providers.PredictionRequest{
    Messages: []types.Message{
        {Role: "user", Content: "Hello"},
    },
}
response, err := provider.Predict(ctx, req)
if err != nil {
    log.Fatal(err)
}

fmt.Println(response.Content)
```

### With Configuration

```go
req := providers.PredictionRequest{
    Messages:    messages,
    MaxTokens:   500,
    Temperature: 0.7,
    TopP:        0.9,
}

response, err := provider.Predict(ctx, req)
```

### Streaming

```go
stream, err := provider.PredictStream(ctx, req)
if err != nil {
    log.Fatal(err)
}

for chunk := range stream {
    if chunk.Error != nil {
        break
    }
    fmt.Print(chunk.Delta)
}
```

## Credential Configuration

PromptKit supports flexible credential configuration with a resolution chain:

### Resolution Order

Credentials are resolved in the following priority order:

1. **`api_key`**: Explicit API key in configuration
2. **`credential_file`**: Read API key from a file path
3. **`credential_env`**: Read API key from specified environment variable
4. **Default env vars**: Fall back to provider-specific defaults (OPENAI_API_KEY, ANTHROPIC_API_KEY, etc.)

### Configuration Examples

```yaml
# Explicit API key (not recommended for production)
credential:
  api_key: "sk-..."

# Read from file (good for secrets management)
credential:
  credential_file: /run/secrets/openai-key

# Read from custom env var (useful for multiple providers)
credential:
  credential_env: OPENAI_PROD_API_KEY
```

### Per-Provider Credentials

Configure different credentials for the same provider type:

```yaml
providers:
  - id: openai-prod
    type: openai
    model: gpt-4o
    credential:
      credential_env: OPENAI_PROD_KEY

  - id: openai-dev
    type: openai
    model: gpt-4o-mini
    credential:
      credential_env: OPENAI_DEV_KEY
```

### Platform Credentials

For cloud platforms, credentials are handled automatically via SDK credential chains:

```yaml
# AWS Bedrock - uses IRSA, IAM roles, or AWS_* env vars
- id: claude-bedrock
  type: claude
  model: claude-3-5-sonnet-20241022
  platform:
    type: bedrock
    region: us-west-2

# GCP Vertex AI - uses Workload Identity or GOOGLE_APPLICATION_CREDENTIALS
- id: claude-vertex
  type: claude
  model: claude-3-5-sonnet-20241022
  platform:
    type: vertex
    region: us-central1
    project: my-gcp-project

# Azure AI - uses Managed Identity or AZURE_* env vars
- id: gpt4-azure
  type: openai
  model: gpt-4o
  platform:
    type: azure
    endpoint: https://my-resource.openai.azure.com
```

## Provider Configuration

### Common Parameters

```go
type PredictionRequest struct {
    System         string                 // System prompt
    Messages       []types.Message        // Conversation history
    Temperature    float32                // Randomness 0-2 (default: 1.0)
    TopP           float32                // Nucleus sampling 0-1 (default: 1.0)
    MaxTokens      int                    // Output limit (default: 4096)
    Seed           *int                   // Reproducibility (optional)
    ResponseFormat *ResponseFormat        // Optional response format (JSON mode)
    Metadata       map[string]any         // Provider-specific extras
}
```

### Temperature

Controls randomness:

- **0.0**: Deterministic, same output
- **0.5**: Balanced
- **1.0**: Creative (default)
- **2.0**: Very random

```go
// Factual tasks
config := &providers.PredictionRequest{Temperature: 0.2}

// Creative tasks
config := &providers.PredictionRequest{Temperature: 1.2}
```

### Max Tokens

Limits output length:

```go
// Short responses
config := &providers.PredictionRequest{MaxTokens: 100}

// Long responses
config := &providers.PredictionRequest{MaxTokens: 4096}
```

## Multi-Provider Strategies

### Fallback

Try providers in order:

```go
providerList := []providers.Provider{primary, secondary, tertiary}

var response providers.PredictionResponse
var err error

for _, provider := range providerList {
    response, err = provider.Predict(ctx, req)
    if err == nil {
        break  // Success
    }
    log.Printf("Provider %s failed: %v", provider.ID(), err)
}
```

### Load Balancing

Distribute across providers:

```go
providers := []providers.Provider{openai1, openai2, claude}
current := 0

func GetNextProvider() providers.Provider {
    provider := providers[current % len(providers)]
    current++
    return provider
}
```

### Cost-Based Routing

Route by cost:

```go
func SelectProvider(complexity string) providers.Provider {
    switch complexity {
    case "simple":
        return gpt4oMini  // Cheapest
    case "complex":
        return gpt4o      // Best quality
    case "long_context":
        return gemini     // Largest context
    default:
        return gpt4oMini
    }
}
```

### Quality-Based Routing

Route by quality needs:

```go
func SelectByQuality(task string) providers.Provider {
    if task == "code_generation" {
        return gpt4o  // Best for code
    } else if task == "long_document" {
        return claude  // Best for long docs
    } else {
        return gpt4oMini  // Good enough
    }
}
```

## Provider Comparison

### Performance

| Provider | Latency | Throughput | Context |
|----------|---------|------------|---------|
| GPT-4o-mini | ~500ms | High | 128K |
| GPT-4o | ~1s | Medium | 128K |
| Claude Sonnet | ~1s | Medium | 200K |
| Claude Haiku | ~400ms | High | 200K |
| Gemini Flash | ~600ms | High | 1M+ |
| Gemini Pro | ~1.5s | Medium | 1M+ |

### Cost

| Provider | Input (per 1M tokens) | Output (per 1M tokens) |
|----------|----------------------|------------------------|
| GPT-4o-mini | $0.15 | $0.60 |
| GPT-4o | $2.50 | $10.00 |
| Claude Haiku | $0.25 | $1.25 |
| Claude Sonnet | $3.00 | $15.00 |
| Gemini Flash | $0.075 | $0.30 |
| Gemini Pro | $1.25 | $5.00 |

### Use Cases

**GPT-4o-mini**: General purpose, cost-effective  
**GPT-4o**: Complex reasoning, code generation  
**Claude Haiku**: Fast responses, high volume  
**Claude Sonnet**: Long documents, analysis  
**Gemini Flash**: Multimodal, cost-effective  
**Gemini Pro**: Very long context, research  

## Provider Selection Guide

### Choose GPT-4o-mini when:
- Cost is primary concern
- Tasks are straightforward
- High volume needed
- Quick responses required

### Choose GPT-4o when:
- Quality is critical
- Complex reasoning needed
- Code generation
- Mathematical tasks

### Choose Claude Sonnet when:
- Long document analysis
- Detailed writing
- Research tasks
- Need 200K context

### Choose Claude Haiku when:
- Speed critical
- Simple tasks
- High throughput
- Cost-effective

### Choose Gemini Flash when:
- Multimodal input
- Cost-effective
- Good balance
- Video processing

### Choose Gemini Pro when:
- Very long context (1M+ tokens)
- Research papers
- Large codebases
- Book analysis

## Best Practices

### Resource Management

✅ **Close providers**
```go
provider := openai.NewProvider("my-openai", "gpt-4o-mini", "", providers.ProviderDefaults{}, false)
defer provider.Close()  // Essential
```

✅ **Reuse providers**
```go
// Good: One provider, many requests
provider := createProvider()
for _, req := range requests {
    provider.Predict(ctx, req)
}

// Bad: New provider per request
for _, req := range requests {
    provider := createProvider()
    provider.Predict(ctx, req)
    provider.Close()
}
```

### Error Handling

✅ **Handle provider errors**
```go
response, err := provider.Predict(ctx, req)
if err != nil {
    if errors.Is(err, ErrRateLimited) {
        // Wait and retry
    } else if errors.Is(err, ErrInvalidKey) {
        // Check credentials
    } else {
        // Fallback provider
    }
}
```

### Timeouts

✅ **Set context timeouts**
```go
ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
defer cancel()

response, err := provider.Predict(ctx, req)
```

## Testing with Providers

### Mock Provider

```go
import "github.com/AltairaLabs/PromptKit/runtime/providers/mock"

func TestWithMock(t *testing.T) {
    mockProvider := mock.NewMockProvider()
    mockProvider.SetResponse("Test response")
    
    response, err := mockProvider.Predict(ctx, req)
    assert.NoError(t, err)
    assert.Equal(t, "Test response", response.Content)
}
```

### Benefits

- No API calls
- No costs
- Fast tests
- Predictable responses
- Offline testing

## Monitoring Providers

### Track Usage

```go
type ProviderMetrics struct {
    RequestCount   int
    ErrorCount     int
    TotalCost      float64
    TotalTokens    int
    AvgLatency     time.Duration
}

func TrackRequest(provider string, response *providers.PredictionResponse, err error) {
    metrics := GetMetrics(provider)
    metrics.RequestCount++
    
    if err != nil {
        metrics.ErrorCount++
    } else {
        metrics.TotalCost += response.Cost
        metrics.TotalTokens += response.Usage.TotalTokens
    }
}
```

### Monitor Costs

Track costs using the event bus and provider cost calculation:

```go
bus := events.NewEventBus()
bus.Subscribe(events.EventProviderCallCompleted, func(e *events.Event) {
    data := e.Data.(events.ProviderCallCompletedData)
    fmt.Printf("Call cost: $%.4f\n", data.Cost)
})
```

## Summary

Providers are:

✅ **Abstracted** - Common interface for all LLMs  
✅ **Flexible** - Easy to switch or combine  
✅ **Configurable** - Fine-tune behavior  
✅ **Testable** - Mock for unit tests  
✅ **Monitorable** - Track usage and costs  

## Related Documentation

- [Provider System Explanation](../runtime/explanation/provider-system) - Architecture details
- [Provider Reference](../runtime/reference/providers) - API documentation
- [Cloud Provider Examples](../arena/examples/cloud-providers) - Bedrock, Vertex, Azure examples
- [Multi-Provider Fallback](../runtime/how-to/fallback-providers) - Implementation guide
- [Cost Monitoring](../runtime/how-to/monitor-costs) - Track expenses
