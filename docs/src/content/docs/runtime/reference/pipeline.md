---
title: Pipeline
sidebar:
  order: 2
---
The Pipeline component chains stages together for streaming LLM execution.

## Overview

The Pipeline executes stages concurrently via goroutines, passing `StreamElement` objects through channels. It supports:

- **Streaming execution**: True streaming with elements flowing as produced
- **Concurrent stages**: Each stage runs in its own goroutine
- **Backpressure**: Channel-based flow control
- **Graceful shutdown**: Wait for all stages to complete
- **Multiple modes**: Text, VAD, and ASM pipeline configurations

## Core Types

### StreamElement

```go
type StreamElement struct {
    // Content (at most one is typically set)
    Text      *string
    Audio     *AudioData
    Video     *VideoData
    Image     *ImageData
    Message   *types.Message
    ToolCall  *types.ToolCall
    Parts     []types.ContentPart

    // Metadata for inter-stage communication
    Metadata  map[string]interface{}

    // Control and priority
    Priority  Priority  // Low, Normal, High, Critical
    Error     error
    Timestamp time.Time
}
```

The fundamental unit of data flowing through the pipeline.

### AudioData

```go
type AudioData struct {
    Samples    []byte
    SampleRate int
    Channels   int
    Format     AudioFormat  // AudioFormatPCM16, AudioFormatPCM32, etc.
}
```

Audio content for speech stages.

### Stage

```go
type Stage interface {
    Name() string
    Type() StageType
    Process(ctx context.Context, input <-chan StreamElement, output chan<- StreamElement) error
}
```

Interface for all pipeline stages.

### StageType

```go
type StageType int

const (
    StageTypeTransform    StageType = iota  // 1:1 or 1:N transformation
    StageTypeAccumulate                      // N:1 accumulation
    StageTypeGenerate                        // 0:N generation
    StageTypeSink                            // N:0 terminal
    StageTypeBidirectional                   // Full duplex
)
```

Classification of stage behavior.

### Priority

```go
type Priority int

const (
    PriorityLow      Priority = iota
    PriorityNormal
    PriorityHigh
    PriorityCritical
)
```

Element priority for QoS-aware scheduling.

### StreamPipeline

```go
type StreamPipeline struct {
    stages []Stage
    config *PipelineConfig
}
```

Executable pipeline of connected stages.

### PipelineConfig

```go
type PipelineConfig struct {
    ChannelBufferSize       int           // Default: 16
    ExecutionTimeout        time.Duration // Default: 30s
    GracefulShutdownTimeout time.Duration // Default: 10s
    PriorityQueue           bool          // Enable priority scheduling
    Metrics                 bool          // Enable per-stage metrics
    Tracing                 bool          // Enable distributed tracing
}
```

Pipeline runtime configuration.

## Constructor Functions

### NewPipelineBuilder

```go
func NewPipelineBuilder() *PipelineBuilder
```

Creates a new pipeline builder with default configuration.

**Example**:
```go
pipeline := stage.NewPipelineBuilder().
    Chain(
        stage.NewProviderStage(provider, nil, nil, config),
    ).
    Build()
```

### NewPipelineBuilderWithConfig

```go
func NewPipelineBuilderWithConfig(config *PipelineConfig) *PipelineBuilder
```

Creates a pipeline builder with custom configuration.

**Example**:
```go
config := stage.DefaultPipelineConfig().
    WithChannelBufferSize(32).
    WithMetrics(true)

pipeline := stage.NewPipelineBuilderWithConfig(config).
    Chain(stages...).
    Build()
```

### DefaultPipelineConfig

```go
func DefaultPipelineConfig() *PipelineConfig
```

Returns default configuration.

**Example**:
```go
config := stage.DefaultPipelineConfig()
config.ChannelBufferSize = 32  // Override defaults
```

## PipelineConfig Methods

### WithChannelBufferSize

```go
func (c *PipelineConfig) WithChannelBufferSize(size int) *PipelineConfig
```

Sets the buffer size for inter-stage channels.

### WithExecutionTimeout

```go
func (c *PipelineConfig) WithExecutionTimeout(timeout time.Duration) *PipelineConfig
```

Sets maximum execution time per pipeline run.

### WithGracefulShutdownTimeout

```go
func (c *PipelineConfig) WithGracefulShutdownTimeout(timeout time.Duration) *PipelineConfig
```

Sets timeout for graceful shutdown.

### WithPriorityQueue

```go
func (c *PipelineConfig) WithPriorityQueue(enabled bool) *PipelineConfig
```

Enables priority-based element scheduling.

### WithMetrics

```go
func (c *PipelineConfig) WithMetrics(enabled bool) *PipelineConfig
```

Enables per-stage metrics collection.

### WithTracing

```go
func (c *PipelineConfig) WithTracing(enabled bool) *PipelineConfig
```

