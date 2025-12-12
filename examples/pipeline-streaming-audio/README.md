# Pipeline Streaming Audio Example

This example demonstrates the new **Pipeline-based streaming architecture** with VAD (Voice Activity Detection) and TTS (Text-to-Speech) integrated as middleware.

## What's New

This example showcases the **StreamInput architecture** where:
- **Input chunks** flow through `StreamInput` channel → VAD middleware → Pipeline
- **Output chunks** flow through Pipeline → TTS middleware → `StreamOutput` channel
- **Symmetric streaming**: both input and output are chunk-based, processed through middleware

## Architecture

```
┌──────────────────────────────────────────────────────────────┐
│                  BidirectionalSession                         │
│                    (Pipeline Mode)                            │
└────────────────┬─────────────────────────────────────────────┘
                 │
    Input        │        Output
    ─────►       │         ◄─────
                 │
┌────────────────▼─────────────────────────────────────────────┐
│                      Pipeline                                 │
│                                                               │
│  StreamInput → VAD → StateStore → Provider → TTS → StreamOut │
│     channel      │       │           │         │      channel │
│                  │       │           │         │              │
│                  ▼       ▼           ▼         ▼              │
│              [blocks  [loads   [streams   [adds audio         │
│               until    state]   response]  to chunks]         │
│               turn                                            │
│               complete]                                       │
└───────────────────────────────────────────────────────────────┘
```

### Key Components

1. **VAD Middleware** (`middleware.NewVADMiddleware`)
   - Reads audio chunks from `StreamInput` channel
   - Detects speech vs. silence using VAD analyzer
   - **Blocks until turn complete** (silence threshold reached)
   - Transcribes buffered audio to text
   - Creates a `Message` with the transcribed text
   - Continues to next middleware (StateStore, Provider, etc.)

2. **TTS Middleware** (`middleware.NewTTSMiddleware`)
   - Implements `StreamChunk()` hook (called for each output chunk)
   - Synthesizes audio for text chunks
   - Adds audio data to `chunk.MediaDelta` field
   - Streams audio back to client in real-time

3. **BidirectionalSession with Pipeline**
   - Creates `streamInput` and `streamOutput` channels automatically
   - Routes `SendChunk()` calls to `streamInput`
   - Returns `streamOutput` from `Response()`
   - Manages Pipeline execution lifecycle

## Features Demonstrated

### 1. Voice Activity Detection (VAD)
```go
// Configure VAD for turn detection
vadConfig := middleware.VADConfig{
    Threshold:         0.3,     // Below 0.3 = silence
    MinSpeechDuration: 300 * time.Millisecond,
    MaxTurnDuration:   30 * time.Second,
    SilenceDuration:   700 * time.Millisecond,
}

vadMiddleware := middleware.NewVADMiddleware(
    vadAnalyzer,
    transcriptionService,
    vadConfig,
)
```

### 2. Text-to-Speech (TTS)
```go
// Configure TTS for output audio
ttsConfig := &middleware.TTSConfig{
    Voice:     "alloy",
    MIMEType:  "audio/mpeg",
    MinLength: 10, // Only synthesize chunks with 10+ chars
}

ttsMiddleware := middleware.NewTTSMiddleware(
    ttsService,
    ttsConfig,
)
```

### 3. Pipeline Composition
```go
// Create pipeline with VAD, Provider, and TTS
p := pipeline.NewPipeline(
    vadMiddleware,           // 1. VAD: blocks until turn complete
    stateStoreMiddleware,    // 2. Load conversation history
    providerMiddleware,      // 3. Stream LLM response
    ttsMiddleware,           // 4. Add audio to output chunks
)

// Create session with Pipeline
session, err := session.NewBidirectionalSession(&session.BidirectionalConfig{
    Pipeline: p,
})
```

### 4. Streaming Input and Output
```go
// Send audio chunks
audioChunk := &providers.StreamChunk{
    MediaDelta: &types.MediaContent{
        Data:     pcmAudioBytes,
        MIMEType: "audio/pcm",
    },
}
session.SendChunk(ctx, audioChunk)

// Receive response chunks (with audio)
responseChan := session.Response()
for chunk := range responseChan {
    if chunk.Delta != "" {
        fmt.Printf("Text: %s\n", chunk.Delta)
    }
    if chunk.MediaDelta != nil {
        // Play audio chunk
        playAudio(chunk.MediaDelta.Data)
    }
}
```

## Comparison with SDK Audio Session

