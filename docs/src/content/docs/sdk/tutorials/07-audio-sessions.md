---
title: "Tutorial 7: Audio Sessions"
sidebar:
  order: 7
---
Build voice-enabled conversations with real-time audio streaming.

## What You'll Learn

- Two modes for voice conversations: VAD and ASM
- Setting up VAD mode with STT and TTS services
- Setting up ASM mode for native audio LLMs
- Handling audio input and output streams
- Turn detection and interruption handling

## Prerequisites

- Completed [Tutorial 1: First Conversation](01-first-conversation)
- Basic understanding of audio streaming concepts
- API keys for OpenAI or Google (for Gemini Live)

## Two Modes for Voice

PromptKit supports two modes for voice conversations:

| Mode | Description | Use Case |
|------|-------------|----------|
| **VAD** | Voice Activity Detection pipeline | Standard LLMs (GPT-4, Claude) with separate STT/TTS |
| **ASM** | Audio Streaming Model | Native multimodal LLMs (Gemini Live) |

## VAD Mode: Voice with Text LLMs

VAD mode enables voice conversations with any text-based LLM by adding STT (speech-to-text) and TTS (text-to-speech) stages to the pipeline.

### Pipeline Flow

```
Audio Input → [AudioTurn] → [STT] → [LLM] → [TTS] → Audio Output
```

### Step 1: Create the Pack

Create `voice-assistant.pack.json`:

```json
{
  "id": "voice-assistant",
  "name": "Voice Assistant",
  "version": "1.0.0",
  "template_engine": {
    "version": "v1",
    "syntax": "{{variable}}"
  },
  "prompts": {
    "assistant": {
      "id": "assistant",
      "name": "Voice Assistant",
      "version": "1.0.0",
      "system_template": "You are a helpful voice assistant. Keep responses concise and natural for spoken conversation. The user's name is {{user_name}}.",
      "parameters": {
        "temperature": 0.7,
        "max_tokens": 150
      }
    }
  }
}
```

### Step 2: Configure VAD Mode

```go
package main

import (
    "context"
    "log"
    "os"

    "github.com/AltairaLabs/PromptKit/sdk"
    "github.com/AltairaLabs/PromptKit/runtime/stt"
    "github.com/AltairaLabs/PromptKit/runtime/tts"
)

func main() {
    // Create STT and TTS services
    sttService := stt.NewOpenAI(os.Getenv("OPENAI_API_KEY"))
    ttsService := tts.NewOpenAI(os.Getenv("OPENAI_API_KEY"))

    // Open duplex conversation with VAD mode
    conv, err := sdk.OpenDuplex("./voice-assistant.pack.json", "assistant",
        sdk.WithVADMode(sttService, ttsService, sdk.DefaultVADModeConfig()),
    )
    if err != nil {
        log.Fatal(err)
    }
    defer conv.Close()

    conv.SetVar("user_name", "Alice")

    // Start audio processing
    ctx := context.Background()
    audioIn, audioOut, err := conv.StartAudio(ctx)
    if err != nil {
        log.Fatal(err)
    }

    // Feed audio from microphone and play output
    // (See complete example for audio I/O implementation)
}
```

### Step 3: Customize VAD Configuration

```go
// Custom VAD settings for different environments
vadConfig := &sdk.VADModeConfig{
    SilenceDuration:   500 * time.Millisecond, // Shorter pause detection
    MinSpeechDuration: 100 * time.Millisecond, // Faster response
    MaxTurnDuration:   20 * time.Second,       // Shorter max turn
    SampleRate:        16000,                  // 16kHz audio
    Language:          "en",                   // English
    Voice:             "nova",                 // TTS voice
    Speed:             1.1,                    // Slightly faster speech
}

conv, _ := sdk.OpenDuplex("./voice-assistant.pack.json", "assistant",
    sdk.WithVADMode(sttService, ttsService, vadConfig),
)
```

### VAD Configuration Options

| Option | Default | Description |
|--------|---------|-------------|
| `SilenceDuration` | 800ms | Silence required to detect turn end |
| `MinSpeechDuration` | 200ms | Minimum speech before turn can complete |
| `MaxTurnDuration` | 30s | Maximum turn length |
| `SampleRate` | 16000 | Audio sample rate in Hz |
| `Language` | "en" | Language hint for STT |
| `Voice` | "alloy" | TTS voice ID |
| `Speed` | 1.0 | TTS speech rate (0.5-2.0) |

## ASM Mode: Native Audio LLMs

ASM (Audio Streaming Model) mode is for LLMs with native bidirectional audio support, like Gemini Live API. Audio streams directly to and from the model without separate STT/TTS stages.

### Pipeline Flow