Enables distributed tracing support.

## PipelineBuilder Methods

### Chain

```go
func (b *PipelineBuilder) Chain(stages ...Stage) *PipelineBuilder
```

Adds stages in sequence.

**Example**:
```go
pipeline := stage.NewPipelineBuilder().
    Chain(
        stage.NewStateStoreLoadStage(stateConfig),
        stage.NewPromptAssemblyStage(registry, task, vars),
        stage.NewProviderStage(provider, tools, policy, config),
        stage.NewStateStoreSaveStage(stateConfig),
    ).
    Build()
```

### Branch

```go
func (b *PipelineBuilder) Branch(from string, to ...string) *PipelineBuilder
```

Creates branching from one stage to multiple destinations.

**Example**:
```go
pipeline := stage.NewPipelineBuilder().
    Chain(
        stage.NewProviderStage(provider, tools, policy, config),
    ).
    Branch("provider", "tts", "logger").
    Build()
```

### Build

```go
func (b *PipelineBuilder) Build() *StreamPipeline
```

Constructs the final pipeline. Validates DAG structure.

## StreamPipeline Methods

### Execute

```go
func (p *StreamPipeline) Execute(ctx context.Context, input <-chan StreamElement) (<-chan StreamElement, error)
```

Executes pipeline in streaming mode.

**Parameters**:
- `ctx`: Context for cancellation/timeout
- `input`: Channel of input elements

**Returns**: Channel of output elements

**Example**:
```go
// Create input
input := make(chan stage.StreamElement, 1)
msg := types.Message{Role: "user"}
msg.AddTextPart("Hello!")
input <- stage.NewMessageElement(msg)
close(input)

// Execute
output, err := pipeline.Execute(ctx, input)
if err != nil {
    return err
}

// Process output
for elem := range output {
    if elem.Text != nil {
        fmt.Print(*elem.Text)
    }
    if elem.Error != nil {
        log.Printf("Error: %v", elem.Error)
    }
}
```

### ExecuteSync

```go
func (p *StreamPipeline) ExecuteSync(ctx context.Context, elements ...StreamElement) (*ExecutionResult, error)
```

Executes pipeline synchronously, collecting all output.

**Parameters**:
- `ctx`: Context for cancellation/timeout
- `elements`: Input elements

**Returns**: Collected execution result

**Example**:
```go
msg := types.Message{Role: "user"}
msg.AddTextPart("What is 2+2?")
elem := stage.NewMessageElement(msg)

result, err := pipeline.ExecuteSync(ctx, elem)
if err != nil {
    return err
}

fmt.Printf("Response: %s\n", result.Response.GetTextContent())
```

### Shutdown

```go
func (p *StreamPipeline) Shutdown(timeout time.Duration) error
```

Gracefully shuts down pipeline.

**Parameters**:
- `timeout`: Maximum time to wait for stages to complete

**Returns**: Error if shutdown times out

**Example**:
```go
defer pipeline.Shutdown(10 * time.Second)
```

## StreamElement Constructor Functions

### NewTextElement

```go
func NewTextElement(text string) StreamElement
```

Creates element with text content.

### NewMessageElement

```go
func NewMessageElement(msg types.Message) StreamElement
```

Creates element with message content.

### NewAudioElement

```go
func NewAudioElement(audio *AudioData) StreamElement
```

Creates element with audio content.

### NewErrorElement

```go
func NewErrorElement(err error) StreamElement
```

Creates element representing an error.

### NewElementWithMetadata

```go
func NewElementWithMetadata(metadata map[string]interface{}) StreamElement
```

Creates element with only metadata.

## Built-In Stages

### Core Stages

#### StateStoreLoadStage

```go
func NewStateStoreLoadStage(config *StateStoreConfig) *StateStoreLoadStage
```

Loads conversation history from persistent storage.

**Configuration**:
```go
config := &pipeline.StateStoreConfig{
    Store:          stateStore,
    ConversationID: "session-123",
}
```

#### StateStoreSaveStage

```go
func NewStateStoreSaveStage(config *StateStoreConfig) *StateStoreSaveStage
```

Saves conversation state after processing.

#### PromptAssemblyStage

```go
func NewPromptAssemblyStage(registry *prompt.Registry, taskType string, variables map[string]string) *PromptAssemblyStage
```

Loads and assembles prompts from registry.

**Sets metadata**:
- `system_prompt`: Assembled system prompt
- `allowed_tools`: Tools allowed for this prompt
- `validators`: Validator configurations

#### TemplateStage

```go
func NewTemplateStage() *TemplateStage
```

Processes `{{variable}}` substitution in content.

#### ValidationStage

```go
func NewValidationStage(registry *validators.Registry, config *ValidationConfig) *ValidationStage
```

Validates content against registered validators.

