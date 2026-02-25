---
title: Pipeline Architecture
sidebar:
  order: 1
---
Understanding Runtime's stage-based streaming pipeline design.

## Overview

Runtime uses a **stage-based streaming architecture** for processing LLM requests. This design provides true streaming execution, concurrent processing, and a composable DAG (Directed Acyclic Graph) of processing units.

## Core Concept

A pipeline is a DAG of stages that process streaming elements:

```d2
direction: right

input: Input
stage1: Stage 1
stage2: Stage 2
stageN: Stage N
output: Output
branchA: Branch A
branchB: Branch B

input -> stage1 -> stage2 -> stageN -> output
stage1 -> branchA
stage2 -> branchB
```

Each stage:
- Runs in its own goroutine
- Receives elements via input channel
- Sends processed elements via output channel
- Supports true streaming (elements flow as they're produced)

## Why Stages?

### Streaming First

The stage architecture is designed for streaming scenarios like voice applications:
1. **Streaming input**: Audio chunks from microphone
2. **Accumulation**: VAD detects turn boundaries
3. **Processing**: Transcribe, call LLM, generate TTS
4. **Streaming output**: Audio chunks to speaker

Stages model this as a reactive stream where data flows through connected processing units:

```go
pipeline := stage.NewPipelineBuilder().
    Chain(
        stage.NewAudioTurnStage(vadConfig),      // Accumulate until turn complete
        stage.NewSTTStage(sttService, sttConfig), // Transcribe audio
        stage.NewProviderStage(provider, tools, policy, config), // Call LLM
        stage.NewTTSStageWithInterruption(ttsService, handler, ttsConfig), // Generate audio
    ).
    Build()
```

## StreamElement

The fundamental unit of data flowing through the pipeline:

```go
type StreamElement struct {
    // Content types (at most one is set)
    Text      *string
    Audio     *AudioData
    Video     *VideoData
    Image     *ImageData
    Message   *types.Message
    ToolCall  *types.ToolCall
    Parts     []types.ContentPart

    // Metadata for inter-stage communication
    Metadata  map[string]interface{}

    // Control and observability
    Priority  Priority  // Low, Normal, High, Critical
    Error     error
    Timestamp time.Time
}
```

**Key insight**: Each element carries one content type, enabling type-safe routing and priority scheduling.

## Stage Interface

All stages implement:

```go
type Stage interface {
    Name() string
    Type() StageType
    Process(ctx context.Context, input <-chan StreamElement, output chan<- StreamElement) error
}
```

### Stage Types

| Type | Pattern | Example |
|------|---------|---------|
| Transform | 1:1 or 1:N | Validation, enrichment |
| Accumulate | N:1 | VAD buffering, message collection |
| Generate | 0:N | LLM streaming, TTS |
| Sink | N:0 | State store save, metrics |
| Observe | 1:1 (pass-through) | RecordingStage |
| Bidirectional | Varies | WebSocket session |

### Contract

Every stage must:
1. Read from input channel until closed
2. Send results to output channel
3. Close output channel when done
4. Respect context cancellation

```go
func (s *MyStage) Process(ctx context.Context, input <-chan StreamElement, output chan<- StreamElement) error {
    defer close(output)  // Always close output

    for elem := range input {
        // Process element
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

## Pipeline Execution

### Streaming Execution

Elements flow through the pipeline as they're produced:

```go
output, err := pipeline.Execute(ctx, input)
if err != nil {
    return err
}

for elem := range output {
    // Process elements as they arrive
    if elem.Text != nil {
        fmt.Print(*elem.Text)
    }
}
```

### Synchronous Execution

For request/response patterns, `ExecuteSync` collects all output:

```go
result, err := pipeline.ExecuteSync(ctx, inputElements...)
// result.Messages contains all messages
// result.Response contains the final response
```

This is just `Execute()` + drain and accumulate.

## Execution Modes

### Text Mode (Request/Response)

Standard HTTP-based LLM interactions:

```d2
direction: right
Message -> RecordingIn -> StateStoreLoad -> PromptAssembly -> Template -> Provider -> RecordingOut -> StateStoreSave -> Response
```

`RecordingIn` and `RecordingOut` are optional `RecordingStage` instances that capture user input and assistant output as events on the EventBus. They pass data through unchanged.

Hooks (guardrails, tool hooks) run inside `ProviderStage` — they are not separate pipeline stages. Provider hooks execute before/after each LLM call, and chunk interceptors run on each streaming chunk within the provider stage.

**Use cases**: Chat applications, content generation

### VAD Mode (Voice Activity Detection)

For voice applications using text-based LLMs:

```d2
direction: right
Audio -> AudioTurn -> STT -> StateStoreLoad -> PromptAssembly -> Template -> Provider -> TTS -> StateStoreSave -> Audio
```

**Use cases**: Voice assistants, telephony integrations

### ASM Mode (Audio Streaming)

For native multimodal LLMs with real-time audio:

```d2
direction: right
"Audio/Text" -> StateStoreLoad -> PromptAssembly -> Template -> DuplexProvider -> StateStoreSave -> "Audio/Text"
```

**Use cases**: Gemini Live API, real-time voice conversations

## Concurrency Model

### Goroutine Lifecycle

Each stage runs in its own goroutine, managed by the pipeline:

```go
func (p *Pipeline) Execute(ctx context.Context, input <-chan StreamElement) (<-chan StreamElement, error) {
    ctx, cancel := context.WithCancel(ctx)

    // Start each stage in its own goroutine
    current := input
    for _, stg := range p.stages {
        output := make(chan StreamElement, p.config.ChannelBufferSize)

        go func(s Stage, in <-chan StreamElement, out chan<- StreamElement) {
            if err := s.Process(ctx, in, out); err != nil {
                // Error handling
            }
        }(stg, current, output)

        current = output
    }

    return current, nil
}
```

### Backpressure

Channel-based communication naturally handles backpressure:
- Slow consumers block producers
- Buffer size controls latency vs. throughput tradeoff
- No unbounded buffering

### Shutdown

Graceful shutdown propagates through the pipeline:

```go
func (p *Pipeline) Shutdown(timeout time.Duration) error {
    p.cancel()  // Cancel context

    // Wait for all stages to complete
    done := make(chan struct{})
    go func() {
        p.wg.Wait()
        close(done)
    }()

    select {
    case <-done:
        return nil
    case <-time.After(timeout):
        return ErrShutdownTimeout
    }
}
```

## Pipeline Builder

The builder constructs pipelines with a fluent API:

```go
pipeline := stage.NewPipelineBuilder().
    Chain(
        stage.NewStateStoreLoadStage(stateConfig),
        stage.NewPromptAssemblyStage(registry, taskType, vars),
        stage.NewProviderStage(provider, tools, policy, config),
        stage.NewStateStoreSaveStage(stateConfig),
    ).
    Build()
```

### Branching

For parallel processing paths:

```go
pipeline := stage.NewPipelineBuilder().
    Chain(
        stage.NewProviderStage(provider, tools, policy, config),
    ).
    Branch("provider", "tts", "text_output").  // Fork output
    Chain(
        stage.NewTTSStage(ttsService, config),
    ).
    Build()
```

### DAG Validation

The builder validates the pipeline structure:
- Detects cycles
- Verifies all stages are connected
- Checks for duplicate stage names

## Error Handling

### Error Elements

Errors can be sent as elements for downstream handling:

```go
if err := s.validate(elem); err != nil {
    output <- stage.NewErrorElement(err)
    continue  // Process next element
}
```

### Fatal Errors

Returning an error from `Process()` stops the pipeline:

```go
if err := s.criticalOperation(elem); err != nil {
    return err  // Pipeline stops
}
```

### Context Cancellation

All stages should respect context cancellation:

```go
select {
case output <- elem:
    // Success
case <-ctx.Done():
    return ctx.Err()  // Pipeline cancelled
}
```

## Event Integration

The pipeline emits events for observability:

```go
// Automatic events for all stages
EventStageStarted    // When stage begins processing
EventStageCompleted  // When stage finishes successfully
EventStageFailed     // When stage encounters an error

// Pipeline lifecycle
EventPipelineStarted
EventPipelineCompleted
EventPipelineFailed
```

These events are automatically emitted by the pipeline — stage authors don't need to emit them manually.

### RecordingStage

`RecordingStage` is a special observe-only stage that publishes content-carrying events to the EventBus as elements flow through the pipeline:

```go
stage.NewRecordingStage(eventBus, stage.RecordingStageConfig{
    Position:       "output",       // "input" for user messages, "output" for assistant
    SessionID:      sessionID,
    ConversationID: conversationID,
})
```

It records different element types as events:
- **Text / Message** → `EventMessageCreated` (with tool calls and results if present)
- **Audio / Image / Video** → corresponding multimodal events with metadata
- **ToolCall** → `EventToolCallStarted`
- **Error** → `EventStreamInterrupted`

These events can be consumed by any EventBus listener, including:
- **FileEventStore** for JSONL persistence and replay
- **EventBusEvalListener** for automatic eval execution on `message.created` events
- **Prometheus metrics listener** for operational monitoring

See [Observability](/sdk/explanation/observability/) for EventBus architecture and [Eval Framework](/arena/explanation/eval-framework/) for how recorded events trigger evals.

## Performance Characteristics

### Latency

| Scenario | Target | Notes |
|----------|--------|-------|
| Audio chunk → VAD | < 10ms | Minimal buffering |
| Turn complete → LLM request | < 50ms | Channel hop only |
| LLM token → TTS chunk | < 50ms | Parallel processing |
| Channel hop overhead | ~1-2ms | Per stage |

### Memory

- Channel buffers control memory usage
- No full response accumulation needed for streaming
- Element pooling available for high-throughput scenarios

### Throughput

- Concurrent stage execution
- Backpressure prevents unbounded growth
- Priority scheduling for QoS

## Design Decisions

### Why Channels Over Callbacks?

**Decision**: Use Go channels for inter-stage communication

**Rationale**:
- Natural fit for Go's concurrency model
- Built-in backpressure
- Easy to reason about
- Standard error propagation via context

### Why One Goroutine Per Stage?

**Decision**: Each stage runs in exactly one goroutine

**Rationale**:
- Clear ownership of lifecycle
- Predictable resource usage
- Simple debugging (goroutine per stage)
- Easy to add metrics/tracing

### Why Close Output Channel?

**Decision**: Stages must close their output channel when done

**Rationale**:
- Signal completion to downstream stages
- Enable `for range` iteration
- Prevent goroutine leaks
- Clear shutdown semantics

## Summary

The stage-based pipeline architecture provides:

- **True Streaming**: Elements flow as they're produced
- **Concurrency**: Each stage runs independently
- **Backpressure**: Slow consumers naturally throttle producers
- **Composability**: Build complex pipelines from simple stages
- **Observability**: Automatic events for all stages
- **Type Safety**: Strongly typed elements with clear contracts

## Related Topics

- [Stage Reference](../reference/pipeline) - Complete stage API
- [Provider System](provider-system) - How providers integrate
- [State Management](state-management) - Conversation persistence
