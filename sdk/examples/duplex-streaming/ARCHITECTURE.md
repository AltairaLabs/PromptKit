# Duplex Streaming Architecture

## ✅ IMPLEMENTED DESIGN

### Overview

DuplexSession supports two streaming modes with a unified interface:

1. **ASM Mode** (Audio Streaming Models): Continuous bidirectional streaming
2. **VAD Mode** (Voice Activity Detection): Turn-based streaming with buffering

Both modes expose the same API to consumers: `SendChunk()` / `SendText()` for input, `Response()` channel for output.

### Architecture Flow

```
SDK (OpenDuplex)
    ↓
1. Create PipelineBuilder closure (captures conversation context)
    ↓
2. Determine mode: Config != nil → ASM, Config == nil → VAD
    ↓
3. Call NewDuplexSession with:
   - PipelineBuilder (closure)
   - Provider (required)
   - Config (nil for VAD, set for ASM)
    ↓
DuplexSession.NewDuplexSession
    ↓
4. If ASM: provider.CreateStreamSession(ctx, Config) → providerSession
   If VAD: providerSession = nil
    ↓
5. Call PipelineBuilder(ctx, provider, providerSession, convID, store)
   → Returns configured Pipeline
    ↓
6. Create session with streamInput/streamOutput channels
    ↓
Consumer uses SendChunk() → streamInput → Pipeline → streamOutput → Response()
```

### ASM Mode (Audio Streaming Models)

**When**: Config is provided in DuplexSessionConfig
**Models**: gemini-2.0-flash-exp, other models with native bidirectional audio

**Flow**:
1. DuplexSession creates persistent `providerSession` via `provider.CreateStreamSession()`
2. Calls `PipelineBuilder` with provider and session
3. Builder creates pipeline with minimal middleware (no VAD, no TTS)
4. Provider middleware uses `providerSession` for streaming
5. **One long-running** `Pipeline.ExecuteStreamWithInput()` call
6. Continuous streaming: chunks flow through immediately

**Pipeline Middleware**:
- Provider (uses session)
- Logging
- Error handling
- NO VAD (audio chunks flow through)
- NO TTS (model produces audio directly)

### VAD Mode (Voice Activity Detection)

**When**: Config is nil in DuplexSessionConfig
**Models**: Standard text-based LLMs (GPT-4, Claude, etc.) with transcription

**Flow**:
1. DuplexSession does NOT create provider session (nil)
2. Calls `PipelineBuilder` with provider and nil session
3. Builder creates pipeline with VAD/TTS middleware
4. VAD buffers chunks, detects turn boundaries
5. **Multiple** `Pipeline.ExecuteStreamWithInput()` calls (one per turn)
6. Provider middleware makes one-shot calls (no persistent session)

**Pipeline Middleware**:
- VAD (buffers audio until turn complete)
- Provider (makes one-shot request/response calls)
- TTS (converts text response to audio)
- Tools
- Rate limiting
- Logging

### Key Differences

| Aspect | ASM Mode | VAD Mode |
|--------|----------|----------|
| Provider Session | ✅ Created (persistent WebSocket) | ❌ Not created |
| Pipeline Executions | 1 (long-running) | Multiple (per turn) |
| VAD Middleware | ❌ Not included | ✅ Included |
| TTS Middleware | ❌ Not included | ✅ Included |
| Audio Flow | Continuous, immediate | Buffered, turn-based |
| Provider Calls | Uses session | One-shot calls |
| Turn Detection | External (ASM model) | Internal (VAD) |

---

## Implementation Details

### PipelineBuilder Signature

```go
type PipelineBuilder func(
    ctx context.Context,
    provider providers.Provider,         // Always provided
    session providers.StreamInputSession, // nil for VAD, set for ASM
    conversationID string,
    store statestore.Store,
) (*pipeline.Pipeline, error)
```

### SDK Usage Example

```go
func initDuplexSession(conv *Conversation, cfg *config, streamProvider providers.StreamInputSupport) error {
    // Create builder closure capturing conversation context
    pipelineBuilder := func(
        ctx context.Context,
        provider providers.Provider,
        providerSession providers.StreamInputSession,
        convID string,
        stateStore statestore.Store,
    ) (*rtpipeline.Pipeline, error) {
        // Use existing pipeline building logic
        return conv.buildPipelineWithParams(stateStore, convID)
    }

    // Determine streaming config
    var streamConfig *providers.StreamingInputConfig
    if isASMModel(cfg.model) {
        streamConfig = &providers.StreamingInputConfig{
            Type: types.ContentTypeAudio,
            SampleRate: 16000,
            // ... other audio config
        }
    }
    // else streamConfig stays nil → VAD mode

    // Create session
    duplexSession, err := session.NewDuplexSession(ctx, &session.DuplexSessionConfig{
        ConversationID:  conversationID,
        StateStore:      store,
        PipelineBuilder: pipelineBuilder,
        Provider:        streamProvider,
        Config:          streamConfig, // nil = VAD, set = ASM
        Variables:       initialVars,
    })
}
```

