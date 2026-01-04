---
title: Setup Providers
sidebar:
  order: 2
---
Configure LLM providers for OpenAI, Claude, and Gemini.

## Goal

Connect to LLM providers with proper configuration and authentication.

## OpenAI Setup

### Basic Setup

```go
import "github.com/AltairaLabs/PromptKit/runtime/providers/openai"

provider := openai.NewOpenAIProvider(
    "openai",
    "gpt-4o-mini",
    "",  // Default URL
    openai.DefaultProviderDefaults(),
    false,
)
defer provider.Close()
```

### Environment Variables

```bash
export OPENAI_API_KEY="sk-..."
```

### Custom Configuration

```go
customDefaults := providers.ProviderDefaults{
    Temperature: 0.8,
    TopP:        0.95,
    MaxTokens:   2000,
    Pricing: providers.Pricing{
        InputCostPer1K:  0.00015,
        OutputCostPer1K: 0.0006,
    },
}

provider := openai.NewOpenAIProvider(
    "openai",
    "gpt-4o-mini",
    "",
    customDefaults,
    false,
)
```

### Available Models

```go
// Fast and cheap
provider := openai.NewOpenAIProvider("openai", "gpt-4o-mini", "", defaults, false)

// Balanced
provider := openai.NewOpenAIProvider("openai", "gpt-4o", "", defaults, false)

// Advanced reasoning
provider := openai.NewOpenAIProvider("openai", "gpt-4-turbo", "", defaults, false)
```

## Claude Setup

### Basic Setup

```go
import "github.com/AltairaLabs/PromptKit/runtime/providers/claude"

provider := claude.NewClaudeProvider(
    "claude",
    "claude-3-5-sonnet-20241022",
    "",
    claude.DefaultProviderDefaults(),
    false,
)
defer provider.Close()
```

### Environment Variables

```bash
export ANTHROPIC_API_KEY="sk-ant-..."
```

### Available Models

```go
// Fast and cheap
provider := claude.NewClaudeProvider("claude", "claude-3-haiku-20240307", "", defaults, false)

// Balanced (recommended)
provider := claude.NewClaudeProvider("claude", "claude-3-5-sonnet-20241022", "", defaults, false)

// Most capable
provider := claude.NewClaudeProvider("claude", "claude-3-opus-20240229", "", defaults, false)
```

## Gemini Setup

### Basic Setup

```go
import "github.com/AltairaLabs/PromptKit/runtime/providers/gemini"

provider := gemini.NewGeminiProvider(
    "gemini",
    "gemini-1.5-flash",
    "",
    gemini.DefaultProviderDefaults(),
    false,
)
defer provider.Close()
```

### Environment Variables

```bash
export GEMINI_API_KEY="..."
```

### Available Models

```go
// Fast and cheap
provider := gemini.NewGeminiProvider("gemini", "gemini-1.5-flash", "", defaults, false)

// Advanced (large context)
provider := gemini.NewGeminiProvider("gemini", "gemini-1.5-pro", "", defaults, false)
```

## Multi-Provider Setup

### Provider Registry

```go
type ProviderRegistry struct {
    providers map[string]providers.Provider
}

func NewProviderRegistry() *ProviderRegistry {
    return &ProviderRegistry{
        providers: make(map[string]providers.Provider),
    }
}

func (r *ProviderRegistry) Register(name string, provider providers.Provider) {
    r.providers[name] = provider
}

func (r *ProviderRegistry) Get(name string) providers.Provider {
    return r.providers[name]
}

// Setup
registry := NewProviderRegistry()
registry.Register("openai", openaiProvider)
registry.Register("claude", claudeProvider)
registry.Register("gemini", geminiProvider)

// Use
provider := registry.Get("openai")
```

### Fallback Strategy

