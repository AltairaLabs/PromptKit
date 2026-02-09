---
title: Configure Pipeline
sidebar:
  order: 1
---
Set up and configure Runtime pipeline for LLM execution.

## Goal

Create a functional stage-based pipeline with proper configuration for your use case.

## Prerequisites

- Go 1.21+
- API key for LLM provider (OpenAI, Claude, or Gemini)
- Basic understanding of streaming pipelines

## Basic Pipeline

### Step 1: Import Dependencies

```go
import (
    "context"
    "log"

    "github.com/AltairaLabs/PromptKit/runtime/pipeline/stage"
    "github.com/AltairaLabs/PromptKit/runtime/providers/openai"
)
```

### Step 2: Create Provider

```go
provider := openai.NewProvider(
    "openai",
    "gpt-4o-mini",
    "",  // Use default base URL
    providers.ProviderDefaults{Temperature: 0.7, MaxTokens: 2000},
    false,  // Don't include raw output
)
defer provider.Close()
```

### Step 3: Build Pipeline

```go
pipeline := stage.NewPipelineBuilder().
    Chain(
        stage.NewProviderStage(provider, nil, nil, &stage.ProviderConfig{
            MaxTokens:   1500,
            Temperature: 0.7,
        }),
    ).
    Build()
```

### Step 4: Execute

```go
ctx := context.Background()

// Create input element
msg := types.Message{Role: "user"}
msg.AddTextPart("What is 2+2?")
input := make(chan stage.StreamElement, 1)
input <- stage.NewMessageElement(msg)
close(input)

// Execute pipeline
output, err := pipeline.Execute(ctx, input)
if err != nil {
    log.Fatalf("Execution failed: %v", err)
}

// Collect response
for elem := range output {
    if elem.Text != nil {
        log.Printf("Response: %s\n", *elem.Text)
    }
}
```

## Configuration Options

### Pipeline Configuration

```go
config := stage.DefaultPipelineConfig().
    WithChannelBufferSize(32).              // Inter-stage channel buffer
    WithExecutionTimeout(30 * time.Second). // Per-request timeout
    WithGracefulShutdownTimeout(10 * time.Second). // Shutdown grace period
    WithMetrics(true).                      // Enable per-stage metrics
    WithTracing(true)                       // Enable distributed tracing

pipeline := stage.NewPipelineBuilderWithConfig(config).
    Chain(stages...).
    Build()
```

### Provider Stage Configuration

```go
providerConfig := &stage.ProviderConfig{
    MaxTokens:    2000,       // Maximum response tokens
    Temperature:  0.7,        // Randomness (0-2)
    Seed:         &seed,      // Reproducibility
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

provider := openai.NewProvider(
    "custom-openai",
    "gpt-4o-mini",
    "",
    customDefaults,
    false,
)
```

## Multiple Stages

### Adding Prompt Assembly

```go
pipeline := stage.NewPipelineBuilder().
    Chain(
        stage.NewPromptAssemblyStage(promptRegistry, "chat", variables),
        stage.NewTemplateStage(),
        stage.NewProviderStage(provider, nil, nil, config),
    ).
    Build()
```

### Adding Validators

```go
import "github.com/AltairaLabs/PromptKit/runtime/validators"

validatorRegistry := validators.NewRegistry()
validatorRegistry.Register("banned_words", validators.NewBannedWordsValidator([]string{"inappropriate"}))
validatorRegistry.Register("length", validators.NewLengthValidator())

pipeline := stage.NewPipelineBuilder().
    Chain(
        stage.NewPromptAssemblyStage(promptRegistry, "chat", variables),
        stage.NewValidationStage(validatorRegistry, &stage.ValidationConfig{
            FailOnError: true,
        }),
        stage.NewProviderStage(provider, nil, nil, config),
    ).
    Build()
```

### Adding State Persistence

```go
import (
    "github.com/redis/go-redis/v9"
    "github.com/AltairaLabs/PromptKit/runtime/statestore"
    "github.com/AltairaLabs/PromptKit/runtime/pipeline"
)

redisClient := redis.NewClient(&redis.Options{
    Addr: "localhost:6379",
})

store := statestore.NewRedisStore(redisClient)

stateConfig := &pipeline.StateStoreConfig{
    Store:          store,
    ConversationID: "session-123",
}

pipeline := stage.NewPipelineBuilder().
    Chain(
        stage.NewStateStoreLoadStage(stateConfig),
        stage.NewPromptAssemblyStage(promptRegistry, "chat", variables),
        stage.NewProviderStage(provider, nil, nil, config),
        stage.NewStateStoreSaveStage(stateConfig),
    ).
    Build()
```

