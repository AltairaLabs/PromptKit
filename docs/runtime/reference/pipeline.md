---
layout: docs
title: Pipeline
parent: Runtime Reference
grand_parent: Runtime
nav_order: 2
---

# Pipeline Reference

The Pipeline component chains middleware together for LLM execution.

## Overview

The Pipeline executes middleware in sequence, passing an `ExecutionContext` through the chain. It supports:

- **Non-streaming execution**: Complete responses
- **Streaming execution**: Real-time chunk processing  
- **Concurrency control**: Limit concurrent executions
- **Graceful shutdown**: Wait for in-flight executions
- **Timeout management**: Per-execution deadlines

## Core Types

### Pipeline

```go
type Pipeline struct {
    middleware []Middleware
    config     *PipelineRuntimeConfig
    semaphore  *semaphore.Weighted
    wg         sync.WaitGroup
    shutdown   chan struct{}
    shutdownMu sync.RWMutex
    isShutdown bool
}
```

Main pipeline execution engine that chains middleware.

### PipelineRuntimeConfig

```go
type PipelineRuntimeConfig struct {
    MaxConcurrentExecutions int           // Default: 100
    StreamBufferSize        int           // Default: 100
    ExecutionTimeout        time.Duration // Default: 30s
    GracefulShutdownTimeout time.Duration // Default: 10s
}
```

Runtime configuration for pipeline behavior.

### ExecutionContext

```go
type ExecutionContext struct {
    Context      context.Context
    
    // State (mutable by middleware)
    SystemPrompt     string
    Variables        map[string]string
    AllowedTools     []string
    Messages         []types.Message
    Tools            []types.ToolDef
    ToolResults      []types.MessageToolResult
    PendingToolCalls []types.MessageToolCall
    Prompt           string
    
    // Output
    Trace       ExecutionTrace
    Response    *Response
    RawResponse interface{}
    Error       error
    
    // Metadata
    Metadata map[string]interface{}
    CostInfo types.CostInfo
    
    // Streaming
    StreamMode        bool
    StreamOutput      chan providers.StreamChunk
    StreamInterrupted bool
    InterruptReason   string
    
    // Control
    ShortCircuit bool
}
```

Execution state passed through middleware chain.

### ExecutionResult

```go
type ExecutionResult struct {
    Messages []types.Message
    Response *Response
    Trace    ExecutionTrace
    CostInfo types.CostInfo
    Metadata map[string]interface{}
}
```

Result of pipeline execution.

### Middleware

```go
type Middleware interface {
    Process(execCtx *ExecutionContext, next func() error) error
    StreamChunk(execCtx *ExecutionContext, chunk *providers.StreamChunk) error
}
```

Interface for pipeline middleware.

## Constructor Functions

### NewPipeline

```go
func NewPipeline(middleware ...Middleware) *Pipeline
```

Creates pipeline with default configuration.

**Example**:
```go
pipe := pipeline.NewPipeline(
    middleware.ProviderMiddleware(provider, nil, nil, config),
)
```

### NewPipelineWithConfig

```go
func NewPipelineWithConfig(
    config *PipelineRuntimeConfig,
    middleware ...Middleware,
) *Pipeline
```

Creates pipeline with custom configuration.

**Example**:
```go
config := &pipeline.PipelineRuntimeConfig{
    MaxConcurrentExecutions: 50,
    ExecutionTimeout:        60 * time.Second,
}

pipe := pipeline.NewPipelineWithConfig(config, middleware...)
```

### NewPipelineWithConfigValidated

```go
func NewPipelineWithConfigValidated(
    config *PipelineRuntimeConfig,
    middleware ...Middleware,
) (*Pipeline, error)
```

Creates pipeline with validation. Returns error for invalid config values.

**Example**:
```go
pipe, err := pipeline.NewPipelineWithConfigValidated(config, middleware...)
if err != nil {
    log.Fatalf("Invalid config: %v", err)
}
```

### DefaultPipelineRuntimeConfig

```go
func DefaultPipelineRuntimeConfig() *PipelineRuntimeConfig
```

Returns default configuration.

**Example**:
```go
config := pipeline.DefaultPipelineRuntimeConfig()
config.MaxConcurrentExecutions = 200  // Override defaults
```

## Execution Methods

### Execute

```go
func (p *Pipeline) Execute(
    ctx context.Context,
    role string,
    content string,
) (*ExecutionResult, error)
```

