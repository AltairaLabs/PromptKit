---
title: Configure Pipeline
docType: how-to
order: 1
---
# How to Configure Pipeline

Set up and configure Runtime pipeline for LLM execution.

## Goal

Create a functional pipeline with proper configuration for your use case.

## Prerequisites

- Go 1.21+
- API key for LLM provider (OpenAI, Claude, or Gemini)
- Basic understanding of middleware pattern

## Basic Pipeline

### Step 1: Import Dependencies

```go
import (
    "context"
    "log"
    "os"
    
    "github.com/AltairaLabs/PromptKit/runtime/pipeline"
    "github.com/AltairaLabs/PromptKit/runtime/pipeline/middleware"
    "github.com/AltairaLabs/PromptKit/runtime/providers/openai"
)
```

### Step 2: Create Provider

```go
provider := openai.NewOpenAIProvider(
    "openai",
    "gpt-4o-mini",
    "",  // Use default base URL
    openai.DefaultProviderDefaults(),
    false,  // Don't include raw output
)
defer provider.Close()
```

### Step 3: Build Pipeline

```go
pipe := pipeline.NewPipeline(
    middleware.ProviderMiddleware(provider, nil, nil, &middleware.ProviderMiddlewareConfig{
        MaxTokens:   1500,
        Temperature: 0.7,
    }),
)
defer pipe.Shutdown(context.Background())
```

### Step 4: Execute

```go
ctx := context.Background()
result, err := pipe.Execute(ctx, "user", "What is 2+2?")
if err != nil {
    log.Fatalf("Execution failed: %v", err)
}

log.Printf("Response: %s\n", result.Response.Content)
log.Printf("Cost: $%.6f\n", result.CostInfo.TotalCost)
```

## Configuration Options

### Runtime Configuration

```go
config := &pipeline.PipelineRuntimeConfig{
    MaxConcurrentExecutions: 100,              // Limit concurrent requests
    StreamBufferSize:        100,              // Stream chunk buffer
    ExecutionTimeout:        30 * time.Second, // Per-request timeout
    GracefulShutdownTimeout: 10 * time.Second, // Shutdown grace period
}

pipe := pipeline.NewPipelineWithConfig(config,
    middleware.ProviderMiddleware(provider, nil, nil, providerConfig),
)
```

### Provider Configuration

```go
providerConfig := &middleware.ProviderMiddlewareConfig{
    MaxTokens:    2000,       // Maximum response tokens
    Temperature:  0.7,        // Randomness (0-2)
    Seed:         &seed,      // Reproducibility
    DisableTrace: false,      // Enable execution tracing
}
```

### Custom Provider Settings

```go
customDefaults := providers.ProviderDefaults{
    Temperature: 0.8,
    TopP:        0.95,
    MaxTokens:   4000,
    Pricing: providers.Pricing{
        InputCostPer1K:  0.00015,
        OutputCostPer1K: 0.0006,
    },
}

provider := openai.NewOpenAIProvider(
    "custom-openai",
    "gpt-4o-mini",
    "",
    customDefaults,
    false,
)
```

## Multiple Middleware

### Adding Template Middleware

```go
pipe := pipeline.NewPipeline(
    middleware.TemplateMiddleware(),  // Process 
    middleware.ProviderMiddleware(provider, nil, nil, config),
)

// System prompt with variables
execCtx := &pipeline.ExecutionContext{
    SystemPrompt: "You are a  assistant.",
    Variables: map[string]string{
        "role": "helpful",
    },
}
```

### Adding Validators

```go
import "github.com/AltairaLabs/PromptKit/runtime/validators"

validatorList := []validators.Validator{
    validators.NewBannedWordsValidator([]string{"inappropriate"}),
    validators.NewLengthValidator(10, 500),
}

pipe := pipeline.NewPipeline(
    middleware.ProviderMiddleware(provider, nil, nil, config),
    middleware.ValidatorMiddleware(validatorList),
)
```

### Adding State Persistence

```go
import (
    "github.com/redis/go-redis/v9"
    "github.com/AltairaLabs/PromptKit/runtime/statestore"
)

redisClient := redis.NewClient(&redis.Options{
    Addr: "localhost:6379",
})

store := statestore.NewRedisStateStore(redisClient)

pipe := pipeline.NewPipeline(
    middleware.StateStoreMiddleware(store, "session-123"),
    middleware.ProviderMiddleware(provider, nil, nil, config),
)
```

## Environment-Based Configuration

### Production Configuration

```go
func NewProductionPipeline() (*pipeline.Pipeline, error) {
    // Get API key from environment
    apiKey := os.Getenv("OPENAI_API_KEY")
    if apiKey == "" {
        return nil, fmt.Errorf("OPENAI_API_KEY not set")
    }
    
    // Configure provider
    provider := openai.NewOpenAIProvider(
        "openai-prod",
        "gpt-4o-mini",
        "",
        openai.DefaultProviderDefaults(),
        false,
    )
    
    // Production runtime config
    config := &pipeline.PipelineRuntimeConfig{
        MaxConcurrentExecutions: 50,  // Conservative limit
        ExecutionTimeout:        60 * time.Second,
        GracefulShutdownTimeout: 15 * time.Second,
    }
    
    // Build pipeline
    pipe := pipeline.NewPipelineWithConfig(config,
        middleware.TemplateMiddleware(),
        middleware.ProviderMiddleware(provider, nil, nil, &middleware.ProviderMiddlewareConfig{
            MaxTokens:   1500,
            Temperature: 0.7,
        }),
        middleware.ValidatorMiddleware(productionValidators()),
    )
    
    return pipe, nil
}
```