---

## Next Steps

### Immediate

1. ✅ Implement PipelineBuilder pattern in session package
2. ✅ Update OpenDuplex to use builder pattern
3. ⏳ Implement ASM detection logic
4. ⏳ Configure StreamingInputConfig for ASM models
5. ⏳ Update pipeline building to skip VAD/TTS for ASM

### Pipeline Middleware Changes Needed

1. **Provider Middleware**: Check if session exists, use it; otherwise make one-shot call
2. **VAD Middleware**: Only include in VAD mode pipelines
3. **TTS Middleware**: Only include in VAD mode pipelines
4. **Tool Middleware**: Handle tools in both modes
5. **State Store**: Handle message persistence for both modes

### Testing

1. Test ASM mode with gemini-2.0-flash-exp
2. Test VAD mode with standard models
3. Test continuous streaming scenarios
4. Test turn detection accuracy
5. Test error handling in both modes
6. Test resource cleanup

---

#### A. Audio Capture Goroutine
**Purpose**: Continuously capture microphone input and send to Gemini

**Flow**:
```
1. Open PortAudio input stream (16kHz, mono)
2. Loop forever:
   - Read audio frame (100ms chunks = 1600 samples)
   - Convert int16 samples to bytes (PCM16)
   - Create StreamChunk with MediaData
   - Call conv.SendChunk(ctx, chunk)
   - Visual feedback: █ for audio, ░ for silence
```

**Current Issue**: Sending raw audio chunks immediately without initialization

#### B. Response Processor Goroutine
**Purpose**: Receive and process streaming responses from Gemini

**Flow**:
```
1. Loop forever:
   - Call conv.Response() to get response channel
   - For each chunk from channel:
     - If MediaData: queue audio for playback
     - If Delta (text): print to console
     - If FinishReason: complete, get next response
```

**Current Issue**: Getting error "contents is not specified" - Gemini expects initial message

#### C. Audio Playback Goroutine
**Purpose**: Play audio responses through speakers

**Flow**:
```
1. Open PortAudio output stream (24kHz, mono)
2. Loop forever:
   - Read from audioQueue channel
   - Buffer audio data
   - Convert bytes to int16
   - Write to speaker stream
```

**Status**: Not tested yet due to earlier errors

### 3. Data Flow

```
Microphone Input (16kHz PCM16)
        ↓
  [Audio Capture]
        ↓
  StreamChunk { MediaData: audio bytes }
        ↓
  conv.SendChunk(ctx, chunk)
        ↓
  DuplexSession → Gemini Live API
        ↓
  Response Channel ← Gemini Live API
        ↓
  StreamChunk { MediaData: audio bytes, Delta: text }
        ↓
  [Response Processor]
        ↓
  audioQueue channel
        ↓
  [Audio Playback] → Speakers
```

## How Demo Works (For Reference)

The `demo_streaming_test.go` shows the provider-level flow:

```go
// 1. Create provider
provider := gemini.NewProvider(...)

// 2. Create stream session (WebSocket connection)
session, _ := provider.CreateStreamSession(ctx, &StreamInputRequest{
    Config: types.StreamingMediaConfig{
        Type:       types.ContentTypeAudio,
        SampleRate: 16000,
        ...
    },
})

// 3. Start response listener
go func() {
    for chunk := range session.Response() {
        // Process audio/text chunks
    }
}()

// 4. Send audio chunks
for _, chunk := range audioChunks {
    session.SendChunk(ctx, chunk)
}

// 5. Send text prompt to trigger response
session.SendText(ctx, "Please respond to the audio")

// 6. Responses stream back
```

**Key Pattern**: Audio chunks + text prompt → Response

This works because it's **direct to provider, no pipeline middleware buffering**.

The SDK must replicate this flow but ADD pipeline middleware appropriately.

---

## Potential Solutions

### ✅ CORRECT Solution: Match Gemini's Expected Pattern

Based on `demo_streaming_test.go`, here's what we need to do:

```go
// Pattern 1: Turn-Based with VAD (Recommended)
func interactiveAudioExample() {
    // 1. Start response listener
    go processResponses()
    
    // 2. Capture audio with VAD
    go func() {
        for {
            // Detect speech
            audioBuffer := captureUntilSilence()
            
            // Send audio chunks
            sendAudioChunks(audioBuffer)
            
            // Send text prompt to trigger response
            conv.SendText(ctx, "Please respond to what I said")
            
            // Wait for response to complete before next turn
            waitForResponse()
        }
    }()
}
```

### Pattern 2: Continuous Audio + Periodic Prompts
```go
// Stream audio continuously but send periodic prompts
go func() {
    for {
        audioChunk := captureAudio()
        conv.SendChunk(ctx, audioChunk)
        
        // Every N seconds or on VAD trigger
        if shouldTriggerResponse() {
            conv.SendText(ctx, "respond")
        }
    }
}()
```

