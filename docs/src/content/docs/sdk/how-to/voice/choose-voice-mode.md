---
title: Choose a voice mode
description: Open a duplex voice session in VAD mode, with a text LLM and separate STT and TTS services, or in ASM mode, with a native audio model.
sidebar:
  order: 1
verified:
  commit: 8489bf3d95c7498f9de0f024769db9399c24e884
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

Pass the STT service, the TTS service and a `VADModeConfig` to `WithVADMode`. A `nil` config uses `DefaultVADModeConfig()`. A `nil` service keeps the one an earlier `WithSTTProvider`, `WithTTS` or `WithTTSProvider` option configured.

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

## Step 3: Pass the caller and host context to tool calls

To tell the session who it acts for, pass `sdk.WithCaller`. Tool executors, tool hooks and session hooks receive the caller on their requests, and it does not depend on any context. See [Act for a caller](/sdk/how-to/tools/act-for-a-caller/). Realtime sessions (ASM mode) run their tool calls through the same tool hooks, policy and approval gate as text turns.

A duplex session runs its tool calls on its own context, not on the context of any `SendChunk` call. If your tool handlers read other host values from the context, such as a logger or a database handle, open the session with `sdk.OpenDuplexContext` instead of `sdk.OpenDuplex`:

```go
conv, err := sdk.OpenDuplexContext(ctx, "./voice-assistant.pack.json", "assistant", opts...)
```

Every tool call in the session sees the values on `ctx`, as a text turn's tool calls see the values on the context passed to `Send`. Session-start hooks and forks of the session see them too. `sdk.ResumeDuplexContext`, `sdk.OpenVoiceContext` and `PackTemplate.OpenDuplexContext` take the context the same way.

Canceling `ctx` does not end the session; `conv.Close` does. A deadline on `ctx` bounds only the open. The session keeps the values on `ctx` for its whole life, so build a context with what the tool calls need rather than passing a request's context, whose span, logger or transaction ends with the request.

## Realtime tool calls

In ASM mode the pipeline runs the model's tool calls and sends their results to the session, as a text turn does:

- The session is offered the tools a text turn would be: the prompt's tools, capability tools such as `memory__recall`, and tools a skill grants. Tools in `StreamingInputConfig.Tools` are added, and a tool declared there replaces a conversation tool of the same name.
- OpenAI Realtime is given new tools mid-session when a skill grants them. Gemini Live fixes its tools when the session starts, so a call to a tool granted later is refused.
- The session is sent only `StreamingInputConfig.ToolChoice`. A tool policy `tool_choice` of `none` offers no tools; the policy's other choices are not sent, because they would force a tool call after every tool result.
- The calls of one model turn run in parallel, up to the tool policy's `MaxParallelToolCalls`.
- The tool policy (`sdk.WithToolPolicy` merged with the prompt's `tool_policy`) applies: `Blocklist`, `MaxToolCallsPerTurn`, `MaxIdenticalToolCalls`, `MaxCallsPerMinute` and `MaxRounds`. A call a limit stops gets a refusal result, and `Response()` carries the limit as a chunk with `Error` set. Every later call of the same tool turn is refused, and the session stays open.
- If the model calls tools twice more after its tool turn was stopped, both batches get refusals. A third batch closes the session.
- When the session's spend passes `MaxCostUSD`, the calls of that model turn are refused and the session closes.
- Each model turn emits `provider.call.completed` with its tokens and cost. Gemini Live reports the usage of a turn that calls tools together with the reply that follows it, so that turn's event carries no tokens and the reply's carries both. OpenAI Realtime reports each turn's own usage.
- Tool hooks, tool result rewriters and the approval gate apply to each call. Results reach the model formatted as on a text turn.
- A chunk with `Error` set does not close `Response()`. The channel closes when the session ends.
- The model receives the text of a tool result. Neither OpenAI Realtime nor Gemini Live takes a tool result's image, audio or other non-text parts, so the session sends their text only and logs one warning naming the provider and the part types dropped. A result with no text at all reaches the model as a placeholder naming those types, such as `[tool returned image content this session cannot receive]`.
- `conv.Close` ends the session at once, so a tool call still running then is dropped with it and its result never reaches the model. A runtime pipeline whose input channel closes, with no drain, keeps answering tool calls until the `DuplexProviderStage` 30-second final-response timeout closes the session, and a call still running then is dropped the same way.

## Verify

Call `conv.Response()`, send audio with `conv.SendChunk`, and read the channel. The first `SendChunk` starts the pipeline. A chunk with `input_transcription` metadata shows the session heard the speech, and a chunk with `assistant_turn_complete` closes the reply. [Handle turns, barge-in and transcripts](/sdk/how-to/voice/handle-turns-and-barge-in/) shows the loop.

## Troubleshooting

- `provider ... does not support duplex streaming`: the provider has no live audio API. Use VAD mode, or a model from the table in Step 1.
- `WithIngestion and WithVADMode are mutually exclusive`: pass one of the two.

## Related

- [Tutorial 7: Audio sessions](/sdk/tutorials/07-audio-sessions/)
- [Handle turns, barge-in and transcripts](/sdk/how-to/voice/handle-turns-and-barge-in/)
- [Act for a caller](/sdk/how-to/tools/act-for-a-caller/)
- [Configure text-to-speech](/sdk/how-to/voice/configure-tts/)
- [`VADModeConfig` reference](/sdk/reference/conversation-manager/)