### Development Configuration

```go
func NewDevelopmentPipeline() *pipeline.Pipeline {
    // Use mock provider for testing
    provider := mock.NewMockProvider("mock", "test-model", true)
    
    // Relaxed config for development
    config := &pipeline.PipelineRuntimeConfig{
        MaxConcurrentExecutions: 10,
        ExecutionTimeout:        10 * time.Second,
    }
    
    return pipeline.NewPipelineWithConfig(config,
        middleware.ProviderMiddleware(provider, nil, nil, &middleware.ProviderMiddlewareConfig{
            MaxTokens:   500,
            Temperature: 1.0,
        }),
    )
}
```

## Common Patterns

### Pipeline Factory

```go
type PipelineConfig struct {
    ProviderType string
    Model        string
    MaxTokens    int
    Temperature  float32
}

func NewPipelineFromConfig(cfg PipelineConfig) (*pipeline.Pipeline, error) {
    var provider providers.Provider
    
    switch cfg.ProviderType {
    case "openai":
        provider = openai.NewOpenAIProvider(
            "openai", cfg.Model, "",
            openai.DefaultProviderDefaults(),
            false,
        )
    case "claude":
        provider = claude.NewClaudeProvider(
            "claude", cfg.Model, "",
            claude.DefaultProviderDefaults(),
            false,
        )
    default:
        return nil, fmt.Errorf("unknown provider: %s", cfg.ProviderType)
    }
    
    return pipeline.NewPipeline(
        middleware.ProviderMiddleware(provider, nil, nil, &middleware.ProviderMiddlewareConfig{
            MaxTokens:   cfg.MaxTokens,
            Temperature: cfg.Temperature,
        }),
    ), nil
}
```

### Conditional Middleware

```go
func BuildPipeline(enableValidation, enableState bool) *pipeline.Pipeline {
    middlewares := []pipeline.Middleware{}
    
    // Always include provider
    middlewares = append(middlewares,
        middleware.ProviderMiddleware(provider, nil, nil, config),
    )
    
    // Conditional validation
    if enableValidation {
        middlewares = append(middlewares,
            middleware.ValidatorMiddleware(validators),
        )
    }
    
    // Conditional state
    if enableState {
        middlewares = append(middlewares,
            middleware.StateStoreMiddleware(store, sessionID),
        )
    }
    
    return pipeline.NewPipeline(middlewares...)
}
```

## Testing Configuration

### Test Pipeline

```go
func TestPipeline(t *testing.T) {
    // Create mock provider
    provider := mock.NewMockProvider("test", "test-model", false)
    provider.AddResponse("test input", "test output")
    
    // Simple test pipeline
    pipe := pipeline.NewPipeline(
        middleware.ProviderMiddleware(provider, nil, nil, &middleware.ProviderMiddlewareConfig{
            MaxTokens: 100,
        }),
    )
    
    // Execute
    result, err := pipe.Execute(context.Background(), "user", "test input")
    if err != nil {
        t.Fatalf("execution failed: %v", err)
    }
    
    if result.Response.Content != "test output" {
        t.Errorf("unexpected response: %s", result.Response.Content)
    }
}
```

## Troubleshooting

### Issue: Timeout Errors

**Problem**: Pipeline executions timing out.

**Solution**: Increase execution timeout:

```go
config := &pipeline.PipelineRuntimeConfig{
    ExecutionTimeout: 120 * time.Second,  // Increase from default 30s
}
```

### Issue: Rate Limiting

**Problem**: Provider rate limit errors.

**Solution**: Reduce concurrency:

```go
config := &pipeline.PipelineRuntimeConfig{
    MaxConcurrentExecutions: 10,  // Reduce from default 100
}
```

### Issue: Memory Growth

**Problem**: Memory usage increasing over time.

**Solution**: Ensure proper cleanup:

```go
defer pipe.Shutdown(context.Background())
defer provider.Close()
defer store.Close()
```

## Best Practices

1. **Always use defer for cleanup**:
   ```go
   defer pipe.Shutdown(context.Background())
   ```

2. **Set appropriate timeouts**:
   ```go
   ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
   defer cancel()
   ```

3. **Use environment variables for secrets**:
   ```go
   apiKey := os.Getenv("OPENAI_API_KEY")
   ```

4. **Configure based on environment**:
   ```go
   if os.Getenv("ENV") == "production" {
       config.MaxConcurrentExecutions = 50
   }
   ```

5. **Monitor and log configuration**:
   ```go
   log.Printf("Pipeline config: max_concurrent=%d, timeout=%v",
       config.MaxConcurrentExecutions,
       config.ExecutionTimeout)
   ```

## Next Steps

- [Setup Providers](setup-providers) - Configure specific providers
- [Implement Tools](implement-tools) - Add function calling
- [Handle Errors](handle-errors) - Robust error handling
- [Streaming Responses](streaming-responses) - Real-time output

## See Also

- [Pipeline Reference](../reference/pipeline) - Complete API
- [Pipeline Tutorial](../tutorials/01-first-pipeline) - Step-by-step guide
