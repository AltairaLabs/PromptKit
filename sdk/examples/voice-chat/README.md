# Voice Chat Example

Interactive voice chat using the SDK with streaming provider support (Gemini Live API).

## Features

- **Bidirectional Streaming**: Real-time audio input/output through provider
- **Pipeline Integration**: Uses SDK pipeline with VAD middleware
- **Provider-native TTS**: Audio responses directly from provider
- **Full-duplex Conversation**: Seamless voice interaction

## Requirements

- **System**:
  - Microphone (default system input)
  - Speakers/audio output
  - PortAudio library installed
  
- **macOS**:
  ```bash
  brew install portaudio
  ```

- **Linux**:
  ```bash
  sudo apt-get install portaudio19-dev
  ```

- **Windows**:
  Download and install PortAudio from http://www.portaudio.com/

- **API Keys**:
  - Gemini API key for streaming audio

## Installation

```bash
cd sdk/examples/voice-chat
go mod tidy
```

## Usage

1. Set your Gemini API key:
   ```bash
   export GEMINI_API_KEY=your-key-here
   ```

2. Run the example:
   ```bash
   go run .
   ```

3. Speak into your microphone
   - The session streams your audio to the provider
   - VAD middleware detects turn boundaries
   - Provider responds with text and/or audio
   - Audio responses play through speakers

4. Press Ctrl+C to exit

## How It Works

This example uses the SDK's proper pipeline architecture:

1. **Provider Session**: Creates streaming session with Gemini Live API
2. **Bidirectional Session**: Wraps provider session with SDK session management
3. **Audio Capture**: Microphone input sent as StreamChunks to session
4. **Pipeline Processing**: VAD middleware detects turns, provider generates responses
5. **Response Handling**: Text and audio responses received via response channel
6. **Audio Playback**: Provider-generated audio played through speakers

## Architecture

```
┌─────────────┐
│ Microphone  │
└──────┬──────┘
       │ PCM chunks
       ▼
┌─────────────────────┐
│ BidirectionalSession│
│   (SDK Pipeline)    │
│                     │
│  ┌─────────────┐   │
│  │ VAD         │   │◄── Turn detection
│  │ Middleware  │   │
│  └─────────────┘   │
│         │           │
│         ▼           │
│  ┌─────────────┐   │
│  │ Provider    │   │◄── Gemini Live API
│  │ Session     │   │
│  └─────────────┘   │
│         │           │
└─────────┴───────────┘
          │
          ▼ Text + Audio
┌─────────────┐
│  Speakers   │
└─────────────┘
```

## Customization

The pipeline handles VAD, transcription, and TTS internally through middleware. Configuration is done through the provider session request.

## Next Steps

- Explore [VAD demo](../vad-demo) for VAD configuration
- Check [streaming example](../streaming) for text streaming
- See SDK documentation for pipeline middleware
