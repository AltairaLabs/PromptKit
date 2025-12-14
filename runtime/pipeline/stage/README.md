# Pipeline Stage Architecture

This package implements Phase 1 of the Pipeline Streaming Architecture Proposal: the foundation for reactive streams-based pipeline execution.

## Overview

The stage architecture introduces a new way to build pipelines using a Directed Acyclic Graph (DAG) of stages instead of a linear middleware chain. This enables:

- **True streaming execution**: Stages process elements as they arrive, not after accumulation
- **Explicit concurrency**: Each stage runs in its own goroutine with clear lifecycle
- **Backpressure support**: Channel-based communication naturally handles slow consumers
- **Flexible topologies**: Support for branching, fan-in, fan-out patterns
- **Backward compatibility**: Existing middleware can be wrapped as stages

## Core Concepts

### StreamElement

The fundamental unit of data flowing through the pipeline. Each element can carry:

- **Content**: Text, Audio, Video, Image, Message, ToolCall, or generic Parts
- **Metadata**: Key-value pairs for passing state between stages
- **Priority**: For QoS-aware scheduling (Low, Normal, High, Critical)
- **Control signals**: Error propagation, end-of-stream markers

```go
// Create different types of elements
textElem := stage.NewTextElement("Hello")
msgElem := stage.NewMessageElement(types.Message{Role: "user", Content: "Hello"})
audioElem := stage.NewAudioElement(&stage.AudioData{
    Samples: audioBytes,
    SampleRate: 16000,
    Format: stage.AudioFormatPCM16,
})
errorElem := stage.NewErrorElement(err)
```

### Stage

A processing unit that transforms streaming elements. Unlike middleware, stages:

- Declare their I/O characteristics via `StageType`
- Operate on channels, not shared context
- Run concurrently in separate goroutines
- Must close their output channel when done

```go
type Stage interface {
    Name() string
    Type() StageType
    Process(ctx context.Context, input <-chan StreamElement, output chan<- StreamElement) error
}
```

**Stage Types:**
- `Transform`: 1:1 or 1:N transformation (validation, enrichment)
- `Accumulate`: N:1 accumulation (VAD buffering, message collection)
- `Generate`: 0:N generation (LLM streaming, TTS)
- `Sink`: N:0 terminal stage (state store save, metrics)
- `Bidirectional`: Full duplex (WebSocket session)

### PipelineBuilder

Constructs the pipeline DAG using a fluent API:

```go
pipeline := stage.NewPipelineBuilder().
    Chain(
        stage1,
        stage2,
        stage3,
    ).
    Branch("stage2", "stage4", "stage5").  // Fan out from stage2
    Build()
```

### StreamPipeline

The executable pipeline that:
- Manages goroutine lifecycle
- Creates channels between stages
- Handles errors and shutdown
- Emits events for observability

```go
// Streaming execution
output, err := pipeline.Execute(ctx, input)
for elem := range output {
    // Process elements as they arrive
}

// Synchronous execution (convenience wrapper)
result, err := pipeline.ExecuteSync(ctx, elements...)
```

## Implementation Status

### ✅ Completed (Phase 1: Foundation)

1. **Core Types**
   - `StreamElement` with full content type support (Text, Audio, Video, Image, Message, ToolCall)
   - `Priority` enum for QoS
   - Helper functions for creating elements

2. **Stage Interface**
   - `Stage` interface with Name, Type, Process methods
   - `StageType` enum (Transform, Accumulate, Generate, Sink, Bidirectional)
   - `BaseStage` for reducing boilerplate
   - Utility stages: `PassthroughStage`, `FilterStage`, `MapStage`
   - `StageFunc` for functional-style stages

3. **PipelineBuilder**
   - Fluent API with `Chain()`, `Connect()`, `Branch()`
   - DAG validation (cycle detection, missing stages, duplicate names)
   - Configuration support

4. **StreamPipeline**
   - Concurrent stage execution
   - Channel-based communication
   - Error propagation
   - Graceful shutdown with timeout
   - `Execute()` for streaming, `ExecuteSync()` for request/response

5. **Configuration**
   - `PipelineConfig` with sensible defaults
   - Channel buffer size (default: 16)
   - Priority queue support (optional)
   - Execution timeouts
   - Metrics and tracing flags

6. **Backward Compatibility**
   - `MiddlewareAdapter` wraps existing `pipeline.Middleware`
   - Accumulates input, calls `Process()`, emits output
   - Zero changes required to existing middleware

7. **Events & Observability**
   - New event types: `stage.started`, `stage.completed`, `stage.failed`
   - Event data includes stage name, type, duration, errors
   - Compatible with existing `EventEmitter` and `EventBus`

8. **Error Handling**
   - `StageError` wraps errors with stage context
   - Error elements propagate through pipeline
   - First error captured and returned

### 🚧 Not Yet Implemented (Future Phases)

