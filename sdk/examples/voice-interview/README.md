# Voice Interview System

A comprehensive voice-enabled interview demonstration showcasing PromptKit's stage-based pipeline architecture with support for streaming, VAD, TTS, and ASM-based models.

## Features

- **Dual Audio Modes**
  - **ASM Mode**: Native bidirectional audio streaming with Gemini 2.0 (continuous, real-time)
  - **VAD Mode**: Voice Activity Detection with turn-based processing and TTS output

- **Stage-Based Pipeline Architecture**
  - Demonstrates the full power of PromptKit's streaming pipeline
  - Real-time audio processing through pipeline stages
  - Seamless integration with multiple provider modes

- **Multiple Interview Topics**
  - Classic Rock Music
  - Space Exploration
  - Programming & Computer Science
  - World History
  - Movies & Cinema

- **Optional Webcam Integration**
  - Send periodic webcam frames for multimodal context
  - Visual engagement analysis

- **Rich Terminal UI**
  - Real-time audio level visualization
  - Progress tracking with score display
  - Live transcript display
  - Beautiful, interactive interface using Bubbletea

## Requirements

### System Dependencies

```bash
# macOS
brew install portaudio ffmpeg

# Ubuntu/Debian
sudo apt-get install portaudio19-dev ffmpeg

# Windows
# Download PortAudio from http://www.portaudio.com/
# Download ffmpeg from https://ffmpeg.org/download.html
```

### Environment

```bash
export GEMINI_API_KEY=your_api_key_here
```

## Quick Start

```bash
# Navigate to the example directory
cd sdk/examples/voice-interview

# Run with default settings (ASM mode, Classic Rock topic)
go run .

# Run with a specific topic
go run . --topic programming

# Run in VAD mode (turn-based with TTS)
go run . --mode vad --topic space

# Enable webcam for visual context
go run . --webcam --topic movies

# List all available topics
go run . --list-topics
```

## Command-Line Options

| Flag | Default | Description |
|------|---------|-------------|
| `--mode` | `asm` | Audio mode: `asm` (native audio) or `vad` (turn-based with TTS) |
| `--topic` | `classic-rock` | Interview topic (see `--list-topics`) |
| `--webcam` | `false` | Enable webcam for visual context |
| `--pack` | `./interview.pack.json` | Path to PromptPack file |
| `--no-ui` | `false` | Disable rich terminal UI |
| `--verbose` | `false` | Enable verbose logging |
| `--list-topics` | - | List available interview topics and exit |

## Architecture

### Pipeline Stages

This example demonstrates the stage-based pipeline architecture:

```
┌─────────────────────────────────────────────────────────────────┐
│                    Voice Interview Pipeline                      │
├─────────────────────────────────────────────────────────────────┤
│                                                                  │
│  ┌──────────────┐    ┌──────────────┐    ┌──────────────┐       │
│  │   Audio      │    │   VAD        │    │  Provider    │       │
│  │   Capture    │───▶│   Stage      │───▶│   Stage      │       │
│  │   Stage      │    │ (if VAD mode)│    │ (ASM/Text)   │       │
│  └──────────────┘    └──────────────┘    └──────────────┘       │
│         │                                        │               │
│         │                                        ▼               │
│         │            ┌──────────────┐    ┌──────────────┐       │
│         │            │   TTS        │◀───│  Response    │       │
│         │            │   Stage      │    │  Processing  │       │
│         │            │ (if VAD mode)│    │              │       │
│         │            └──────────────┘    └──────────────┘       │
│         │                   │                                    │
│         ▼                   ▼                                    │
│  ┌──────────────────────────────────────────────────────┐       │
│  │                  Audio Playback                       │       │
│  └──────────────────────────────────────────────────────┘       │
│                                                                  │
└─────────────────────────────────────────────────────────────────┘
```

### ASM Mode (Audio Streaming Model)

In ASM mode, the pipeline uses native bidirectional audio streaming:

