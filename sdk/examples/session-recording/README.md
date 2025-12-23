# Session Recording Example

This example demonstrates the session recording and replay capabilities of PromptKit.

## Features Demonstrated

1. **Recording**: Capture conversation events to a file-based EventStore
2. **Export**: Save sessions to portable JSON format
3. **Replay**: Deterministically replay sessions using ReplayProvider
4. **Playback**: Timeline-based playback with SessionPlayer

## Prerequisites

Set your API key:

```bash
export OPENAI_API_KEY=your-key
```

## Running

From the example directory:

```bash
cd sdk/examples/session-recording
go run .
```

Or from the repo root:

```bash
go run ./sdk/examples/session-recording
```

## Expected Output

```
=== Session Recording Demo ===

Step 1: Recording a live conversation...
   User: What is the capital of France?
   Assistant: The capital of France is Paris...
   User: What's the population of that city?
   Assistant: Paris has a population of approximately...
   User: Thanks! One more question: what river runs through it?
   Assistant: The Seine River runs through Paris...
   Session recorded: abc123

Step 2: Exporting session to JSON...
   Saved 6 events
   Exported to: /tmp/promptkit-recording-xxx/session.recording.json

Step 3: Replaying session with ReplayProvider...
   Loaded recording with 6 events
   Created ReplayProvider with 3 turns
   Turn 1: The capital of France is Paris...
   Turn 2: Paris has a population of approximately...
   Turn 3: The Seine River runs through Paris...

Step 4: Playing back with SessionPlayer...
   [  0.00s] message.created
   [  0.10s] provider.call.completed
   [  0.20s] message.created
   ...
   Played 6 events

=== Demo Complete ===
```

## Code Walkthrough

### Recording a Session

```go
// Create an event store
store, _ := events.NewFileEventStore("./recordings")

// Open conversation with recording enabled
conv, _ := sdk.Open("./chat.pack.json", "chat", sdk.WithEventStore(store))

// Conversation is automatically recorded
resp, _ := conv.Send(ctx, "Hello!")
```

### Exporting to Portable Format

```go
// Get recorded events
evts, _ := store.GetSessionEvents(ctx, sessionID)

// Build SessionRecording
rec := &recording.SessionRecording{
    Metadata: recording.Metadata{SessionID: sessionID},
    Events:   convertEvents(evts),
}

// Save as JSON
rec.SaveTo("session.json", recording.FormatJSON)
```

### Replaying with ReplayProvider

```go
// Load recording
rec, _ := recording.Load("session.json")

// Create provider (implements providers.Provider interface)
provider, _ := replay.NewProvider(rec, &replay.Config{
    Timing:    replay.TimingInstant,  // No delays
    MatchMode: replay.MatchByTurn,    // Sequential
})

// Use like any provider
resp, _ := provider.Predict(ctx, providers.PredictionRequest{})
fmt.Println(resp.Content)
```

### Timeline Playback

```go
// Create player
player := events.NewSessionPlayer(evts)
player.SetSpeed(2.0)  // 2x speed

// Subscribe and play
ch := player.Subscribe()
go player.Play(ctx)

for evt := range ch {
    fmt.Printf("[%v] %s\n", evt.Offset, evt.Event.Type)
}
```

## Use Cases

- **Testing**: Replay recorded sessions for deterministic tests
- **Debugging**: Inspect what happened during a conversation
- **Training Data**: Export sessions for fine-tuning datasets
- **Demo/Presentation**: Replay interactions without API calls