## Pipeline Modes

### Text Mode (Default)

Standard request/response pipeline:

```go
pipeline := stage.NewPipelineBuilder().
    Chain(
        stage.NewStateStoreLoadStage(stateConfig),
        stage.NewPromptAssemblyStage(promptRegistry, task, vars),
        stage.NewTemplateStage(),
        stage.NewProviderStage(provider, tools, policy, config),
        stage.NewValidationStage(validatorRegistry, validationConfig),
        stage.NewStateStoreSaveStage(stateConfig),
    ).
    Build()
```

### VAD Mode (Voice with Text LLM)

For voice applications using text-based LLMs:

```go
vadConfig := stage.AudioTurnConfig{
    SilenceDuration:   800 * time.Millisecond,
    MinSpeechDuration: 200 * time.Millisecond,
    MaxTurnDuration:   30 * time.Second,
    SampleRate:        16000,
}

sttConfig := stage.STTStageConfig{
    Language:      "en",
    MinAudioBytes: 1600,
}

ttsConfig := stage.TTSConfig{
    Voice: "alloy",
    Speed: 1.0,
}

pipeline := stage.NewPipelineBuilder().
    Chain(
        stage.NewAudioTurnStage(vadConfig),
        stage.NewSTTStage(sttService, sttConfig),
        stage.NewStateStoreLoadStage(stateConfig),
        stage.NewPromptAssemblyStage(promptRegistry, task, vars),
        stage.NewProviderStage(provider, tools, policy, config),
        stage.NewStateStoreSaveStage(stateConfig),
        stage.NewTTSStageWithInterruption(ttsService, interruptionHandler, ttsConfig),
    ).
    Build()
```

### ASM Mode (Duplex Streaming)

For native multimodal LLMs like Gemini Live:

```go
session, _ := gemini.NewStreamSession(ctx, endpoint, apiKey, streamConfig)

pipeline := stage.NewPipelineBuilder().
    Chain(
        stage.NewStateStoreLoadStage(stateConfig),
        stage.NewPromptAssemblyStage(promptRegistry, task, vars),
        stage.NewDuplexProviderStage(session),
        stage.NewStateStoreSaveStage(stateConfig),
    ).
    Build()
```

## Environment-Based Configuration

### Production Configuration

```go
func NewProductionPipeline() (*stage.StreamPipeline, error) {
    // Get API key from environment
    apiKey := os.Getenv("OPENAI_API_KEY")
    if apiKey == "" {
        return nil, fmt.Errorf("OPENAI_API_KEY not set")
    }

    // Configure provider
    provider := openai.NewProvider(
        "openai-prod",
        "gpt-4o-mini",
        "",
        providers.ProviderDefaults{Temperature: 0.7, MaxTokens: 2000},
        false,
    )

    // Production pipeline config
    config := stage.DefaultPipelineConfig().
        WithChannelBufferSize(32).
        WithExecutionTimeout(60 * time.Second).
        WithGracefulShutdownTimeout(15 * time.Second).
        WithMetrics(true)

    // Build pipeline
    return stage.NewPipelineBuilderWithConfig(config).
        Chain(
            stage.NewPromptAssemblyStage(promptRegistry, "chat", nil),
            stage.NewTemplateStage(),
            stage.NewValidationStage(validatorRegistry, validationConfig),
            stage.NewProviderStage(provider, toolRegistry, toolPolicy, &stage.ProviderConfig{
                MaxTokens:   1500,
                Temperature: 0.7,
            }),
        ).
        Build(), nil
}
```

### Development Configuration

