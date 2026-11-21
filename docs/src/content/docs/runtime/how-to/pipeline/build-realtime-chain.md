---
title: Build a Realtime Chain
description: Assemble the stages of a realtime (duplex) pipeline from the provider stage on with stage.NewRealtimeChain, and insert or drop stages at named slots
sidebar:
  order: 4
---

Build the provider-side stages of a realtime (duplex) voice pipeline with `stage.NewRealtimeChain`, in the order the SDK uses, and add or remove stages at named slots.

## Prerequisites

- A provider that implements `providers.StreamInputSupport` (OpenAI Realtime, Gemini Live)
- The front stages of your pipeline: prompt assembly, template, and any input stages such as resampling
- One `TurnState` shared by the front stages and the duplex provider stage

## 1. Create the duplex provider stage

```go
turnState := stage.NewTurnState()

duplex := stage.NewDuplexProviderStageWithTurnState(
    provider, &providers.StreamingInputConfig{}, emitter, turnState,
)
duplex.SetHookRegistry(guardrails)
```

Configure the stage (hooks, tool execution, caller) before you pass it to `NewRealtimeChain`. Build one chain per provider stage: each successful call wires the provider's cues to the cue stage it returns, so a chain built earlier for the same provider stage plays no cues.

## 2. Describe the chain

```go
cfg := stage.RealtimeChainConfig{
    Cue:                    stage.CueOptions{Voice: cueVoice, Cache: clipCache},
    ReorderInputTranscript: true,
    PlayoutHold:            true,
    RecordingStore:         eventStore,
    Recording:              stage.RecordingStageConfig{SessionID: sessionID, IncludeAudio: true},
    MessageBroadcast:       emitter,
    Save: stage.NewIncrementalSaveStage(&stage.IncrementalSaveConfig{
        StateStoreConfig: stateConfig,
        FlushEachTurn:    true,
    }),
    Pacing: stage.RealtimePacingFlushOnCut,
}
```

`NewRealtimeChain` returns the stages in slot order. A slot whose field is unset holds no stage:

| Slot | Stage | Present when |
|---|---|---|
| `RealtimeSlotProvider` | the duplex provider stage | always |
| `RealtimeSlotCue` | the cue stage (`NewDuplexCueStage`), using `Cue` | always, unless dropped |
| `RealtimeSlotTranscriptReorder` | `TranscriptReorderStage` with `InputTranscriptPlaceholder` | `ReorderInputTranscript` |
| `RealtimeSlotPlayoutHold` | `PlayoutHoldStage` | `PlayoutHold` |
| `RealtimeSlotRecording` | `RecordingStage` at `RecordingPositionOutput`, configured by `Recording` | `RecordingStore` is set |
| `RealtimeSlotAudioTap` | your `AudioTap` stage | `AudioTap` is set |
| `RealtimeSlotMemoryExtraction` | your `MemoryExtraction` stage | `MemoryExtraction` is set |
| `RealtimeSlotMessageBroadcast` | `MessageBroadcastStage` | `MessageBroadcast` is set |
| `RealtimeSlotSave` | your `Save` stage | `Save` is set |
| `RealtimeSlotPacing` | `AudioPacingStage` named `audio-pacing-output` | `Pacing` is not `RealtimePacingNone` |

The chain sets the recording stage's position to output, whatever `Recording.Position` holds. A nil `AudioTap`, `MemoryExtraction` or `Save`, including a typed nil such as a nil `*stage.IncrementalSaveStage`, leaves its slot empty.

`Pacing` takes one of three values:

| Value | Effect |
|---|---|
| `RealtimePacingNone` | No pacing stage. Reply audio leaves the chain as fast as the provider sends it. |
| `RealtimePacingFlushOnCut` | Paces reply audio to real time and drops the queued audio of a cut reply. |
| `RealtimePacingPlayThrough` | Paces reply audio to real time and plays a cut reply's queued audio out. |

## 3. Insert your own stages

Put stages directly after a slot with `After`. Stages after one slot keep the order you give them:

```go
cfg.After = map[stage.RealtimeSlot][]stage.Stage{
    stage.RealtimeSlotCue:  {avatarStage},
    stage.RealtimeSlotSave: {turnObserver},
}
```

A slot that holds no stage still places the stages after it at its position. An avatar or video stage goes after `RealtimeSlotCue`, so it sees the cue audio as reply audio.

## 4. Drop stages

List the slots to remove in `Drop`:

```go
cfg.Drop = []stage.RealtimeSlot{stage.RealtimeSlotCue}
```

A dropped slot keeps the stages inserted after it. A dropped cue stage is never wired to the provider stage, so the session's cues play nowhere.

## 5. Build the chain and the pipeline

Build the chain, put your front stages ahead of it and build the pipeline. Disable the execution timeout: a duplex session runs until the caller ends it.

```go
chain, err := stage.NewRealtimeChain(duplex, cfg)
if err != nil {
    return err
}

stages := append([]stage.Stage{
    stage.NewVariableProviderStageWithVarsAndTurnState(vars, nil, turnState),
    stage.NewPromptAssemblyStageWithTurnState(promptRegistry, "voice", vars, turnState),
    stage.NewTemplateStageWithTurnState(nil, turnState),
}, chain...)

pipe, err := stage.NewPipelineBuilderWithConfig(
    stage.DefaultPipelineConfig().WithExecutionTimeout(0),
).Chain(stages...).Build()
```

## Verify

Print the stage names of the chain:

```go
for _, s := range chain {
    fmt.Println(s.Name())
}
```

Without the insertions and drops of steps 3 and 4, the configuration in step 2 prints `duplex_provider`, `cue`, `transcript-reorder`, `playout-hold`, `recording_output`, `message_broadcast`, `incremental_save` and `audio-pacing-output`.

## Troubleshooting

`NewRealtimeChain` returns an error wrapping `stage.ErrRealtimeChainProvider` when the provider stage is nil or `Drop` lists `RealtimeSlotProvider`. It returns an error wrapping `stage.ErrRealtimeChainSlot` when `After` or `Drop` names a slot the chain does not have, or `After` holds a nil stage (typed or untyped). It returns an error wrapping `stage.ErrRealtimeChainPacing` when `Pacing` holds a value other than the three listed in step 2. A call that returns an error leaves the provider stage unchanged.

If `Build` rejects duplicate stage names, rename the stage you inserted: the pipeline builder treats stage names as unique IDs.

## Related

- [Configure pipeline](/runtime/how-to/pipeline/configure-pipeline/)
- [Pipeline reference](/runtime/reference/pipeline/)
- [Choose a voice mode](/sdk/how-to/voice/choose-voice-mode/)