| Feature | SDK Audio Session | Pipeline Streaming |
|---------|-------------------|-------------------|
| **Architecture** | Session-level processing | Pipeline middleware |
| **Turn Detection** | Session manages VAD | VAD middleware |
| **State Management** | Manual | StateStore middleware |
| **Validation** | Post-processing | Validation middleware |
| **System Prompts** | SDK builds prompt | PromptAssembly middleware |
| **TTS** | Post-processing with `SpeakResponse()` | TTS middleware (streaming) |
| **Composability** | Fixed flow | Fully composable |
| **Streaming** | Provider only | End-to-end (input + output) |

## When to Use Pipeline Mode

✅ **Use Pipeline Mode when you need**:
- State management and conversation history
- Validation middleware
- System prompts and prompt assembly
- Tool integration
- Streaming TTS (LLM → TTS in real-time)
- Custom middleware (logging, metrics, guardrails)

❌ **Use SDK Audio Session when**:
- Simple audio conversations without state
- Direct provider access (Gemini 2.0 Live)
- Minimal latency requirements
- No middleware needed

## Running the Example

```bash
# Set API keys
export OPENAI_API_KEY=your-key

# Run the example
cd examples/pipeline-streaming-audio
go run main.go
```

## Implementation Notes

### StreamInput Flow

1. **User sends audio chunk** → `session.SendChunk(chunk)`
2. **First chunk triggers pipeline execution** (background goroutine)
3. **VAD middleware blocks** reading from `StreamInput` channel
4. **VAD detects turn complete** (silence threshold reached)
5. **VAD transcribes audio** → creates Message
6. **Pipeline continues** → StateStore, Provider, TTS
7. **Response streams** through `StreamOutput` channel

### Concurrency & Thread Safety

- **Pipeline execution**: Runs in background goroutine (started by first chunk)
- **Channel buffering**: 100-item buffers for `streamInput` and `streamOutput`
- **Execution start**: Mutex-protected to ensure single execution per session
- **Channel closure**: `streamOutput` closed automatically when Pipeline completes

### Error Handling

```go
// Errors are sent as chunks with FinishReason="error"
for chunk := range session.Response() {
    if chunk.Error != nil {
        log.Printf("Error: %v", chunk.Error)
        break
    }
    if chunk.FinishReason != nil {
        log.Printf("Finished: %s", *chunk.FinishReason)
        break
    }
}
```

## Advanced Usage

### Custom VAD Analyzer

```go
type CustomVAD struct {
    // Your VAD implementation
}

func (v *CustomVAD) Analyze(ctx context.Context, audio []byte) (float64, error) {
    // Return speech probability (0.0-1.0)
    return speechScore, nil
}

func (v *CustomVAD) State() audio.VADState {
    return audio.VADStateSpeaking // or VADStateQuiet
}

// Use in VAD middleware
vadMiddleware := middleware.NewVADMiddleware(
    &CustomVAD{},
    transcriptionService,
    vadConfig,
)
```

### Custom Transcription Service

```go
type CustomTranscriber struct {
    // Your transcription implementation
}

func (t *CustomTranscriber) Transcribe(ctx context.Context, audio []byte) (string, error) {
    // Transcribe audio → text
    return transcribedText, nil
}

// Use in VAD middleware
vadMiddleware := middleware.NewVADMiddleware(
    vadAnalyzer,
    &CustomTranscriber{},
    vadConfig,
)
```

### Custom TTS Service

```go
type CustomTTS struct {
    // Your TTS implementation
}

func (t *CustomTTS) Synthesize(ctx context.Context, text string) ([]byte, string, error) {
    // Synthesize text → audio
    return audioBytes, "audio/mpeg", nil
}

// Use in TTS middleware
ttsMiddleware := middleware.NewTTSMiddleware(
    &CustomTTS{},
    ttsConfig,
)
```

## Testing

The example includes tests demonstrating:
- VAD turn detection with mock audio
- TTS audio synthesis
- End-to-end streaming flow
- Error handling

Run tests:
```bash
go test -v ./...
```

## Related Examples

- [`sdk/examples/audio-session`](../../sdk/examples/audio-session) - SDK-level audio session (simpler, no middleware)
- [`sdk/examples/vad-demo`](../../sdk/examples/vad-demo) - VAD demonstration
- [`sdk/examples/tts-basic`](../../sdk/examples/tts-basic) - Basic TTS usage

## See Also

- [Pipeline Documentation](../../docs/pipeline.md)
- [Middleware Guide](../../docs/middleware.md)
- [Streaming Architecture](../../docs/local-backlog/streaming-session-refactor.md)