```go
func ExecuteWithFallback(providers []providers.Provider, req providers.PredictionRequest) (providers.PredictionResponse, error) {
    var lastErr error
    
    for _, provider := range providers {
        response, err := provider.Predict(ctx, req)
        if err == nil {
            return response, nil
        }
        lastErr = err
        log.Printf("Provider %s failed: %v, trying next...", provider.ID(), err)
    }
    
    return providers.PredictionResponse{}, fmt.Errorf("all providers failed: %w", lastErr)
}

// Usage
providers := []providers.Provider{openaiProvider, claudeProvider, geminiProvider}
response, err := ExecuteWithFallback(providers, req)
```

## Cost Optimization

### Model Selection by Task

```go
func SelectProvider(taskType string) providers.Provider {
    switch taskType {
    case "simple":
        // Use cheapest model
        return openai.NewOpenAIProvider("openai", "gpt-4o-mini", "", defaults, false)
    case "complex":
        // Use more capable model
        return claude.NewClaudeProvider("claude", "claude-3-5-sonnet-20241022", "", defaults, false)
    case "long-context":
        // Use model with large context
        return gemini.NewGeminiProvider("gemini", "gemini-1.5-pro", "", defaults, false)
    default:
        return openai.NewOpenAIProvider("openai", "gpt-4o-mini", "", defaults, false)
    }
}
```

### Token Limits

```go
config := &middleware.ProviderMiddlewareConfig{
    MaxTokens:   500,  // Limit output tokens
    Temperature: 0.7,
}
```

## Testing with Mock Provider

```go
import "github.com/AltairaLabs/PromptKit/runtime/providers/mock"

// Create mock provider
provider := mock.NewMockProvider("mock", "test-model", false)

// Configure responses
provider.AddResponse("Hello", "Hi there!")
provider.AddResponse("What is 2+2?", "4")

// Use in tests
result, err := provider.Predict(ctx, req)
```

## Custom Base URLs

### Azure OpenAI

```go
provider := openai.NewOpenAIProvider(
    "azure-openai",
    "gpt-4",
    "https://your-resource.openai.azure.com/openai/deployments/your-deployment",
    defaults,
    false,
)
```

### OpenAI-Compatible Endpoints

```go
provider := openai.NewOpenAIProvider(
    "local-llm",
    "model-name",
    "http://localhost:8080/v1",
    defaults,
    false,
)
```

## Troubleshooting

### Authentication Errors

**Problem**: API key not found.

**Solution**:
```go
apiKey := os.Getenv("OPENAI_API_KEY")
if apiKey == "" {
    log.Fatal("OPENAI_API_KEY environment variable not set")
}
```

### Rate Limiting

**Problem**: 429 Too Many Requests.

**Solution**: Add retry logic:
```go
var response providers.PredictionResponse
var err error

for i := 0; i < 3; i++ {
    response, err = provider.Predict(ctx, req)
    if err == nil {
        break
    }
    if strings.Contains(err.Error(), "rate_limit") {
        time.Sleep(time.Duration(i+1) * time.Second)
        continue
    }
    break
}
```

### Timeout Errors

**Problem**: Requests timing out.

**Solution**: Increase context timeout:
```go
ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
defer cancel()
```

## Best Practices

1. **Store API keys in environment variables**:
   ```bash
   export OPENAI_API_KEY="sk-..."
   ```

2. **Always close providers**:
   ```go
   defer provider.Close()
   ```

3. **Use appropriate models for tasks**:
   - Simple: gpt-4o-mini, claude-haiku
   - Complex: gpt-4o, claude-sonnet
   - Long context: gemini-1.5-pro

4. **Monitor costs**:
   ```go
   log.Printf("Cost: $%.6f", result.CostInfo.TotalCost)
   ```

5. **Handle errors gracefully**:
   ```go
   if err != nil {
       log.Printf("Provider error: %v", err)
       // Fallback or retry
   }
   ```

## Next Steps

- [Configure Pipeline](configure-pipeline) - Build complete pipeline
- [Monitor Costs](monitor-costs) - Track spending
- [Switch Providers](switch-providers) - Multi-provider strategies

## See Also

- [Provider Reference](../reference/providers) - Complete API
- [Provider Tutorial](../tutorials/02-provider-basics) - Step-by-step guide
