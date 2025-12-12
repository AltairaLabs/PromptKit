# Realtime Voice Interview Example

A complete voice-based interview demonstrating **all the audio features** added in this branch.

This is a **real bidirectional voice conversation** - you speak into your microphone, and the interviewer responds with synthesized speech!

## Features Demonstrated

| Feature | Description |
|---------|-------------|
| **Real Microphone Capture** | Records from system microphone via `sox rec` |
| **VAD** | Voice Activity Detection - detects when user is speaking |
| **Turn Detection** | `SilenceDetector` - knows when user finished their turn |
| **Speech-to-Text** | OpenAI Whisper API transcription |
| **TTS** | Text-to-Speech - speaks the interviewer's responses |
| **Interruption Handling** | Handles user interrupting the assistant |
| **Silence Encouragement** | Prompts user if no speech detected |

## Prerequisites

```bash
# Install sox for microphone capture
brew install sox

# Set your OpenAI API key
export OPENAI_API_KEY=your-key
```

## Architecture

```
┌──────────────┐     ┌─────────────┐     ┌───────────────────┐
│  Microphone  │────►│     VAD     │────►│  Turn Detector    │
│  (sox rec)   │     │  (SimpleVAD)│     │ (SilenceDetector) │
└──────────────┘     └─────────────┘     └───────────────────┘
                                                  │
                                                  ▼ (turn complete)
                                         ┌───────────────────┐
                                         │  Speech-to-Text   │
                                         │    (Whisper)      │
                                         └───────────────────┘
                                                  │
                                                  ▼
┌──────────────┐     ┌─────────────┐     ┌───────────────────┐
│   Speaker    │◄────│     TTS     │◄────│       LLM         │
│  (afplay)    │     │  (OpenAI)   │     │    (GPT-4o)       │
└──────────────┘     └─────────────┘     └───────────────────┘
       ▲                                          │
       │            ┌─────────────┐               │
       └────────────│ Interruption│◄──────────────┘
                    │  Handler    │
                    └─────────────┘
```

## Running

```bash
export OPENAI_API_KEY=your-key
cd sdk/examples/realtime-interview
go run .
```

## What You'll See

```text
🎸 ════════════════════════════════════════════════════════════
          CLASSIC ROCK INTERVIEW - VOICE EDITION
   ════════════════════════════════════════════════════════════

   This demo shows the full PromptKit audio pipeline:
   • Real microphone capture (via sox)
   • VAD (Voice Activity Detection)
   • Turn Detection (silence-based)
   • Speech-to-Text (Whisper API)
   • TTS (Text-to-Speech) responses
   • Interruption handling

🎤 Initializing audio pipeline...
   ✓ VAD initialized (voice activity detection)
   ✓ Turn detector initialized (1.5s silence threshold)
   ✓ Interruption handler initialized (deferred strategy)
   ✓ TTS service initialized (OpenAI)
   ✓ Conversation loaded (rock-interview)

🎙️  Audio pipeline ready!

━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

🎸 [Interviewer]:
   Welcome to the classic rock interview! Let's test your 
   knowledge. Question 1: Which band released 'Dark Side 
   of the Moon' in 1973?
   🔊 [Playing audio response...]

🎤 [Listening... speak now!]
   Recording: 🔴 🔴 🔴 ⚪ ⚪ ✓
   ┌─ VAD Analysis ─────────────────────────────────────┐
   │ ···↑██████████████████↓···
   │ Legend: · quiet  ↑ starting  █ speaking  ↓ stopping
   └────────────────────────────────────────────────────┘
   Transcribing: ✓

👤 [User said]: "Pink Floyd"

🎸 [Interviewer]:
   Correct! Pink Floyd's masterpiece. Score: 10/10.
   Question 2: What was Led Zeppelin's original band name?
   🔊 [Playing audio response...]
```

## Silence Handling

If you don't speak, the interviewer will prompt you:

```text
🤔 [No speech detected - prompting user]
🔊 Hello? Are you there? Feel free to speak when you're ready.
```

After two consecutive silent turns:

```text
😔 [No response detected - wrapping up]
🔊 No worries, we can try this again later. Thanks for stopping by!
```

## Audio Components Used

