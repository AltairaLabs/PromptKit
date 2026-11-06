---
title: Monitor audio in a voice session
description: Tap the user's and the agent's audio with WithAudioMonitor and read it from Conversation.AudioMonitor for level meters, playback and recording UIs.
sidebar:
  order: 4
verified:
  commit: 8489bf3d95c7498f9de0f024769db9399c24e884
  sources:
    - sdk/audio_monitor.go
    - sdk/conversation.go
    - sdk/internal/pipeline/builder.go
    - sdk/options.go
---

Read the audio that flows through a duplex voice session, in both directions, without changing how the session runs.

## Prerequisites

- A voice session opened with `sdk.OpenDuplex` (see [Choose a voice mode](/sdk/how-to/voice/choose-voice-mode/)).
- A host that consumes the frames. The SDK has no audio device and runs no HTTP server.

## Steps

### 1. Turn monitoring on

Pass `sdk.WithAudioMonitor` when you open the conversation:

```go
package main

import (
	"fmt"
	"log"

	"github.com/AltairaLabs/PromptKit/sdk/v2"
)

func main() {
	conv, err := sdk.OpenDuplex("./voice.pack.json", "assistant",
		sdk.WithAudioMonitor(sdk.AudioMonitorOptions{
			Mode:       sdk.AudioMonitorOn,
			Rate:       24000,
			LevelMeter: true,
		}),
	)
	if err != nil {
		log.Fatal(err)
	}
	defer conv.Close()

	go func() {
		for frame := range conv.AudioMonitor() {
			fmt.Printf("%s level=%.2f\n", frame.Direction, frame.Level)
		}
	}()

	// ... send audio and read responses
}
```

`WithAudioMonitor` returns an error for a mode or rate outside the values in the table below, and `OpenDuplex` returns an error that wraps it.

### 2. Choose the options

| Field | Type | Default | Description |
|---|---|---|---|
| `Mode` | `AudioMonitorMode` | `auto` | `auto` monitors when standard output is a terminal. `on` always monitors. `off` never does. |
| `Rate` | `int` | `24000` | Sample rate every frame is resampled to: `16000`, `24000` or `48000`. |
| `LevelMeter` | `bool` | `false` | Sets `Level` on each frame. |
| `LocalSink` | `bool` | `false` | A hint for your host: play the frames on the system audio device. The SDK does not read it. |
| `SSEPlayback` | `bool` | `false` | A hint for your host: relay the frames over SSE to a web UI. The SDK does not read it. |

### 3. Read the frames

`Conversation.AudioMonitor()` returns a receive-only channel of `AudioMonitorFrame`. It returns `nil` when monitoring is off, and a receive from a `nil` channel blocks forever, so check the result before ranging over it in code that runs with `Mode: sdk.AudioMonitorAuto`.

| Field | Type | Description |
|---|---|---|
| `Direction` | `AudioMonitorDirection` | `sdk.AudioMonitorInput` (`"input"`) is the user's audio before the provider sees it. `sdk.AudioMonitorOutput` (`"output"`) is the agent's audio as the pipeline produces it. |
| `Samples` | `[]int16` | Mono 16-bit PCM at `SampleRate`. The receiver owns the slice. |
| `SampleRate` | `int` | The monitor's `Rate`. |
| `Level` | `float64` | RMS level from 0 (silence) to 1 (full scale). Set only when `LevelMeter` is true. |
| `Timestamp` | `time.Time` | The time the pipeline stamped on the audio element. |

Only 16-bit PCM audio produces frames. The monitor resamples each chunk to `Rate` when its source rate differs.

## Verify

Speak into the session and watch `input` frames arrive. When the agent answers, `output` frames follow.

## Slow consumers

The channel holds 256 frames. A frame that finds it full is dropped, so a slow consumer never delays the conversation, and it also loses audio. Drain the channel in its own goroutine and hand the frames on.

`Conversation.Close` closes the channel, which ends a `range` loop.

## Related

- [Handle turns, barge-in and transcripts](/sdk/how-to/voice/handle-turns-and-barge-in/)
- [Conversation reference](/sdk/reference/conversation-manager/)