```
Audio/Text Input → [DuplexProvider] → Audio/Text Output
```

### Step 1: Configure ASM Mode

```go
package main

import (
    "context"
    "log"
    "os"

    "github.com/AltairaLabs/PromptKit/sdk"
    "github.com/AltairaLabs/PromptKit/runtime/providers"
    "github.com/AltairaLabs/PromptKit/runtime/providers/gemini"
    "github.com/AltairaLabs/PromptKit/runtime/types"
)

func main() {
    ctx := context.Background()

    // Create Gemini streaming session
    session, err := gemini.NewStreamSession(ctx,
        "wss://generativelanguage.googleapis.com/ws/google.ai.generativelanguage.v1alpha.GenerativeService.BidiGenerateContent",
        os.Getenv("GEMINI_API_KEY"),
        &providers.StreamingInputConfig{
            Type:       types.ContentTypeAudio,
            SampleRate: 16000,
            Channels:   1,
        },
    )
    if err != nil {
        log.Fatal(err)
    }

    // Open duplex conversation with ASM mode
    conv, err := sdk.OpenDuplex("./voice-assistant.pack.json", "assistant",
        sdk.WithStreamingConfig(&providers.StreamingInputConfig{
            Type:       types.ContentTypeAudio,
            SampleRate: 16000,
            Channels:   1,
        }),
    )
    if err != nil {
        log.Fatal(err)
    }
    defer conv.Close()

    // Start streaming
    audioIn, audioOut, err := conv.StartAudio(ctx)
    if err != nil {
        log.Fatal(err)
    }

    // Stream audio bidirectionally
    // ...
}
```

## Handling Audio Streams

### Sending Audio

```go
// Send audio chunks from microphone
go func() {
    for {
        chunk := readFromMicrophone() // Your audio capture code
        select {
        case audioIn <- chunk:
        case <-ctx.Done():
            return
        }
    }
}()
```

### Receiving Audio

```go
// Play audio output
go func() {
    for chunk := range audioOut {
        if chunk.Error != nil {
            log.Printf("Audio error: %v", chunk.Error)
            continue
        }
        playAudio(chunk.Data) // Your audio playback code
    }
}()
```

## Turn Detection

VAD mode uses turn detection to determine when the user has finished speaking.

### Default Behavior

The default turn detector uses silence duration:
- Waits for `SilenceDuration` of quiet
- Requires at least `MinSpeechDuration` of speech first
- Forces completion after `MaxTurnDuration`

### Custom Turn Detector

```go
import "github.com/AltairaLabs/PromptKit/runtime/audio"

// Create custom turn detector
detector := audio.NewSilenceDetector(audio.SilenceConfig{
    SilenceThreshold:  500 * time.Millisecond,
    MinSpeechDuration: 100 * time.Millisecond,
})

conv, _ := sdk.OpenDuplex("./voice-assistant.pack.json", "assistant",
    sdk.WithTurnDetector(detector),
    sdk.WithVADMode(sttService, ttsService, nil),
)
```

## Interruption Handling

Users can interrupt the assistant while it's speaking (barge-in).

```go
// The TTSStageWithInterruption stage handles this automatically
// When speech is detected during TTS output:
// 1. TTS playback stops
// 2. New user speech is processed
// 3. Assistant responds to the interruption
```

## Complete Example: Voice Interview

See the `sdk/examples/voice-interview/` directory for a complete working example that includes:
- Audio capture from microphone
- Real-time speech processing
- TTS audio playback
- Turn management
- Interruption handling

## Choosing Between VAD and ASM

| Consideration | VAD Mode | ASM Mode |
|---------------|----------|----------|
| LLM Support | Any text LLM | Gemini Live only |
| Latency | Higher (STT + TTS overhead) | Lower (native audio) |
| Flexibility | More control over STT/TTS | Less customization |
| Cost | Separate STT/TTS costs | Single API cost |
| Quality | Depends on STT/TTS providers | Native quality |

## Best Practices

1. **Sample Rate**: Use 16kHz for best compatibility
2. **Buffer Size**: Keep audio buffers small for low latency
3. **Error Handling**: Always handle audio errors gracefully
4. **Cleanup**: Close conversations properly to release resources
5. **Testing**: Test with various audio inputs and network conditions

## What's Next

- [Tutorial 8: TTS Integration](08-tts-integration) - Deep dive into TTS configuration
- [Tutorial 9: Variable Providers](09-variable-providers) - Dynamic context injection

## See Also

- [VADModeConfig Reference](../reference/audio) - Complete VAD configuration
- [TTS Service Reference](../reference/tts) - TTS providers and options
- [Voice Interview Example](https://github.com/AltairaLabs/PromptKit/tree/main/sdk/examples/voice-interview)
