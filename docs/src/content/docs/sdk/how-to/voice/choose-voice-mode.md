---
title: Choose a voice mode
description: Open a duplex voice session in VAD mode, with a text LLM and separate STT and TTS services, or in ASM mode, with a native audio model.
sidebar:
  order: 1
verified:
  commit: 8489bf3d95c7498f9de0f024769db9399c24e884
  sources:
    - runtime/pipeline/stage/cue_bank.go
    - runtime/pipeline/stage/cue_sources.go
    - runtime/pipeline/stage/stages_cue.go
    - runtime/pipeline/stage/stages_duplex_provider_hooks.go
    - runtime/pipeline/stage/stages_speech.go
    - runtime/providers/gemini/finish_reason.go
    - runtime/providers/gemini/setup.go
    - runtime/providers/gemini/streaming_support.go
    - runtime/providers/openai/realtime_interrupt.go
    - runtime/providers/openai/streaming_support.go
    - runtime/stt/cartesia.go
    - runtime/stt/elevenlabs.go
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

A field left at its zero value takes its default (800ms silence, 200ms minimum speech, 30s maximum turn, 16000 Hz); the [`VADModeConfig` reference](/sdk/reference/conversation-manager/#VADModeConfig) lists every field. With no `Voice`, the TTS service speaks in its own default voice (`alloy` for OpenAI TTS). Set `Voice` to one of that service's voices: a voice name from another service, such as `alloy` on ElevenLabs, fails every synthesis.

VAD mode does not resample the input: send audio at `SampleRate`.

Any `stt.Service` fills the STT slot. Besides `stt.NewOpenAI`, `stt.NewElevenLabs` (ElevenLabs Scribe) and `stt.NewCartesia` (Cartesia Ink) take an API key the same way, so one vendor can both hear and speak:

```go
sttService := stt.NewElevenLabs(os.Getenv("ELEVENLABS_API_KEY"))
ttsService := tts.NewElevenLabs(os.Getenv("ELEVENLABS_API_KEY"))
```

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

To pick the voice, set `Metadata: map[string]interface{}{"voice": "Kore"}` on the `StreamingInputConfig`. Gemini Live takes one of Gemini's prebuilt voices (`tts.NewGemini(...).SupportedVoices()` lists them) and speaks Puck when the key is unset; OpenAI Realtime reads the same key. Gemini Live sessions connect to Google's Live endpoint whatever the provider's `base_url`. To send them through a gateway, set the provider's `additional_config.live_url` to the gateway's full WebSocket URL, path included. It must be `wss://` or `ws://`, and a `ws://` URL is logged as a warning because it sends the API key in plaintext.

For OpenAI Realtime, use `sdk.WithModel("gpt-realtime")` and a `SampleRate` of 24000. The [`openai-realtime` example](https://github.com/AltairaLabs/PromptKit/tree/main/sdk/examples/openai-realtime) shows the full configuration.

## Step 3: Pass the caller and host context to tool calls

To tell the session who it acts for, pass `sdk.WithCaller`. Tool executors, tool hooks, provider hooks, chunk interceptors and session hooks receive the caller on their requests. PromptKit never reads the caller from a context: a caller you put on `ctx` reaches no tool or hook as the caller. See [Act for a caller](/sdk/how-to/tools/act-for-a-caller/). Realtime sessions (ASM mode) run their tool calls through the same tool hooks, policy and approval gate as text turns.

A duplex session runs its tool calls on its own context, not on the context of any `SendChunk` call. If your tool handlers read other host values from the context, such as a logger or a database handle, open the session with `sdk.OpenDuplexContext` instead of `sdk.OpenDuplex`:

```go
conv, err := sdk.OpenDuplexContext(ctx, "./voice-assistant.pack.json", "assistant", opts...)
```

Every tool call in the session sees the values on `ctx`, as a text turn's tool calls see the values on the context passed to `Send`. Session-start hooks and forks of the session see them too. `sdk.ResumeDuplexContext`, `sdk.OpenVoiceContext` and `PackTemplate.OpenDuplexContext` take the context the same way.

This `ctx` supplies values; it does not set how long the session lives. A deadline on `ctx` bounds only the open, and canceling `ctx` afterwards does not end the session. The session keeps the values on `ctx` for its whole life, so build a context with what the tool calls need rather than passing a request's context, whose span, logger or transaction ends with the request.

Two things end the session:

- Canceling the context passed to `conv.Start` stops it at once. A model call or TTS synthesis still running is canceled, audio queued for the speaker is dropped, and `Done()` closes. Call `conv.Close` afterwards to run the session evals and release the conversation.
- `conv.Close` ends it gracefully: it ends the input, lets the turn in flight finish and be saved, and then closes. It waits at most `session.DefaultDrainTimeout` (30 seconds) for the pipeline to take the end of input, then a short grace for the pipeline to finish, and cancels what is still running. A `Response()` channel left unread does not hold it: while `Close` runs, a chunk that finds the channel full is dropped.

The session also ends without `conv.Close`: when nothing happens in it for the idle timeout (`sdk.WithIdleTimeout`, 30 seconds by default; audio you send, silence included, counts as activity in both modes), when the provider reports an error, or when the provider closes the connection. `Response()` closes after any error that ended the session, and `Done()` closes. Input sent after that (`SendChunk`, `SendText`, `SendFrame`, `SendVideoChunk`, `TriggerStart`, `ContinueDuplex`) returns at once with the error that ended the session, which wraps `stage.ErrIdleTimeout` for the idle timeout, or with `sdk.ErrSessionClosed` when the provider closed the session without one (match it with `errors.Is`). Input a send handed over shortly before the session ended may never reach the provider: it is lost with the session. `conv.SessionError()` returns the same error once the session has ended, and nil while it runs or when it ended without one.

## Realtime tool calls

In ASM mode the pipeline runs the model's tool calls and sends their results to the session, as a text turn does:

- The session is offered the tools a text turn would be: the prompt's tools, capability tools such as `memory__recall`, and tools a skill grants. Tools in `StreamingInputConfig.Tools` are added, and a tool declared there replaces a conversation tool of the same name.
- A call to a registered tool the conversation does not offer, such as a pack tool the prompt does not list or a tool whose skill grant was revoked, is refused: it gets the result `Tool <name> was not run: it is not available this turn` and its handler does not run. Tools in `StreamingInputConfig.Tools` are offered. This and the refusals of tools the session lacks or still holds come after the tool policy's `Blocklist` and the tool hooks, in the order of the calls. These checks are made where the pipeline runs the calls: in SDK sessions, and in a host pipeline whose `DuplexProviderStage` was given `SetToolExecution`. A session learns from its pipeline whether a stage runs the calls, so a host pipeline given `SetToolExecution` runs each call once even when the session also has a `ToolRegistry`. A host pipeline without it, whose session runs the calls from `DuplexSessionConfig.ToolRegistry`, is not checked. A tool excluded after repeated failures still runs.
- OpenAI Realtime is given new tools mid-session when a skill grants them. Gemini Live fixes its tools when the session starts, so a call to a tool granted later is refused, and so is a call to a tool the conversation has since withdrawn, such as one whose skill grant was revoked.
- The session is sent only `StreamingInputConfig.ToolChoice`. A tool policy `tool_choice` of `none` offers no tools; the policy's other choices are not sent, because they would force a tool call after every tool result.
- The calls of one model turn run in parallel, up to the tool policy's `MaxParallelToolCalls`.
- The tool policy (`sdk.WithToolPolicy` merged with the prompt's `tool_policy`) applies: `Blocklist`, `MaxToolCallsPerTurn`, `MaxIdenticalToolCalls`, `MaxCallsPerMinute` and `MaxRounds`. A call a limit stops gets a refusal result, and `Response()` carries the limit as a chunk with `Error` set. Every later call of the same tool turn is refused, and the session stays open.
- If the model calls tools twice more after its tool turn was stopped, both batches get refusals. A third batch closes the session.
- A batch of calls that are all refused because their tools are not available counts the same way. The first two get their refusals; a third stops the tool turn and closes the session, with an error naming the tool. A batch in which any call runs does not count.
- When the session's spend passes `MaxCostUSD`, the calls of that model turn are refused and the session closes. The refusals are recorded in the conversation but not sent to the session, because a tool result starts another paid reply.
- Each model turn emits one `provider.call.completed` with its tokens and cost when it completes. OpenAI Realtime reports each turn's usage with the turn. Gemini 2.5 Live reports the usage of a turn that calls tools together with the reply that follows it, so that turn's event carries no tokens and the reply's carries both. Gemini 3 Live reports a tool-call turn's usage after its tool calls, and Gemini Live reports an interrupted reply's usage after the interruption. Because Gemini Live reports usage after a call ends, its call's `provider.call.completed` that has no cost waits up to 300 ms for that usage and carries it; the interruption, the tool calls and the next call do not wait. Only a provider that reports usage after a call ends (`providers.LateUsageReporter`) has its completions wait; OpenAI Realtime's go out at once. Usage that arrives later is a `provider.call.usage` event with that turn's `CallID`, after its `provider.call.completed`. Completions still go out in the order the calls ended, and a session that closes sends any completion still waiting. A call's total cost is its `provider.call.completed` cost plus its `provider.call.usage` costs.
- Cost a session reports after its turn has ended counts toward `MaxCostUSD` when it arrives, and a budget it exceeds is reported on the call it belongs to. Each realtime session numbers its provider calls, so the cost goes to its own call: on that call's `provider.call.completed` while the call is open, otherwise as a `provider.call.usage`. A provider that does not number its calls has such cost reported on the call started last; cost for a call too old for the session to remember is a `provider.call.usage` with no `CallID`. Calls follow the provider's own replies, and every call that starts completes once: when its reply ends (with or without a message), when an interruption names it, when the provider moves on to its next reply, or when the session closes. User input never ends a call: a reply still under way when the user speaks or types goes on to its end, and the user's turn starts with the provider's next reply. A call whose message was not emitted (none was produced) has `NoMessage` set on its `provider.call.completed`. A spent budget or a guardrail blocking the turn still completes the call and counts its cost. So does a provider error that ends the session. A message carries only its own call's text and tool calls: when a call is superseded, what it had produced so far is dropped, and output the provider sends for a call that has already completed (a canceled response's trailing audio or tool calls) is dropped too, apart from its cost. In the conversation it goes on the next message the session emits, so the messages' `CostInfo` adds up to what the session spent. When the session closes it goes on the last message emitted then; if none is emitted, it is counted and reported on its event but appears in no message.
- Tool hooks, tool result rewriters and the approval gate apply to each call. Results reach the model formatted as on a text turn.
- A chunk with `Error` set does not close `Response()`. The channel closes when the session ends.
- A tool result's media reaches the model where the realtime API can carry it:
  - OpenAI Realtime takes a tool result as text only, so the result's images (PNG, JPEG) and audio follow it in a user message, and the result's text says they are attached there. Audio is sent in the session's input format, 16-bit PCM at 24 kHz, so it must be a 16-bit mono PCM WAV or raw PCM whose MIME type gives its rate, such as `audio/pcm;rate=16000` (`audio/L16` is read as big-endian). The user message is one client event, which OpenAI limits to 15 MiB, so media that would take it over is described instead. With input transcription on, the transcript of tool audio is not taken as something the caller said.
  - Gemini Live takes a tool result's images (PNG, JPEG, WebP) in the function response, and no other media there. Gemini 2.x Live models ignore an image there without an error, so on those set the provider's `additional_config.function_response_media` to `false`, and the images are described instead.

  Media a session cannot send, or cannot load, is described in the result's text instead, as on a text turn. The description names the media's type and MIME type and says why it was not sent, such as `[This tool returned an audio (audio/wav) that was not sent: Gemini Live does not accept audio in a tool result.]`. A result with only such media is never sent empty.
- `conv.Close` ends the session at once. A tool call still running then is canceled (its handler's context is done) and its result never reaches the model. The pipeline waits for a canceled call for its short input grace period (100 ms by default) and then ends without it; a result the call returns later is dropped. A runtime pipeline whose input channel closes, with no drain, keeps answering tool calls until the `DuplexProviderStage` 30-second final-response timeout closes the session. The timeout is extended while a tool call runs or the reply to its result is under way, up to 5 minutes after input closed; a call still running then is canceled the same way.
- The conversation stores each tool call message followed by a result for every one of its calls, so `sdk.Resume` can continue it on a text provider. A call canceled when the session ended is stored with what its handler returned, usually `context canceled`. A call whose round never started, a call that did not return within the grace period, and a call still held for approval are stored with the error result `Tool <name> not executed: the session closed before it ran`. A message that arrives while a round runs, such as a user turn or the reply to it, is stored after the round's results. It also reaches every stage after the `DuplexProviderStage` (message recording, `message.created` events, the state store) only once the round's results are out, together with the turn ends and errors that follow it, so a long tool call delays those, not the audio or text streamed meanwhile. A call held for approval holds them until the host answers it.
- A spoken reply is stored with its transcript and its audio. When the conversation is resumed on a text model that cannot take audio (OpenAI models without audio input, on Chat Completions or Responses, and Claude), each assistant message is sent as its transcript and its audio is left out. The caller's own audio is not left out: those providers still refuse it. Gemini text models take audio, so they are sent both.

## Guardrails in realtime voice

In ASM mode the model speaks as it generates. Its audio reaches you as it is produced, and the transcript of that audio arrives alongside it. Output guardrails judge the transcript, and input guardrails judge the transcript of the caller's speech. Neither can hold back audio:

- A streaming output check (`banned_words`, `length`) runs on each transcript delta and can cut a reply part-way. A realtime session forwards each text delta once it passes, holding back the end of one that could be the start of a banned word, as text streaming does. The audio cannot be held back: it plays as it arrives. The guardrail's message arrives on a `Response()` chunk with `ReplacesText` set: show it in place of the reply's text so far. Every other output guardrail checks the completed reply. A completed-reply check corrects the stored reply, but the caller has already heard all of it.
- When a guardrail blocks a reply, the session cuts it. OpenAI Realtime cancels the response and cuts the model's copy of the reply to the audio the caller has heard, so the model does not see the blocked words in later turns. A response the server has not created yet is canceled as soon as it is. Gemini Live cannot stop a reply, so its remaining text and audio are dropped. Gemini keeps the whole reply in its own context.
- `Response()` delivers a chunk with `Interrupted` set, as on barge-in. Flush your player when you get it. Audio already played cannot be unsaid. `conv.Start` on a session from `sdk.OpenVoice` flushes its sinks for you.
- The guardrail's message then arrives as text, and the pipeline plays it as audio, so the caller hears why the reply stopped. The model does not say it: the message never enters the model's conversation, and no guardrail judges it, so a message that names what it blocks ("We don't talk about Bruno.") plays in full. The audio chunks carry `Metadata["cue"]` set to `guardrail_message`, which tells them from the model's audio. A barge-in cuts them as it cuts a reply. The turn ends with finish reason `safety` with the message as its text and one failed validation; the played audio adds no message. A recording (`sdk.WithRecording`) holds it as assistant audio with `generated_from` `cue`. The session goes on to the next turn. A hook that denies without enforcing, on typed text, spoken input or the reply, ends the turn the same way, with the default blocked message.
- Typed text (`conv.SendText`) an input guardrail blocks never reaches the model. The caller gets the message as text and hears it the same way, with no `Interrupted` chunk, because no reply was playing.
- The audio comes from the first of these that can render it:
  1. The session itself, on OpenAI Realtime. It speaks the message in a response outside the conversation (`conversation: "none"`), in the session's exact voice; the model's context never holds it, and it is not a reply on `Response()`. The words it said are checked against the message (case, punctuation and spacing aside), and it is asked once more when they differ. Its cost is reported as a `tts.call.completed` event with source `cue`. Gemini Live has no such response and skips this.
  2. The TTS service set with `sdk.WithCueVoice`, when it renders PCM. It speaks in the session's voice when it has a voice of that name (Gemini Live's voices in Gemini TTS, OpenAI Realtime's `alloy` in OpenAI TTS), and in its default voice otherwise. On OpenAI Realtime it is used only for a message the session could not say, so clips pre-rendered for it are not played there.
  3. A short tone (an earcon), the same for every message.

  A session whose replies are text only gets the text alone. The pack's guardrail messages are rendered when the session starts. On OpenAI Realtime only the first session of a voice renders each message; later sessions in the process, or in other processes sharing the clip cache, play it from the cache. A message not known in advance, such as a check's own replacement text, is rendered when it is first needed; one not ready within 1.5 seconds, including a pack message blocked before its first render is done, plays the tone instead. Rendered clips are kept in a cache shared by every conversation in the process (64 MiB), so a message is rendered once; `sdk.WithCueVoice(svc, sdk.CueClipCache(cache))` replaces it, for example with a store several processes share. `sdk.WithTTS`, `sdk.WithTTSProvider` and a `role: tts` provider set the VAD-mode reply voice, not the cue voice: on a realtime session they have no effect, and `Open` logs a warning once per process.
- With playout tracking, the stored reply records how much of it the caller heard, with `playout_cut` `guardrail` (see [Handle turns, barge-in and transcripts](/sdk/how-to/voice/handle-turns-and-barge-in/)).
- The caller's speech is checked once its transcript arrives. By then the model has heard the audio and is usually replying. A block cuts that reply, and the turn's text is the guardrail's message. When the transcript arrives after the reply has completed, the session can only stop the audio still playing: the stored reply stays as it was, and the guardrail's firing is reported as a `validation.failed` event.
- A turn is cut once and its message is shown and played once, however many guardrails fire on it. On OpenAI Realtime an output guardrail usually cuts the reply before the caller's transcript arrives; an input guardrail that then blocks the same turn adds its firing to the turn and has the turn forgotten, but does not cut again or repeat the message. A reply already cut, by a guardrail or a barge-in, is not cut again when its late transcript is blocked, so the message playing in its place plays to its end.
- After a block the model is told, because it never hears the guardrail's message: the session adds a short note to the model's conversation that asks for no reply and that the caller neither hears nor sees. A blocked turn gets one note, however many guardrails fire on it. After an output block the note says the reply was withheld because it broke a content rule and not to repeat that content, so a follow-up such as "which one?" does not bring the blocked word back. After an input block it says a message from the caller was blocked, so the model does not answer it; on OpenAI Realtime, which removes the turn, it also says the message was removed, so the model does not read the caller's next words as following its own last reply. The note goes once the blocked turn has ended, never while the reply is still being generated, because Gemini Live takes it as client content, which would interrupt the generation and cut the guardrail's message playing in its place. OpenAI Realtime adds the note as a system message; Gemini Live sends it as context that does not end the turn. The note does not name the guardrail, because a guardrail is usually named after what it blocks and its name would bring the blocked topic back into the model's conversation. It is not in the transcript, and is reported as a `context.note_added` event, whose `Validator` field names the guardrails that blocked the turn. A blocked typed turn never reaches the model and gets no note.
- A blocked spoken turn is also taken out of the model's memory where the provider allows it. OpenAI Realtime deletes the caller's turn and the reply that was cut from the server's conversation, so a later turn such as "tell me more about him" cannot bring the blocked topic back. Gemini Live has no way to remove a turn: the model still remembers what the caller said and can come back to it in a later reply. On Gemini, pair an input guardrail with an output guardrail on the same content, so a later reply that returns to it is cut too.
- Output guardrails run on the session's reply goroutine: a slow streaming output check holds back the reply's next audio while it runs. An input check on the caller's speech, such as an LLM judge, does not hold back a reply already streaming. The reply goes on while the check runs, and a block cuts it when the check returns. When no reply is streaming the session waits up to 200 ms for the check. A reply's turn ends only once the checks on its input are done, and a reply that starts while a check is still running waits for it before any of its text, audio, reasoning or tool calls go out, so a blocked reply is never heard. A transcript that arrives after its reply has completed is shown once its check is done.
- When Gemini Live's own safety filters end a turn, the turn's finish reason is `safety`.

If a reply must be checked before anyone hears it, or the caller's speech before the model hears it, use VAD mode. There the STT text passes the input guardrails before the model is called, and the TTS stage speaks each reply only once it is complete, after every output guardrail has passed it. A blocked reply is replaced whole by the guardrail's message, which the same TTS service speaks, so a cut reply is never partly heard. The [`realtime-showcase` example](https://github.com/AltairaLabs/PromptKit/tree/main/sdk/examples/realtime-showcase) runs the same pack and guardrails in both modes (`-mode asm` and `-mode vad`).

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
