# Duplex Streaming Example

This example demonstrates bidirectional streaming using `OpenDuplex()` with the Gemini Live API.

## Features

- Real-time bidirectional streaming
- Text and audio chunk streaming
- Response handling with streaming chunks
- Duplex session lifecycle management

## Requirements

- Gemini API key with Live API access enabled
- Model: `gemini-2.0-flash-exp` (supports streaming input)

**Note:** The Gemini Live API is currently in preview and requires special access. If you encounter authentication errors, visit https://ai.google.dev/ to request Live API access.

## Setup

1. Set your Gemini API key:
```bash
export GEMINI_API_KEY=your-key-here
```

2. Run the example:
```bash
go run .
```

## What It Does

The example demonstrates three scenarios:

1. **Text Streaming**: Send a text message and receive a streaming response
2. **Multiple Chunks**: Build up a message by sending multiple text chunks
3. **Audio Support**: Framework for sending audio chunks (commented out)

## API Usage

```go
// Open a duplex streaming conversation
conv, err := sdk.OpenDuplex(
    "./duplex.pack.json",
    "assistant",
    sdk.WithModel("gemini-2.0-flash-exp"),
    sdk.WithAPIKey(apiKey),
)
defer conv.Close()

// Send text
conv.SendText(ctx, "Hello!")

// Get response channel
respCh, _ := conv.Response()

// Receive streaming responses
for chunk := range respCh {
    fmt.Print(chunk.Content)
    if chunk.Done {
        break
    }
}
```

## OpenDuplex vs Stream

- **OpenDuplex**: Full bidirectional streaming with the model. You can send multiple chunks and receive responses in real-time.
- **Stream**: Unary mode with streaming responses. You send one complete message and receive a streaming response.

Use OpenDuplex when you need:
- Real-time audio/video streaming
- Interactive back-and-forth during model response
- Voice conversation applications
- Live media processing
