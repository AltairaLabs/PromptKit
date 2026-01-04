---
title: "Tutorial 8: TTS Integration"
sidebar:
  order: 8
---
Add text-to-speech capabilities to your conversations.

## What You'll Learn

- Setting up TTS providers (OpenAI, ElevenLabs, Cartesia)
- Configuring voice, speed, and audio formats
- Streaming vs single-shot synthesis
- Integrating TTS with conversations

## Prerequisites

- Completed [Tutorial 7: Audio Sessions](07-audio-sessions)
- API key for a TTS provider

## TTS Service Interface

All TTS providers implement the same interface:

```go
type Service interface {
    Name() string
    Synthesize(ctx context.Context, text string, config SynthesisConfig) (io.ReadCloser, error)
    SupportedVoices() []Voice
    SupportedFormats() []AudioFormat
}
```

## Setting Up TTS Providers

### OpenAI TTS

```go
import "github.com/AltairaLabs/PromptKit/runtime/tts"

// Create OpenAI TTS service
ttsService := tts.NewOpenAI(os.Getenv("OPENAI_API_KEY"))

// Available voices: alloy, echo, fable, onyx, nova, shimmer
// Available models: tts-1 (fast), tts-1-hd (high quality)
```

### ElevenLabs TTS

```go
import "github.com/AltairaLabs/PromptKit/runtime/tts"

// Create ElevenLabs TTS service
ttsService := tts.NewElevenLabs(os.Getenv("ELEVENLABS_API_KEY"))

// Wide variety of voices available
// Check SupportedVoices() for options
```

### Cartesia TTS

```go
import "github.com/AltairaLabs/PromptKit/runtime/tts"

// Create Cartesia TTS service
ttsService := tts.NewCartesia(os.Getenv("CARTESIA_API_KEY"))

// Supports interactive streaming mode for low latency
```

## Synthesis Configuration

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

### Available Formats

| Format | Constant | Use Case |
|--------|----------|----------|
| MP3 | `tts.FormatMP3` | Most compatible |
| Opus | `tts.FormatOpus` | Best for streaming |
| AAC | `tts.FormatAAC` | Apple devices |
| FLAC | `tts.FormatFLAC` | Lossless quality |
| PCM | `tts.FormatPCM16` | Raw audio processing |
| WAV | `tts.FormatWAV` | PCM with header |

## Basic Synthesis

### Single-Shot Synthesis

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

### Streaming Synthesis

For lower latency, use streaming synthesis (if supported):

```go
// Check if provider supports streaming
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

## Integrating with Conversations

### VAD Mode with TTS

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

### Manual TTS in Text Mode

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

## Voice Selection

### Listing Available Voices

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

### Voice Characteristics (OpenAI)

| Voice | Character |
|-------|-----------|
| alloy | Neutral, versatile |
| echo | Warm, smooth |
| fable | Expressive, British |
| onyx | Deep, authoritative |
| nova | Friendly, youthful |
| shimmer | Clear, professional |

## Error Handling

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
    case errors.Is(err, tts.ErrTextTooLong):
        log.Printf("Text exceeds maximum length")
    default:
        log.Printf("Synthesis failed: %v", err)
    }
    return
}
```

## Performance Optimization

### Caching

For repeated phrases, cache synthesized audio:

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

### Pre-synthesis

For common responses, synthesize in advance:

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

## Best Practices

1. **Voice Consistency**: Use the same voice throughout a conversation
2. **Speed Adjustment**: Slower for complex info, faster for casual chat
3. **Format Selection**: Use Opus for streaming, MP3 for storage
4. **Error Handling**: Gracefully handle synthesis failures
5. **Resource Cleanup**: Always close readers when done

## Cost Considerations

| Provider | Pricing Model |
|----------|---------------|
| OpenAI | Per character |
| ElevenLabs | Per character (tiers) |
| Cartesia | Per character |

Estimate costs before production deployment.

## What's Next

- [Tutorial 9: Variable Providers](09-variable-providers) - Dynamic context injection

## See Also

- [TTS API Reference](../reference/tts) - Complete API documentation
- [Audio Sessions Tutorial](07-audio-sessions) - Full voice integration
