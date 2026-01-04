---
title: Session Recording Architecture
sidebar:
  order: 6
---
This document explains how PromptKit's session recording system captures, stores, and replays LLM conversations with full fidelity.

## Overview

Session recording provides a complete audit trail of LLM interactions. Unlike simple logging, recordings capture:

- **Precise timing**: Millisecond-accurate event timestamps
- **Complete data**: All messages, tool calls, and media
- **Reconstructable state**: Enough information to replay conversations exactly

```
┌─────────────────┐     ┌─────────────────┐     ┌─────────────────┐
│   Live Session  │ ──► │   Event Store   │ ──► │   Recording     │
│   (Emitter)     │     │   (JSONL)       │     │   (Replay)      │
└─────────────────┘     └─────────────────┘     └─────────────────┘
        │                       │                       │
        ▼                       ▼                       ▼
   Real-time              Persistent              Synchronized
   events                 storage                 playback
```

## Event-Driven Architecture

### Event Emitter

The `Emitter` is the heart of session recording. It captures events as they occur:

```go
emitter := events.NewEmitter(eventBus, runID, scenarioID, conversationID)

// Events are emitted automatically during conversation
emitter.ConversationStarted(systemPrompt)
emitter.MessageCreated(role, content)
emitter.AudioInput(audioData)
emitter.ProviderCallStarted(provider, model)
emitter.ToolCallStarted(toolName, args)
// ...
```

Events flow through an EventBus to registered subscribers:

```
Emitter ──► EventBus ──┬──► FileEventStore (persistence)
                       ├──► Metrics Collector
                       └──► Real-time UI
```

### Event Types

Events are categorized by their domain:

| Category | Events | Purpose |
|----------|--------|---------|
| Conversation | `started`, `ended` | Session lifecycle |
| Message | `created`, `updated` | Content exchange |
| Audio | `input`, `output` | Voice data |
| Provider | `call.started`, `call.completed` | LLM API calls |
| Tool | `call.started`, `call.completed` | Function execution |
| Validation | `started`, `completed` | Guardrail checks |

### Event Structure

Each event contains:

```go
type Event struct {
    Type      EventType       // Event category
    Timestamp time.Time       // When it occurred
    SessionID string          // Session identifier
    Sequence  int64           // Ordering guarantee
    Data      interface{}     // Event-specific payload
}
```

## Storage Format

### FileEventStore

Events are persisted in JSONL (JSON Lines) format:

```jsonl
{"seq":1,"event":{"type":"conversation.started","timestamp":"2024-01-15T10:30:00Z",...}}
{"seq":2,"event":{"type":"message.created","timestamp":"2024-01-15T10:30:00.1Z",...}}
{"seq":3,"event":{"type":"audio.input","timestamp":"2024-01-15T10:30:00.2Z",...}}
```

Benefits:
- **Append-only**: Safe for concurrent writes
- **Streamable**: Process without loading entire file
- **Human-readable**: Easy debugging

### SessionRecording Format

For export/import, recordings use a structured format:

```jsonl
{"type":"metadata","session_id":"...","start_time":"...","duration":"..."}
{"type":"event","offset":"100ms","event_type":"message.created",...}
{"type":"event","offset":"200ms","event_type":"audio.input",...}
```

The loader auto-detects both formats:

```go
rec, err := recording.Load("session.jsonl")  // Works with either format
```

## Media Timeline

For recordings with audio/video, the `MediaTimeline` provides synchronized access:

```
Recording
    │
    ▼
MediaTimeline
    ├── TrackAudioInput ──► User speech segments
    ├── TrackAudioOutput ──► Assistant speech segments
    └── TrackVideo ──► Video frames (if present)
```

### Track Structure

Each track contains time-ordered segments:

```go
type MediaSegment struct {
    Offset    time.Duration  // Start time relative to session
    Duration  time.Duration  // Segment length
    Data      []byte         // Raw media data
    Format    AudioFormat    // Sample rate, channels, encoding
}
```

### Audio Reconstruction

Audio can be exported as standard WAV files:

```go
timeline := rec.ToMediaTimeline(nil)
timeline.ExportAudioToWAV(events.TrackAudioInput, "user.wav")
timeline.ExportAudioToWAV(events.TrackAudioOutput, "assistant.wav")
```

