---
title: Stage Design
docType: explanation
order: 3
---
# Stage Design

Understanding Runtime's composable stage architecture.

## Overview

Stages are the core abstraction in Runtime. Every component that processes streaming data is a stage.

## Core Concept

A stage transforms streaming elements:

```go
type Stage interface {
    Name() string
    Type() StageType
    Process(ctx context.Context, input <-chan StreamElement, output chan<- StreamElement) error
}
```

Stages connect via channels to form pipelines:

```
[Stage A] ──channel──▶ [Stage B] ──channel──▶ [Stage C]
```

Each stage runs in its own goroutine, enabling concurrent processing.

## Why Stages?

### Problem: Cross-Cutting Concerns

Applications need many features:

- State management
- Prompt assembly
- Validation
- LLM execution
- Speech-to-text
- Text-to-speech
- Metrics
- Error handling

Without stages, code becomes tangled:

```go
// Bad: Everything in one function
func execute(audio []byte) []byte {
    // VAD detection
    if !detectSpeech(audio) {
        return nil
    }

    // Transcribe
    text := transcribe(audio)

    // Load state
    history := loadFromRedis(sessionID)

    // Call LLM
    response := openai.Complete(text, history)

    // Save state
    saveToRedis(sessionID, response)

    // Generate speech
    return synthesize(response)
}
```

**Problems**:
- Hard to test individual components
- Can't reuse logic
- No streaming support
- Difficult to add features

### Solution: Stage Pipeline

With stages, concerns are separate and stream-capable:

```go
pipeline := stage.NewPipelineBuilder().
    Chain(
        stage.NewAudioTurnStage(vadConfig),
        stage.NewSTTStage(sttService, sttConfig),
        stage.NewStateStoreLoadStage(stateConfig),
        stage.NewProviderStage(provider, tools, policy, config),
        stage.NewStateStoreSaveStage(stateConfig),
        stage.NewTTSStage(ttsService, ttsConfig),
    ).
    Build()
```

Each stage focuses on one thing, and data streams through.

## Design Principles

### 1. Single Responsibility

Each stage does one thing:

**StateStoreLoadStage**: Only loads conversation history
**PromptAssemblyStage**: Only assembles prompts
**ValidationStage**: Only validates content
**ProviderStage**: Only calls the LLM

### 2. Composability

Stages combine easily:

```go
// Simple pipeline
pipeline := stage.NewPipelineBuilder().
    Chain(
        stage.NewPromptAssemblyStage(registry, task, vars),
        stage.NewProviderStage(provider, tools, policy, config),
    ).
    Build()

// Add state management
pipeline := stage.NewPipelineBuilder().
    Chain(
        stage.NewStateStoreLoadStage(stateConfig),  // Added
        stage.NewPromptAssemblyStage(registry, task, vars),
        stage.NewProviderStage(provider, tools, policy, config),
        stage.NewStateStoreSaveStage(stateConfig),  // Added
    ).
    Build()

// Add validation
pipeline := stage.NewPipelineBuilder().
    Chain(
        stage.NewStateStoreLoadStage(stateConfig),
        stage.NewPromptAssemblyStage(registry, task, vars),
        stage.NewValidationStage(validators, config),  // Added
        stage.NewProviderStage(provider, tools, policy, config),
        stage.NewStateStoreSaveStage(stateConfig),
    ).
    Build()
```

### 3. Explicit Ordering

Order matters. User controls it:

```go
pipeline := stage.NewPipelineBuilder().
    Chain(
        stage.NewStateStoreLoadStage(stateConfig),   // 1. Load history
        stage.NewPromptAssemblyStage(registry, task, vars), // 2. Assemble prompt
        stage.NewValidationStage(validators, config), // 3. Validate
        stage.NewProviderStage(provider, tools, policy, config), // 4. Execute
        stage.NewStateStoreSaveStage(stateConfig),   // 5. Save state
    ).
    Build()
```

### 4. Streaming First

Stages process elements as they flow, not in batches:

```go
func (s *MyStage) Process(ctx context.Context, input <-chan StreamElement, output chan<- StreamElement) error {
    defer close(output)

    for elem := range input {
        // Process each element immediately
        result := s.transform(elem)
        output <- result
    }
    return nil
}
```

## Stage Types

### Transform Stage

Transforms elements 1:1 or 1:N:

