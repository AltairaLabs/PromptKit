---
title: "Tutorial 8: TTS Integration"
sidebar:
  order: 8
verified:
  commit: 885cafa787a84bfba66f158fe51cda15a077ec23
  sources:
    - runtime/tts/cartesia.go
    - runtime/tts/cartesia_interactive.go
    - runtime/tts/elevenlabs.go
    - runtime/tts/errors.go
    - runtime/tts/openai.go
    - runtime/tts/service.go
---
In this tutorial we add text-to-speech to a conversation. We set up a TTS provider (OpenAI, ElevenLabs or Cartesia), choose a voice, speed and audio format, synthesize speech in one shot and as a stream, and wire the service into a conversation.

## Prerequisites

- Completed [Tutorial 7: Audio Sessions](/sdk/tutorials/07-audio-sessions/)
- API key for a TTS provider

## The TTS service interface

All TTS providers implement the same interface:

```go
type Service interface {
    Name() string
    Synthesize(ctx context.Context, text string, config SynthesisConfig) (io.ReadCloser, error)
    SupportedVoices() []Voice
    SupportedFormats() []AudioFormat
}
```

## Step 1: Set up a TTS provider

### OpenAI TTS

```go
import "github.com/AltairaLabs/PromptKit/runtime/v2/tts"

// Create OpenAI TTS service
ttsService := tts.NewOpenAI(os.Getenv("OPENAI_API_KEY"))

// Available voices: alloy, echo, fable, onyx, nova, shimmer
// Available models: tts-1 (fast), tts-1-hd (high quality), gpt-4o-mini-tts (expressive)
```

The service is ready to use; no network call happens until the first `Synthesize`.

### ElevenLabs TTS

```go
import "github.com/AltairaLabs/PromptKit/runtime/v2/tts"

// Create ElevenLabs TTS service
ttsService := tts.NewElevenLabs(os.Getenv("ELEVENLABS_API_KEY"))

// Wide variety of voices available
// Check SupportedVoices() for options
```

### Cartesia TTS

```go
import "github.com/AltairaLabs/PromptKit/runtime/v2/tts"

// Create Cartesia TTS service
ttsService := tts.NewCartesia(os.Getenv("CARTESIA_API_KEY"))

// Supports interactive streaming mode for low latency
```

## Step 2: Configure synthesis

```go
config := tts.SynthesisConfig{
    Voice:    "nova",      // Voice ID
    Format:   tts.FormatMP3, // Output format
    Speed:    1.0,         // Speech rate (0.25-4.0)
    Pitch:    0,           // Pitch adjustment (-20 to 20)
    Language: "en-US",     // Language code
    Model:    "tts-1-hd",  // Model (provider-specific)
}
```

### Available formats

| Format | Constant | Use Case |
|--------|----------|----------|
| MP3 | `tts.FormatMP3` | Most compatible |
| Opus | `tts.FormatOpus` | Best for streaming |
| AAC | `tts.FormatAAC` | Apple devices |
| FLAC | `tts.FormatFLAC` | Lossless quality |
| PCM | `tts.FormatPCM16` | Raw audio processing |
| WAV | `tts.FormatWAV` | PCM with header |

## Step 3: Synthesize speech

### Single-shot synthesis

```go
ctx := context.Background()

config := tts.SynthesisConfig{
    Voice:  "alloy",
    Format: tts.FormatMP3,
    Speed:  1.0,
}

// Synthesize text to audio
reader, err := ttsService.Synthesize(ctx, "Hello, how can I help you?", config)
if err != nil {
    log.Fatal(err)
}
defer reader.Close()

// Read audio data
audioData, _ := io.ReadAll(reader)
// Play or save audioData...
```

`audioData` holds the complete MP3 for the sentence. Writing it to a `.mp3` file with `os.WriteFile` gives a clip we can play.

### Streaming synthesis

For lower latency, use streaming synthesis (if supported):

```go
// Check if provider supports streaming (Cartesia does)
var ttsService tts.Service = tts.NewCartesia(os.Getenv("CARTESIA_API_KEY"))
streamingService, ok := ttsService.(tts.StreamingService)
if !ok {
    log.Fatal("Provider doesn't support streaming")
}

// Start streaming synthesis
chunks, err := streamingService.SynthesizeStream(ctx, "Hello, how can I help you?", config)
if err != nil {
    log.Fatal(err)
}

// Process chunks as they arrive
for chunk := range chunks {
    if chunk.Error != nil {
        log.Printf("Error: %v", chunk.Error)
        break
    }
    playAudioChunk(chunk.Data)
    if chunk.Final {
        break
    }
}
```