**Configuration**:
```go
config := &stage.ValidationConfig{
    FailOnError:     true,
    SuppressErrors:  false,
}
```

#### ProviderStage

```go
func NewProviderStage(
    provider providers.Provider,
    toolRegistry *tools.Registry,
    toolPolicy *ToolPolicy,
    config *ProviderConfig,
) *ProviderStage
```

Executes LLM calls with streaming and tool support.

**Configuration**:
```go
config := &stage.ProviderConfig{
    MaxTokens:   1500,
    Temperature: 0.7,
    Seed:        &seed,
}
```

**Tool Policy**:
```go
policy := &pipeline.ToolPolicy{
    BlockedTools: []string{"dangerous_tool"},
    ToolChoice:   "auto",  // "auto", "none", "required", or specific tool name
}
```

### Streaming/Speech Stages

#### AudioTurnStage

```go
func NewAudioTurnStage(config AudioTurnConfig) (*AudioTurnStage, error)
```

VAD-based turn detection and audio accumulation.

**Configuration**:
```go
config := stage.AudioTurnConfig{
    SilenceDuration:   800 * time.Millisecond,
    MinSpeechDuration: 200 * time.Millisecond,
    MaxTurnDuration:   30 * time.Second,
    SampleRate:        16000,
}
```

#### STTStage

```go
func NewSTTStage(service stt.Service, config STTStageConfig) *STTStage
```

Speech-to-text transcription.

**Configuration**:
```go
config := stage.STTStageConfig{
    Language:      "en",
    SkipEmpty:     true,
    MinAudioBytes: 1600,  // 50ms at 16kHz
}
```

#### TTSStage

```go
func NewTTSStage(service tts.Service, config TTSConfig) *TTSStage
```

Text-to-speech synthesis.

**Configuration**:
```go
config := stage.TTSConfig{
    Voice: "alloy",
    Speed: 1.0,
}
```

#### TTSStageWithInterruption

```go
func NewTTSStageWithInterruption(
    service tts.Service,
    handler *audio.InterruptionHandler,
    config TTSConfig,
) *TTSStageWithInterruption
```

TTS with barge-in/interruption support.

#### DuplexProviderStage

```go
func NewDuplexProviderStage(session providers.StreamInputSession) *DuplexProviderStage
```

Bidirectional WebSocket streaming for native audio LLMs.

#### VADAccumulatorStage

```go
func NewVADAccumulatorStage(config VADConfig) *VADAccumulatorStage
```

Audio buffering with voice activity detection.

### Advanced Stages

#### RouterStage

```go
func NewRouterStage(routeFunc func(StreamElement) string, outputs map[string]chan<- StreamElement) *RouterStage
```

Dynamic routing based on element content.

#### MergeStage

```go
func NewMergeStage(inputs ...<-chan StreamElement) *MergeStage
```

Combines multiple input streams.

#### MetricsStage

```go
func NewMetricsStage(inner Stage) *MetricsStage
```

Wraps a stage to collect performance metrics.

**Metrics collected**:
- Latency (min/max/avg)
- Throughput (elements/sec)
- Error count

#### TracingStage

```go
func NewTracingStage(inner Stage, tracer Tracer) *TracingStage
```

Adds distributed tracing to a stage.

### Utility Stages

#### DebugStage

```go
func NewDebugStage(output io.Writer) *DebugStage
```

Logs all elements as JSON for debugging.

#### VariableProviderStage

```go
func NewVariableProviderStage(providers []variables.Provider) *VariableProviderStage
```

Resolves variables from multiple sources.

#### MediaExternalizerStage

```go
func NewMediaExternalizerStage(storage MediaStorage, config MediaExternalizerConfig) *MediaExternalizerStage
```

Uploads large media to external storage.

#### ContextBuilderStage

```go
func NewContextBuilderStage(config ContextBuilderConfig) *ContextBuilderStage
```

Manages token budget with truncation strategies.

**Configuration**:
```go
config := stage.ContextBuilderConfig{
    TokenBudget:        4000,
    TruncationStrategy: "sliding_window",  // or "summarize"
}
```

## BaseStage

Base implementation for custom stages:

```go
type BaseStage struct {
    name      string
    stageType StageType
}

func NewBaseStage(name string, stageType StageType) BaseStage
func (s *BaseStage) Name() string
func (s *BaseStage) Type() StageType
```

**Example custom stage**:
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

