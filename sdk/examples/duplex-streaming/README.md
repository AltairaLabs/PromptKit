# Duplex Streaming Example

This example demonstrates bidirectional streaming using `OpenDuplex()` with the Gemini Live API.

## Features

- **Interactive Voice Mode**: Real-time audio capture from microphone with voice activity detection
- Real-time bidirectional streaming
- Text and audio chunk streaming
- Response handling with streaming chunks
- Duplex session lifecycle management

## Requirements

- Gemini API key with Live API access enabled
- Model: `gemini-2.0-flash-exp` (supports streaming input)
- **Microphone** (for interactive voice mode)
- **PortAudio library** (for audio capture)

**Note:** The Gemini Live API is currently in preview and requires special access. If you encounter authentication errors, visit https://ai.google.dev/ to request Live API access.

## Setup

1. Set your Gemini API key:
```bash
export GEMINI_API_KEY=your-key-here
```

2. Install PortAudio (for audio capture):
```bash
# macOS
brew install portaudio

# Ubuntu/Debian
sudo apt-get install portaudio19-dev

# Fedora
sudo dnf install portaudio-devel
```

3. Run the example:
```bash
# Interactive voice mode (default)
go run .

# Text streaming only
go run . text

# Multiple chunks example
go run . chunks
```

## Modes

The example supports three modes:

1. **interactive** (default): Real-time voice input via microphone
   - Captures audio from your microphone continuously
   - Streams audio chunks to Gemini in real-time (bidirectional)
   - Receives and plays audio responses through speakers
   - Also displays text transcription for debugging

2. **text**: Text streaming example
   - Sends a text message
   - Receives streaming response

3. **chunks**: Multiple chunk sending
   - Sends message in multiple chunks
   - Demonstrates incremental content building

## API Usage

### Interactive Audio Mode

```go
// Open duplex conversation
conv, err := sdk.OpenDuplex("./duplex.pack.json", "assistant")

// Send audio chunk
audioData := string(pcmBytes) // PCM16 audio data
chunk := &providers.StreamChunk{
    MediaDelta: &types.MediaContent{
        MIMEType: types.MIMETypeAudioWAV,
        Data:     &audioData,
    },
}
conv.SendChunk(ctx, chunk)

// Receive streaming responses
respCh, _ := conv.Response()
for chunk := range respCh {
    fmt.Print(chunk.Delta)
    if chunk.FinishReason != nil {
        break
    }
}
```

### Text Streaming

```go
// Open duplex conversation
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
    fmt.Print(chunk.Delta)
    if chunk.FinishReason != nil {
        break
    }
}
```

## How It Works

### Interactive Voice Mode

1. **Audio Capture**: Uses PortAudio to capture microphone input at 16kHz mono PCM16
2. **Continuous Streaming**: Audio is streamed continuously to Gemini Live API (no turn detection)
3. **Bidirectional Audio**: Gemini ASM model streams audio responses back in real-time
4. **Audio Playback**: Responses are played through speakers at 24kHz
5. **Text Display**: Text transcription also shown for debugging

Visual feedback during capture:
- `█` = Audio detected (high energy)
- `░` = Low/no audio

## OpenDuplex vs Stream

- **OpenDuplex**: Full bidirectional streaming with the model. You can send multiple chunks and receive responses in real-time.
- **Stream**: Unary mode with streaming responses. You send one complete message and receive a streaming response.

Use OpenDuplex when you need:
- Real-time audio/video streaming
- Interactive back-and-forth during model response
- Voice conversation applications
- Live media processing
