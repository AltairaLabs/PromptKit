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

### ✅ Completed (Phase 2: Core Request/Response Stages)

1. **PromptAssemblyStage** (`stages_core.go`)
   - Loads prompts from registry with variable substitution
   - Extracts validators from prompt configuration
   - Emits system prompt and allowed tools via metadata

2. **ValidationStage** (`stages_core.go`)
   - Dynamic validator execution from metadata
   - Supports multiple validators in sequence
   - Emits validation results with pass/fail status

3. **StateStoreLoadStage** (`stages_core.go`)
   - Loads conversation history from state store
   - Marks historical messages with `from_history` flag
   - Prepends history to current messages

4. **StateStoreSaveStage** (`stages_core.go`)
   - Saves conversation state after processing
   - Merges metadata from messages
   - Updates state store with new messages

5. **ProviderStage** (`stages_provider.go`)
   - Request/response LLM execution
   - Multi-round tool call support
   - Full tool execution via `toolRegistry.ExecuteAsync()`
   - Policy enforcement (blocklist, ToolChoice)
   - Complete/Failed/Pending status handling

### ✅ Completed (Phase 3: Deprecation & Migration)

1. **Middleware Deprecation**
   - `Middleware` interface marked deprecated in `types.go`
   - Comprehensive migration guide in README
   - Quick migration path using `MiddlewareAdapter`
   - Full migration path to native stages

### ✅ Completed (Phase 4: Streaming Stages)

1. **VADAccumulatorStage** (`stages_streaming.go`)
   - Buffers audio chunks until turn complete
   - VAD-based silence detection
   - Transcribes buffered audio to text
   - Configurable thresholds and durations

2. **TTSStage** (`stages_streaming.go`)
   - Synthesizes audio for text elements
   - Adds AudioData to StreamElements
   - Configurable min text length and empty handling
   - Resilient error handling

3. **DuplexProviderStage** (`stages_streaming.go`)
   - Bidirectional WebSocket streaming
   - Concurrent input/output forwarding
   - StreamElement ⟷ StreamChunk conversion
   - Handles audio and text elements

### ✅ Completed (Phase 5: Advanced Features)

1. **RouterStage** (`stages_advanced.go`)
   - Dynamic routing with configurable routing functions
   - Multiple output channel support
   - Thread-safe output management

2. **MergeStage** (`stages_advanced.go`)
   - Fan-in patterns (N:1 merging)
   - Concurrent processing of multiple inputs
   - Adds merge_input_index metadata

3. **StageMetrics + MetricsStage** (`stages_advanced.go`)
   - Per-stage performance monitoring
   - Tracks latency (min/max/avg), throughput, errors
   - Thread-safe metrics collection
   - Transparent wrapper pattern

4. **PriorityChannel** (`stages_advanced.go`)
   - Priority-based scheduling (Critical/High/Normal/Low)
   - Separate queues per priority level
   - Context-aware blocking operations

5. **TracingStage** (`stages_advanced.go`)
   - Element-level distributed tracing
   - Trace ID generation and propagation
   - Per-stage timing information
   - GetTraceInfo() helper function

### ✅ Completed (Phase 6b: Utility Stages) - Dec 14, 2024

**Package**: `runtime/pipeline/stage/`
**File**: `stages_utilities.go` (612 LOC)
**Commit**: `7509d76` - feat(pipeline): implement Phase 6b - 5 utility stages for complete middleware parity

**Implemented Stages**:
- ✅ `DebugStage` - Pipeline debugging with JSON snapshot logging
- ✅ `TemplateStage` - Variable substitution ({{variable}} → value)
- ✅ `VariableProviderStage` - Dynamic variable resolution from providers
- ✅ `MediaExternalizerStage` - External media storage for large content
- ✅ `ContextBuilderStage` - Token budget management with truncation strategies

**Features**:
- All 5 remaining middleware now have stage equivalents
- Channel-based communication with proper cleanup
- Context cancellation support
- Metadata propagation throughout pipeline
- Configurable behavior (thresholds, policies, strategies)

**Status**: Phase 6b complete. All middleware have stage equivalents.
**Lines Added**: ~612 LOC in stages_utilities.go

### 📊 Implementation Statistics

- **Total LOC**: 4,233 lines across 13 files
- **Stages Implemented**: 20+ production stages (core, streaming, advanced, utilities)
- **Test Status**: All existing pipeline tests pass
- **Backward Compatibility**: ✅ Verified with 5 CI examples (50 test scenarios)
- **TODOs Remaining**: 2 (embedding-based relevance, LLM summarization in ContextBuilderStage)
- **Commits**: 8 commits across all phases

### 🎯 Future Enhancements (Optional)