- **Phase 2: Core Stages**
  - PromptAssemblyStage
  - ValidationStage
  - StateStoreLoad/SaveStage
  - ProviderStage (request/response mode)

- **Phase 3: Streaming Stages**
  - VADAccumulatorStage
  - ProviderStage (streaming mode)
  - TTSStage
  - WebSocketBridgeStage

- **Phase 4: Advanced Features**
  - Priority-based scheduling (PriorityChannel)
  - Metrics collection (per-stage latency, throughput)
  - Element-level tracing
  - Dynamic branching (RouterStage)
  - Fan-in/merge stages

## Usage Examples

### Simple Linear Pipeline

```go
pipeline := stage.NewPipelineBuilder().
    Chain(
        stage.NewPassthroughStage("input"),
        stage.NewPassthroughStage("process"),
        stage.NewPassthroughStage("output"),
    ).
    Build()

input := make(chan stage.StreamElement, 1)
input <- stage.NewMessageElement(types.Message{Role: "user", Content: "Hello"})
close(input)

output, _ := pipeline.Execute(ctx, input)
for elem := range output {
    // Process streaming output
}
```

### Using MiddlewareAdapter

```go
// Wrap existing middleware
promptMiddleware := middleware.NewPromptAssemblyMiddleware(prompt, vars)
providerMiddleware := middleware.NewProviderMiddleware(provider, config)

pipeline := stage.NewPipelineBuilder().
    Chain(
        stage.WrapMiddleware("prompt", promptMiddleware),
        stage.WrapMiddleware("provider", providerMiddleware),
    ).
    Build()

// Executes just like before, but using the new architecture
result, _ := pipeline.ExecuteSync(ctx, stage.NewTextElement("Hello"))
```

### Custom Stage

```go
type UppercaseStage struct {
    stage.BaseStage
}

func NewUppercaseStage() *UppercaseStage {
    return &UppercaseStage{
        BaseStage: stage.NewBaseStage("uppercase", stage.StageTypeTransform),
    }
}

func (s *UppercaseStage) Process(ctx context.Context, input <-chan stage.StreamElement, output chan<- stage.StreamElement) error {
    defer close(output)

    for elem := range input {
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

### Branching Pipeline

```go
pipeline := stage.NewPipelineBuilder().
    Chain(inputStage, processorStage).
    Branch("processor",
        "textOutput",    // Text elements go here
        "audioOutput",   // Audio elements go here
    ).
    Build()
```

## Configuration

```go
config := stage.DefaultPipelineConfig().
    WithChannelBufferSize(32).           // Larger buffers = more throughput
    WithPriorityQueue(true).             // Enable priority scheduling
    WithExecutionTimeout(60 * time.Second).
    WithMetrics(true).                   // Enable per-stage metrics
    WithTracing(true)                    // Enable element-level tracing

pipeline := stage.NewPipelineBuilderWithConfig(config).
    Chain(stages...).
    Build()
```

## Testing

All core pipeline tests pass with the new architecture:

```bash
go test ./runtime/pipeline -v
# PASS
# ok  	github.com/AltairaLabs/PromptKit/runtime/pipeline	0.184s
```

Example tests demonstrate usage:

```bash
go test ./runtime/pipeline/stage -run=Example -v
# PASS
# ok  	github.com/AltairaLabs/PromptKit/runtime/pipeline/stage	0.185s
```

## Migration Path

### For New Code

Use the stage architecture directly:

```go
pipeline := stage.NewPipelineBuilder().
    Chain(/* your stages */).
    Build()
```

### For Existing Code

Wrap existing middleware with zero changes:

```go
pipeline := stage.NewPipelineBuilder().
    Chain(
        stage.WrapMiddleware("name", existingMiddleware),
    ).
    Build()
```

The adapter handles all translation between ExecutionContext and StreamElements.

## Architecture Benefits

1. **True Streaming**: Elements flow through as soon as they're ready, no accumulation
2. **Lower Latency**: Parallel stage execution, no sequential blocking
3. **Backpressure**: Slow consumers naturally slow producers via channel blocking
4. **Clear Concurrency**: Each stage = one goroutine, explicit lifecycle
5. **Flexible Topology**: DAG supports branching, fan-in, fan-out
6. **Better Testing**: Stages are independently testable units
7. **Observability**: Per-stage events, metrics, tracing
8. **Backward Compatible**: Existing middleware works via adapter

## Next Steps (Phase 2)

1. Convert core middleware to native stages:
   - PromptAssemblyStage
   - ValidationStage
   - StateStoreLoad/SaveStage
   - ProviderStage

2. Verify Arena compatibility with adapted stages

3. Performance benchmarking vs. current implementation

4. Documentation and examples for stage authoring

## References

- [Pipeline Streaming Architecture Proposal](../../../../docs/local-backlog/PIPELINE_STREAMING_ARCHITECTURE_PROPOSAL.md)
- [Current Pipeline Implementation](../pipeline.go)
- [Middleware Interface](../types.go)
