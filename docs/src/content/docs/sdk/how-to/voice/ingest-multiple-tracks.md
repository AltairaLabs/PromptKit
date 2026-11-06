---
title: Ingest several audio tracks into one agent
description: Feed named audio tracks, such as a caller and a customer, to one agent with MultiTrackIngestion and speaker-labeled transcripts.
sidebar:
  order: 5
verified:
  commit: 8489bf3d95c7498f9de0f024769db9399c24e884
  sources:
    - sdk/conversation.go
    - sdk/ingestion_multitrack.go
    - sdk/options.go
---

Transcribe two or more audio sources independently and give a single agent one stream of speaker-labeled turns.

## Prerequisites

- A duplex conversation opened with `sdk.OpenDuplex` (see [Choose a voice mode](/sdk/how-to/voice/choose-voice-mode/)).
- One `base.STTProvider` per track. `base` is `github.com/AltairaLabs/PromptKit/runtime/v2/providers/base`.
- A text provider for the agent. `WithIngestion` and `WithVADMode` are mutually exclusive, so do not pass both.

## Steps

### 1. Describe the tracks

Each `IngestionTrack` is one audio source.

| Field | Type | Description |
|---|---|---|
| `Source` | `string` | Matches `StreamChunk.Source` on audio sent for this track. Unique across tracks. |
| `Speaker` | `string` | Prefix for the track's transcripts: `"CUSTOMER"` yields `CUSTOMER: <transcript>`. Empty means no prefix. |
| `STT` | `base.STTProvider` | Transcribes the track. Required. |
| `TurnConfig` | `*stage.AudioTurnConfig` | Overrides turn detection for the track. `nil` uses `stage.DefaultAudioTurnConfig()`. |

When tracks set their own VAD in `TurnConfig`, give each track a separate instance. The detector keeps state, and a shared one mixes the tracks' audio and corrupts turn detection on both.

### 2. Open the session with the ingestion graph

```go
cfg := sdk.MultiTrackIngestionConfig{
	Tracks: []sdk.IngestionTrack{
		{Source: "customer", Speaker: "CUSTOMER", STT: customerSTT},
		{Source: "agent", Speaker: "AGENT", STT: agentSTT},
	},
	OnTranscript: func(speaker, text string) {
		fmt.Printf("%s: %s\n", speaker, text)
	},
}

conv, err := sdk.OpenDuplex("./call.pack.json", "assistant",
	sdk.WithIngestion(sdk.MultiTrackIngestion(cfg)),
)
if err != nil {
	return err
}
defer conv.Close()
```

`OnTranscript` is optional. It receives the speaker label and the raw transcript of every completed turn on any track, which suits mirroring a live transcript to a UI.

### 3. Send audio with a source

Set `Source` on each chunk you pass to `Conversation.SendChunk`:

```go
err := conv.SendChunk(ctx, &providers.StreamChunk{
	Source: "customer",
	// ... audio fields
})
```

The graph routes the chunk to the track whose `Source` matches. Audio with a source that matches no track is dropped.

## What the graph does

Each track runs its own resample, voice-activity turn detection, speech-to-text and labeling stages. The graph merges the labeled transcripts into the single stream the agent consumes.

`MultiTrackIngestion` forces `EmitEndOfTurn` on every track, so the agent responds once per detected turn instead of only when the session closes. Control signals (end of stream, end of turn, interrupt) carry no source, so the graph sends them to every track.

`MultiTrackIngestion` returns an error from the ingestion function when `Tracks` is empty, when a track has no `STT`, or when two tracks share a `Source`.

## Verify

Send audio on each `Source`. `OnTranscript` fires once per completed turn with that track's speaker label.

## Related

- [Handle turns, barge-in and transcripts](/sdk/how-to/voice/handle-turns-and-barge-in/)
- [Monitor audio in a voice session](/sdk/how-to/voice/monitor-audio/)
