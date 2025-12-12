# Streaming Audio Example

This example demonstrates **true bidirectional audio streaming** using PromptKit's `OpenAudioSession()` API with Gemini's Live API.

## Architecture

```
┌──────────────┐                      ┌───────────────────┐
│  Microphone  │─────┬───────────────►│                   │
│  (chunks)    │     │                │   Gemini Live     │
└──────────────┘     │ bidirectional  │   (streaming)     │
                     │    WebSocket   │                   │
┌──────────────┐     │                │                   │
│   Speaker    │◄────┴────────────────│                   │
│  (chunks)    │                      └───────────────────┘
└──────────────┘
        ▲
        │           ┌─────────────┐
        └───────────│     VAD     │ (turn detection)
                    └─────────────┘
```

## Key Differences from Batch Mode

| Feature | Batch (voice-interview) | Streaming (this example) |
|---------|------------------------|--------------------------|
| Input | Record full utterance | Stream chunks in real-time |
| STT | Whisper API (batch) | Model native (streaming) |
| Response | Wait for completion | Start immediately |
| Latency | High (seconds) | Low (milliseconds) |
| Interruption | Not supported | Natural handling |

## Prerequisites

```bash
# Install audio tools
brew install sox

# Set API key
export GOOGLE_API_KEY=your-gemini-api-key
```

## Usage

```bash
go run .
```

Speak naturally - the model will respond in real-time as you speak. You can interrupt the model at any time.

## How It Works

1. **Audio Capture**: sox streams microphone audio as raw PCM chunks
2. **VAD**: Voice Activity Detection identifies when you're speaking
3. **Turn Detection**: Silence detector knows when you've finished
4. **Streaming**: Audio chunks sent to Gemini via WebSocket
5. **Response**: Audio response streams back while you might still be speaking
6. **Playback**: sox plays response audio chunks as they arrive

## API Usage

```go
// Open conversation with streaming provider
conv, _ := sdk.Open("./assistant.pack.json", "voice",
    sdk.WithProvider(geminiProvider),
    sdk.WithVAD(vad),
    sdk.WithTurnDetector(turnDetector),
)

// Open streaming audio session
session, _ := conv.OpenAudioSession(ctx,
    sdk.WithSessionVAD(vad),
    sdk.WithSessionTurnDetector(turnDetector),
    sdk.WithInterruptionStrategy(audio.InterruptionDeferred),
    sdk.WithAutoCompleteTurn(true),
)

// Stream audio chunks
for chunk := range audioSource {
    session.SendChunk(ctx, chunk)
}

// Receive responses
for resp := range session.Response() {
    switch resp.Type {
    case audio.ResponseTypeAudio:
        playAudio(resp.AudioData)
    case audio.ResponseTypeText:
        fmt.Println(resp.Text)
    }
}
```
