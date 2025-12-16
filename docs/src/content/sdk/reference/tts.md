---
title: TTS API Reference
docType: reference
order: 4
---
# TTS API Reference

Complete reference for text-to-speech services.

## Service Interface

```go
type Service interface {
    Name() string
    Synthesize(ctx context.Context, text string, config SynthesisConfig) (io.ReadCloser, error)
    SupportedVoices() []Voice
    SupportedFormats() []AudioFormat
}
```

### Methods

#### Name

```go
func (s Service) Name() string
```

Returns the provider identifier (e.g., "openai", "elevenlabs").

#### Synthesize

```go
func (s Service) Synthesize(ctx context.Context, text string, config SynthesisConfig) (io.ReadCloser, error)
```

Converts text to audio. Returns a reader for streaming audio data. The caller is responsible for closing the reader.

#### SupportedVoices

```go
func (s Service) SupportedVoices() []Voice
```

Returns available voices for this provider.

#### SupportedFormats

```go
func (s Service) SupportedFormats() []AudioFormat
```

Returns supported audio output formats.

## StreamingService Interface

```go
type StreamingService interface {
    Service
    SynthesizeStream(ctx context.Context, text string, config SynthesisConfig) (<-chan AudioChunk, error)
}
```

Extends Service with streaming synthesis capabilities for lower latency.

## SynthesisConfig

```go
type SynthesisConfig struct {
    Voice    string      // Voice ID
    Format   AudioFormat // Output format
    Speed    float64     // Speech rate (0.25-4.0)
    Pitch    float64     // Pitch adjustment (-20 to 20)
    Language string      // Language code
    Model    string      // TTS model (provider-specific)
}
```

### Fields

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `Voice` | `string` | "alloy" | Voice ID (provider-specific) |
| `Format` | `AudioFormat` | MP3 | Output audio format |
| `Speed` | `float64` | 1.0 | Speech rate multiplier |
| `Pitch` | `float64` | 0 | Pitch adjustment in semitones |
| `Language` | `string` | "" | Language code (e.g., "en-US") |
| `Model` | `string` | "" | TTS model (e.g., "tts-1-hd") |

### Constructor

```go
func DefaultSynthesisConfig() SynthesisConfig
```

Returns sensible defaults for synthesis.

## Voice Type

```go
type Voice struct {
    ID          string // Provider-specific identifier
    Name        string // Human-readable name
    Language    string // Primary language code
    Gender      string // "male", "female", "neutral"
    Description string // Voice characteristics
    Preview     string // URL to voice sample
}
```

## AudioFormat Type

```go
type AudioFormat struct {
    Name       string // Format identifier
    MIMEType   string // Content type
    SampleRate int    // Sample rate in Hz
    BitDepth   int    // Bits per sample
    Channels   int    // Number of channels
}
```

### Predefined Formats

| Constant | Name | MIME Type | Use Case |
|----------|------|-----------|----------|
| `FormatMP3` | mp3 | audio/mpeg | Most compatible |
| `FormatOpus` | opus | audio/opus | Best for streaming |
| `FormatAAC` | aac | audio/aac | Apple devices |
| `FormatFLAC` | flac | audio/flac | Lossless quality |
| `FormatPCM16` | pcm | audio/pcm | Raw processing |
| `FormatWAV` | wav | audio/wav | PCM with header |

## AudioChunk Type

```go
type AudioChunk struct {
    Data  []byte // Raw audio bytes
    Index int    // Chunk sequence number
    Final bool   // Last chunk indicator
    Error error  // Error during synthesis
}
```

## Providers

### OpenAI TTS

```go
func NewOpenAI(apiKey string) Service
```

Creates an OpenAI TTS service.

**Voices:**

| ID | Character |
|----|-----------|
| alloy | Neutral, versatile |
| echo | Warm, smooth |
| fable | Expressive, British |
| onyx | Deep, authoritative |
| nova | Friendly, youthful |
| shimmer | Clear, professional |

**Models:**
- `tts-1`: Fast, optimized for real-time
- `tts-1-hd`: High quality, longer latency

**Example:**

```go
service := tts.NewOpenAI(os.Getenv("OPENAI_API_KEY"))

config := tts.SynthesisConfig{
    Voice:  "nova",
    Format: tts.FormatMP3,
    Model:  "tts-1-hd",
}

reader, _ := service.Synthesize(ctx, "Hello world", config)
```

### ElevenLabs TTS

```go
func NewElevenLabs(apiKey string) Service
```

Creates an ElevenLabs TTS service.

**Features:**
- Wide variety of voices
- Voice cloning support
- Multilingual support

**Example:**

```go
service := tts.NewElevenLabs(os.Getenv("ELEVENLABS_API_KEY"))

// List available voices
voices := service.SupportedVoices()
for _, v := range voices {
    fmt.Printf("%s: %s\n", v.ID, v.Name)
}
```

### Cartesia TTS

```go
func NewCartesia(apiKey string) Service
```

Creates a Cartesia TTS service.

**Features:**
- Ultra-low latency
- Interactive streaming mode
- Emotion control

**Example:**

```go
service := tts.NewCartesia(os.Getenv("CARTESIA_API_KEY"))
```

## Error Types

```go
var (
    ErrInvalidVoice    = errors.New("invalid voice")
    ErrInvalidFormat   = errors.New("unsupported format")
    ErrTextTooLong     = errors.New("text exceeds maximum length")
    ErrRateLimited     = errors.New("rate limited")
    ErrServiceDown     = errors.New("service unavailable")
)
```

## Usage Examples

### Basic Synthesis

```go
service := tts.NewOpenAI(apiKey)

reader, err := service.Synthesize(ctx, "Hello!", tts.DefaultSynthesisConfig())
if err != nil {
    log.Fatal(err)
}
defer reader.Close()

data, _ := io.ReadAll(reader)
// Use audio data...
```

### Streaming Synthesis

```go
service := tts.NewCartesia(apiKey)

streamingService, ok := service.(tts.StreamingService)
if !ok {
    log.Fatal("Provider doesn't support streaming")
}

chunks, err := streamingService.SynthesizeStream(ctx, "Hello world!", config)
if err != nil {
    log.Fatal(err)
}

for chunk := range chunks {
    if chunk.Error != nil {
        log.Printf("Error: %v", chunk.Error)
        break
    }
    playAudio(chunk.Data)
}
```

### Custom Configuration

```go
config := tts.SynthesisConfig{
    Voice:    "onyx",
    Format:   tts.FormatOpus,
    Speed:    0.9,           // Slightly slower
    Pitch:    -2,            // Slightly lower
    Language: "en-US",
    Model:    "tts-1-hd",
}

reader, _ := service.Synthesize(ctx, text, config)
```

## See Also

- [TTS Tutorial](../tutorials/08-tts-integration) - Getting started
- [Audio Reference](audio) - Audio session API
- [VAD Mode](audio#vadmodeconfig) - Voice activity detection