### Pattern 3: Command-Triggered Responses
```go
// Stream audio, respond on user command (e.g., press key)
go streamAudio()

go func() {
    for keyPress := range keyboardInput {
        conv.SendText(ctx, "Please respond to the audio")
    }
}()
```

### ❌ What Won't Work
- Continuous audio streaming without text prompts
- Calling Response() before sending any data
- Expecting Gemini to auto-detect when to respond

## Questions to Answer

### ✅ ANSWERED from demo_streaming_test.go:

1. **Does Gemini Live API support true bidirectional streaming?**
   - ✅ YES, but it's REQUEST → RESPONSE pattern
   - You can stream audio chunks continuously
   - BUT: You need a text prompt to trigger a response
   - It's not "always on" conversation

2. **What's the proper initialization sequence?**
   - ✅ Call `CreateStreamSession()` with config
   - ✅ Start response listener goroutine
   - ✅ Send audio chunks (optional)
   - ✅ Send text prompt (REQUIRED to get response)
   
3. **Audio format expectations?**
   - ✅ PCM16, 16kHz, mono
   - ✅ Base64 encoded for sending
   - ✅ ChunkSize: 3200 bytes (100ms at 16kHz)
   
4. **How does interruption work?**
   - ⚠️ Not clear if you can interrupt mid-response
   - The demo shows turn-based: send → receive → send
   - Need to test: Can we SendText() while receiving?

### 🔍 STILL NEED TO INVESTIGATE:

1. **Pipeline vs Direct Provider**:
   - ⚠️ **CRITICAL**: Does the pipeline preserve streaming semantics?
   - Does SendText() through conv.SendText() work the same as session.SendText()?
   - Is the pipeline buffering or modifying our chunks?

2. **How does Pipeline handle duplex streaming?**:
   - Check `pipeline.ExecuteStreaming()` implementation
   - Does it support bidirectional input properly?
   - Are there middleware that interfere with streaming?

3. **DuplexSession modes**:
   - Pipeline mode vs Provider mode
   - Which mode is OpenDuplex using?
   - Should we bypass pipeline for pure streaming?

4. **True duplex behavior**:
   - Can we send audio while receiving audio response?
   - Or is it strictly turn-based?

5. **Multiple prompts**:
   - Can we send multiple text prompts while audio streams?
   - How to handle overlapping responses?

## Next Steps

### IMMEDIATE FIX (Required):

1. **Add Text Prompt After Audio**
   ```go
   // In captureAndStreamAudio(), after VAD detects silence:
   
   // Send buffered audio chunks
   sendAudioChunks(audioBuffer)
   
   // ⭐ ADD THIS: Trigger response with text prompt
   conv.SendText(ctx, "Please respond to what I just said")
   ```

2. **Fix Response Listener Order**
   ```go
   // Response listener should run continuously
   // It's already started in a goroutine - that's correct!
   // But it needs to handle multiple response cycles
   ```

3. **Implement Turn Management**
   - Don't send audio while processing response
   - Wait for FinishReason before next audio capture
   - Or implement interruption logic

### TESTING PLAN:

**Phase 1: Get Basic Working**
- ✅ Start response listener
- ✅ Capture audio with VAD (buffer until silence)
- ✅ Send audio chunks
- ⭐ **ADD: Send text prompt**
- ✅ Display response
- Repeat

**Phase 2: Improve UX**
- Add better visual feedback (state machine)
- Show when Gemini is "thinking"
- Play audio responses through speakers
- Handle overlapping audio/responses

**Phase 3: Optimize**
- Reduce latency
- Test interruption scenarios
- Add error recovery
- Profile performance

### RECOMMENDED FIRST CHANGE:

**File**: `main_interactive.go`  
**Function**: `captureAndStreamAudio()`  
**Change**: After detecting silence and sending audio, add:

```go
// After sending audio chunk:
if err := ah.conv.SendChunk(ctx, chunk); err != nil {
    log.Printf("Failed to send audio chunk: %v", err)
    continue
}

// ⭐ NEW: Trigger response
if err := ah.conv.SendText(ctx, "Please respond to the audio I just sent"); err != nil {
    log.Printf("Failed to send prompt: %v", err)
}

## Code Structure Issues

### Goroutine Termination
- ✅ Fixed: Added `close(audioQueue)` on shutdown
- ✅ Fixed: Using context cancellation in loops
- ✅ Fixed: Select statements with ctx.Done()
- Remaining: Ensure Response() doesn't block forever

### Error Handling
- Need better error propagation
- Should stop other goroutines if one fails critically
- Add timeout for Response() calls

### Visual Feedback
- Currently shows █ and ░ but hard to read
- Should show clear state: listening/speaking/processing
- Add timestamps or duration indicators
