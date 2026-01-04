---
title: Voice Activity Detection (VAD) Demo
description: Example demonstrating vad-demo
sidebar:
  order: 100
---


This example demonstrates Voice Activity Detection using PromptKit's audio package.

## Features

- **SimpleVAD**: Basic voice activity detection using RMS energy analysis
- **State Tracking**: Monitor transitions between quiet/starting/speaking/stopping
- **Configurable Parameters**: Tune sensitivity for different environments
- **Event Notifications**: React to state changes in real-time

## Running

```bash
cd sdk/examples/vad-demo
go run .
```

This example runs with simulated audio data - no microphone required.

## VAD States

| State | Description |
|-------|-------------|
| `quiet` | No voice activity detected |
| `starting` | Voice beginning (within start threshold) |
| `speaking` | Active speech detected |
| `stopping` | Voice ending (within stop threshold) |

## Configuration

### Default Parameters

```go
params := audio.DefaultVADParams()
// Confidence: 0.5
// StartSecs: 0.2
// StopSecs: 0.8
// MinVolume: 0.01
// SampleRate: 16000
```

### Strict VAD (noisy environments)

```go
params := audio.VADParams{
    Confidence: 0.7,   // Higher confidence required
    StartSecs:  0.3,   // Longer speech to trigger
    StopSecs:   1.2,   // Allow longer pauses
    MinVolume:  0.02,  // Higher volume threshold
    SampleRate: 16000,
}
```

### Sensitive VAD (quiet environments)

```go
params := audio.VADParams{
    Confidence: 0.3,   // More sensitive
    StartSecs:  0.1,   // Quick start detection
    StopSecs:   0.5,   // Quick end detection
    MinVolume:  0.005, // Detect quiet speech
    SampleRate: 16000,
}
```

## State Change Events

```go
vad, _ := audio.NewSimpleVAD(audio.DefaultVADParams())
stateChanges := vad.OnStateChange()

go func() {
    for event := range stateChanges {
        fmt.Printf("State: %s -> %s (confidence: %.2f)\n",
            event.PrevState, event.State, event.Confidence)
    }
}()
```

## Integration with SDK

VAD is typically used with audio sessions:

```go
conv, _ := sdk.Open("./pack.json", "assistant")

// Create audio session with VAD
session, _ := conv.OpenAudioSession(ctx,
    sdk.WithSessionVAD(audio.NewSimpleVAD(audio.DefaultVADParams())),
)

// VAD automatically processes audio chunks
session.SendChunk(ctx, audioChunk)
```

## Notes

- VAD is energy-based (RMS volume analysis)
- Works with 16-bit PCM audio at configurable sample rates
- Default sample rate is 16kHz (common for speech recognition)
- Transition thresholds prevent false positives from brief sounds
