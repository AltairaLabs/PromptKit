---
title: Duplex Configuration Reference
docType: reference
order: 7
---
# Duplex Configuration Reference

Complete reference for configuring duplex (bidirectional) streaming scenarios in PromptArena.

## Overview

Duplex mode enables real-time bidirectional audio streaming for testing voice assistants and conversational AI. When enabled, audio is streamed in chunks and turn boundaries are detected dynamically.

**Requires**: Gemini Live API (provider type: `gemini`, model: `gemini-2.0-flash-exp` or similar)

---

## Scenario Configuration

Enable duplex mode by adding the `duplex` field to your scenario spec:

```yaml
apiVersion: promptkit.altairalabs.ai/v1alpha1
kind: Scenario
metadata:
  name: voice-assistant-test
spec:
  id: voice-assistant-test
  task_type: voice-assistant
  streaming: true  # Required for duplex

  duplex:
    timeout: "5m"
    turn_detection:
      mode: asm
    resilience:
      max_retries: 2
      partial_success_min_turns: 2
```

---

## DuplexConfig

The main duplex configuration object.

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `timeout` | string | `"10m"` | Maximum session duration (Go duration format) |
| `turn_detection` | [TurnDetectionConfig](#turndetectionconfig) | `mode: asm` | Turn boundary detection settings |
| `resilience` | [DuplexResilienceConfig](#duplexresilienceconfig) | See below | Error handling and retry behavior |

### Example

```yaml
duplex:
  timeout: "5m30s"
  turn_detection:
    mode: vad
    vad:
      silence_threshold_ms: 600
      min_speech_ms: 200
  resilience:
    max_retries: 2
    inter_turn_delay_ms: 500
```

---

## TurnDetectionConfig

Configures how turn boundaries are detected during duplex streaming.

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `mode` | string | `"asm"` | Detection mode: `"vad"` or `"asm"` |
| `vad` | [VADConfig](#vadconfig) | - | Voice activity detection settings (when mode is `vad`) |

### Turn Detection Modes

| Mode | Name | Description |
|------|------|-------------|
| `asm` | Provider-Native | The provider (Gemini) handles turn detection internally using its automatic speech detection |
| `vad` | Voice Activity Detection | Client-side VAD with configurable silence thresholds |

### ASM Mode (Provider-Native)

```yaml
duplex:
  turn_detection:
    mode: asm
```

**Best for**: Simple tests, trusting provider behavior, less configuration.

**How it works**: The Gemini Live API automatically detects when the speaker stops talking and triggers a response.

### VAD Mode (Client-Side)

```yaml
duplex:
  turn_detection:
    mode: vad
    vad:
      silence_threshold_ms: 600
      min_speech_ms: 200
      max_turn_duration_s: 60
```

**Best for**: Precise control over turn boundaries, testing interruption handling, consistent behavior across providers.

---

## VADConfig

Voice Activity Detection configuration (used when `turn_detection.mode` is `"vad"`).

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `silence_threshold_ms` | int | `500` | Silence duration (ms) to trigger turn end |
| `min_speech_ms` | int | `1000` | Minimum speech duration before silence counts |
| `max_turn_duration_s` | int | `60` | Force turn end after this duration (seconds) |

### Example

```yaml
duplex:
  turn_detection:
    mode: vad
    vad:
      silence_threshold_ms: 800   # Longer silence for natural speech pauses
      min_speech_ms: 300          # Short utterances still count
      max_turn_duration_s: 30     # Limit long turns
```

### Tuning Guidelines

| Scenario | silence_threshold_ms | min_speech_ms |
|----------|---------------------|---------------|
| Quick responses | 400-500 | 150-200 |
| Natural conversation | 600-800 | 200-300 |
| TTS with pauses | 1000-1500 | 500-800 |
| Slow/deliberate speech | 1200-2000 | 800-1000 |

---

## DuplexResilienceConfig

Error handling and retry behavior for duplex sessions.

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `max_retries` | int | `0` | Retry attempts for failed turns |
| `retry_delay_ms` | int | `1000` | Delay between retries (ms) |
| `inter_turn_delay_ms` | int | `500` | Delay between turns (ms) |
| `selfplay_inter_turn_delay_ms` | int | `1000` | Delay after self-play turns (ms) |
| `partial_success_min_turns` | int | `1` | Minimum completed turns for partial success |
| `ignore_last_turn_session_end` | bool | `true` | Treat session end on final turn as success |

### Example

```yaml
duplex:
  resilience:
    max_retries: 2
    retry_delay_ms: 2000
    inter_turn_delay_ms: 500
    selfplay_inter_turn_delay_ms: 1500
    partial_success_min_turns: 3
    ignore_last_turn_session_end: true
```

### Partial Success

When `partial_success_min_turns` is set, sessions that end unexpectedly after completing at least that many turns are treated as successful:

```yaml
resilience:
  partial_success_min_turns: 2  # Accept if 2+ turns complete
```

This is useful for exploratory testing where completing all turns isn't critical.

### Session End Handling

By default, if the session ends on the final expected turn, it's treated as success:

```yaml
resilience:
  ignore_last_turn_session_end: true   # Default
```

Set to `false` if you need the final turn to complete normally without session termination.

---

## TTSConfig

Text-to-speech configuration for self-play audio generation.

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `provider` | string | Yes | TTS provider: `"openai"`, `"elevenlabs"`, `"cartesia"`, `"mock"` |
| `voice` | string | Yes* | Voice ID for synthesis (*optional for mock with audio_files) |
| `audio_files` | []string | No | PCM audio files for mock provider (rotated through) |
| `sample_rate` | int | No | Output sample rate in Hz (default: 24000) |

### Example: OpenAI TTS

```yaml
turns:
  - role: selfplay-user
    persona: curious-customer
    turns: 3
    tts:
      provider: openai
      voice: alloy
```

### Example: Mock TTS with Pre-recorded Audio

```yaml
turns:
  - role: selfplay-user
    persona: test-persona
    turns: 3
    tts:
      provider: mock
      audio_files:
        - audio/question1.pcm
        - audio/question2.pcm
        - audio/question3.pcm
      sample_rate: 16000  # Match your file sample rate
```

### Available OpenAI Voices

| Voice | Description |
|-------|-------------|
| `alloy` | Neutral, balanced |
| `echo` | Warm, engaging |
| `fable` | Expressive, dynamic |
| `onyx` | Deep, authoritative |
| `nova` | Friendly, conversational |
| `shimmer` | Clear, professional |

---

## Audio Turn Parts

In duplex scenarios, user turns contain audio parts instead of text:

```yaml
turns:
  - role: user
    parts:
      - type: audio
        media:
          file_path: audio/greeting.pcm
          mime_type: audio/L16
```

### Audio Requirements

| Parameter | Value | Description |
|-----------|-------|-------------|
| Format | Raw PCM | No headers (not WAV) |
| Sample Rate | 16000 Hz | Required by Gemini Live API |
| Bit Depth | 16-bit | Signed integer |
| Channels | Mono | Single channel |
| MIME Type | `audio/L16` | Linear PCM |

### Converting Audio Files

```bash
# WAV to PCM
ffmpeg -i input.wav -f s16le -ar 16000 -ac 1 output.pcm

# MP3 to PCM
ffmpeg -i input.mp3 -f s16le -ar 16000 -ac 1 output.pcm

# Verify format
ffprobe -show_format -show_streams output.pcm
```

---

## Provider Configuration

Duplex requires a Gemini provider with streaming enabled:

```yaml
# providers/gemini-live.provider.yaml
apiVersion: promptkit.altairalabs.ai/v1alpha1
kind: Provider
metadata:
  name: gemini-live

spec:
  id: gemini-live
  type: gemini
  model: gemini-2.0-flash-exp

  defaults:
    temperature: 0.7
    max_tokens: 1000

  # Gemini-specific configuration
  additional_config:
    audio_enabled: true
    response_modalities:
      - AUDIO   # Returns audio + text transcription
```

### Response Modalities

| Modality | Description |
|----------|-------------|
| `AUDIO` | Returns audio response with text transcription |
| `TEXT` | Returns text-only response (no audio) |

**Note**: Gemini Live API supports only ONE modality at a time. `AUDIO` mode includes text transcription via `outputAudioTranscription`.

---

## Complete Scenario Example

```yaml
apiVersion: promptkit.altairalabs.ai/v1alpha1
kind: Scenario
metadata:
  name: voice-assistant-comprehensive

spec:
  id: voice-assistant-comprehensive
  task_type: voice-assistant
  description: "Full duplex voice assistant test with self-play"
  streaming: true

  duplex:
    timeout: "5m"
    turn_detection:
      mode: vad
      vad:
        silence_threshold_ms: 800
        min_speech_ms: 250
        max_turn_duration_s: 45
    resilience:
      max_retries: 2
      retry_delay_ms: 2000
      inter_turn_delay_ms: 500
      selfplay_inter_turn_delay_ms: 1200
      partial_success_min_turns: 3
      ignore_last_turn_session_end: true

  turns:
    # Initial audio greeting
    - role: user
      parts:
        - type: audio
          media:
            file_path: audio/greeting.pcm
            mime_type: audio/L16
      assertions:
        - type: content_matches
          params:
            pattern: "(?i)(hello|hi|welcome)"

    # Self-play generates follow-up questions
    - role: selfplay-user
      persona: curious-customer
      turns: 3
      tts:
        provider: openai
        voice: nova
      assertions:
        - type: content_matches
          params:
            pattern: ".{20,}"  # At least 20 chars

  conversation_assertions:
    - type: content_includes_any
      params:
        patterns:
          - "help"
          - "assist"
          - "support"
```

---

## Validation Errors

Common configuration errors and solutions:

| Error | Cause | Solution |
|-------|-------|----------|
| `invalid duplex timeout format` | Timeout not in Go duration format | Use format like `"5m"`, `"30s"`, `"1h30m"` |
| `invalid turn detection mode` | Mode not `vad` or `asm` | Use `mode: vad` or `mode: asm` |
| `silence_threshold_ms must be non-negative` | Negative VAD threshold | Use positive values |
| `tts provider is required` | Missing TTS provider | Add `provider: openai` or similar |
| `tts voice is required` | Missing voice ID | Add `voice: alloy` or similar |

---

## See Also

- [Tutorial: Duplex Voice Testing](../tutorials/06-duplex-testing) - Step-by-step guide
- [Duplex Architecture](../explanation/duplex-architecture) - How duplex streaming works
- [Assertions Reference](./assertions) - All assertion types
- [CLI Commands Reference](./cli-commands) - Command-line options
