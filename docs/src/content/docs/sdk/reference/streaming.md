---
title: Streaming Package Reference
sidebar:
  order: 6
---
Complete reference for the `runtime/streaming` package, which provides generic utilities for bidirectional streaming communication with LLM providers.

## Overview

The streaming package extracts common patterns used in duplex (bidirectional) streaming conversations:

- **Response Processing** - State machine for handling provider responses
- **Audio Streaming** - Utilities for sending audio chunks to providers
- **Tool Execution** - Interface for streaming tool calls
- **Response Collection** - Patterns for managing streaming responses

## Import

```go
import "github.com/AltairaLabs/PromptKit/runtime/streaming"
```

---

## Response Processing

### ResponseAction

Indicates what action to take after processing a response element.

```go
type ResponseAction int

const (
    ResponseActionContinue  ResponseAction = iota // Keep waiting
    ResponseActionComplete                        // Turn completed
    ResponseActionError                           // Error occurred
    ResponseActionToolCalls                       // Execute tool calls
)
```

| Action | Description |
|--------|-------------|
| `ResponseActionContinue` | Informational element (e.g., interruption signal), continue waiting |
| `ResponseActionComplete` | Response complete, turn finished |
| `ResponseActionError` | Error occurred or empty response |
| `ResponseActionToolCalls` | Tool calls received, need execution |

### ProcessResponseElement

```go
func ProcessResponseElement(elem *stage.StreamElement, logPrefix string) (ResponseAction, error)
```

Core state machine for duplex streaming response handling. Analyzes a stream element and determines the appropriate action.

**Parameters:**
- `elem` - Stream element from the pipeline
- `logPrefix` - Prefix for log messages

**Returns:**
- `ResponseAction` - Action to take
- `error` - Only set when action is `ResponseActionError`

**Example:**
```go
for elem := range outputChan {
    action, err := streaming.ProcessResponseElement(&elem, "MySession")
    switch action {
    case streaming.ResponseActionContinue:
        continue
    case streaming.ResponseActionComplete:
        return nil
    case streaming.ResponseActionError:
        return err
    case streaming.ResponseActionToolCalls:
        // Execute tools and send results
    }
}
```

### ErrEmptyResponse

```go
var ErrEmptyResponse = errors.New("empty response, likely interrupted")
```

Returned when a response element has no content. This typically indicates an interrupted response that wasn't properly handled.

---

## Audio Streaming

### AudioStreamer

Provides utilities for streaming audio data through a pipeline.

```go
type AudioStreamer struct {
    ChunkSize       int // Bytes per chunk (default: 640)
    ChunkIntervalMs int // Interval between chunks (default: 20ms)
}
```

### NewAudioStreamer

```go
func NewAudioStreamer() *AudioStreamer
```

Creates a new audio streamer with default settings:
- ChunkSize: 640 bytes (20ms at 16kHz 16-bit mono)
- ChunkIntervalMs: 20ms

### StreamBurst

```go
func (a *AudioStreamer) StreamBurst(
    ctx context.Context,
    audioData []byte,
    sampleRate int,
    inputChan chan<- stage.StreamElement,
) error
```

Sends all audio data as fast as possible without pacing. Preferred for pre-recorded audio to avoid false turn detections from natural speech pauses.

**Parameters:**
- `ctx` - Context for cancellation
- `audioData` - Raw PCM audio bytes
- `sampleRate` - Sample rate in Hz (typically 16000)
- `inputChan` - Pipeline input channel

**Example:**
```go
streamer := streaming.NewAudioStreamer()
err := streamer.StreamBurst(ctx, audioData, 16000, inputChan)
```

### StreamRealtime

```go
func (a *AudioStreamer) StreamRealtime(
    ctx context.Context,
    audioData []byte,
    sampleRate int,
    inputChan chan<- stage.StreamElement,
) error
```

Sends audio data paced to match real-time playback. Each chunk is sent with a delay matching its duration.

**Note:** This mode can cause issues with providers that detect speech pauses mid-utterance. Use `StreamBurst` for pre-recorded audio.

### SendChunk

```go
func (a *AudioStreamer) SendChunk(
    ctx context.Context,
    chunk []byte,
    sampleRate int,
    inputChan chan<- stage.StreamElement,
) error
```

Sends a single audio chunk through the pipeline.

### SendEndOfStream

```go
func SendEndOfStream(
    ctx context.Context,
    inputChan chan<- stage.StreamElement,
) error
```

Signals that audio input is complete for the current turn. This triggers the provider to generate a response.

**Example:**
```go
// Stream audio
streamer.StreamBurst(ctx, audioData, 16000, inputChan)

// Signal end of input
streaming.SendEndOfStream(ctx, inputChan)
```

### Audio Constants

```go
const (
    DefaultChunkSize       = 640   // 20ms at 16kHz 16-bit mono
    DefaultSampleRate      = 16000 // Required by Gemini Live API
    DefaultChunkIntervalMs = 20    // Interval for real-time mode
)
```

---

## Tool Execution

### ToolExecutor

Interface for executing tool calls during streaming sessions.

```go
type ToolExecutor interface {
    Execute(ctx context.Context, toolCalls []types.MessageToolCall) (*ToolExecutionResult, error)
}
```

Implementations are responsible for:
- Looking up tools in a registry
- Executing the tool functions
- Formatting results for the provider
- Handling execution errors

### ToolExecutionResult

