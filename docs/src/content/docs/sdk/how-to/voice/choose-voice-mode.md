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

To tell the session who it acts for, pass `sdk.WithCaller`. Tool executors, tool hooks, provider hooks, chunk interceptors and session hooks receive the caller on their requests, and it does not depend on any context. See [Act for a caller](/sdk/how-to/tools/act-for-a-caller/). Realtime sessions (ASM mode) run their tool calls through the same tool hooks, policy and approval gate as text turns.

A duplex session runs its tool calls on its own context, not on the context of any `SendChunk` call. If your tool handlers read other host values from the context, such as a logger or a database handle, open the session with `sdk.OpenDuplexContext` instead of `sdk.OpenDuplex`:

```go
conv, err := sdk.OpenDuplexContext(ctx, "./voice-assistant.pack.json", "assistant", opts...)
```

Every tool call in the session sees the values on `ctx`, as a text turn's tool calls see the values on the context passed to `Send`. Session-start hooks and forks of the session see them too. `sdk.ResumeDuplexContext`, `sdk.OpenVoiceContext` and `PackTemplate.OpenDuplexContext` take the context the same way.

Canceling `ctx` does not end the session; `conv.Close` does. A deadline on `ctx` bounds only the open. The session keeps the values on `ctx` for its whole life, so build a context with what the tool calls need rather than passing a request's context, whose span, logger or transaction ends with the request.

The session also ends without `conv.Close`: when nothing happens in it for the idle timeout (`sdk.WithIdleTimeout`, 30 seconds by default), when the provider reports an error, or when the provider closes the connection. `Response()` closes after any error that ended the session, and `Done()` closes. Input sent after that (`SendChunk`, `SendText`, `SendFrame`, `SendVideoChunk`, `TriggerStart`, `ContinueDuplex`) returns at once with the error that ended the session, which wraps `stage.ErrIdleTimeout` for the idle timeout, or with `sdk.ErrSessionClosed` when the provider closed the session without one (match it with `errors.Is`). Input a send handed over shortly before the session ended may never reach the provider: it is lost with the session. `conv.SessionError()` returns the same error once the session has ended, and nil while it runs or when it ended without one.

## Realtime tool calls

In ASM mode the pipeline runs the model's tool calls and sends their results to the session, as a text turn does:

