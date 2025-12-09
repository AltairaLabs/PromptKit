# Audio Session Demo

This example demonstrates a complete voice AI pipeline using the PromptKit SDK.

## Features

- **VAD + Turn Detection**: Detect when users start and stop speaking
- **TTS Integration**: Convert LLM responses to speech
- **Interruption Handling**: Handle users interrupting the assistant

## Running

```bash
export OPENAI_API_KEY=your-key
cd sdk/examples/audio-session
go run .
```

## What It Demonstrates

### 1. VAD + Turn Detection
Shows how VAD states flow through turn detection to determine when a user has finished their turn:

```
User: [silence] -> [speaking] -> [pause] -> [speaking] -> [silence]
         |            |            |           |            |
       quiet      speaking      stopping    speaking      quiet
                                   |                        |
                           (natural pause)          (turn complete!)
```

### 2. LLM -> TTS Integration
Complete flow from user input to spoken response:

```go
// Get LLM response
resp, _ := conv.Send(ctx, "What's the weather?")

// Convert to speech
audio, _ := conv.SpeakResponse(ctx, resp,
    sdk.WithTTSVoice(tts.VoiceNova),
)
```

### 3. Interruption Handling
Handle users interrupting the assistant mid-response:

```go
handler := audio.NewInterruptionHandler(audio.InterruptionImmediate, vad)

handler.OnInterrupt(func() {
    // Stop TTS playback
    // Start listening to user
})
```

## Interruption Strategies

| Strategy | Behavior |
|----------|----------|
| `InterruptionIgnore` | Continue speaking, ignore user |
| `InterruptionImmediate` | Stop immediately, listen |
| `InterruptionDeferred` | Finish sentence, then listen |

## Real Application Flow

```
┌─────────────────┐
│   Microphone    │
└────────┬────────┘
         │ audio chunks
         ▼
┌─────────────────┐
│      VAD        │──► State changes (quiet/speaking/stopping)
└────────┬────────┘
         │
         ▼
┌─────────────────┐
│ Turn Detector   │──► End of turn signal
└────────┬────────┘
         │
         ▼
┌─────────────────┐
│ Speech-to-Text  │──► Transcribed text
└────────┬────────┘
         │
         ▼
┌─────────────────┐
│      LLM        │──► Response text
└────────┬────────┘
         │
         ▼
┌─────────────────┐
│      TTS        │──► Audio stream
└────────┬────────┘
         │
         ▼
┌─────────────────┐
│    Speaker      │
└─────────────────┘
```

## Integration with SDK Audio Sessions

```go
conv, _ := sdk.Open("./pack.json", "assistant",
    sdk.WithTTS(ttsService),
)

// Open audio session with full pipeline
session, _ := conv.OpenAudioSession(ctx,
    sdk.WithSessionVAD(vad),
    sdk.WithSessionTurnDetector(turnDetector),
    sdk.WithInterruptionStrategy(audio.InterruptionImmediate),
    sdk.WithAutoCompleteTurn(),
)

// Stream audio to session
for chunk := range microphoneInput {
    session.SendChunk(ctx, chunk)
}

// Get streaming responses
for response := range session.Response() {
    // Play audio chunks
}
```

## Output Files

The example generates:
- `response.mp3` - TTS output for the assistant's response

Play with: `afplay response.mp3` (macOS)
