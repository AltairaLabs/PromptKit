# Multi-track Audio Ingestion

A generic demo of [`sdk.MultiTrackIngestion`](../../ingestion_multitrack.go): two
speakers' audio arrive on separate tracks, are transcribed independently, and the
merged, speaker-labeled transcript drives an assistant that fires **once per turn**
over a `WithIngestion` duplex.

## What it shows

- **Per-track routing** — audio tagged `Source: "speaker-a" | "speaker-b"` is fanned
  out to one independent `resample → VAD turn → STT → label` pipeline per track.
- **Speaker labeling** — each transcript becomes a `SPEAKER-A: …` / `SPEAKER-B: …`
  user message the agent can attribute.
- **Per-turn firing** — the duplex agent runs once per completed turn, not once per
  session.

## Run it

Keyless (no API keys — synthetic audio, a scripted transcriber, and a mock LLM):

    go run .

With real Claude for the assistant:

    export ANTHROPIC_API_KEY=sk-...
    go run . --live

`--live` swaps only the LLM. Transcription stays scripted so the demo needs no STT
key; see `conversation.go` for why the audio is synthetic and the STT is a stand-in.
Add `--verbose` to see the pipeline's own INFO/DEBUG logs.

Expected keyless output:

    🎙  multi-track ingestion demo
       mode: keyless (mock LLM, scripted STT)

      SPEAKER-A:  Morning — are we still on for the two o'clock review?
      assistant:  Noted — thanks.
      SPEAKER-B:  Yes, I've booked the small room and sent the agenda.
      assistant:  Noted — thanks.
      ...

## How it's wired

```go
ingest := sdk.MultiTrackIngestion(sdk.MultiTrackIngestionConfig{
    Tracks: []sdk.IngestionTrack{
        {Source: "speaker-a", Speaker: "SPEAKER-A", STT: newScriptedSTT("speaker-a")},
        {Source: "speaker-b", Speaker: "SPEAKER-B", STT: newScriptedSTT("speaker-b")},
    },
    OnTranscript: printTranscriptLine,
})
conv, _ := sdk.OpenDuplex("./assistant.pack.json", "assist",
    sdk.WithProvider(mockOrClaude),
    sdk.WithIngestion(ingest),
)
```

Audio is fed with `conv.SendChunk`, tagging each frame's `Source` so the router
splits it onto the right track. `MultiTrackIngestion` broadcasts control signals
(end-of-turn / end-of-stream) to every track, so `Close` drains cleanly instead of
deadlocking on a merge that never sees end-of-stream.

## Extending it

Adding a third speaker is one more `IngestionTrack`. Each track that sets an explicit
`TurnConfig.VAD` needs its own VAD instance — the detector is stateful and must not be
shared across tracks.