Executes pipeline in non-streaming mode.

**Parameters**:
- `ctx`: Context for cancellation/timeout
- `role`: Message role (`"user"`, `"assistant"`, `"system"`)
- `content`: Message content (empty role skips message append)

**Returns**: Complete execution result

**Example**:
```go
result, err := pipe.Execute(ctx, "user", "Hello!")
if err != nil {
    return err
}

fmt.Println(result.Response.Content)
fmt.Printf("Cost: $%.6f\n", result.CostInfo.TotalCost)
```

### ExecuteStream

```go
func (p *Pipeline) ExecuteStream(
    ctx context.Context,
    role string,
    content string,
) (<-chan providers.StreamChunk, error)
```

Executes pipeline in streaming mode.

**Parameters**:
- `ctx`: Context for cancellation/timeout
- `role`: Message role
- `content`: Message content

**Returns**: Channel of stream chunks

**Example**:
```go
streamChan, err := pipe.ExecuteStream(ctx, "user", "Write a story")
if err != nil {
    return err
}

for chunk := range streamChan {
    if chunk.Error != nil {
        log.Printf("Stream error: %v\n", chunk.Error)
        break
    }
    
    if chunk.Delta != "" {
        fmt.Print(chunk.Delta)  // Print incremental text
    }
    
    if chunk.FinalResult != nil {
        // Stream complete
        fmt.Printf("\n\nTokens: %d\n", chunk.FinalResult.CostInfo.InputTokens)
    }
}
```

### ExecuteWithMessage

```go
func (p *Pipeline) ExecuteWithMessage(
    ctx context.Context,
    message types.Message,
) (*ExecutionResult, error)
```

Executes pipeline with pre-constructed message.

**Parameters**:
- `ctx`: Context
- `message`: Complete message object (multimodal, tool calls, etc.)

**Returns**: Execution result

**Example**:
```go
msg := types.Message{
    Role:    "user",
    Content: "Describe this image",
    Parts: []types.ContentPart{
        {Type: "image", ImageURL: &types.ImageURL{URL: "data:image/jpeg;base64,..."}},
    },
}

result, err := pipe.ExecuteWithMessage(ctx, msg)
```

### ExecuteStreamWithMessage

```go
func (p *Pipeline) ExecuteStreamWithMessage(
    ctx context.Context,
    message types.Message,
) (<-chan providers.StreamChunk, error)
```

Streaming execution with pre-constructed message.

**Example**:
```go
streamChan, err := pipe.ExecuteStreamWithMessage(ctx, multimodalMsg)
for chunk := range streamChan {
    // Process chunks
}
```

## Lifecycle Methods

### Shutdown

```go
func (p *Pipeline) Shutdown(ctx context.Context) error
```

Gracefully shuts down pipeline, waiting for in-flight executions.

**Parameters**:
- `ctx`: Context with timeout (uses `GracefulShutdownTimeout` if no deadline)

**Returns**: Error if shutdown times out

**Example**:
```go
// Graceful shutdown
ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
defer cancel()

if err := pipe.Shutdown(ctx); err != nil {
    log.Printf("Shutdown error: %v", err)
}
```

## ExecutionContext Methods

### InterruptStream

```go
func (ctx *ExecutionContext) InterruptStream(reason string)
```

Stops streaming execution.

**Example**:
```go
func (v *CustomValidator) StreamChunk(
    execCtx *pipeline.ExecutionContext,
    chunk *providers.StreamChunk,
) error {
    if detectBannedContent(chunk.Delta) {
        execCtx.InterruptStream("banned content detected")
        return errors.New("content policy violation")
    }
    return nil
}
```

### Tool Call Management

```go
func (ctx *ExecutionContext) AddPendingToolCall(toolCall types.MessageToolCall)
func (ctx *ExecutionContext) HasPendingToolCalls() bool
func (ctx *ExecutionContext) GetPendingToolCall(id string) *types.MessageToolCall
func (ctx *ExecutionContext) RemovePendingToolCall(id string) bool
func (ctx *ExecutionContext) ClearPendingToolCalls()
```

Manage pending tool calls for human-in-the-loop scenarios.