```go
func NewDevelopmentPipeline() *stage.StreamPipeline {
    // Use mock provider for testing
    provider := mock.NewProvider("mock", "test-model", true)

    // Relaxed config for development
    config := stage.DefaultPipelineConfig().
        WithChannelBufferSize(8).
        WithExecutionTimeout(10 * time.Second)

    return stage.NewPipelineBuilderWithConfig(config).
        Chain(
            stage.NewDebugStage(os.Stdout),  // Log all elements
            stage.NewProviderStage(provider, nil, nil, &stage.ProviderConfig{
                MaxTokens:   500,
                Temperature: 1.0,
            }),
        ).
        Build()
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
    EnableState  bool
    EnableDebug  bool
}

func NewPipelineFromConfig(cfg PipelineConfig) (*stage.StreamPipeline, error) {
    var provider providers.Provider

    switch cfg.ProviderType {
    case "openai":
        provider = openai.NewProvider(
            "openai", cfg.Model, "",
            providers.ProviderDefaults{Temperature: 0.7, MaxTokens: 2000},
            false,
        )
    case "claude":
        provider = claude.NewProvider(
            "claude", cfg.Model, "",
            providers.ProviderDefaults{Temperature: 0.7, MaxTokens: 4096},
            false,
        )
    default:
        return nil, fmt.Errorf("unknown provider: %s", cfg.ProviderType)
    }

    // Build stage list
    stages := []stage.Stage{}

    if cfg.EnableDebug {
        stages = append(stages, stage.NewDebugStage(os.Stdout))
    }

    if cfg.EnableState {
        stages = append(stages, stage.NewStateStoreLoadStage(stateConfig))
    }

    stages = append(stages, stage.NewProviderStage(provider, nil, nil, &stage.ProviderConfig{
        MaxTokens:   cfg.MaxTokens,
        Temperature: cfg.Temperature,
    }))

    if cfg.EnableState {
        stages = append(stages, stage.NewStateStoreSaveStage(stateConfig))
    }

    return stage.NewPipelineBuilder().
        Chain(stages...).
        Build(), nil
}
```

### Synchronous Execution Helper

```go
func ExecuteSync(ctx context.Context, pipeline *stage.StreamPipeline, message string) (*types.Message, error) {
    // Create input
    msg := types.Message{Role: "user"}
    msg.AddTextPart(message)
    input := make(chan stage.StreamElement, 1)
    input <- stage.NewMessageElement(msg)
    close(input)

    // Execute
    output, err := pipeline.Execute(ctx, input)
    if err != nil {
        return nil, err
    }

    // Collect response
    var response *types.Message
    for elem := range output {
        if elem.Message != nil {
            response = elem.Message
        }
        if elem.Error != nil {
            return nil, elem.Error
        }
    }

    return response, nil
}
```

## Testing Configuration

### Test Pipeline

```go
func TestPipeline(t *testing.T) {
    // Create mock provider
    provider := mock.NewProvider("test", "test-model", false)
    provider.AddResponse("test input", "test output")

    // Simple test pipeline
    pipeline := stage.NewPipelineBuilder().
        Chain(
            stage.NewProviderStage(provider, nil, nil, &stage.ProviderConfig{
                MaxTokens: 100,
            }),
        ).
        Build()

    // Create input
    msg := types.Message{Role: "user"}
    msg.AddTextPart("test input")
    input := make(chan stage.StreamElement, 1)
    input <- stage.NewMessageElement(msg)
    close(input)

    // Execute
    output, err := pipeline.Execute(context.Background(), input)
    if err != nil {
        t.Fatalf("execution failed: %v", err)
    }

    // Check output
    for elem := range output {
        if elem.Message != nil {
            content := elem.Message.GetTextContent()
            if content != "test output" {
                t.Errorf("unexpected response: %s", content)
            }
        }
    }
}
```

## Troubleshooting

### Issue: Timeout Errors

**Problem**: Pipeline executions timing out.

**Solution**: Increase execution timeout:

```go
config := stage.DefaultPipelineConfig().
    WithExecutionTimeout(120 * time.Second)  // Increase from default 30s
```

### Issue: Backpressure

**Problem**: Slow consumers causing producer blocking.

**Solution**: Increase channel buffer size:

```go
config := stage.DefaultPipelineConfig().
    WithChannelBufferSize(64)  // Increase from default 16
```

### Issue: Memory Growth

**Problem**: Memory usage increasing over time.

**Solution**: Ensure proper cleanup:

```go
defer provider.Close()
defer mcpRegistry.Close()
```

## Best Practices

1. **Always use defer for cleanup**:
   ```go
   defer pipeline.Shutdown(10 * time.Second)
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
       config = config.WithMetrics(true).WithTracing(true)
   }
   ```

5. **Order stages correctly**:
   - State load before prompt assembly
   - Validation before provider
   - State save after provider

## Next Steps

- [Setup Providers](setup-providers) - Configure specific providers
- [Handle Errors](handle-errors) - Robust error handling
- [Streaming Responses](streaming-responses) - Real-time output
- [Create Custom Stages](create-custom-stages) - Build your own stages

## See Also

- [Pipeline Reference](../reference/pipeline) - Complete API
- [Pipeline Tutorial](../tutorials/01-first-pipeline) - Step-by-step guide
- [Stage Design](../explanation/stage-design) - Architecture explanation
