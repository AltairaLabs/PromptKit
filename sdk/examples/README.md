# SDK Examples

Interactive examples demonstrating PromptKit SDK features.

## Overview

All examples use the **Pipeline architecture** - audio, TTS, VAD, and other features flow through Pipeline middleware rather than SDK-specific methods.

## Available Examples

### 🎙️ [streaming-tts](./streaming-tts/)
Demonstrates streaming text-to-speech through the Pipeline.
- Streaming LLM responses
- TTS middleware converting text to audio
- Real-time audio generation during streaming

### 🗣️ [voice-chat](./voice-chat/)
Demonstrates voice conversation through the Pipeline.
- VAD (Voice Activity Detection)
- Turn detection
- TTS responses
- Full voice conversation flow

### 🔄 [streaming](./streaming/)
Basic streaming example showing real-time LLM responses.

### 🎨 [multimodal](./multimodal/)
Working with images, audio, and other media types.

### 🛠️ [tools](./tools/)
Function calling and tool integration.

### 📊 [variables](./variables/)
Dynamic variable substitution in prompts.

### 🔍 [vad-demo](./vad-demo/)
Voice Activity Detection examples.

### 👤 [hitl](./hitl/)
Human-in-the-loop workflows.

### 👋 [hello](./hello/)
Simple getting started example.

## Running Examples

All examples follow the pattern:

```bash
cd <example-name>
export OPENAI_API_KEY=your-key  # or appropriate provider key
go run .
```

Examples with the `_interactive.go` suffix are designed to be run interactively and demonstrate features visually.

## Architecture Note

**Pipeline-First Design:**

These examples demonstrate the correct architecture where TTS, VAD, and audio processing happen **through Pipeline middleware**, not via SDK convenience methods.

```
✅ Correct: Configure Pipeline → Everything flows through middleware
❌ Old way: SDK methods bypass Pipeline
```

Benefits:
- Consistent interface for all processing
- Middleware composability  
- Testable with mocks
- Observable via hooks
- Lower latency (streaming audio generation)

For integration tests and lower-level Pipeline usage, see `tests/integration/`.
