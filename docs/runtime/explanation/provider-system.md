---
layout: default
title: Provider System
parent: Runtime Explanation
grand_parent: Runtime
nav_order: 2
---

# Provider System

Understanding how Runtime abstracts LLM providers.

## Overview

Runtime uses a **provider abstraction** to work with multiple LLM services (OpenAI, Anthropic Claude, Google Gemini) through a unified interface.

## Core Concept

All providers implement the same interface:

```go
type Provider interface {
    Complete(ctx context.Context, messages []Message, config *ProviderConfig) (*ProviderResponse, error)
    CompleteStream(ctx context.Context, messages []Message, config *ProviderConfig) (StreamReader, error)
    GetProviderName() string
    Close() error
}
```

This allows code like:

```go
// Same code works with any provider
result, err := provider.Complete(ctx, messages, config)
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
var provider types.Provider

// OpenAI
provider = openai.NewOpenAIProvider(...)

// Or Claude
provider = anthropic.NewAnthropicProvider(...)

// Or Gemini
provider = gemini.NewGeminiProvider(...)

// Same code!
response, err := provider.Complete(ctx, messages, config)
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

### Complete Method

Synchronous completion:

```go
Complete(ctx context.Context, messages []Message, config *ProviderConfig) (*ProviderResponse, error)
```

**Parameters**:
- `ctx`: Timeout and cancellation
- `messages`: Conversation history
- `config`: Model parameters (temperature, max tokens, etc.)

**Returns**:
- `ProviderResponse`: LLM's response
- `error`: Any errors

### CompleteStream Method

Streaming completion:

```go
CompleteStream(ctx context.Context, messages []Message, config *ProviderConfig) (StreamReader, error)
```

Returns a stream reader for real-time output.

### Lifecycle Methods

```go
GetProviderName() string  // Returns "openai", "claude", "gemini"
Close() error             // Cleanup resources
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

### Provider-Specific Defaults

Each provider has sensible defaults:

```go
// OpenAI defaults
openai.DefaultProviderDefaults() // temperature: 1.0, max_tokens: 4096

// Claude defaults  
anthropic.DefaultProviderDefaults() // temperature: 1.0, max_tokens: 4096

// Gemini defaults
gemini.DefaultProviderDefaults() // temperature: 0.9, max_tokens: 8192
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
providers := []types.Provider{primary, secondary, tertiary}

for _, provider := range providers {
    result, err := provider.Complete(ctx, messages, config)
    if err == nil {
        return result, nil
    }
    log.Printf("Provider %s failed: %v", provider.GetProviderName(), err)
}

return nil, errors.New("all providers failed")
```

### Load Balancing

Distribute across providers:

```go
type LoadBalancer struct {
    providers []types.Provider
    current   int
}

func (lb *LoadBalancer) Execute(...) (*ProviderResponse, error) {
    provider := lb.providers[lb.current % len(lb.providers)]
    lb.current++
    return provider.Complete(...)
}
```

### Cost-Based Routing

Route to cheapest provider:

```go
func selectProvider(taskComplexity string) types.Provider {
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
mockProvider := mock.NewMockProvider()
mockProvider.SetResponse("Hello! How can I help?")

pipe := pipeline.NewPipeline(
    middleware.ProviderMiddleware(mockProvider, nil, nil, config),
)

result, _ := pipe.Execute(ctx, "user", "Hi")
// result.Response.Content == "Hello! How can I help?"
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
provider := openai.NewOpenAIProvider(...)
defer provider.Close()

for _, prompt := range prompts {
    provider.Complete(ctx, messages, config)  // Reuses connections
}
```

### Resource Cleanup

Always close providers:

```go
provider := openai.NewOpenAIProvider(...)
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

result, err := provider.Complete(ctx, messages, config)
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

- [Pipeline Architecture](pipeline-architecture.md) - How providers fit in pipelines
- [Tool Integration](tool-integration.md) - Function calling across providers
- [Providers Reference](../reference/providers.md) - Complete API

## Further Reading

- Strategy pattern (Gang of Four)
- Adapter pattern for API translation
- Repository pattern for provider management
