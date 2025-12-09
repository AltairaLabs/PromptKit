# TTS Basic Example

This example demonstrates text-to-speech (TTS) synthesis with the PromptKit SDK.

## Features

- **LLM + TTS Integration**: Get a response from the LLM, then convert it to speech
- **Multiple Voices**: Try different voices (alloy, nova, onyx, shimmer)
- **Voice Customization**: Adjust speed and use different models (tts-1, tts-1-hd)
- **File Output**: Save audio files in MP3 format

## Running

```bash
export OPENAI_API_KEY=your-key
cd sdk/examples/tts-basic
go run .
```

## Output

The example generates 4 audio files:
- `story_alloy.mp3` - Default voice
- `story_nova.mp3` - Female voice
- `story_slow.mp3` - Slower speed with deep male voice
- `story_hd.mp3` - High-definition quality

Play them with:
```bash
afplay story_alloy.mp3  # macOS
```

## Available Voices

| Voice | Description |
|-------|-------------|
| `alloy` | Neutral, balanced voice |
| `echo` | Male voice |
| `fable` | British accent |
| `onyx` | Deep male voice |
| `nova` | Female voice |
| `shimmer` | Soft female voice |

## TTS Options

```go
// Set voice
sdk.WithTTSVoice(tts.VoiceNova)

// Set speed (0.25 to 4.0, default 1.0)
sdk.WithTTSSpeed(1.25)

// Use HD model for better quality
sdk.WithTTSModel(tts.ModelTTS1HD)

// Set output format
sdk.WithTTSFormat(tts.FormatMP3)
```

## Notes

- Requires `OPENAI_API_KEY` environment variable
- OpenAI TTS pricing: ~$15/1M characters (tts-1) or ~$30/1M characters (tts-1-hd)
- Audio is streamed back efficiently for low latency