- **Continuous streaming**: Audio flows continuously in both directions
- **No turn detection needed**: The model handles conversation flow
- **Lower latency**: Real-time response without waiting for turn boundaries
- **Requires**: Gemini 2.0 Flash Exp or similar ASM-capable models

### VAD Mode (Voice Activity Detection)

In VAD mode, the pipeline uses turn-based processing:

- **Turn detection**: VAD stage detects speech/silence boundaries
- **Accumulation**: Speech is accumulated until silence detected
- **TTS output**: Text responses are converted to speech
- **Works with**: Any text-based LLM + TTS service

## Project Structure

```
voice-interview/
├── main.go                 # Entry point with mode selection
├── interview.pack.json     # PromptPack configuration
├── README.md              # This file
├── audio/
│   └── portaudio.go       # Audio capture/playback
├── video/
│   └── webcam.go          # Webcam capture (optional)
├── interview/
│   ├── controller.go      # Interview orchestration
│   ├── state.go           # State management
│   └── questions.go       # Question banks
└── ui/
    └── app.go             # Bubbletea terminal UI
```

## Customization

### Adding New Topics

Edit `interview/questions.go` to add new question banks:

```go
func myCustomQuestions() *QuestionBank {
    return &QuestionBank{
        Topic:       "My Custom Topic",
        Description: "Description of the topic",
        Questions: []Question{
            {
                ID:       "custom-1",
                Text:     "Your question here?",
                Answer:   "Expected answer",
                Hint:     "Optional hint",
                Category: "category",
            },
            // Add more questions...
        },
    }
}
```

Then register it in `GetQuestionBank()`.

### Modifying the Interview Flow

The interview behavior is defined in `interview.pack.json`. Modify the system template to change:

- Interviewer personality
- Scoring guidelines
- Feedback style
- Response format

### Custom Audio Configuration

Adjust audio settings in `audio/portaudio.go`:

```go
const (
    InputSampleRate       = 16000  // Microphone sample rate
    OutputSampleRate      = 24000  // Speaker sample rate
    Channels              = 1      // Mono audio
    InputFramesPerBuffer  = 1600   // 100ms chunks
    EnergyThreshold       = 500    // VAD sensitivity
)
```

## Troubleshooting

### No Audio Input

1. Check microphone permissions in system settings
2. Verify PortAudio installation: `brew info portaudio`
3. List audio devices: The app will show available devices on startup

### Webcam Not Working

1. Ensure ffmpeg is installed: `ffmpeg -version`
2. Check camera permissions
3. Try a different device index: The app uses device 0 by default

### API Errors

1. Verify `GEMINI_API_KEY` is set correctly
2. Check API quota and rate limits
3. Ensure you have access to the required models:
   - ASM mode: `gemini-2.0-flash-exp`
   - VAD mode: `gemini-2.5-flash`

### UI Display Issues

Run with `--no-ui` flag for simple terminal output if the rich UI doesn't render correctly.

## Example Session

```
╔══════════════════════════════════════════════════════════════╗
║         🎤 Voice Interview System - PromptKit Demo           ║
╠══════════════════════════════════════════════════════════════╣
║  Topic: Classic Rock Music                                   ║
║  Mode:  ASM (Native Audio)                                   ║
║  Questions: 5                                                ║
╠══════════════════════════════════════════════════════════════╣
║  Controls:                                                   ║
║    • Speak naturally into your microphone                    ║
║    • Press Ctrl+C to end the interview                       ║
╚══════════════════════════════════════════════════════════════╝

🎤 [████████████████░░░░░░░░░░░░░░] 53%

Question 1 of 5
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
Q1: Which band released the album 'Dark Side of the Moon' in 1973?

🤖 That's correct! Pink Floyd released this iconic album...
👤 Pink Floyd

Score: 10/50  │  Progress: 20%
```

## Related Examples

- [`duplex-streaming`](../duplex-streaming/) - Basic duplex streaming example
- [`streaming`](../streaming/) - Text streaming example
- [`multimodal`](../multimodal/) - Image/audio input example

## License

This example is part of PromptKit and is available under the same license.