- Performance benchmarking suite
- Element pooling for GC optimization
- Grafana/Prometheus metrics exporters
- Additional router strategies (weighted, round-robin)

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

## Migrating from Deprecated Middleware Interface

**Note**: The `pipeline.Middleware` interface is deprecated as of Phase 3 and will be removed in the next major version.

### Quick Migration (Recommended for Compatibility)

Use `MiddlewareAdapter` to wrap existing middleware without any code changes:

```go
// Before (deprecated)
pipeline := pipeline.NewPipeline(
    promptMiddleware,
    providerMiddleware,
    validationMiddleware,
)

// After (using stage architecture with adapters)
stagePipeline := stage.NewPipelineBuilder().
    Chain(
        stage.WrapMiddleware("prompt", promptMiddleware),
        stage.WrapMiddleware("provider", providerMiddleware),
        stage.WrapMiddleware("validation", validationMiddleware),
    ).
    Build()
```

**Benefits**: Zero code changes, maintains backward compatibility

**Limitations**: Doesn't get full streaming benefits (elements are accumulated)

### Full Migration (Recommended for Performance)

Convert middleware to native stages for true streaming execution:

```go
// Before: Middleware that implements Process() and StreamChunk()
type MyMiddleware struct {}

func (m *MyMiddleware) Process(ctx *pipeline.ExecutionContext, next func() error) error {
    // Transform messages
    ctx.Messages = append(ctx.Messages, types.Message{...})
    return next()
}

func (m *MyMiddleware) StreamChunk(ctx *pipeline.ExecutionContext, chunk *providers.StreamChunk) error {
    // Optional: intercept streaming chunks
    return nil
}

// After: Native Stage implementation
type MyStage struct {
    stage.BaseStage
}

func NewMyStage() *MyStage {
    return &MyStage{
        BaseStage: stage.NewBaseStage("my_stage", stage.StageTypeTransform),
    }
}

func (s *MyStage) Process(ctx context.Context, input <-chan stage.StreamElement, output chan<- stage.StreamElement) error {
    defer close(output)

    for elem := range input {
        // Transform elements
        if elem.Message != nil {
            // Modify message
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

**Benefits**:
- True streaming with lower latency
- Concurrent execution
- Better testability
- Clearer data flow

**Migration Checklist**:
1. ✅ Replace `pipeline.Middleware` with `stage.Stage`
2. ✅ Change `Process(ctx *pipeline.ExecutionContext, next func() error)` to `Process(ctx context.Context, input <-chan stage.StreamElement, output chan<- stage.StreamElement)`
3. ✅ Replace shared `ExecutionContext` access with channel-based element passing
4. ✅ Remove `StreamChunk()` method (use channel-based streaming instead)
5. ✅ Add `defer close(output)` at the start of Process
6. ✅ Handle context cancellation with `select { case <-ctx.Done(): }`
7. ✅ Test with both streaming and synchronous execution

### Common Migration Patterns

#### Pattern 1: Message Transformation

```go
// Middleware: Accumulates all messages before processing
func (m *MyMiddleware) Process(ctx *ExecutionContext, next func() error) error {
    ctx.Messages = transform(ctx.Messages)
    return next()
}

// Stage: Transforms each message as it arrives
func (s *MyStage) Process(ctx context.Context, input <-chan StreamElement, output chan<- StreamElement) error {
    defer close(output)
    for elem := range input {
        if elem.Message != nil {
            elem.Message = transform(elem.Message)
        }
        output <- elem
    }
    return nil
}
```

#### Pattern 2: State Access via Metadata

```go
// Middleware: Shared context state
ctx.SystemPrompt = "..."
ctx.AllowedTools = []string{...}

// Stage: Metadata propagation
elem.Metadata["system_prompt"] = "..."
elem.Metadata["allowed_tools"] = []string{...}
```

#### Pattern 3: Error Handling

```go
// Middleware: Return error to stop chain
if err != nil {
    return err
}

// Stage: Send error element
if err != nil {
    output <- stage.NewErrorElement(err)
    return err
}
```

### Deprecation Timeline

- **Now (Phase 3)**: `Middleware` interface marked deprecated
- **Next Minor Version**: Documentation moved to legacy section
- **Next Major Version**: `Middleware` interface removed

### Need Help?

- See [example_test.go](./example_test.go) for complete stage examples
- Read [PIPELINE_STREAMING_ARCHITECTURE_PROPOSAL.md](../../../../docs/local-backlog/PIPELINE_STREAMING_ARCHITECTURE_PROPOSAL.md) for architecture details
- Check existing stage implementations in [stages_core.go](./stages_core.go) for reference patterns

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
