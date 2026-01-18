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
var provider types.Provider
response, err := provider.Complete(ctx, messages, config)
```

## Provider Interface

```go
type Provider interface {
    Complete(ctx context.Context, messages []Message, config *ProviderConfig) (*ProviderResponse, error)
    CompleteStream(ctx context.Context, messages []Message, config *ProviderConfig) (StreamReader, error)
    GetProviderName() string
    Close() error
}
```

## Using Providers

### Basic Usage

```go
// Create provider
provider, err := openai.NewOpenAIProvider(apiKey, "gpt-4o-mini")
if err != nil {
    log.Fatal(err)
}
defer provider.Close()

// Send request
messages := []types.Message{
    {Role: "user", Content: "Hello"},
}
response, err := provider.Complete(ctx, messages, nil)
if err != nil {
    log.Fatal(err)
}

fmt.Println(response.Content)
```

### With Configuration

```go
config := &types.ProviderConfig{
    MaxTokens:   500,
    Temperature: 0.7,
    TopP:        0.9,
}

response, err := provider.Complete(ctx, messages, config)
```

### Streaming

```go
stream, err := provider.CompleteStream(ctx, messages, config)
if err != nil {
    log.Fatal(err)
}
defer stream.Close()

for {
    chunk, err := stream.Recv()
    if err != nil {
        break
    }
    fmt.Print(chunk.Content)
}
```

## Provider Configuration

### Common Parameters

```go
type ProviderConfig struct {
    MaxTokens     int      // Output limit (default: 4096)
    Temperature   float64  // Randomness 0-2 (default: 1.0)
    TopP          float64  // Nucleus sampling 0-1 (default: 1.0)
    Seed          *int     // Reproducibility (optional)
    StopSequences []string // Stop generation (optional)
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
config := &types.ProviderConfig{Temperature: 0.2}

// Creative tasks
config := &types.ProviderConfig{Temperature: 1.2}
```

### Max Tokens

Limits output length:

```go
// Short responses
config := &types.ProviderConfig{MaxTokens: 100}

// Long responses
config := &types.ProviderConfig{MaxTokens: 4096}
```

## Multi-Provider Strategies

### Fallback

Try providers in order:

```go
providers := []types.Provider{primary, secondary, tertiary}

var response *types.ProviderResponse
var err error

for _, provider := range providers {
    response, err = provider.Complete(ctx, messages, config)
    if err == nil {
        break  // Success
    }
    log.Printf("Provider %s failed: %v", provider.GetProviderName(), err)
}
```

### Load Balancing

Distribute across providers:

```go
providers := []types.Provider{openai1, openai2, claude}
current := 0

func GetNextProvider() types.Provider {
    provider := providers[current % len(providers)]
    current++
    return provider
}
```

### Cost-Based Routing

Route by cost:

```go
func SelectProvider(complexity string) types.Provider {
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
func SelectByQuality(task string) types.Provider {
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
provider, _ := openai.NewOpenAIProvider(...)
defer provider.Close()  // Essential
```

✅ **Reuse providers**
```go
// Good: One provider, many requests
provider := createProvider()
for _, prompt := range prompts {
    provider.Complete(ctx, prompt, config)
}

// Bad: New provider per request
for _, prompt := range prompts {
    provider := createProvider()
    provider.Complete(ctx, prompt, config)
    provider.Close()
}
```

### Error Handling

✅ **Handle provider errors**
```go
response, err := provider.Complete(ctx, messages, config)
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

response, err := provider.Complete(ctx, messages, config)
```

## Testing with Providers

### Mock Provider

```go
import "github.com/AltairaLabs/PromptKit/runtime/providers/mock"

func TestWithMock(t *testing.T) {
    mockProvider := mock.NewMockProvider()
    mockProvider.SetResponse("Test response")
    
    response, err := mockProvider.Complete(ctx, messages, nil)
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

func TrackRequest(provider string, response *ProviderResponse, err error) {
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

```go
costTracker := middleware.NewCostTracker()

// Use in pipeline
pipe := pipeline.NewPipeline(
    middleware.ProviderMiddleware(provider, nil, costTracker, config),
)

// Check costs
fmt.Printf("Total cost: $%.4f\n", costTracker.TotalCost())
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
- [Multi-Provider Fallback](../runtime/how-to/fallback-providers) - Implementation guide
- [Cost Monitoring](../runtime/how-to/monitor-costs) - Track expenses