func (s *MyStage) Process(ctx context.Context, input <-chan stage.StreamElement, output chan<- stage.StreamElement) error {
    defer close(output)

    for elem := range input {
        // Transform element
        if elem.Text != nil {
            upper := strings.ToUpper(*elem.Text)
            elem.Text = &upper
        }

        select {
        case output <- elem:
        case <-ctx.Done():
            return ctx.Err()
        }
    }
    return nil
}
```

## Error Handling

### Error Elements

Send errors as elements for downstream handling:

```go
if err := validate(elem); err != nil {
    output <- stage.NewErrorElement(err)
    continue
}
```

### Fatal Errors

Return error to stop the pipeline:

```go
if err := criticalOperation(); err != nil {
    return err  // Pipeline stops
}
```

### Context Cancellation

Always check for cancellation:

```go
select {
case output <- elem:
case <-ctx.Done():
    return ctx.Err()
}
```

## Metadata Keys

Standard metadata keys used by built-in stages:

| Key | Type | Set By | Used By |
|-----|------|--------|---------|
| `system_prompt` | `string` | PromptAssemblyStage | ProviderStage |
| `allowed_tools` | `[]string` | PromptAssemblyStage | ProviderStage |
| `validators` | `[]ValidatorConfig` | PromptAssemblyStage | ValidationStage |
| `variables` | `map[string]string` | VariableProviderStage | TemplateStage |
| `conversation_id` | `string` | StateStoreLoadStage | StateStoreSaveStage |
| `from_history` | `bool` | StateStoreLoadStage | - |
| `validation_results` | `[]ValidationResult` | ValidationStage | - |
| `cost_info` | `types.CostInfo` | ProviderStage | - |
| `latency_ms` | `int64` | ProviderStage | - |

## Configuration Tuning

### Channel Buffer Size

| Use Case | Recommended | Notes |
|----------|-------------|-------|
| Low latency | 4-8 | Minimize buffering |
| High throughput | 32-64 | Allow producer ahead |
| Memory constrained | 8-16 | Balance |

### Timeout Settings

| Pipeline Type | Execution Timeout | Shutdown Timeout |
|---------------|-------------------|------------------|
| Simple chat | 30s | 5s |
| Tool-heavy | 120s | 30s |
| Voice (VAD) | 300s | 10s |
| Voice (ASM) | 600s | 15s |

## Examples

### Text Pipeline

```go
pipeline := stage.NewPipelineBuilder().
    Chain(
        stage.NewStateStoreLoadStage(stateConfig),
        stage.NewPromptAssemblyStage(registry, "chat", vars),
        stage.NewTemplateStage(),
        stage.NewProviderStage(provider, tools, policy, config),
        stage.NewStateStoreSaveStage(stateConfig),
    ).
    Build()

// Execute
msg := types.Message{Role: "user"}
msg.AddTextPart("Hello!")
input := make(chan stage.StreamElement, 1)
input <- stage.NewMessageElement(msg)
close(input)

output, _ := pipeline.Execute(ctx, input)
for elem := range output {
    if elem.Text != nil {
        fmt.Print(*elem.Text)
    }
}
```

### VAD Voice Pipeline

```go
pipeline := stage.NewPipelineBuilder().
    Chain(
        stage.NewAudioTurnStage(vadConfig),
        stage.NewSTTStage(sttService, sttConfig),
        stage.NewStateStoreLoadStage(stateConfig),
        stage.NewPromptAssemblyStage(registry, task, vars),
        stage.NewProviderStage(provider, tools, policy, providerConfig),
        stage.NewStateStoreSaveStage(stateConfig),
        stage.NewTTSStageWithInterruption(ttsService, handler, ttsConfig),
    ).
    Build()

// Feed audio chunks
for audioChunk := range microphoneStream {
    input <- stage.NewAudioElement(&stage.AudioData{
        Samples:    audioChunk,
        SampleRate: 16000,
        Format:     stage.AudioFormatPCM16,
    })
}
```

### ASM Duplex Pipeline

```go
session, _ := gemini.NewStreamSession(ctx, endpoint, apiKey, streamConfig)

pipeline := stage.NewPipelineBuilder().
    Chain(
        stage.NewStateStoreLoadStage(stateConfig),
        stage.NewPromptAssemblyStage(registry, task, vars),
        stage.NewDuplexProviderStage(session),
        stage.NewStateStoreSaveStage(stateConfig),
    ).
    Build()
```

## Best Practices

1. **Always close output channels**: Use `defer close(output)` at start of Process
2. **Check context cancellation**: Use select with `ctx.Done()`
3. **Use metadata for state**: Pass data between stages via metadata
4. **Handle errors gracefully**: Decide between error elements and fatal returns
5. **Order stages correctly**: State load → Prompt assembly → Provider → State save
6. **Clean up resources**: Use `defer pipeline.Shutdown(timeout)`

## See Also

- [Stage Design](../explanation/stage-design) - Stage architecture
- [Pipeline Architecture](../explanation/pipeline-architecture) - How pipelines work
- [Configure Pipeline](../how-to/configure-pipeline) - Configuration guide
- [Provider Reference](providers) - LLM providers
- [Types Reference](types) - Data structures