Audio chunks arrive and play while the rest of the sentence is still being synthesized; the loop ends on the final chunk.

## Step 4: Integrate with conversations

### VAD mode with TTS

```go
sttService := stt.NewOpenAI(os.Getenv("OPENAI_API_KEY"))
ttsService := tts.NewOpenAI(os.Getenv("OPENAI_API_KEY"))

// Configure VAD mode with custom TTS settings
vadConfig := &sdk.VADModeConfig{
    Voice: "nova",  // Use Nova voice
    Speed: 1.1,     // Slightly faster
}

conv, _ := sdk.OpenDuplex("./assistant.pack.json", "voice",
    sdk.WithVADMode(sttService, ttsService, vadConfig),
)
```

### Manual TTS in text mode

```go
// Open text conversation
conv, _ := sdk.Open("./assistant.pack.json", "chat")

// Send message and get response
resp, _ := conv.Send(ctx, "Tell me a joke")

// Synthesize the response
ttsService := tts.NewOpenAI(os.Getenv("OPENAI_API_KEY"))
reader, _ := ttsService.Synthesize(ctx, resp.Text(), tts.DefaultSynthesisConfig())
defer reader.Close()

// Play the audio
audioData, _ := io.ReadAll(reader)
playAudio(audioData)
```

The model's joke plays aloud.

## Step 5: Choose a voice

### Listing available voices

```go
voices := ttsService.SupportedVoices()
for _, voice := range voices {
    fmt.Printf("%s: %s (%s, %s)\n",
        voice.ID,
        voice.Name,
        voice.Language,
        voice.Gender,
    )
}
```

### Voice characteristics (OpenAI)

| Voice | Character |
|-------|-----------|
| alloy | Balanced, versatile |
| echo | Clear male voice |
| fable | Expressive, British |
| onyx | Deep, authoritative |
| nova | Warm, friendly |
| shimmer | Soft, calm |

Listing the voices prints one line per voice with its ID, name, language and gender.

## Step 6: Handle errors

```go
reader, err := ttsService.Synthesize(ctx, text, config)
if err != nil {
    switch {
    case errors.Is(err, tts.ErrInvalidVoice):
        log.Printf("Voice '%s' not supported", config.Voice)
    case errors.Is(err, tts.ErrRateLimited):
        log.Printf("Rate limited, retrying...")
        time.Sleep(time.Second)
        // Retry...
    case errors.Is(err, tts.ErrQuotaExceeded):
        log.Printf("Quota exceeded")
    default:
        log.Printf("Synthesis failed: %v", err)
    }
    return
}
```

An unsupported voice now logs `Voice '...' not supported` instead of a generic failure.

## Step 7: Cache synthesized audio

For repeated phrases, we cache the synthesized audio:

```go
var cache = make(map[string][]byte)

func synthesizeWithCache(text string, config tts.SynthesisConfig) ([]byte, error) {
    key := text + config.Voice + config.Format.Name
    if cached, ok := cache[key]; ok {
        return cached, nil
    }

    reader, err := ttsService.Synthesize(ctx, text, config)
    if err != nil {
        return nil, err
    }
    defer reader.Close()

    data, err := io.ReadAll(reader)
    if err != nil {
        return nil, err
    }

    cache[key] = data
    return data, nil
}
```

The second call with the same text, voice and format returns the cached bytes without a request to the provider.

### Pre-synthesis

For common responses, we synthesize in advance:

```go
greetings := []string{
    "Hello! How can I help you today?",
    "I'm sorry, I didn't catch that.",
    "Is there anything else I can help with?",
}

for _, text := range greetings {
    reader, _ := ttsService.Synthesize(ctx, text, config)
    data, _ := io.ReadAll(reader)
    reader.Close()
    cache[text] = data
}
```

## Next steps

- [Tutorial 9: Variable Providers](/sdk/tutorials/09-variable-providers/) - Dynamic context injection

## See Also

- [TTS API Reference](/runtime/reference/tts/) - Complete API documentation
- [Audio Sessions Tutorial](/sdk/tutorials/07-audio-sessions/) - Full voice integration
