---
title: "Tutorial 7: Audio Sessions"
sidebar:
  order: 7
verified:
  commit: 885cafa787a84bfba66f158fe51cda15a077ec23
  sources:
    - runtime/audio/silence.go
    - runtime/providers/gemini/stream_session_integration.go
    - runtime/providers/gemini/streaming_support.go
    - runtime/providers/openai/streaming_support.go
    - runtime/providers/streaming.go
    - runtime/providers/streaming_input.go
    - runtime/stt/openai.go
    - runtime/tts/openai.go
    - runtime/types/streaming.go
    - sdk/conversation.go
    - sdk/examples/openai-realtime/main.go
    - sdk/options.go
    - sdk/sdk.go
---
In this tutorial we build a voice-enabled conversation with real-time audio streaming. We set it up in two modes, [VAD](/glossary#vad) with [STT](/glossary#stt) and [TTS](/glossary#tts) services and [ASM](/glossary#asm) with a native audio LLM, then handle the audio streams, turn detection and interruptions.

## Prerequisites

- Completed [Tutorial 1: First Conversation](/sdk/tutorials/01-first-conversation/)
- Basic understanding of audio streaming concepts
- API keys for OpenAI or Google (for Gemini Live)

## Two modes for voice

PromptKit supports two modes for voice conversations:

| Mode | Description | Use Case |
|------|-------------|----------|
| **VAD** | Voice Activity Detection pipeline | Standard LLMs (GPT-4, Claude) with separate STT/TTS |
| **ASM** | Audio Streaming Model | Native audio LLMs (Gemini Live, OpenAI Realtime) |

## VAD mode: voice with text LLMs

VAD mode enables voice conversations with any text-based LLM by adding STT (speech-to-text) and TTS (text-to-speech) stages to the pipeline.

### Pipeline flow

```
Audio Input → [AudioTurn] → [STT] → [LLM] → [TTS] → Audio Output
```

### Step 1: Create the pack

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

### Step 2: Configure VAD mode

```go
package main

import (
    "context"
    "log"
    "os"

    "github.com/AltairaLabs/PromptKit/sdk/v2"
    "github.com/AltairaLabs/PromptKit/runtime/v2/stt"
    "github.com/AltairaLabs/PromptKit/runtime/v2/tts"
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

    // Set variables before the first input: a duplex session renders its
    // system prompt once, when the first chunk starts the pipeline.
    conv.SetVar("user_name", "Alice")

    // Send audio chunks and receive responses
    ctx := context.Background()

    // Receive streaming audio/text responses in a goroutine
    respCh, _ := conv.Response()
    go func() {
        for chunk := range respCh {
            // Handle audio or text output
            _ = chunk
        }
    }()

    // Send audio chunks from microphone
    // conv.SendChunk(ctx, chunk)
    // (See complete example for audio I/O implementation)
    _ = ctx
}
```

Run it. The session opens without an error and then waits for audio. Sending audio (see Handling audio streams) is what produces responses.

### Step 3: Customize the VAD configuration

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

The session now ends a turn after 500 ms of silence instead of the default 800 ms.

### VAD configuration options

| Option | Default | Description |
|--------|---------|-------------|
| `SilenceDuration` | 800ms | Silence required to detect turn end |
| `MinSpeechDuration` | 200ms | Minimum speech before turn can complete |
| `MaxTurnDuration` | 30s | Maximum turn length |
| `SampleRate` | 16000 | Audio sample rate in Hz |
| `Language` | "en" | Language hint for STT |
| `Voice` | "alloy" | TTS voice ID |
| `Speed` | 1.0 | TTS speech rate (0.5-2.0) |
| `OutputSampleRate` | TTS service's rate | Sample rate of the reply audio in Hz, resampled when it differs from the service's (for example 16000 for telephony) |

## ASM mode: native audio LLMs

ASM (Audio Streaming Model) mode is for LLMs with native bidirectional audio support: the Gemini Live API and OpenAI Realtime. Audio streams directly to and from the model without separate STT/TTS stages.

### Pipeline flow

```
Audio/Text Input → [DuplexProvider] → Audio/Text Output
```

### Step 1: Configure ASM mode

ASM mode needs a provider that streams audio to the model: Gemini Live or OpenAI Realtime. We set `GEMINI_API_KEY` or `OPENAI_API_KEY`, pick a model the provider's live API accepts, and describe the audio we send with `WithStreamingConfig`.

```go
package main

import (
    "log"

    "github.com/AltairaLabs/PromptKit/runtime/v2/providers"
    "github.com/AltairaLabs/PromptKit/runtime/v2/types"
    "github.com/AltairaLabs/PromptKit/sdk/v2"
)

func main() {
    // Open duplex conversation with ASM mode
    conv, err := sdk.OpenDuplex("./voice-assistant.pack.json", "assistant",
        sdk.WithModel("gemini-3.8-live"),
        sdk.WithStreamingConfig(&providers.StreamingInputConfig{
            Config: types.StreamingMediaConfig{
                Type:       types.ContentTypeAudio,
                SampleRate: 16000,
                Channels:   1,
            },
        }),
    )
    if err != nil {
        log.Fatal(err)
    }
    defer conv.Close()

    conv.SetVar("user_name", "Alice")

    // Receive streaming responses in a goroutine
    respCh, err := conv.Response()
    if err != nil {
        log.Fatal(err)
    }
    go func() {
        for chunk := range respCh {
            // Handle audio or text output
            _ = chunk
        }
    }()

    // Send audio chunks (see "Handling Audio Streams")
}
```

For OpenAI Realtime, use `sdk.WithModel("gpt-realtime")` and a 24000 Hz `SampleRate`. The `sdk/examples/openai-realtime/` example shows the full configuration.

Run it. The session opens against the live API and waits for audio, as in VAD mode.

## Handling audio streams

### Sending audio

We wrap each block of PCM bytes from our capture code in a `StreamChunk` and pass it to `SendChunk`:

```go
// Send audio chunks from microphone
go func() {
    for ctx.Err() == nil {
        pcm := readFromMicrophone() // Your audio capture code
        err := conv.SendChunk(ctx, &providers.StreamChunk{
            MediaData: &providers.StreamMediaData{
                MIMEType: "audio/pcm",
                Data:     pcm,
            },
        })
        if err != nil {
            log.Printf("Send error: %v", err)
            return
        }
    }
}()
```

### Receiving audio

Audio output arrives on the channel from `conv.Response()`:

```go
// Play audio output
for chunk := range respCh {
    if chunk.Error != nil {
        log.Printf("Audio error: %v", chunk.Error)
        continue
    }
    if chunk.MediaData != nil {
        playAudio(chunk.MediaData.Data) // Your audio playback code
    }
}
```

## Turn detection

VAD mode uses turn detection to determine when the user has finished speaking.

### Default behavior

The default turn detector uses silence duration:
- Waits for `SilenceDuration` of quiet
- Requires at least `MinSpeechDuration` of speech first
- Forces completion after `MaxTurnDuration`

### Custom turn detector

```go
import "github.com/AltairaLabs/PromptKit/runtime/v2/audio"

// Create custom turn detector with a silence threshold
detector := audio.NewSilenceDetector(500 * time.Millisecond)

conv, _ := sdk.OpenDuplex("./voice-assistant.pack.json", "assistant",
    sdk.WithTurnDetector(detector),
    sdk.WithVADMode(sttService, ttsService, nil),
)
```

## Interruption handling

Users can interrupt the assistant while it's speaking (barge-in).

In VAD mode, speech counts as barge-in from the moment the user's turn ends until
the reply's audio would have finished playing. That covers the model generating
the reply, synthesis, and playback, where the session assumes your player starts
each audio chunk as it arrives. The pipeline cancels whatever is still in flight
and drops the reply's remaining audio. Audio already delivered is in your
player's buffer, so the session tells you to flush it: a chunk on `Response()`
arrives with `Interrupted` set. The same signal arrives in ASM mode.

The speech that interrupted the reply is the user's next turn, and it is
transcribed in full. Speech that starts after the reply has finished playing is
an ordinary turn, with no `Interrupted` chunk.

Barge-in during playback means the microphone must not pick up the reply itself.
Use headphones, or capture audio with echo cancellation (browsers and phones
provide it).

```go
for chunk := range responses {
    if chunk.Interrupted {
        player.Flush() // stop playing the reply the user talked over
        continue
    }
    if chunk.MediaData != nil {
        player.Write(chunk.MediaData.Data)
    }
}
```

## Transcripts and turn boundaries

VAD and ASM mode mark turns on `Response()` the same way, through chunk metadata:

- `Metadata["input_transcription"]` (a string) carries what the user said, once per user turn. In VAD mode it is the STT stage's transcript.
- `Metadata["assistant_turn_complete"]` (a bool) marks the end of each assistant turn. In VAD mode that includes a reply cut short by barge-in, whose boundary arrives before its `Interrupted` chunk. A reply interrupted before the model produced any of it has no assistant turn to close, so only the `Interrupted` chunk arrives.
- `Metadata["finish_reason"]` (a string), on the same chunk, says why the assistant turn ended, in the same values as `Message.FinishReason` on a text turn: `stop`, `max_output_tokens`, `safety`, `interrupted` and so on. It is absent when the provider reported no reason.

None of these sets `FinishReason`, so a loop over `Response()` keeps reading across turns.

```go
for chunk := range responses {
    if t, ok := chunk.Metadata["input_transcription"].(string); ok {
        fmt.Println("you:", t)
    }
    if chunk.Delta != "" {
        fmt.Print(chunk.Delta)
    }
    if done, _ := chunk.Metadata["assistant_turn_complete"].(bool); done {
        fmt.Println()
    }
}
```

## Complete example: voice interview

See the `sdk/examples/voice-interview/` directory for a complete working example that includes:
- Audio capture from microphone
- Real-time speech processing
- TTS audio playback
- Turn management
- Interruption handling

## Choosing between VAD and ASM

| Consideration | VAD mode | ASM mode |
|---------------|----------|----------|
| LLM Support | Any text LLM | Gemini Live, OpenAI Realtime |
| Latency | Higher (STT + TTS overhead) | Lower (native audio) |
| Flexibility | More control over STT/TTS | Less customization |
| Cost | Separate STT/TTS costs | Single API cost |
| Quality | Depends on STT/TTS providers | Native quality |

## Next steps

- [Tutorial 8: TTS Integration](/sdk/tutorials/08-tts-integration/) - Deep dive into TTS configuration
- [Tutorial 9: Variable Providers](/sdk/tutorials/09-variable-providers/) - Dynamic context injection

## See Also

- [VADModeConfig Reference](/runtime/reference/audio/) - Complete VAD configuration
- [TTS Service Reference](/runtime/reference/tts/) - TTS providers and options
- [Voice Interview Example](https://github.com/AltairaLabs/PromptKit/tree/main/sdk/examples/voice-interview)
