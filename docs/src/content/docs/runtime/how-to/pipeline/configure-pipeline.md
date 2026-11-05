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

    "github.com/AltairaLabs/PromptKit/runtime/v2/pipeline/stage"
    "github.com/AltairaLabs/PromptKit/runtime/v2/providers/openai"
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
pipeline, err := stage.NewPipelineBuilder().
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
input <- stage.NewMessageElement(&msg)
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
    WithIdleTimeout(60 * time.Second).      // Cancel after 60s of inactivity (default: 30s)
    WithGracefulShutdownTimeout(10 * time.Second) // Shutdown grace period

pipeline, err := stage.NewPipelineBuilderWithConfig(config).
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
pipeline, err := stage.NewPipelineBuilder().
    Chain(
        stage.NewPromptAssemblyStage(promptRegistry, "chat", variables),
        stage.NewTemplateStage(),
        stage.NewProviderStage(provider, nil, nil, config),
    ).
    Build()
```

### Adding Guardrail Hooks

```go
import (
    "github.com/AltairaLabs/PromptKit/runtime/v2/hooks"
    "github.com/AltairaLabs/PromptKit/runtime/v2/hooks/guardrails"
)

bannedWords, err := guardrails.NewGuardrailHook("banned_words", map[string]any{
    "words": []any{"inappropriate"},
})
if err != nil {
    log.Fatal(err)
}
maxLength, err := guardrails.NewGuardrailHook("length", map[string]any{
    "max_characters": 2000,
})
if err != nil {
    log.Fatal(err)
}

hookRegistry := hooks.NewRegistry(
    hooks.WithProviderHook(bannedWords),
    hooks.WithProviderHook(maxLength),
)

pipeline, err := stage.NewPipelineBuilder().
    Chain(
        stage.NewPromptAssemblyStage(promptRegistry, "chat", variables),
        stage.NewProviderStageWithHooks(provider, nil, nil, config, nil, hookRegistry),
    ).
    Build()
```

### Adding State Persistence

```go
import (
    "github.com/redis/go-redis/v9"
    "github.com/AltairaLabs/PromptKit/runtime/v2/statestore"
    "github.com/AltairaLabs/PromptKit/runtime/v2/pipeline"
)

redisClient := redis.NewClient(&redis.Options{
    Addr: "localhost:6379",
})

store := statestore.NewRedisStore(redisClient)

stateConfig := &pipeline.StateStoreConfig{
    Store:          store,
    ConversationID: "session-123",
}

pipeline, err := stage.NewPipelineBuilder().
    Chain(
        stage.NewStateStoreLoadStage(stateConfig),
        stage.NewPromptAssemblyStage(promptRegistry, "chat", variables),
        stage.NewProviderStage(provider, nil, nil, config),
        stage.NewIncrementalSaveStage(&stage.IncrementalSaveConfig{StateStoreConfig: stateConfig}),
    ).
    Build()
```

## Pipeline Modes

### Text Mode (Default)

Standard request/response pipeline:

```go
pipeline, err := stage.NewPipelineBuilder().
    Chain(
        stage.NewStateStoreLoadStage(stateConfig),
        stage.NewPromptAssemblyStage(promptRegistry, task, vars),
        stage.NewTemplateStage(),
        stage.NewProviderStage(provider, tools, policy, config),
        stage.NewIncrementalSaveStage(&stage.IncrementalSaveConfig{StateStoreConfig: stateConfig}),
    ).
    Build()
```

### VAD Mode (Voice with Text LLM)

For voice applications using text-based LLMs:

```go
// One handler, shared by the turn, STT and TTS stages, decides when the
// caller's speech interrupts the reply (barge-in).
interruptionHandler := audio.NewInterruptionHandler(audio.InterruptionImmediate, nil)

audioTurn, err := stage.NewAudioTurnStage(stage.AudioTurnConfig{
    SilenceDuration:     800 * time.Millisecond,
    MinSpeechDuration:   200 * time.Millisecond,
    MaxTurnDuration:     30 * time.Second,
    SampleRate:          16000,
    EmitEndOfTurn:       true, // fire the model once per turn, not at session close
    InterruptionHandler: interruptionHandler,
})
if err != nil {
    return err
}

sttConfig := stage.DefaultSTTStageConfig()
sttConfig.Language = "en"
sttConfig.InterruptionHandler = interruptionHandler

ttsConfig := stage.DefaultTTSStageWithInterruptionConfig()
ttsConfig.Voice = "alloy"
ttsConfig.InterruptionHandler = interruptionHandler

providerConfig.Streaming = true // one tool loop per EndOfTurn, history kept across turns

pipeline, err := stage.NewPipelineBuilder().
    Chain(
        audioTurn,
        stage.NewSTTStage(sttService, sttConfig),
        stage.NewStateStoreLoadStage(stateConfig),
        stage.NewPromptAssemblyStage(promptRegistry, task, vars),
        stage.NewProviderStage(provider, tools, policy, providerConfig),
        stage.NewIncrementalSaveStage(&stage.IncrementalSaveConfig{StateStoreConfig: stateConfig}),
        stage.NewTTSStageWithInterruption(ttsService, ttsConfig),
    ).
    Build()
