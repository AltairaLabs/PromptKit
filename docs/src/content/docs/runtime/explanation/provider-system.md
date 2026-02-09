---
title: Provider System
sidebar:
  order: 2
---
Understanding how Runtime abstracts LLM providers.

## Overview

Runtime uses a **provider abstraction** to work with multiple LLM services (OpenAI, Anthropic Claude, Google Gemini) through a unified interface.

## Core Concept

All providers implement the same interface:

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

This allows code like:

```go
// Same code works with any provider
resp, err := provider.Predict(ctx, req)
```

## Why Provider Abstraction?

### Problem: Vendor Lock-In

Without abstraction:

```go
// Tied to OpenAI
response := openai.ChatCompletion(...)

// Want to switch to Claude? Rewrite everything
response := anthropic.Messages(...)

// Different APIs, different parameters, different response formats
```

### Solution: Common Interface

With abstraction:

```go
// Works with any provider
var provider providers.Provider

// OpenAI
provider = openai.NewProvider(...)

// Or Claude
provider = claude.NewProvider(...)

// Or Gemini
provider = gemini.NewProvider(...)

// Same code!
resp, err := provider.Predict(ctx, req)
```

## Benefits

**1. Provider Independence**
- Switch providers without code changes
- Test with different models easily
- Compare provider performance

**2. Fallback Strategies**
- Try multiple providers automatically
- Graceful degradation
- Increased reliability

**3. Cost Optimization**
- Route to cheapest provider
- Use expensive models only when needed
- Track costs across providers

**4. Testing**
- Mock providers for unit tests
- Predictable test behavior
- No API calls in tests

## Provider Interface

### Predict Method

Synchronous prediction:

```go
Predict(ctx context.Context, req PredictionRequest) (PredictionResponse, error)
```

**Parameters**:
- `ctx`: Timeout and cancellation
- `req`: Prediction request with messages, temperature, max tokens, etc.

**Returns**:
- `PredictionResponse`: LLM's response with content, cost info, and tool calls
- `error`: Any errors

### PredictStream Method

Streaming prediction:

```go
PredictStream(ctx context.Context, req PredictionRequest) (<-chan StreamChunk, error)
```

Returns a channel of stream chunks for real-time output.

### Lifecycle Methods

```go
ID() string      // Returns provider identifier
Model() string   // Returns model name
Close() error    // Cleanup resources
```

## Provider Configuration

Unified config works across all providers:

```go
type ProviderConfig struct {
    MaxTokens     int      // Output limit
    Temperature   float64  // Randomness (0.0-2.0)
    TopP          float64  // Nucleus sampling
    Seed          *int     // Reproducibility
    StopSequences []string // Stop generation
}
```

### Provider Defaults

All providers accept a `ProviderDefaults` struct:

```go
defaults := providers.ProviderDefaults{
    Temperature: 0.7,
    TopP:        0.95,
    MaxTokens:   2000,
    Pricing: providers.Pricing{
        InputCostPer1K:  0.00015,
        OutputCostPer1K: 0.0006,
    },
}
```

## Implementation Details

### OpenAI Provider

**Models**: GPT-4o, GPT-4o-mini, GPT-4-turbo, GPT-3.5-turbo

**Features**:
- Function calling
- JSON mode
- Vision (image inputs)
- Streaming
- Reproducible outputs (seed)

**Pricing**: Per-token, varies by model

**API**: REST over HTTPS

### Claude Provider (Anthropic)

**Models**: Claude 3.5 Sonnet, Claude 3.5 Haiku, Claude 3 Opus

**Features**:
- Function calling
- Vision (image inputs)
- Long context (200K tokens)
- Streaming
- System prompts

**Pricing**: Per-token, varies by model

**API**: REST over HTTPS

### Gemini Provider (Google)

**Models**: Gemini 1.5 Pro, Gemini 1.5 Flash

**Features**:
- Function calling
- Vision and video inputs
- Long context (1M+ tokens)
- Streaming
- Multimodal understanding

**Pricing**: Per-token, free tier available

**API**: REST over HTTPS

## Message Format

Runtime uses a common message format:

```go
type Message struct {
    Role       string
    Content    string
    ToolCalls  []MessageToolCall
    ToolCallID string
}
```

**Roles**:
- `system`: System instructions
- `user`: User messages
- `assistant`: AI responses
- `tool`: Tool execution results

### Translation Layer

Each provider translates to its native format:

**Runtime → OpenAI**:
```go
{Role: "user", Content: "Hello"}
→
{role: "user", content: "Hello"}
```

**Runtime → Claude**:
```go
{Role: "user", Content: "Hello"}
→
{role: "user", content: "Hello"}
```