```go
type ToolExecutionResult struct {
    // For sending back to the streaming provider
    ProviderResponses []providers.ToolResponse

    // For state store capture
    ResultMessages []types.Message
}
```

### SendToolResults

```go
func SendToolResults(
    ctx context.Context,
    result *ToolExecutionResult,
    inputChan chan<- stage.StreamElement,
) error
```

Sends tool execution results back through the pipeline to the provider, and includes tool result messages for state store capture.

### BuildToolResponseElement

```go
func BuildToolResponseElement(result *ToolExecutionResult) stage.StreamElement
```

Creates a stream element containing tool results. The element includes:
- `tool_responses` metadata for the provider
- `tool_result_messages` for state store capture

### ExecuteAndSend

```go
func ExecuteAndSend(
    ctx context.Context,
    executor ToolExecutor,
    toolCalls []types.MessageToolCall,
    inputChan chan<- stage.StreamElement,
) error
```

Convenience function that executes tool calls and sends results through the pipeline in one operation.

---

## Response Collection

### ResponseCollectorConfig

```go
type ResponseCollectorConfig struct {
    ToolExecutor ToolExecutor // Called when tool calls are received
    LogPrefix    string       // Prepended to log messages
}
```

### ResponseCollector

Manages response collection from a streaming session, processing elements, handling tool calls, and signaling completion.

```go
type ResponseCollector struct {
    // ...
}
```

### NewResponseCollector

```go
func NewResponseCollector(config ResponseCollectorConfig) *ResponseCollector
```

Creates a new response collector with the given configuration.

### Start

```go
func (c *ResponseCollector) Start(
    ctx context.Context,
    outputChan <-chan stage.StreamElement,
    inputChan chan<- stage.StreamElement,
) <-chan error
```

Begins collecting responses in a goroutine. Returns a channel that receives `nil` on success or an error on failure.

The collector will:
1. Process incoming stream elements
2. Execute tool calls via the ToolExecutor (if configured)
3. Send tool results back through inputChan
4. Signal completion or error through the returned channel

**Example:**
```go
config := streaming.ResponseCollectorConfig{
    ToolExecutor: myExecutor,
    LogPrefix:    "Session-123",
}
collector := streaming.NewResponseCollector(config)
doneChan := collector.Start(ctx, outputChan, inputChan)

// Wait for completion
if err := <-doneChan; err != nil {
    log.Printf("Response collection failed: %v", err)
}
```

### DrainStaleMessages

```go
func DrainStaleMessages(outputChan <-chan stage.StreamElement) (int, error)
```

Removes any buffered messages from the output channel. Useful for clearing state between turns.

**Returns:**
- Number of messages drained
- `ErrSessionEnded` if the session ended during drain

### WaitForResponse

```go
func WaitForResponse(ctx context.Context, responseDone <-chan error) error
```

Convenience function for blocking until a response is received.

---

## Error Types

```go
var (
    ErrEmptyResponse = errors.New("empty response, likely interrupted")
    ErrSessionEnded  = errors.New("session ended")
)
```

---

## Complete Example

```go
package main

import (
    "context"
    "github.com/AltairaLabs/PromptKit/runtime/streaming"
    "github.com/AltairaLabs/PromptKit/runtime/pipeline/stage"
)

// MyToolExecutor implements streaming.ToolExecutor
type MyToolExecutor struct {
    registry *tools.Registry
}

func (e *MyToolExecutor) Execute(
    ctx context.Context,
    toolCalls []types.MessageToolCall,
) (*streaming.ToolExecutionResult, error) {
    var responses []providers.ToolResponse
    var messages []types.Message

    for _, call := range toolCalls {
        result, err := e.registry.Execute(ctx, call.Function.Name, call.Function.Arguments)
        if err != nil {
            responses = append(responses, providers.ToolResponse{
                ToolCallID: call.ID,
                Result:     err.Error(),
                IsError:    true,
            })
        } else {
            responses = append(responses, providers.ToolResponse{
                ToolCallID: call.ID,
                Result:     string(result),
            })
        }
    }

    return &streaming.ToolExecutionResult{
        ProviderResponses: responses,
        ResultMessages:    messages,
    }, nil
}

func streamAudioTurn(ctx context.Context, audioData []byte, inputChan chan<- stage.StreamElement, outputChan <-chan stage.StreamElement) error {
    // Stream audio in burst mode
    streamer := streaming.NewAudioStreamer()
    if err := streamer.StreamBurst(ctx, audioData, 16000, inputChan); err != nil {
        return err
    }

    // Signal end of input
    if err := streaming.SendEndOfStream(ctx, inputChan); err != nil {
        return err
    }

    // Collect response with tool handling
    executor := &MyToolExecutor{registry: myRegistry}
    collector := streaming.NewResponseCollector(streaming.ResponseCollectorConfig{
        ToolExecutor: executor,
        LogPrefix:    "AudioTurn",
    })

    doneChan := collector.Start(ctx, outputChan, inputChan)
    return streaming.WaitForResponse(ctx, doneChan)
}
```

---

## See Also

- [Audio API Reference](audio) - VAD mode, ASM mode, turn detection
- [TTS API Reference](tts) - Text-to-speech services
- [Duplex Configuration](../../arena/reference/duplex-config) - Arena duplex configuration
- [Duplex Architecture](../../arena/explanation/duplex-architecture) - How duplex streaming works
