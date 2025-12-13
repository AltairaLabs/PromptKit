# Bidirectional Streaming Example

This example demonstrates how to use the SDK's bidirectional streaming capabilities for real-time communication with LLMs.

## Overview

Bidirectional streaming enables:
- Real-time input/output with the LLM
- Multiple messages without closing the connection
- Low-latency interactive experiences
- Support for voice, text, and media streaming

## Requirements

- Go 1.21 or later
- An LLM provider that supports bidirectional streaming (e.g., Gemini with Live API)
- API credentials for your chosen provider

## Usage

### Basic Text Streaming

```go
// Create bidirectional stream
bidi, err := conv.StreamBiDi(ctx)
if err != nil {
    log.Fatal(err)
}
defer bidi.Close()

// Send input
go func() {
    bidi.SendText(ctx, "Hello!")
    time.Sleep(time.Second)
    bidi.SendText(ctx, "How are you?")
}()

// Receive output
for chunk := range bidi.Output() {
    if chunk.Error != nil {
        log.Printf("Error: %v", chunk.Error)
        break
    }
    if chunk.Type == sdk.ChunkText {
        fmt.Print(chunk.Text)
    }
}
```

### Media Streaming

For audio or video streaming, use `SendChunk` with media data:

```go
// Send audio chunks
for audioData := range micInput {
    chunk := &providers.StreamChunk{
        MediaDelta: &types.MediaContent{
            Format: "audio/pcm16",
            Data:   audioData,
        },
    }
    if err := bidi.SendChunk(ctx, chunk); err != nil {
        log.Printf("Error: %v", err)
        break
    }
}
```

## Running the Example

```bash
# Set your API key
export OPENAI_API_KEY=your-key-here
# or for Gemini
export GEMINI_API_KEY=your-key-here

# Run the example
go run .
```

## Key Concepts

### BiDiStream

The `BiDiStream` type provides:
- `SendText(ctx, text)` - Send text messages
- `SendChunk(ctx, chunk)` - Send media or complex input
- `Output()` - Channel of response chunks
- `Close()` - End the session
- `Error()` - Check for errors

### Session Lifecycle

1. Create stream with `conv.StreamBiDi(ctx)`
2. Send input via `SendText()` or `SendChunk()`
3. Receive output from `Output()` channel
4. Close with `Close()` when done

### Error Handling

Always check for errors in the output stream:

```go
for chunk := range bidi.Output() {
    if chunk.Error != nil {
        log.Printf("Error: %v", chunk.Error)
        break
    }
    // Process chunk...
}
```

## Advanced Usage

### Multiple Concurrent Sessions

You can create multiple bidirectional streams from the same conversation:

```go
bidi1, _ := conv.StreamBiDi(ctx)
bidi2, _ := conv.StreamBiDi(ctx)
// Each operates independently
```

### Integration with Voice Chat

See the [voice-chat example](../voice-chat) for a complete implementation of voice interaction using bidirectional streaming with audio input/output.

## Limitations

- Provider must support `StreamInputSupport` interface
- Currently supported providers:
  - Google Gemini (Live API)
  - OpenAI (Realtime API - future)
- Text-only providers will return an error

## See Also

- [Voice Chat Example](../voice-chat) - Full voice interaction
- [Streaming Example](../streaming) - Unidirectional streaming
- [SDK Documentation](../../README.md)