The export process:
1. Collects all segments for the track
2. Concatenates PCM data in time order
3. Writes RIFF/WAVE header with format info
4. Outputs standard 16-bit PCM WAV

## Replay System

### ReplayPlayer

The `ReplayPlayer` provides synchronized access to recordings:

```go
player, _ := recording.NewReplayPlayer(rec)

// Seek to any position
player.Seek(5 * time.Second)

// Query state at current position
state := player.GetState()
// state.CurrentEvents - events at this moment
// state.RecentEvents - events in last 2 seconds
// state.Messages - all messages up to this point
// state.AudioInputActive - is user speaking?
// state.AudioOutputActive - is assistant speaking?
```

### Playback State

At any position, you can access:

| Field | Description |
|-------|-------------|
| `Position` | Current offset from session start |
| `Timestamp` | Absolute timestamp |
| `CurrentEvents` | Events within 50ms of position |
| `RecentEvents` | Events in last 2 seconds |
| `Messages` | Accumulated conversation |
| `AudioInputActive` | User audio present |
| `AudioOutputActive` | Assistant audio present |
| `ActiveAnnotations` | Annotations in scope |

### Annotation Correlation

Annotations can be attached to recordings for review:

```go
player.SetAnnotations([]*annotations.Annotation{
    // Session-level annotation
    annotations.ForSession().WithScore("quality", 0.92),

    // Time-range annotation
    annotations.InTimeRange(start, end).WithComment("Good response"),

    // Event-targeted annotation
    annotations.ForEvent(eventSeq).WithLabel("category", "greeting"),
})
```

During playback, active annotations are included in state queries.

## Replay Provider

For deterministic replay, the `ReplayProvider` simulates the original provider:

```go
provider, _ := replay.NewProviderFromRecording(rec)

// Returns the same responses as the original session
response, _ := provider.Complete(ctx, messages, opts)
```

Use cases:
- **Regression testing**: Verify behavior against known-good responses
- **Debugging**: Reproduce exact conversation flows
- **Offline testing**: No API calls needed

## Data Flow Summary

```
┌─────────────────────────────────────────────────────────────────┐
│                        LIVE SESSION                              │
│                                                                   │
│  User ──► Pipeline ──► Provider ──► Response ──► User            │
│              │                          │                         │
│              ▼                          ▼                         │
│          Emitter ────────────────► EventBus                       │
│                                        │                          │
└────────────────────────────────────────┼──────────────────────────┘
                                         │
                                         ▼
┌─────────────────────────────────────────────────────────────────┐
│                        STORAGE                                    │
│                                                                   │
│  EventBus ──► FileEventStore ──► session.jsonl                   │
│                                                                   │
└────────────────────────────────────────┬──────────────────────────┘
                                         │
                                         ▼
┌─────────────────────────────────────────────────────────────────┐
│                        REPLAY                                     │
│                                                                   │
│  session.jsonl ──► SessionRecording ──► ReplayPlayer             │
│                           │                    │                  │
│                           ▼                    ▼                  │
│                    MediaTimeline         Synchronized            │
│                    (WAV export)          Playback                │
│                                                                   │
└─────────────────────────────────────────────────────────────────┘
```

## Performance Considerations

### Recording Overhead

- **CPU**: Minimal - events are queued and written asynchronously
- **Memory**: Bounded - segments are written incrementally
- **Disk**: Proportional to conversation length and audio duration

### Audio Data Size

Audio is stored as base64-encoded PCM:
- Input: 16kHz, 16-bit mono = ~32KB/second
- Output: 24kHz, 16-bit mono = ~48KB/second
- Base64 encoding adds ~33% overhead

A 5-minute voice conversation generates approximately:
- Raw audio: ~24MB
- JSONL with metadata: ~32MB

### Optimization Tips

1. **Disable for CI**: Skip recording for quick validation runs
2. **Compress archives**: JSONL compresses well (70-80% reduction)
3. **Retention policies**: Auto-delete old recordings
4. **Selective recording**: Only enable for specific scenarios

## Next Steps

- **[Session Recording How-To](/arena/how-to/session-recording/)** - Practical usage guide
- **[Duplex Architecture](/arena/explanation/duplex-architecture/)** - Voice conversation system
- **[Testing Philosophy](/arena/explanation/testing-philosophy/)** - Why test prompts?