**Example**:
```go
// Add pending tool call
execCtx.AddPendingToolCall(types.MessageToolCall{
    ID:   "call_123",
    Name: "approve_payment",
    Arguments: json.RawMessage(`{"amount": 1000}`),
})

// Check for pending calls
if execCtx.HasPendingToolCalls() {
    // Pause execution, wait for human approval
}

// Complete tool call
if call := execCtx.GetPendingToolCall("call_123"); call != nil {
    result := processApproval(call)
    execCtx.RemovePendingToolCall("call_123")
}
```

## Configuration

### Runtime Configuration

```go
config := &pipeline.PipelineRuntimeConfig{
    // Limit concurrent executions (prevents provider overload)
    MaxConcurrentExecutions: 100,
    
    // Stream buffer size (tune for throughput vs memory)
    StreamBufferSize: 100,
    
    // Per-execution timeout (0 = no timeout)
    ExecutionTimeout: 30 * time.Second,
    
    // Shutdown grace period
    GracefulShutdownTimeout: 10 * time.Second,
}
```

**Tuning Guidelines**:

- **MaxConcurrentExecutions**: Set based on provider rate limits
  - OpenAI: 3,500 RPM → ~58 concurrent for 30s requests
  - Anthropic: 4,000 RPM → ~66 concurrent
  - Consider lower values for stability

- **StreamBufferSize**: Balance memory vs throughput
  - 100 = ~100 chunks buffered (~10KB typical)
  - Increase for fast consumers
  - Decrease for memory-constrained environments

- **ExecutionTimeout**: Set based on expected response time
  - Short requests (simple Q&A): 10-20s
  - Long requests (code generation): 60-120s
  - Tool-heavy requests: 120-300s

- **GracefulShutdownTimeout**: Allow time for cleanup
  - Simple pipelines: 5-10s
  - Complex pipelines with tools: 30-60s

### Execution Configuration

Passed via middleware (see [Middleware Reference](middleware.md)):

```go
config := &middleware.ProviderMiddlewareConfig{
    MaxTokens:    1500,
    Temperature:  0.7,
    Seed:         &seed,
    DisableTrace: false,
}

pipe := pipeline.NewPipeline(
    middleware.ProviderMiddleware(provider, toolRegistry, toolPolicy, config),
)
```

## Error Handling

### Common Errors

```go
var (
    ErrPipelineShuttingDown = errors.New("pipeline is shutting down")
)
```

### Error Patterns

**Timeout Error**:
```go
result, err := pipe.Execute(ctx, "user", "Hello")
if errors.Is(err, context.DeadlineExceeded) {
    log.Println("Execution timeout")
}
```

**Shutdown Error**:
```go
result, err := pipe.Execute(ctx, "user", "Hello")
if errors.Is(err, pipeline.ErrPipelineShuttingDown) {
    log.Println("Pipeline shutting down, reject new requests")
}
```

**Middleware Errors**:
```go
result, err := pipe.Execute(ctx, "user", "Hello")
if err != nil {
    // Check execution context for detailed error
    if result != nil && result.Trace.Events != nil {
        for _, event := range result.Trace.Events {
            if event.Type == "error" {
                log.Printf("Middleware error: %v", event.Details)
            }
        }
    }
}
```

### Partial Results

Pipeline may return partial results on error:

```go
result, err := pipe.Execute(ctx, "user", "Hello")
if err != nil {
    // Check if we got any messages before failure
    if result != nil && len(result.Messages) > 0 {
        log.Printf("Partial execution: %d messages received", len(result.Messages))
        
        // You can still access trace, cost info, etc.
        log.Printf("Cost before error: $%.6f", result.CostInfo.TotalCost)
    }
}
```

## Examples

### Basic Pipeline

```go
// Create provider
provider := openai.NewOpenAIProvider(
    "openai",
    "gpt-4o-mini",
    "",
    openai.DefaultProviderDefaults(),
    false,
)

// Build pipeline
pipe := pipeline.NewPipeline(
    middleware.ProviderMiddleware(provider, nil, nil, &middleware.ProviderMiddlewareConfig{
        MaxTokens:   1500,
        Temperature: 0.7,
    }),
)

// Execute
ctx := context.Background()
result, err := pipe.Execute(ctx, "user", "What is 2+2?")
if err != nil {
    log.Fatal(err)
}

fmt.Printf("Response: %s\n", result.Response.Content)
fmt.Printf("Tokens: %d input, %d output\n", 
    result.CostInfo.InputTokens,
    result.CostInfo.OutputTokens)
```

### With Template Middleware