```go
type UppercaseStage struct {
    stage.BaseStage
}

func (s *UppercaseStage) Process(ctx context.Context, input <-chan StreamElement, output chan<- StreamElement) error {
    defer close(output)

    for elem := range input {
        if elem.Text != nil {
            upper := strings.ToUpper(*elem.Text)
            elem.Text = &upper
        }
        output <- elem
    }
    return nil
}
```

### Accumulate Stage

Collects multiple elements into one:

```go
type MessageCollectorStage struct {
    stage.BaseStage
}

func (s *MessageCollectorStage) Process(ctx context.Context, input <-chan StreamElement, output chan<- StreamElement) error {
    defer close(output)

    var messages []types.Message
    for elem := range input {
        if elem.Message != nil {
            messages = append(messages, *elem.Message)
        }
    }

    // Emit single element with all messages
    output <- stage.StreamElement{
        Metadata: map[string]interface{}{
            "messages": messages,
        },
    }
    return nil
}
```

### Generate Stage

Produces multiple elements from one input:

```go
type TokenizerStage struct {
    stage.BaseStage
}

func (s *TokenizerStage) Process(ctx context.Context, input <-chan StreamElement, output chan<- StreamElement) error {
    defer close(output)

    for elem := range input {
        if elem.Text != nil {
            // Split into tokens, emit each
            tokens := strings.Fields(*elem.Text)
            for _, token := range tokens {
                t := token
                output <- stage.StreamElement{Text: &t}
            }
        }
    }
    return nil
}
```

### Sink Stage

Terminal stage that consumes without producing:

```go
type LoggerStage struct {
    stage.BaseStage
    logger *log.Logger
}

func (s *LoggerStage) Process(ctx context.Context, input <-chan StreamElement, output chan<- StreamElement) error {
    defer close(output)

    for elem := range input {
        s.logger.Printf("Element: %+v", elem)
        // Don't emit anything
    }
    return nil
}
```

## Built-In Stages

### Core Stages

| Stage | Purpose |
|-------|---------|
| `StateStoreLoadStage` | Load conversation history |
| `StateStoreSaveStage` | Persist conversation state |
| `PromptAssemblyStage` | Assemble prompts from registry |
| `TemplateStage` | Variable substitution |
| `ValidationStage` | Content validation |
| `ProviderStage` | LLM execution with tool support |

### Streaming Stages

| Stage | Purpose |
|-------|---------|
| `AudioTurnStage` | VAD-based turn detection |
| `STTStage` | Speech-to-text transcription |
| `TTSStage` | Text-to-speech synthesis |
| `TTSStageWithInterruption` | TTS with barge-in support |
| `DuplexProviderStage` | Bidirectional WebSocket streaming |
| `VADAccumulatorStage` | Audio buffering with VAD |

### Advanced Stages

| Stage | Purpose |
|-------|---------|
| `RouterStage` | Dynamic routing to multiple outputs |
| `MergeStage` | Combine multiple inputs |
| `MetricsStage` | Performance monitoring |
| `TracingStage` | Distributed tracing |
| `PriorityChannel` | Priority-based scheduling |

### Utility Stages

| Stage | Purpose |
|-------|---------|
| `DebugStage` | Pipeline debugging |
| `VariableProviderStage` | Dynamic variable resolution |
| `MediaExternalizerStage` | External media storage |
| `ContextBuilderStage` | Token budget management |

## Creating Custom Stages

### Basic Pattern

```go
type MyStage struct {
    stage.BaseStage
    config MyConfig
}

func NewMyStage(config MyConfig) *MyStage {
    return &MyStage{
        BaseStage: stage.NewBaseStage("my_stage", stage.StageTypeTransform),
        config:    config,
    }
}

func (s *MyStage) Process(ctx context.Context, input <-chan StreamElement, output chan<- StreamElement) error {
    defer close(output)  // Always close output

    for elem := range input {
        // Transform element
        result := s.transform(elem)

        // Send with cancellation check
        select {
        case output <- result:
        case <-ctx.Done():
            return ctx.Err()
        }
    }
    return nil
}
```

### With Configuration

```go
type RateLimitConfig struct {
    RequestsPerSecond int
    BurstSize         int
}

type RateLimitStage struct {
    stage.BaseStage
    limiter *rate.Limiter
}

func NewRateLimitStage(config RateLimitConfig) *RateLimitStage {
    return &RateLimitStage{
        BaseStage: stage.NewBaseStage("rate_limit", stage.StageTypeTransform),
        limiter:   rate.NewLimiter(rate.Limit(config.RequestsPerSecond), config.BurstSize),
    }
}

func (s *RateLimitStage) Process(ctx context.Context, input <-chan StreamElement, output chan<- StreamElement) error {
    defer close(output)

    for elem := range input {
        if err := s.limiter.Wait(ctx); err != nil {
            return err
        }
        output <- elem
    }
    return nil
}
```

