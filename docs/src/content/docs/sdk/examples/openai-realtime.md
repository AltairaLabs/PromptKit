---
title: OpenAI Realtime API Example
description: Example demonstrating openai-realtime
sidebar:
  order: 100
---


This example demonstrates bidirectional audio streaming with the OpenAI Realtime API using PromptKit.

## Features

- **Bidirectional Audio Streaming**: Send and receive audio simultaneously at 24kHz
- **Server-Side VAD**: OpenAI's voice activity detection handles turn-taking
- **Function Calling**: Execute tools/functions during streaming sessions
- **Input Transcription**: Get transcripts of what the user said
- **Multiple Voices**: Choose from alloy, echo, shimmer, ash, ballad, coral, sage, verse

## Prerequisites

1. **OpenAI API Key** with Realtime API access
2. **PortAudio** (for audio modes):
   ```bash
   # macOS
   brew install portaudio

   # Ubuntu/Debian
   sudo apt-get install portaudio19-dev

   # Windows
   # Download from http://www.portaudio.com/
   ```

## Usage

### Text Mode (No PortAudio Required)

```bash
export OPENAI_API_KEY=your-key
go run .
```

### Interactive Voice Mode

```bash
export OPENAI_API_KEY=your-key
go run -tags portaudio .
```

### Available Modes (with PortAudio)

```bash
# Interactive voice chat (default)
go run -tags portaudio . interactive

# Function calling demo (ask about weather)
go run -tags portaudio . tools

# Real-time translator (English to Spanish)
go run -tags portaudio . translator
```

## How It Works

### Architecture

```
                    PromptKit SDK
                         |
                         v
            +-----------------------+
            |   OpenDuplex()        |
            |   - Creates session   |
            |   - Manages WebSocket |
            +-----------------------+
                         |
         +---------------+---------------+
         |                               |
         v                               v
+------------------+           +------------------+
| Audio Capture    |           | Audio Playback   |
| (Microphone)     |           | (Speakers)       |
| 24kHz PCM16      |           | 24kHz PCM16      |
+------------------+           +------------------+
         |                               ^
         v                               |
+------------------+           +------------------+
| SendChunk()      |           | Response()       |
| - Audio chunks   |           | - Audio deltas   |
| - Text messages  |           | - Text deltas    |
+------------------+           +------------------+
         |                               ^
         v                               |
+-------------------------------------------------+
|              OpenAI Realtime API                |
|              (WebSocket Connection)             |
|                                                 |
|  - gpt-4o-realtime-preview model               |
|  - Server-side VAD (Voice Activity Detection)  |
|  - Function/Tool calling                       |
|  - Audio transcription                         |
+-------------------------------------------------+
```

### Audio Format

OpenAI Realtime API uses:
- **Sample Rate**: 24kHz (24000 Hz)
- **Bit Depth**: 16-bit signed integer
- **Channels**: Mono (1 channel)
- **Encoding**: PCM16 (little-endian)

### Code Example

```go
package main

import (
    "context"
    "github.com/AltairaLabs/PromptKit/runtime/providers"
    "github.com/AltairaLabs/PromptKit/runtime/types"
    "github.com/AltairaLabs/PromptKit/sdk"
)

func main() {
    // Open duplex conversation
    conv, _ := sdk.OpenDuplex(
        "./openai-realtime.pack.json",
        "assistant",
        sdk.WithModel("gpt-4o-realtime-preview"),
        sdk.WithAPIKey(os.Getenv("OPENAI_API_KEY")),
        sdk.WithStreamingConfig(&providers.StreamingInputConfig{
            Config: types.StreamingMediaConfig{
                Type:       types.ContentTypeAudio,
                SampleRate: 24000,
                Channels:   1,
                Encoding:   "pcm16",
                BitDepth:   16,
                ChunkSize:  4800,
            },
            Metadata: map[string]interface{}{
                "voice":               "alloy",
                "modalities":          []string{"text", "audio"},
                "input_transcription": true,
            },
        }),
    )
    defer conv.Close()

    // Send audio chunk
    chunk := &providers.StreamChunk{
        MediaDelta: &types.MediaContent{
            MIMEType: "audio/pcm",
            Data:     &audioData, // PCM16 bytes as string
        },
    }
    conv.SendChunk(ctx, chunk)

    // Or send text
    conv.SendText(ctx, "Hello!")

    // Receive streaming response
    respCh, _ := conv.Response()
    for chunk := range respCh {
        if chunk.MediaDelta != nil {
            // Play audio
        }
        if chunk.Delta != "" {
            // Print text
        }
    }
}
```

## Voice Options

| Voice | Description |
|-------|-------------|
| `alloy` | Neutral, balanced |
| `echo` | Warm, conversational |
| `shimmer` | Clear, expressive |
| `ash` | Deep, authoritative |
| `ballad` | Melodic, storytelling |
| `coral` | Bright, energetic |
| `sage` | Calm, thoughtful |
| `verse` | Dynamic, engaging |

## Function Calling

The tools demo shows how to handle function calls during streaming:

```go
// Define tools in StreamingInputConfig
Tools: []providers.StreamingToolDefinition{
    {
        Name:        "get_weather",
        Description: "Get the current weather for a location",
        Parameters: map[string]interface{}{...},
    },
},

// Handle tool calls in response
if chunk.ToolCalls != nil {
    for _, tc := range chunk.ToolCalls {
        result := executeToolCall(tc.Name, tc.Arguments)
        // Resolve the tool call and continue the conversation
        conv.ResolveTool(tc.ID)
        conv.Continue(ctx)
        _ = result
    }
}
```

## Troubleshooting

### "OPENAI_API_KEY environment variable is required"
Set your API key:
```bash
export OPENAI_API_KEY=sk-...
```

### "failed to initialize PortAudio"
Install PortAudio for your platform (see Prerequisites above).

### No audio output
- Check your speaker/headphone volume
- Ensure the correct audio device is selected as default
- Try a different voice option

### Echo/feedback issues
Use headphones to prevent the microphone from picking up speaker output.

## Related Examples

- [`duplex-streaming`](../duplex-streaming/) - Gemini Live API streaming
- [`voice-chat`](../voice-chat/) - Traditional STT/TTS voice chat
- [`voice-interview`](../voice-interview/) - Full voice interview application

## Resources

- [OpenAI Realtime API Documentation](https://platform.openai.com/docs/guides/realtime)
- [PromptKit Streaming Documentation](https://promptpack.org/docs/streaming)