- The session is offered the tools a text turn would be: the prompt's tools, capability tools such as `memory__recall`, and tools a skill grants. Tools in `StreamingInputConfig.Tools` are added, and a tool declared there replaces a conversation tool of the same name.
- A call to a registered tool the conversation does not offer, such as a pack tool the prompt does not list or a tool whose skill grant was revoked, is refused: it gets the result `Tool <name> was not run: it is not available this turn` and its handler does not run. Tools in `StreamingInputConfig.Tools` are offered. This and the refusals of tools the session lacks or still holds come after the tool policy's `Blocklist` and the tool hooks, in the order of the calls. These checks are made where the pipeline runs the calls: in SDK sessions, and in a host pipeline whose `DuplexProviderStage` was given `SetToolExecution`. A host pipeline that runs the calls itself (`DuplexSessionConfig.PipelineRunsTools` unset) is not checked. A tool excluded after repeated failures still runs.
- OpenAI Realtime is given new tools mid-session when a skill grants them. Gemini Live fixes its tools when the session starts, so a call to a tool granted later is refused, and so is a call to a tool the conversation has since withdrawn, such as one whose skill grant was revoked.
- The session is sent only `StreamingInputConfig.ToolChoice`. A tool policy `tool_choice` of `none` offers no tools; the policy's other choices are not sent, because they would force a tool call after every tool result.
- The calls of one model turn run in parallel, up to the tool policy's `MaxParallelToolCalls`.
- The tool policy (`sdk.WithToolPolicy` merged with the prompt's `tool_policy`) applies: `Blocklist`, `MaxToolCallsPerTurn`, `MaxIdenticalToolCalls`, `MaxCallsPerMinute` and `MaxRounds`. A call a limit stops gets a refusal result, and `Response()` carries the limit as a chunk with `Error` set. Every later call of the same tool turn is refused, and the session stays open.
- If the model calls tools twice more after its tool turn was stopped, both batches get refusals. A third batch closes the session.
- A batch of calls that are all refused because their tools are not available counts the same way. The first two get their refusals; a third stops the tool turn and closes the session, with an error naming the tool. A batch in which any call runs does not count.
- When the session's spend passes `MaxCostUSD`, the calls of that model turn are refused and the session closes. The refusals are recorded in the conversation but not sent to the session, because a tool result starts another paid reply.
- Each model turn emits one `provider.call.completed` with its tokens and cost when it completes. OpenAI Realtime reports each turn's usage with the turn. Gemini 2.5 Live reports the usage of a turn that calls tools together with the reply that follows it, so that turn's event carries no tokens and the reply's carries both. Gemini 3 Live reports a tool-call turn's usage after its tool calls: it arrives as a `provider.call.usage` event with that turn's `CallID`. A call's total cost is its `provider.call.completed` cost plus its `provider.call.usage` costs.
- Cost a session reports after its turn has ended counts toward `MaxCostUSD` when it arrives, and a budget it exceeds is reported on the call it belongs to. Each realtime session numbers its provider calls, so the cost goes to its own call: on that call's `provider.call.completed` while the call is open, otherwise as a `provider.call.usage`. A provider that does not number its calls has such cost reported on the call started last; cost for a call too old for the session to remember is a `provider.call.usage` with no `CallID`. Calls follow the provider's own replies, and every call that starts completes once: when its reply ends (with or without a message), when an interruption names it, when the provider moves on to its next reply, or when the session closes. User input never ends a call: a reply still under way when the user speaks or types goes on to its end, and the user's turn starts with the provider's next reply. A call whose message was not emitted (none was produced, or a guardrail denied it) has `NoMessage` set on its `provider.call.completed`. A spent budget or an output guardrail denying the turn still completes the call and counts its cost. So does a provider error that ends the session. A message carries only its own call's text and tool calls: when a call is superseded, what it had produced so far is dropped, and output the provider sends for a call that has already completed (a canceled response's trailing audio or tool calls) is dropped too, apart from its cost. In the conversation it goes on the next message the session emits, so the messages' `CostInfo` adds up to what the session spent. When the session closes it goes on the last message emitted then; if none is emitted, it is counted and reported on its event but appears in no message.
- Tool hooks, tool result rewriters and the approval gate apply to each call. Results reach the model formatted as on a text turn.
- A chunk with `Error` set does not close `Response()`. The channel closes when the session ends.
- The model receives the text of a tool result. Neither OpenAI Realtime nor Gemini Live takes a tool result's image, audio or other non-text parts, so the session sends their text only and logs one warning naming the provider and the part types dropped. A result with no text at all reaches the model as a placeholder naming those types, such as `[tool returned image content this session cannot receive]`.
- `conv.Close` ends the session at once. A tool call still running then is canceled (its handler's context is done) and its result never reaches the model. A runtime pipeline whose input channel closes, with no drain, keeps answering tool calls until the `DuplexProviderStage` 30-second final-response timeout closes the session. The timeout is extended while a tool call runs or the reply to its result is under way, up to 5 minutes after input closed; a call still running then is canceled the same way. The pipeline returns only once the canceled call has.
- The conversation stores each tool call message followed by a result for every one of its calls, so `sdk.Resume` can continue it on a text provider. A call canceled when the session ended is stored with what its handler returned, usually `context canceled`. A call whose round never started is stored with the error result `Tool <name> not executed: the session closed before it ran`. A message that arrives while a round runs, such as a user turn or the reply to it, is stored after the round's results. It also reaches every stage after the `DuplexProviderStage` (message recording, `message.created` events, the state store) only once the round's tools have returned, so a long tool call delays those messages, not the audio or text streamed from them. A call held for approval does not hold back the messages after it.
- A spoken reply is stored with its transcript and its audio. When the conversation is resumed on a text model that cannot take audio (OpenAI models without audio input, on Chat Completions or Responses, and Claude), each assistant message is sent as its transcript and its audio is left out. The caller's own audio is not left out: those providers still refuse it. Gemini text models take audio, so they are sent both.

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