**Runtime → Gemini**:
```go
{Role: "user", Content: "Hello"}
→
{role: "user", parts: [{text: "Hello"}]}
```

This translation is invisible to users.

## Tool Support

All providers support function calling:

### Tool Definition

```go
type ToolDef struct {
    Name        string
    Description string
    Parameters  json.RawMessage  // JSON schema
}
```

### Provider-Specific Formats

**OpenAI**:
```json
{
  "type": "function",
  "function": {
    "name": "get_weather",
    "description": "Get current weather",
    "parameters": { ... }
  }
}
```

**Claude**:
```json
{
  "name": "get_weather",
  "description": "Get current weather",
  "input_schema": { ... }
}
```

**Gemini**:
```json
{
  "name": "get_weather",
  "description": "Get current weather",
  "parameters": { ... }
}
```

Runtime handles conversion automatically.

## Design Decisions

### Why Common Interface?

**Decision**: All providers implement the same interface

**Rationale**:
- Enables provider-agnostic code
- Simplifies switching and testing
- Reduces coupling to vendor APIs

**Trade-off**: Can't expose provider-specific features directly. Instead, features are added to the interface when widely supported.

### Why Separate Providers?

**Decision**: One provider instance per LLM service

**Rationale**:
- Clear resource ownership
- Independent configuration
- Separate connection pools
- Explicit lifecycle management

**Alternative considered**: Multi-provider registry was considered but rejected as too complex.

### Why Not Adapter Pattern?

**Decision**: Providers translate directly, no adapter layer

**Rationale**:
- Simpler implementation
- Fewer layers
- Better performance
- Easier debugging

**Trade-off**: Translation code lives in each provider. This is acceptable as translation is straightforward.

## Multi-Provider Patterns

### Fallback Strategy

Try providers in order:

```go
providers := []providers.Provider{primary, secondary, tertiary}

for _, provider := range providerList {
    resp, err := provider.Predict(ctx, req)
    if err == nil {
        return resp, nil
    }
    log.Printf("Provider %s failed: %v", provider.ID(), err)
}

return nil, errors.New("all providers failed")
```

### Load Balancing

Distribute across providers:

```go
type LoadBalancer struct {
    providers []providers.Provider
    current   int
}

func (lb *LoadBalancer) Execute(ctx context.Context, req providers.PredictionRequest) (providers.PredictionResponse, error) {
    provider := lb.providers[lb.current % len(lb.providers)]
    lb.current++
    return provider.Predict(ctx, req)
}
```

### Cost-Based Routing

Route to cheapest provider:

```go
func selectProvider(taskComplexity string) providers.Provider {
    switch taskComplexity {
    case "simple":
        return openaiMini  // Cheapest
    case "complex":
        return claude      // Best quality
    case "long_context":
        return gemini      // Largest context
    default:
        return openaiMini
    }
}
```

## Testing with Mock Provider

Runtime includes a mock provider:

```go
mockProvider := mock.NewProvider("mock", "mock-model", false)

resp, _ := mockProvider.Predict(ctx, providers.PredictionRequest{
    Messages: []types.Message{{Role: "user", Content: "Hi"}},
})
fmt.Println(resp.Content)
```

**Benefits**:
- No API calls
- Predictable responses
- Fast tests
- No cost

## Performance Considerations

### Connection Reuse

Providers maintain HTTP connection pools:

```go
// Good: Reuse provider
provider := openai.NewProvider(...)
defer provider.Close()

for _, prompt := range prompts {
    provider.Predict(ctx, req)  // Reuses connections
}
```

### Resource Cleanup

Always close providers:

```go
provider := openai.NewProvider(...)
defer provider.Close()  // Essential!
```

Without closing:
- Connection leaks
- Goroutine leaks
- Memory growth

### Timeout Management

Use contexts for timeouts:

```go
ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
defer cancel()

resp, err := provider.Predict(ctx, req)
```

## Future Extensibility

The provider interface can be extended to support:

**New Features**:
- Audio generation
- Image generation
- Embeddings
- Fine-tuned models

**New Providers**:
- Mistral
- Cohere
- Local models (Ollama)
- Custom endpoints

## Summary

Provider abstraction provides:

✅ **Vendor Independence**: Switch providers easily  
✅ **Unified API**: Same code for all providers  
✅ **Fallback Support**: Try multiple providers  
✅ **Testing**: Mock providers for tests  
✅ **Extensibility**: Add providers without breaking changes  

## Related Topics

- [Pipeline Architecture](pipeline-architecture) - How providers fit in pipelines
- [Tool Integration](tool-integration) - Function calling across providers
- [Providers Reference](../reference/providers) - Complete API

## Further Reading

- Strategy pattern (Gang of Four)
- Adapter pattern for API translation
- Repository pattern for provider management