### 1. Microphone Capture (sox rec)

```go
cmd := exec.CommandContext(ctx, "rec",
    "-q",             // Quiet mode
    "-r", "16000",    // 16kHz sample rate
    "-c", "1",        // Mono
    "-b", "16",       // 16-bit
    tmpPath,          // Output file
    "trim", "0", "8", // Max 8 seconds
    "silence", "1", "0.2", "1%",  // Start when sound
    "1", "1.5", "1%",             // Stop after 1.5s silence
)
```

### 2. VAD (Voice Activity Detection)

```go
vadParams := audio.VADParams{
    Confidence: 0.3,   // Sensitive for varied mics
    StartSecs:  0.2,   // Quick start detection
    StopSecs:   0.8,   // Allow brief pauses
    MinVolume:  0.005, // Detect softer speech
    SampleRate: 16000,
}
vad, _ := audio.NewSimpleVAD(vadParams)
```

### 3. Turn Detection (SilenceDetector)

```go
// 1.5 second silence = end of turn (for thinking time)
turnDetector := audio.NewSilenceDetector(1500 * time.Millisecond)

// Process VAD states
for audioChunk := range microphone {
    vad.Analyze(ctx, audioChunk)
    endOfTurn, _ := turnDetector.ProcessVADState(ctx, vad.State())
    if endOfTurn {
        // User finished speaking, process their input
    }
}
```

### 3. TTS (Text-to-Speech)

```go
ttsService := tts.NewOpenAI(apiKey)

conv, _ := sdk.Open("./pack.json", "interviewer",
    sdk.WithTTS(ttsService),
)

// Speak a response
audioReader, _ := conv.SpeakResponse(ctx, resp,
    sdk.WithTTSVoice(tts.VoiceNova),
    sdk.WithTTSSpeed(1.05),
)
```

### 4. Interruption Handling

```go
// Create handler with deferred strategy
// (finish current sentence before yielding to user)
handler := audio.NewInterruptionHandler(
    audio.InterruptionDeferred,
    vad,
)

handler.OnInterrupt(func() {
    // Stop TTS playback, start listening
})

// Mark when bot is speaking
handler.SetBotSpeaking(true)
// ... play TTS ...
handler.SetBotSpeaking(false)
```

### 5. Speech-to-Text (Whisper)

```go
// Transcribe audio using OpenAI Whisper API
func transcribeAudio(ctx context.Context, audioPath string) (string, error) {
    file, _ := os.Open(audioPath)
    defer file.Close()

    var buf bytes.Buffer
    writer := multipart.NewWriter(&buf)
    part, _ := writer.CreateFormFile("file", "audio.wav")
    io.Copy(part, file)
    writer.WriteField("model", "whisper-1")
    writer.WriteField("language", "en")
    writer.Close()

    req, _ := http.NewRequestWithContext(ctx, "POST",
        "https://api.openai.com/v1/audio/transcriptions", &buf)
    req.Header.Set("Authorization", "Bearer "+apiKey)
    req.Header.Set("Content-Type", writer.FormDataContentType())
    
    // ... handle response ...
}
```

## Interruption Strategies

| Strategy | Behavior |
|----------|----------|
| `InterruptionIgnore` | Continue speaking, ignore user input |
| `InterruptionImmediate` | Stop immediately, yield to user |
| `InterruptionDeferred` | Finish current sentence, then yield |

## How It All Works Together

```go
for questionNumber := range questions {
    // 1. Record user's answer (sox rec with silence detection)
    audioPath := captureFromMicrophone()
    
    // 2. Analyze with VAD (show user what's happening)
    analyzeWithVAD(audioPath)
    
    // 3. Transcribe to text (Whisper API)
    transcript := transcribeAudio(ctx, audioPath)
    
    // 4. Send to LLM, get response
    response, _ := conv.Send(ctx, transcript)
    
    // 5. Speak response (TTS + afplay)
    speakResponse(ctx, response)
    
    // 6. Loop back for next question
}
```

## Related Examples

- `sdk/examples/vad-demo/` - VAD basics
- `sdk/examples/tts-basic/` - TTS basics  
- `sdk/examples/audio-session/` - Full audio session API