```

### ASM Mode (Duplex Streaming)

For native multimodal LLMs like Gemini Live:

```go
// provider implements providers.StreamInputSupport (for example, Gemini Live)
pipeline, err := stage.NewPipelineBuilder().
    Chain(
        stage.NewStateStoreLoadStage(stateConfig),
        stage.NewPromptAssemblyStage(promptRegistry, task, vars),
        stage.NewDuplexProviderStage(provider, &providers.StreamingInputConfig{}),
        stage.NewIncrementalSaveStage(&stage.IncrementalSaveConfig{StateStoreConfig: stateConfig}),
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
        WithGracefulShutdownTimeout(15 * time.Second)

    // Build pipeline
    return stage.NewPipelineBuilderWithConfig(config).
        Chain(
            stage.NewPromptAssemblyStage(promptRegistry, "chat", nil),
            stage.NewTemplateStage(),
                stage.NewProviderStage(provider, toolRegistry, toolPolicy, &stage.ProviderConfig{
                MaxTokens:   1500,
                Temperature: 0.7,
            }),
        ).
        Build()
}
```

### Development Configuration

```go
func NewDevelopmentPipeline() (*stage.StreamPipeline, error) {
    // Use mock provider for testing
    provider := mock.NewProvider("mock", "test-model", true)

    // Relaxed config for development
    config := stage.DefaultPipelineConfig().
        WithChannelBufferSize(8).
        WithExecutionTimeout(10 * time.Second)

    return stage.NewPipelineBuilderWithConfig(config).
        Chain(
            stage.NewDebugStage("input"),  // Log all elements
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
        stages = append(stages, stage.NewDebugStage("input"))
    }

    if cfg.EnableState {
        stages = append(stages, stage.NewStateStoreLoadStage(stateConfig))
    }

    stages = append(stages, stage.NewProviderStage(provider, nil, nil, &stage.ProviderConfig{
        MaxTokens:   cfg.MaxTokens,
        Temperature: cfg.Temperature,
    }))

    if cfg.EnableState {
        stages = append(stages, stage.NewIncrementalSaveStage(&stage.IncrementalSaveConfig{StateStoreConfig: stateConfig}))
    }

    return stage.NewPipelineBuilder().
        Chain(stages...).
        Build()
}
```

### Synchronous Execution Helper

```go
func ExecuteSync(ctx context.Context, pipeline *stage.StreamPipeline, message string) (*types.Message, error) {
    // Create input
    msg := types.Message{Role: "user"}
    msg.AddTextPart(message)
    input := make(chan stage.StreamElement, 1)
    input <- stage.NewMessageElement(&msg)
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

## Session Metadata

Put session-level context in `TurnState.ProviderRequestMetadata` and build the provider stage with `NewProviderStageWithTurnState`. The stage copies the map into `PredictionRequest.Metadata` on every call:

```go
turnState := stage.NewTurnState()
turnState.ProviderRequestMetadata = map[string]interface{}{
    "session_id": sessionID,
    "tenant_id":  tenantID,
    "user_id":    userID,
}

providerStage := stage.NewProviderStageWithTurnState(
    provider, nil, nil, &stage.ProviderConfig{}, nil, nil, turnState,
)
pipe, err := stage.NewPipelineBuilder().Chain(providerStage).Build()
```

Providers receive these values in `PredictionRequest.Metadata`.

## Testing Configuration

### Test Pipeline

```go
func TestPipeline(t *testing.T) {
    // Create mock provider
    provider := mock.NewProviderWithRepository("test", "test-model", false,
        mock.NewInMemoryMockRepository("test output"))

    // Simple test pipeline
    pipeline, err := stage.NewPipelineBuilder().
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
    input <- stage.NewMessageElement(&msg)
    close(input)

    // Execute
    output, err := pipeline.Execute(context.Background(), input)
    if err != nil {
        t.Fatalf("execution failed: %v", err)
    }

    // Check output
    for elem := range output {
        if elem.Message != nil {
            content := elem.Message.GetContent()
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

**Solution**: Increase the idle timeout. It resets on each activity (provider call, round completion, streaming chunk) and is held open for as long as a tool call is running, so a slow tool does not trip it (bound those with the tool's own `TimeoutMs`):

```go
config := stage.DefaultPipelineConfig().
    WithIdleTimeout(120 * time.Second)  // Increase from default 30s
```

For a hard wall-clock limit, use `WithExecutionTimeout` (disabled by default).

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
   defer pipeline.Shutdown(context.Background())
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
       config = config.WithExecutionTimeout(60 * time.Second)
   }
   ```

5. **Order stages correctly**:
   - State load before prompt assembly
   - State save after provider

## Next Steps

- [Setup Providers](/runtime/how-to/providers/setup-providers/) - Configure specific providers
- [Handle Errors](/runtime/how-to/pipeline/handle-errors/) - Robust error handling
- [Streaming Responses](/runtime/how-to/pipeline/streaming-responses/) - Real-time output

## See Also

- [Pipeline Reference](/runtime/reference/pipeline/) - Complete API
- [Pipeline Tutorial](/runtime/tutorials/01-first-pipeline/) - Step-by-step guide
- [Stage Design](/runtime/explanation/stage-design/) - Architecture explanation
