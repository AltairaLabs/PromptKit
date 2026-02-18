---
title: Session Recording
---
Learn how to capture detailed session recordings for debugging, replay, and analysis of Arena test runs.

## Why Use Session Recording?

- **Deep debugging**: See exact event sequences and timing
- **Audio reconstruction**: Export voice conversations as WAV files
- **Replay capability**: Recreate test runs with deterministic providers
- **Performance analysis**: Analyze per-event timing and latency
- **Annotation support**: Add labels, scores, and comments to recordings

## Enable Session Recording

Add the recording configuration to your arena config:

```yaml
# arena.yaml
apiVersion: promptkit.altairalabs.ai/v1alpha1
kind: Arena
metadata:
  name: my-arena

spec:
  defaults:
    output:
      dir: out
      formats: [json, html]
      recording:
        enabled: true           # Enable recording
        dir: recordings         # Subdirectory for recordings
```

Run your tests normally:

```bash
promptarena run --scenario my-test
```

Recordings are saved to `out/recordings/` as JSONL files.

## Recording Format

Each recording is a JSONL file containing:

1. **Metadata line**: Session info, timing, provider details
2. **Event lines**: Individual events with timestamps and data

```jsonl
{"type":"metadata","session_id":"run-123","start_time":"2024-01-15T10:30:00Z","duration":"5.2s",...}
{"type":"event","timestamp":"2024-01-15T10:30:00.100Z","event_type":"conversation.started",...}
{"type":"event","timestamp":"2024-01-15T10:30:00.500Z","event_type":"message.created",...}
{"type":"event","timestamp":"2024-01-15T10:30:01.200Z","event_type":"audio.input",...}
```

## Captured Events

Session recordings capture a comprehensive event stream:

| Event Type | Description |
|------------|-------------|
| `conversation.started` | Session initialization with system prompt |
| `message.created` | User and assistant messages |
| `audio.input` | User audio chunks (voice conversations) |
| `audio.output` | Assistant audio chunks (voice responses) |
| `provider.call.started` | LLM API call initiation |
| `provider.call.completed` | LLM API call completion with tokens/cost |
| `tool.call.started` | Tool/function call initiation |
| `tool.call.completed` | Tool result with timing |
| `validation.*` | Validator execution and results |

## Working with Recordings

### Load and Inspect a Recording

```go
package main

import (
    "fmt"
    "github.com/AltairaLabs/PromptKit/runtime/recording"
)

func main() {
    // Load recording
    rec, err := recording.Load("out/recordings/run-123.jsonl")
    if err != nil {
        panic(err)
    }

    // Print metadata
    fmt.Printf("Session: %s\n", rec.Metadata.SessionID)
    fmt.Printf("Duration: %v\n", rec.Metadata.Duration)
    fmt.Printf("Events: %d\n", rec.Metadata.EventCount)
    fmt.Printf("Provider: %s\n", rec.Metadata.ProviderName)
    fmt.Printf("Model: %s\n", rec.Metadata.Model)

    // Iterate events
    for _, event := range rec.Events {
        fmt.Printf("[%v] %s\n", event.Offset, event.Type)
    }
}
```

### Export Audio to WAV

For voice conversations, extract audio tracks as WAV files:

```go
// Create replay player
player, err := recording.NewReplayPlayer(rec)
if err != nil {
    panic(err)
}

timeline := player.Timeline()

// Export user audio
if timeline.HasTrack(events.TrackAudioInput) {
    err := timeline.ExportAudioToWAV(events.TrackAudioInput, "user_audio.wav")
    if err != nil {
        fmt.Printf("Export failed: %v\n", err)
    }
}

// Export assistant audio
if timeline.HasTrack(events.TrackAudioOutput) {
    err := timeline.ExportAudioToWAV(events.TrackAudioOutput, "assistant_audio.wav")
    if err != nil {
        fmt.Printf("Export failed: %v\n", err)
    }
}
```

### Synchronized Playback

Use the ReplayPlayer for time-synchronized event access:

```go
player, _ := recording.NewReplayPlayer(rec)

// Seek to specific position
player.Seek(2 * time.Second)

// Get state at current position
state := player.GetState()
fmt.Printf("Position: %s\n", player.FormatPosition())
fmt.Printf("Current events: %d\n", len(state.CurrentEvents))
fmt.Printf("Messages so far: %d\n", len(state.Messages))
fmt.Printf("Audio input active: %v\n", state.AudioInputActive)
fmt.Printf("Audio output active: %v\n", state.AudioOutputActive)

// Advance through recording
for {
    events := player.Advance(100 * time.Millisecond)
    if player.Position() >= player.Duration() {
        break
    }
    for _, e := range events {
        fmt.Printf("[%v] %s\n", e.Offset, e.Type)
    }
}
```

### Add Annotations

Attach annotations to recordings for review and analysis:

