---
title: Audio API Reference
sidebar:
  order: 3
---
Complete reference for audio and voice conversation APIs.

## VADModeConfig

Configuration for VAD (Voice Activity Detection) mode.

```go
type VADModeConfig struct {
    SilenceDuration   time.Duration
    MinSpeechDuration time.Duration
    MaxTurnDuration   time.Duration
    SampleRate        int
    Language          string
    Voice             string
    Speed             float64
}
```

### Fields

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `SilenceDuration` | `time.Duration` | 800ms | Silence required to detect turn end |
| `MinSpeechDuration` | `time.Duration` | 200ms | Minimum speech before turn can complete |
| `MaxTurnDuration` | `time.Duration` | 30s | Maximum turn length before forcing completion |
| `SampleRate` | `int` | 16000 | Audio sample rate in Hz |
| `Language` | `string` | "en" | Language hint for STT (e.g., "en", "es", "fr") |
| `Voice` | `string` | "alloy" | TTS voice ID |
| `Speed` | `float64` | 1.0 | TTS speech rate (0.5-2.0) |

### Constructor

```go
func DefaultVADModeConfig() *VADModeConfig
```

Returns a VADModeConfig with sensible defaults.

## SDK Options

### WithVADMode

```go
func WithVADMode(sttService stt.Service, ttsService tts.Service, cfg *VADModeConfig) Option
```

Configures VAD mode for voice conversations with standard text-based LLMs.

**Parameters:**
- `sttService`: Speech-to-text service
- `ttsService`: Text-to-speech service
- `cfg`: VAD configuration (nil uses defaults)

**Example:**

```go
sttService := stt.NewOpenAI(os.Getenv("OPENAI_API_KEY"))
ttsService := tts.NewOpenAI(os.Getenv("OPENAI_API_KEY"))

conv, _ := sdk.OpenDuplex("./pack.json", "voice",
    sdk.WithVADMode(sttService, ttsService, nil),
)
```

### WithStreamingConfig

```go
func WithStreamingConfig(config *providers.StreamingInputConfig) Option
```

Configures ASM (Audio Streaming Model) mode for native multimodal LLMs.

**Parameters:**
- `config`: Streaming configuration with audio type, sample rate, channels

**Example:**

```go
conv, _ := sdk.OpenDuplex("./pack.json", "voice",
    sdk.WithStreamingConfig(&providers.StreamingInputConfig{
        Type:       types.ContentTypeAudio,
        SampleRate: 16000,
        Channels:   1,
    }),
)
```

### WithTurnDetector

```go
func WithTurnDetector(detector audio.TurnDetector) Option
```

Configures a custom turn detector for audio sessions.

**Example:**

```go
detector := audio.NewSilenceDetector(audio.SilenceConfig{
    SilenceThreshold:  500 * time.Millisecond,
    MinSpeechDuration: 100 * time.Millisecond,
})

conv, _ := sdk.OpenDuplex("./pack.json", "voice",
    sdk.WithTurnDetector(detector),
    sdk.WithVADMode(sttService, ttsService, nil),
)
```

## TurnDetector Interface

```go
type TurnDetector interface {
    // ProcessAudio processes an audio chunk and returns turn state
    ProcessAudio(samples []byte, sampleRate int) TurnState

    // Reset resets the detector state
    Reset()
}

type TurnState int

const (
    TurnStateListening TurnState = iota
    TurnStateSpeaking
    TurnStateComplete
)
```

## StreamingInputConfig

Configuration for ASM mode streaming.

```go
type StreamingInputConfig struct {
    Type       types.ContentType // Audio, video, or text
    SampleRate int               // Audio sample rate (e.g., 16000)
    Channels   int               // Number of audio channels (1=mono, 2=stereo)
}
```

## Audio Pipeline Stages

### AudioTurnStage

VAD-based turn detection and audio accumulation.

```go
config := stage.AudioTurnConfig{
    SilenceDuration:      800 * time.Millisecond,
    MinSpeechDuration:    200 * time.Millisecond,
    MaxTurnDuration:      30 * time.Second,
    SampleRate:           16000,
    InterruptionHandler:  handler,
}

turnStage, _ := stage.NewAudioTurnStage(config)
```

### STTStage

Speech-to-text transcription stage.

```go
config := stage.STTStageConfig{
    Language:      "en",
    SkipEmpty:     true,
    MinAudioBytes: 1600, // 50ms at 16kHz
}

sttStage := stage.NewSTTStage(sttService, config)
```

### TTSStageWithInterruption

Text-to-speech with barge-in support.

```go
config := stage.TTSConfig{
    Voice: "alloy",
    Speed: 1.0,
}

ttsStage := stage.NewTTSStageWithInterruption(ttsService, handler, config)
```

### DuplexProviderStage

Bidirectional streaming for native audio LLMs.

```go
duplexStage := stage.NewDuplexProviderStage(session)
```

## AudioData Type

```go
type AudioData struct {
    Samples    []byte      // Raw audio bytes
    SampleRate int         // Sample rate in Hz
    Channels   int         // Number of channels
    Format     AudioFormat // Audio format (PCM16, etc.)
}
```

### Audio Formats

| Constant | Description |
|----------|-------------|
| `AudioFormatPCM16` | 16-bit signed PCM |
| `AudioFormatPCM32` | 32-bit signed PCM |
| `AudioFormatFloat32` | 32-bit float PCM |

## InterruptionHandler

Coordinates interruption detection between AudioTurnStage and TTSStage.

```go
handler := audio.NewInterruptionHandler()

// Used internally by VAD pipeline
// Automatically handles barge-in detection
```

## Error Types

```go
var (
    ErrAudioSessionClosed = errors.New("audio session closed")
    ErrInvalidSampleRate  = errors.New("invalid sample rate")
    ErrInvalidChannels    = errors.New("invalid channel count")
)
```

## See Also

- [Audio Sessions Tutorial](../tutorials/07-audio-sessions) - Getting started
- [TTS Reference](tts) - Text-to-speech API
- [Pipeline Reference](/runtime/reference/pipeline) - Stage documentation
