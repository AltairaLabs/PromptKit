---
title: Choose a voice mode
description: Open a duplex voice session in VAD mode, with a text LLM and separate STT and TTS services, or in ASM mode, with a native audio model.
sidebar:
  order: 1
verified:
  commit: 885cafa787a84bfba66f158fe51cda15a077ec23
  sources:
    - runtime/providers/gemini/setup.go
    - runtime/providers/gemini/streaming_support.go
    - runtime/providers/openai/streaming_support.go
    - sdk/conversation.go
    - sdk/examples/openai-realtime/main.go
    - sdk/internal/pipeline/builder_vad.go
    - sdk/options.go
    - sdk/sdk.go
    - sdk/session/duplex_session.go
---
Open a duplex voice session in the mode that fits your model: VAD mode for a text LLM, ASM mode for a model that takes audio directly.

## Prerequisites

- A pack with the prompt the session uses. [Tutorial 7](/sdk/tutorials/07-audio-sessions/) creates `voice-assistant.pack.json`.
- For VAD mode, API keys for the model and for your STT and TTS services.
- For ASM mode, `GEMINI_API_KEY` for Gemini Live or `OPENAI_API_KEY` for OpenAI Realtime.

## Step 1: Pick the mode

| | VAD mode | ASM mode |
|---|---|---|
| Option | `sdk.WithVADMode` | `sdk.WithStreamingConfig` |
| Models | any text LLM | Gemini Live, OpenAI Realtime |
| Pipeline | `AudioTurnStage` → `STTStage` → `ProviderStage` → `TTSStageWithInterruption` | the provider's own live session |
| Speech services | your choice of STT and TTS service | the model's own |

`OpenDuplex` refuses a provider that cannot stream input unless you pass `WithVADMode` (or `WithIngestion`).

## Step 2a: Open in VAD mode

Pass the STT service, the TTS service and a `VADModeConfig` to `WithVADMode`. A `nil` config uses `DefaultVADModeConfig()`.

```go
package main

import (
	"log"
	"os"
	"time"

	"github.com/AltairaLabs/PromptKit/runtime/v2/stt"
	"github.com/AltairaLabs/PromptKit/runtime/v2/tts"
	"github.com/AltairaLabs/PromptKit/sdk/v2"
)

func main() {
	sttService := stt.NewOpenAI(os.Getenv("OPENAI_API_KEY"))
	ttsService := tts.NewOpenAI(os.Getenv("OPENAI_API_KEY"))

	vadConfig := &sdk.VADModeConfig{
		SilenceDuration:   500 * time.Millisecond, // end the turn sooner
		MinSpeechDuration: 100 * time.Millisecond,
		MaxTurnDuration:   20 * time.Second,
		SampleRate:        16000,
		Language:          "en",
		Voice:             "nova",
		Speed:             1.1,
	}

	conv, err := sdk.OpenDuplex("./voice-assistant.pack.json", "assistant",
		sdk.WithVADMode(sttService, ttsService, vadConfig),
	)
	if err != nil {
		log.Fatal(err)
	}
	defer conv.Close()
}
```

A field left at its zero value takes its default (800ms silence, 200ms minimum speech, 30s maximum turn, 16000 Hz, `alloy`); the [`VADModeConfig` reference](/sdk/reference/conversation-manager/#VADModeConfig) lists every field.

VAD mode does not resample the input: send audio at `SampleRate`.

## Step 2b: Open in ASM mode

Pick a model the provider's live API accepts and describe the audio you send with `WithStreamingConfig`:

```go
package main

import (
	"log"

	"github.com/AltairaLabs/PromptKit/runtime/v2/providers"
	"github.com/AltairaLabs/PromptKit/runtime/v2/types"
	"github.com/AltairaLabs/PromptKit/sdk/v2"
)

func main() {
	conv, err := sdk.OpenDuplex("./voice-assistant.pack.json", "assistant",
		sdk.WithModel("gemini-3.8-live"),
		sdk.WithStreamingConfig(&providers.StreamingInputConfig{
			Config: types.StreamingMediaConfig{
				Type:       types.ContentTypeAudio,
				SampleRate: 16000,
				Channels:   1,
			},
		}),
	)
	if err != nil {
		log.Fatal(err)
	}
	defer conv.Close()
}
```

For OpenAI Realtime, use `sdk.WithModel("gpt-realtime")` and a `SampleRate` of 24000. The [`openai-realtime` example](https://github.com/AltairaLabs/PromptKit/tree/main/sdk/examples/openai-realtime) shows the full configuration.

## Verify

Call `conv.Response()`, send audio with `conv.SendChunk`, and read the channel. The first `SendChunk` starts the pipeline. A chunk with `input_transcription` metadata shows the session heard the speech, and a chunk with `assistant_turn_complete` closes the reply. [Handle turns, barge-in and transcripts](/sdk/how-to/voice/handle-turns-and-barge-in/) shows the loop.

## Troubleshooting

- `provider ... does not support duplex streaming`: the provider has no live audio API. Use VAD mode, or a model from the table in Step 1.
- `WithIngestion and WithVADMode are mutually exclusive`: pass one of the two.

## Related

- [Tutorial 7: Audio sessions](/sdk/tutorials/07-audio-sessions/)
- [Handle turns, barge-in and transcripts](/sdk/how-to/voice/handle-turns-and-barge-in/)
- [Configure text-to-speech](/sdk/how-to/voice/configure-tts/)
- [`VADModeConfig` reference](/sdk/reference/conversation-manager/)