```go
import "github.com/AltairaLabs/PromptKit/runtime/annotations"

// Create annotations
anns := []*annotations.Annotation{
    {
        ID:        "quality-1",
        Type:      annotations.TypeScore,
        SessionID: rec.Metadata.SessionID,
        Target:    annotations.ForSession(),
        Key:       "overall_quality",
        Value:     annotations.NewScoreValue(0.92),
    },
    {
        ID:        "highlight-1",
        Type:      annotations.TypeComment,
        SessionID: rec.Metadata.SessionID,
        Target:    annotations.InTimeRange(startTime, endTime),
        Key:       "observation",
        Value:     annotations.NewCommentValue("Good response latency"),
    },
}

// Attach to player
player.SetAnnotations(anns)

// Query active annotations at any position
state := player.GetStateAt(1500 * time.Millisecond)
for _, ann := range state.ActiveAnnotations {
    fmt.Printf("Annotation: %s = %v\n", ann.Key, ann.Value)
}
```

## Replay Provider

Use recordings for deterministic test replay:

```go
import "github.com/AltairaLabs/PromptKit/runtime/providers/replay"

// Create replay provider from recording
provider, err := replay.NewProviderFromRecording(rec)
if err != nil {
    panic(err)
}

// Use like any other provider - returns recorded responses
response, err := provider.Complete(ctx, messages, opts)
```

This enables:
- Regression testing without API calls
- Reproducing exact conversation flows
- Testing against known-good responses

## Evaluating Recorded Conversations

Use the **Eval** configuration type to validate and test saved conversations with assertions and LLM judges:

```yaml
# evals/validate-recording.eval.yaml
apiVersion: promptkit.altairalabs.ai/v1alpha1
kind: Eval
metadata:
  name: validate-support-session

spec:
  id: support-validation
  description: Validate customer support conversation quality
  
  recording:
    path: recordings/session-abc123.recording.json
    type: session
  
  judge_targets:
    default:
      type: openai
      model: gpt-4o
      id: quality-judge
  
  assertions:
    - type: llm_judge
      params:
        judge: default
        criteria: "Did the agent provide helpful and accurate information?"
        expected: pass
    
    - type: llm_judge
      params:
        judge: default
        criteria: "Was the conversation tone professional and empathetic?"
        expected: pass
```

Reference the eval in your arena configuration:

```yaml
# arena.yaml
apiVersion: promptkit.altairalabs.ai/v1alpha1
kind: Arena
metadata:
  name: evaluation-suite

spec:
  providers:
    - file: providers/openai-gpt4o.provider.yaml
  
  evals:
    - file: evals/validate-recording.eval.yaml
```

Run evaluations:

```bash
promptarena run --config arena.yaml
```

This workflow enables:
- **Quality assurance**: Validate conversation quality with LLM judges
- **Regression testing**: Ensure consistency across model updates
- **Batch evaluation**: Test multiple recordings with the same criteria
- **CI/CD integration**: Automated conversation quality checks

See the **[Eval Configuration Reference](/arena/reference/config-schema#eval-saved-conversation-evaluation)** for complete documentation.

## Complete Example

See `examples/session-replay/` for a full working example:

```bash
cd examples/session-replay
go run demo/replay_example.go

# Or with your own recording:
go run demo/replay_example.go path/to/recording.jsonl
```

The example demonstrates:
- Loading recordings
- Creating a replay player
- Simulating playback with event correlation
- Exporting audio tracks
- Working with annotations

## Recording Storage

### File Organization

```
out/
└── recordings/
    ├── run-abc123.jsonl     # One file per test run
    ├── run-def456.jsonl
    └── run-ghi789.jsonl
```

### Storage Considerations

- **Size**: Recordings with audio can be large (audio data is base64-encoded)
- **Retention**: Consider cleanup policies for old recordings
- **Compression**: JSONL files compress well with gzip

### CI/CD Integration

Save recordings as artifacts for debugging failed tests:

```yaml
# .github/workflows/test.yml
- name: Run Arena Tests
  run: promptarena run --ci

- name: Upload Recordings
  if: failure()
  uses: actions/upload-artifact@v4
  with:
    name: session-recordings
    path: out/recordings/
    retention-days: 7
```

## What's Captured vs. What's Not

### Captured in Recordings
- Complete event stream with precise timestamps
- Audio chunks for voice conversations
- Message content and metadata
- Tool calls with arguments and results
- Provider call timing and token usage
- Validation results

### Not Captured (stored separately)
- Post-run human feedback (UserFeedback)
- Session tags added after completion
- External annotations (stored in separate files)

## Next Steps

- **[Duplex/Voice Testing](/arena/how-to/setup-voice-testing/)** - Set up voice conversation testing
- **[Replay Provider](/arena/explanation/replay-provider/)** - Use recordings for deterministic replay
- **[Configuration Schema](/arena/reference/config-schema/)** - Full configuration reference