```go
pipe := pipeline.NewPipeline(
    middleware.TemplateMiddleware(),  // Process {{variables}}
    middleware.ProviderMiddleware(provider, nil, nil, config),
)

// System prompt with variables
execCtx := &pipeline.ExecutionContext{
    SystemPrompt: "You are a {{role}} assistant helping with {{topic}}.",
    Variables: map[string]string{
        "role":  "helpful",
        "topic": "programming",
    },
}
```

### With Validators

```go
pipe := pipeline.NewPipeline(
    middleware.ProviderMiddleware(provider, nil, nil, config),
    middleware.ValidatorMiddleware([]validators.Validator{
        validators.NewBannedWordsValidator([]string{"inappropriate"}),
        validators.NewLengthValidator(10, 500),
    }),
)
```

### Production Pipeline

```go
// Configure runtime
config := &pipeline.PipelineRuntimeConfig{
    MaxConcurrentExecutions: 50,
    ExecutionTimeout:        60 * time.Second,
    GracefulShutdownTimeout: 15 * time.Second,
}

// Build middleware stack
pipe := pipeline.NewPipelineWithConfig(config,
    middleware.TemplateMiddleware(),
    middleware.ProviderMiddleware(provider, toolRegistry, toolPolicy, providerConfig),
    middleware.ValidatorMiddleware(validators),
    middleware.StateStoreMiddleware(stateStore),
)

// Setup graceful shutdown
ctx, cancel := context.WithCancel(context.Background())
defer cancel()

go func() {
    <-shutdownSignal
    cancel()
    pipe.Shutdown(context.Background())
}()

// Execute with timeout
execCtx, execCancel := context.WithTimeout(ctx, 30*time.Second)
defer execCancel()

result, err := pipe.Execute(execCtx, "user", "Hello")
```

## Best Practices

### 1. Resource Management

```go
// Always shutdown pipeline
defer pipe.Shutdown(context.Background())

// Always close providers
defer provider.Close()
```

### 2. Context Handling

```go
// Use timeout contexts
ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
defer cancel()

result, err := pipe.Execute(ctx, "user", "Hello")
```

### 3. Streaming Cleanup

```go
// Always drain streaming channels
streamChan, err := pipe.ExecuteStream(ctx, "user", "Hello")
if err != nil {
    return err
}

for chunk := range streamChan {
    if chunk.Error != nil {
        log.Printf("Stream error: %v", chunk.Error)
        break  // Channel will be closed by pipeline
    }
    processChunk(chunk)
}
```

### 4. Concurrency Tuning

```go
// Calculate based on provider limits
// Example: OpenAI 3,500 RPM, 30s avg response
maxConcurrent := (3500 / 60) * 30  // ~1,750 concurrent

// But be conservative
config.MaxConcurrentExecutions = 100  // Leave headroom
```

### 5. Error Recovery

```go
// Implement retry logic at application level
func executeWithRetry(pipe *pipeline.Pipeline, ctx context.Context, msg string) (*pipeline.ExecutionResult, error) {
    for i := 0; i < 3; i++ {
        result, err := pipe.Execute(ctx, "user", msg)
        if err == nil {
            return result, nil
        }
        
        if errors.Is(err, pipeline.ErrPipelineShuttingDown) {
            return nil, err  // Don't retry shutdown
        }
        
        time.Sleep(time.Duration(i+1) * time.Second)
    }
    return nil, fmt.Errorf("max retries exceeded")
}
```

## Performance Considerations

### Memory

- Fresh `ExecutionContext` per call prevents contamination
- Large conversation histories increase memory usage
- Stream buffer size affects memory: 100 chunks ≈ 10KB typical

### Throughput

- Non-streaming: ~50-100 executions/sec (provider-dependent)
- Streaming: ~10% overhead vs non-streaming
- Concurrent executions limited by `MaxConcurrentExecutions`

### Latency

- Non-streaming: Wait for complete response
- Streaming: First chunk in ~200-500ms (TTFT)
- Middleware overhead: ~1-5ms per middleware

## See Also

- [Middleware Reference](middleware.md) - Available middleware
- [Provider Reference](providers.md) - LLM providers
- [Types Reference](types.md) - Data structures
- [Pipeline How-To](../how-to/configure-pipeline.md) - Configuration guide
- [Pipeline Explanation](../explanation/pipeline-architecture.md) - Architecture details