### Error Handling

```go
func (s *MyStage) Process(ctx context.Context, input <-chan StreamElement, output chan<- StreamElement) error {
    defer close(output)

    for elem := range input {
        result, err := s.process(elem)
        if err != nil {
            // Option 1: Send error element and continue
            output <- stage.NewErrorElement(err)
            continue

            // Option 2: Return error to stop pipeline
            // return err
        }
        output <- result
    }
    return nil
}
```

## Design Patterns

### Pre-Processing

Stages that prepare input:

- **StateStoreLoadStage**: Load conversation history
- **PromptAssemblyStage**: Build prompt from registry
- **TemplateStage**: Substitute variables
- **ValidationStage**: Check input constraints

### Post-Processing

Stages that handle output:

- **ValidationStage**: Check output constraints
- **StateStoreSaveStage**: Persist conversation
- **MetricsStage**: Record performance
- **DebugStage**: Log for debugging

### Branching

Split stream to multiple destinations:

```go
pipeline := stage.NewPipelineBuilder().
    Chain(
        stage.NewProviderStage(provider, tools, policy, config),
    ).
    Branch("provider", "tts", "logger").
    Build()
```

### Merging

Combine multiple streams:

```go
mergeStage := stage.NewMergeStage(inputChannels...)
```

## Ordering Best Practices

### Recommended Order

```go
pipeline := stage.NewPipelineBuilder().
    Chain(
        // 1. Load state first
        stage.NewStateStoreLoadStage(stateConfig),

        // 2. Resolve variables
        stage.NewVariableProviderStage(providers),

        // 3. Assemble prompt
        stage.NewPromptAssemblyStage(registry, task, vars),

        // 4. Apply templates
        stage.NewTemplateStage(),

        // 5. Validate input
        stage.NewValidationStage(inputValidators, config),

        // 6. Execute LLM
        stage.NewProviderStage(provider, tools, policy, config),

        // 7. Validate output
        stage.NewValidationStage(outputValidators, config),

        // 8. Save state
        stage.NewStateStoreSaveStage(stateConfig),
    ).
    Build()
```

### Order Principles

**State early**: Load history before prompt assembly
**Variables before templates**: Resolve values, then substitute
**Validation twice**: Before and after LLM execution
**Save last**: Persist after all processing complete

## Performance Considerations

### Channel Buffer Size

Control latency vs. throughput:

```go
config := stage.DefaultPipelineConfig().
    WithChannelBufferSize(32)  // Larger buffer = higher throughput, more latency
```

### Concurrent Processing

Stages run in parallel by default. Heavy stages don't block light ones.

### Backpressure

Slow consumers automatically throttle producers via channel blocking.

## Testing Stages

### Unit Testing

Test stages in isolation:

```go
func TestUppercaseStage(t *testing.T) {
    s := NewUppercaseStage()

    input := make(chan stage.StreamElement, 1)
    output := make(chan stage.StreamElement, 1)

    go s.Process(context.Background(), input, output)

    text := "hello"
    input <- stage.StreamElement{Text: &text}
    close(input)

    result := <-output
    assert.Equal(t, "HELLO", *result.Text)
}
```

### Integration Testing

Test stages in pipeline:

```go
func TestPipeline(t *testing.T) {
    pipeline := stage.NewPipelineBuilder().
        Chain(
            NewUppercaseStage(),
            NewTrimStage(),
        ).
        Build()

    text := "  hello  "
    input := make(chan stage.StreamElement, 1)
    input <- stage.StreamElement{Text: &text}
    close(input)

    output, _ := pipeline.Execute(context.Background(), input)

    result := <-output
    assert.Equal(t, "HELLO", *result.Text)
}
```

## Summary

Stage design provides:

- **Single Responsibility**: Each stage has one job
- **Composability**: Mix and match stages
- **Streaming**: Process elements as they flow
- **Concurrency**: Stages run in parallel
- **Testability**: Test stages independently
- **Extensibility**: Easy to add custom stages

## Related Topics

- [Pipeline Architecture](pipeline-architecture) - How stages form pipelines
- [State Management](state-management) - StateStore stages
- [Stage Reference](../reference/pipeline) - Complete API
