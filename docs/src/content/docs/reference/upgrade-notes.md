---
title: Upgrade Notes
description: Changes that need action when moving between PromptKit versions.
verified:
  commit: 871dab9b182c410568ed21bad890a9d87d74b2c2
  sources:
    - runtime/a2a/client.go
    - runtime/a2a/types.go
    - runtime/credentials/azure.go
    - runtime/credentials/credential.go
    - runtime/credentials/gcp.go
    - runtime/evals/handlers/a2a_eval.go
    - runtime/evals/handlers/content_excludes.go
    - runtime/evals/handlers/cosine_similarity.go
    - runtime/evals/handlers/directional.go
    - runtime/evals/handlers/groups.go
    - runtime/evals/handlers/judge_defaults.go
    - runtime/evals/handlers/llm_judge.go
    - runtime/evals/handlers/param_names.go
    - runtime/evals/handlers/sentence_count.go
    - runtime/evals/handlers/tool_efficiency.go
    - runtime/evals/handlers/topic_policy.go
    - runtime/evals/params.go
    - runtime/evals/registry.go
    - runtime/evals/runner.go
    - runtime/evals/types.go
    - runtime/evals/validate.go
    - runtime/evals/wrappers.go
    - runtime/hooks/guardrails/adapter.go
    - runtime/hooks/hooks.go
    - runtime/hooks/registry.go
    - runtime/inference/all/all.go
    - runtime/inference/openai/provider.go
    - runtime/inference/openai/register.go
    - runtime/inference/provider.go
    - runtime/logger/logger.go
    - runtime/media/image.go
    - runtime/metrics/collector.go
    - runtime/persistence/common/repository.go
    - runtime/pipeline/stage/compactor.go
    - runtime/pipeline/stage/composition_executor.go
    - runtime/pipeline/stage/config.go
    - runtime/pipeline/stage/errors.go
    - runtime/pipeline/stage/idle_timeout.go
    - runtime/pipeline/stage/pipeline.go
    - runtime/pipeline/stage/stages_duplex_provider_hooks.go
    - runtime/pipeline/stage/stages_provider.go
    - runtime/pipeline/stage/stages_recording.go
    - runtime/pipeline/stage/state_handoff.go
    - runtime/pipeline/stage/tool_call_rate.go
    - runtime/prompt/pack.go
    - runtime/prompt/registry.go
    - runtime/providers/base/factory.go
    - runtime/providers/base/platform_transport.go
    - runtime/providers/base_embedding.go
    - runtime/providers/base_provider.go
    - runtime/providers/bedrock/embedding.go
    - runtime/providers/claude/claude.go
    - runtime/providers/claude/claude_tools.go
    - runtime/providers/claude/context_window_table.go
    - runtime/providers/claude/schema.go
    - runtime/providers/errors.go
    - runtime/providers/gemini/context_window_table.go
    - runtime/providers/gemini/gemini_tools.go
    - runtime/providers/gemini/setup.go
    - runtime/providers/gemini/streaming_support.go
    - runtime/providers/imagen/factory.go
    - runtime/providers/internal/streaming/credential_headers.go
    - runtime/providers/mock/mock_config_check.go
    - runtime/providers/ollama/embedding_register.go
    - runtime/providers/ollama/ollama_tools.go
    - runtime/providers/openai/context_window_table.go
    - runtime/providers/openai/openai.go
    - runtime/providers/openai/openai_multimodal.go
    - runtime/providers/openai/openai_tools.go
    - runtime/providers/openai/realtime_config.go
    - runtime/providers/openai/realtime_platform.go
    - runtime/providers/openai/responses_learning.go
    - runtime/providers/openai/streaming_support.go
    - runtime/providers/openai/streaming_support_integration.go
    - runtime/providers/provider.go
    - runtime/providers/registry.go
    - runtime/providers/rejected_params.go
    - runtime/providers/retry.go
    - runtime/providers/stream_metrics.go
    - runtime/providers/vertex/embedding.go
    - runtime/providers/vllm/factory.go
    - runtime/providers/vllm/vllm_tools.go
    - runtime/skills/active_set.go
    - runtime/skills/executor.go
    - runtime/storage/interfaces.go
    - runtime/storage/local/filestore.go
    - runtime/storage/types.go
    - runtime/stt/openai.go
    - runtime/tools/exec_executor.go
    - runtime/tools/http_executor.go
    - runtime/tools/http_mapping.go
    - runtime/tools/registry.go
    - runtime/tools/types.go
    - runtime/tts/elevenlabs.go
    - runtime/tts/errors.go
    - runtime/tts/http_helpers.go
    - runtime/tts/openai.go
    - sdk/agui/adapter.go
    - sdk/audio_monitor.go
    - sdk/azure_platform_integration_test.go
    - sdk/client_tools.go
    - sdk/errors.go
    - sdk/internal/pipeline/builder.go
    - sdk/internal/pipeline/builder_vad.go
    - sdk/options.go
    - sdk/provider_refs.go
    - sdk/sdk.go
    - server/a2a/ownership.go
    - server/a2a/protocol.go
    - server/a2a/server.go
---

Release notes are generated from merged pull requests. This page carries the
changes that need you to do something, with what to change and why. Sections
are grouped by the release that first contained the change, newest first.
Unreleased holds changes that are on `main` and in no release.

## Unreleased

The entries are grouped by area: turn numbering, guardrails, realtime and voice sessions, tools, streaming and AG-UI, caller identity, providers, retries, and the A2A server.

### Turn numbering

#### Hooks, validation events and eval results number turns from 0

Every turn index PromptKit hands a host now counts from 0: 0 for a conversation's first turn, 1 for its second.

- `hooks.ProviderRequest.TurnIndex` and `events.ValidationEventData.TurnIndex` were documented as 0-based and were 0-based in a realtime session, but on the text path (`Send`, `Stream`) the first turn reported 1. Validation events from a func guardrail (`guardrails.InputFunc`, `guardrails.OutputFunc`) reported 0 on every turn, on both paths; they now carry the turn too.
- `events.EvalEventData.TurnIndex` (on `eval.completed` and `eval.failed`), and the `EvalContext.TurnIndex` and `EvalResult.TurnIndex` of the evals a conversation runs, were 1-based. A session or conversation eval carries its last turn's index, so a two-turn session reports 1, not 2. That holds for a realtime session too, whose session evals reported 0 however many turns it had. A serialized `EvalResult` now always has `turn_index`, so a first-turn result reads `"turn_index": 0` instead of leaving the key out.

| If you | You will see | Change |
|---|---|---|
| Read `TurnIndex` in a `BeforeCall` or `AfterCall` hook, or `TurnIndex` in an exec hook's request | the first text turn is 0, not 1 | drop any `-1` that turned it 0-based |
| Read `TurnIndex` on `validation.started`, `validation.passed` or `validation.failed` events | the first text turn is 0, not 1; a func guardrail's events carry the turn instead of 0 | expect 0-based values; records stored before the upgrade hold 1-based ones for text turns |
| Read `TurnIndex` on `eval.completed` or `eval.failed` events, or on the results a `MetricResultWriter` or an eval hook receives | every turn is one lower: the first is 0, not 1 | drop any `-1`; records stored before the upgrade hold 1-based values |
| Read `EvalContext.TurnIndex` in an eval handler, or `turn_index` in an exec eval handler's request | the first turn is 0, not 1 | as above |
| Join validation events to eval events by turn number | the two now carry the same number for the same turn | drop any offset between them |
| Sample turns with `sample_turns` | the same rate, but a given session samples different turns, since the sampling hash includes the turn index | nothing, unless you depend on which turns were sampled |

`sdk.BlockedMessage.TurnIndex` was already 0-based on every path, and `sdk.Evaluate` passes on the `TurnIndex` you give it. `hooks.SessionEvent.TurnIndex` is still the number of completed turns.

### Guardrails

#### Topic, PII and safety output guardrails hold a streamed reply by segment

An output guardrail now holds a streamed reply back from the caller until it has judged it, as its `stream_hold` param says. Without the param, `topic_policy`, `pii_leakage`, `toxicity`, `bias`, `role_violation` and `text_toxicity` hold per segment: `Stream` releases a sentence or a few at a time, each once the judge passed it. `llm_judge` holds the whole reply. When the guardrail fails the reply, its message replaces the text already streamed, marked `ReplacesText`.

| If you | You will see | Change |
|---|---|---|
| Stream replies under one of those output guardrails | text arrives later (the first segment after one judge call), and the judge is called up to 8 times per model round instead of once | set `stream_hold: none` on the guardrail to stream as before, or `stream_hold: turn` for one judge call per round; `sdk.WithStreamHold` tunes the segment size and the call cap |
| Stream replies under an `llm_judge` output guardrail | nothing arrives until the judge passed the completed reply | set `stream_hold: none` to stream as before, or `stream_hold: segment` when its criteria judge only what the reply says |
| Render streamed text | a guardrail that fails a streamed reply after it reached you ends it with a delta marked `ReplacesText` | replace the turn's text with that delta's text |

#### A streamed chunk reaches the caller only after the output guardrails pass it

`ProviderStage` sent each streamed chunk to the caller before the chunk interceptors (the streaming output guardrails) judged it, so a blocked reply reached the caller in pieces and the guardrail's message never arrived as text. A chunk now goes out only once every interceptor has passed it. An interceptor can hold back the end of a chunk that could be the start of a banned word (`hooks.Decision.Hold`; `content_excludes` implements `evals.PartialHolder`), and releases it when later text rules the word out or the reply ends. An unguarded stream holds nothing back. One output guard decides what a firing means on the text path and the realtime path alike.

| If you | You will see | Change |
|---|---|---|
| Render streamed text (`Stream`, `StreamRaw`, `sdk.TextDeltaEvent`, `stream.Chunk`, the A2A server's stream) | a guardrail's cut ends the reply with one text chunk carrying its message and `ReplacesText` set, and finish reason `safety`. Text before the banned content may already have been shown | when `ReplacesText` is set, discard the text the turn has shown and show that chunk's text. `stream.CollectText` and the final `Content` do this. The A2A server sends the update with `append` false, so an A2A client replaces the artifact's text |
| Read `providers.StreamChunk` from a session or provider yourself | `ReplacesText` on the chunk whose `Delta` replaces the turn's text | honor it as above |
| Match `providers.ValidationAbortError` on a text stream | a chunk guardrail's deny, or a streaming check that errors, times out or returns no result, ends the reply with the guardrail's message, where the stream aborted with that error | detect a block by finish reason `safety` or the `validation.failed` event |
| Count validation events | a streamed chunk's check reports `validation.failed` only when it fires; the completed reply's check reports `validation.started` and `validation.passed` | nothing |
| Stream from a model that sends text and a tool call in one chunk, with a chunk guardrail that truncates or replaces the reply | the reply's tool calls do not run, as for a reply blocked after the call | nothing |
| Read the finish reason of a streamed reply a chunk interceptor blocked | `safety` on the returned and stored message, where it kept the provider's `stop` | nothing |
| Read the cost of a streamed call a guardrail cut | the turn ends at the cut. Usage that arrives within 250 ms is on the call's `provider.call.completed`; later usage is a `provider.call.usage` event with the call's `CallID`, where the call reported $0 | add `provider.call.usage` costs to the call's total, or use `hooks.OnProviderUsage` |
| Write a chunk interceptor or provider hook that rewrites the chunk or message in place | it still works | return the text the enforcement leaves in `hooks.Decision.Content` |
| Have two guardrails fire on one chunk | both firings are recorded | nothing |

#### A blocked input stays in the transcript and out of the model's context

When an input guardrail blocked a user turn, the blocked message and the canned reply were sent to the model on every later turn, so the model learned what had been refused. The transcript is unchanged: the state store, `Messages()`, recordings and events keep both messages. The provider request, summaries, memory extraction, the retrieval index and a workflow's carry-forward summary leave the pair out. Guardrail hooks and evals still see the full history. The canned reply is marked with `Meta[types.MetaBlockedInput]`, which the state store keeps. Output blocks are kept: their replacement stands in for the model's reply.

| If you | You will see | Change |
|---|---|---|
| Have an input guardrail block a turn, then continue the conversation | later provider requests do not hold the blocked message or its reply | nothing |
| Resume a conversation stored before the upgrade | a blocked pair is recognized by its input-direction guardrail validation and left out too | nothing |
| Build model context from stored messages in your own pipeline | the blocked pairs, unless you filter them | pass the history through `stage.ExcludeFromModelContext` |

#### Composition steps run their prompts' guardrails

A composition opened no prompt of its own, so only the opened prompt's validators were compiled, and a step prompt's guardrails never ran. `Open` now compiles the validators of every prompt the composition's steps run, parallel branches included, and gives each step its prompt's validators ahead of the host's hooks (`CompositionExecutorDeps.StepHookRegistry`). Host hooks run once per step call.

| If you | You will see | Change |
|---|---|---|
| Open a composition whose step prompts declare `validators:` | they run on each step, and block it as they do on a turn | review what they do to the composition |
| Declare a step validator that cannot be built | `Open` fails, naming it | fix the validator |
| Choose a blocked message's text with `sdk.WithBlockedMessage` | it applies to a step's block too, where the step used the guardrail's own message | nothing |

### Realtime and voice sessions

#### Realtime sessions can hold a spoken reply for its guardrails

`sdk.StreamHoldConfig` has a `Realtime` field. Set to `sdk.RealtimeHoldTurn` or `sdk.RealtimeHoldSegment`, a realtime session holds a reply's audio until the output guardrails that hold it have judged it (see [Realtime sessions](/reference/checks/#realtime-sessions)). It is off by default, so a session you do not change plays as before. Under a hold, each partial-reply judgment is bounded by 3 seconds unless `SegmentTimeout` or the guardrail's `stream_hold_timeout` sets another bound; one that runs past it fails closed.

Two provider-level changes come with it:

- A Gemini Live session whose pipeline holds reply audio (`providers.PlayoutHolder.HoldPlayout`) sends a chunk marked `GenerationComplete` when a reply's content is complete, ahead of its turn completion. The chunk carries nothing else, and the duplex pipeline consumes it. A session that does not hold reply audio sends no such chunk, so `Response()` reads as before.
- `providers.PlayoutHolder` and `providers.TranscriptLagger` are new optional session interfaces. OpenAI Realtime implements the first; Gemini Live implements both. A `PlayoutHolder` session counts audio as heard from `ReleaseAudio`, which refuses once the caller has spoken over the held audio, so the check and the count are one step. It also treats a caller speaking over audio the pipeline still holds as a barge-in, and reports it per reply (`HeldAudioInterrupted(generation)`) until the pipeline says it dropped what it held of that reply (`HeldAudioDropped(generation)`); a newer reply's held audio is left as it is. OpenAI Realtime then cuts its model's copy of each held item to what was released of it, so the model does not take a dropped or unheard reply as heard.

| If you | You will see | Change |
|---|---|---|
| Read the `Response()` channel of a Gemini Live session you called `HoldPlayout` on | an extra empty chunk with `GenerationComplete` set before each reply's turn completion | skip it, or use it as the reply's end |
| Want a realtime reply judged before it is heard | the reply plays as it arrives, as before | set `Realtime` in `sdk.WithStreamHold` |
| Delimit turns by `assistant_turn_complete` on `Response()` under a hold | a held reply the caller spoke over before any of it was released, or that was canceled unheard, sends no `assistant_turn_complete` and stores no assistant turn, so two caller turns can come in a row | treat a caller turn after a caller turn as the reply to the first having been dropped |
| Hold a realtime reply that calls tools on OpenAI Realtime | when the reply is dropped unheard, its calls do not run, but the model's conversation keeps the `function_call` item without an output | nothing to change: the model sees a call it got no result for |
| Hold replies on a session without output transcription | each held reply fails closed with the blocked message, since there is no transcript to judge | turn on the session's output transcription |
| Hold replies on Gemini Live | a turn-held reply's first audio waits a further 1.5 seconds after its generation completes, for the transcript's last words | set `Holdback` to change the wait |

#### Realtime tool calls run through the tool runner

A realtime (ASM) session ran each tool call itself against the tool registry, one at a time, with an approval check but no tool policy or tool hooks, and was offered only `StreamingInputConfig.Tools`. Its calls now run in the `DuplexProviderStage` through the runner a text turn uses. [Choose a voice mode](/sdk/how-to/voice/choose-voice-mode/#realtime-tool-calls) lists the rules.

| If you | You will see | Change |
|---|---|---|
| Open a realtime session on a pack with tools | the session is offered the prompt's tools, capability tools and skill grants, merged with `StreamingInputConfig.Tools` (yours win on a name clash) | drop tools from `StreamingInputConfig.Tools` that only repeated the pack's |
| Set a tool policy (`WithToolPolicy` or the prompt's `tool_policy`) | `Blocklist`, `MaxToolCallsPerTurn`, `MaxIdenticalToolCalls`, `MaxCallsPerMinute`, `MaxRounds` and `MaxParallelToolCalls` apply to realtime calls; a stopped call gets a refusal and `Response()` a chunk with `Error` set | check the limits suit voice turns |
| Register tool hooks, `ToolResultRewriter` hooks or an approval check (`OnToolAsync`) | they run on realtime calls, and results reach the model formatted as on a text turn | nothing |
| Have the model call a registered tool the turn did not offer | the result `Tool <name> was not run: it is not available this turn`; the handler does not run | offer the tool, or declare it in `StreamingInputConfig.Tools` |
| Build a pipeline with a `DuplexProviderStage` given `SetToolExecution`, and give the session the same `ToolRegistry` | each call runs once: the session learns from the pipeline that a stage runs the calls | nothing |
| Close a session while a tool call runs | the call is canceled and waited for up to the input grace period (100 ms by default); a call that never ran is stored with `Tool <name> not executed: the session closed before it ran` | nothing |

#### Realtime guardrails cut the reply, play the message, and tell the model

In a realtime session a guardrail that blocks a reply now cuts it: OpenAI Realtime cancels the response and truncates the model's copy to the heard audio, and `Response()` delivers a chunk with `Interrupted` set. Input guardrails run on the caller's spoken transcript. The guardrail's message arrives as text and plays as audio from the session's own out-of-band voice (OpenAI Realtime), the `sdk.WithCueVoice` service, or a short tone. After a block, a note is added to the model's conversation, reported as a `context.note_added` event. The note says a content rule blocked the turn and does not name the guardrail, because a guardrail named after what it blocks would bring the topic back. See [Choose a voice mode](/sdk/how-to/voice/choose-voice-mode/#guardrails-in-realtime-voice).

| If you | You will see | Change |
|---|---|---|
| Play `Response()` audio yourself | a chunk with `Interrupted` set when a guardrail cuts a reply | flush your player on it; `conv.Start` on an `OpenVoice` session does |
| Play `Response()` audio yourself | audio chunks with `Metadata["cue"]` set to `guardrail_message` after a cut: the guardrail's message, which the model never says | play them as reply audio; use the key to tell them apart |
| Have a hook deny without enforcing, on typed text, spoken input or the reply | the turn ends with the default blocked message and finish reason `safety`; the session stays open, where it ended with a `HookDeniedError` | nothing |
| Read a blocked spoken turn on OpenAI Realtime | the caller's turn and the cut reply are deleted from the server's conversation | nothing |
| Read `context.note_added` | `Validator` names each guardrail that blocked the turn once, in firing order | read the names there, not from the note's text |
| Count TTS spend | a `tts.call.completed` event with source `cue` for each rendered message, on the session that rendered it; a cached clip costs nothing | include it in cost reports |

#### Voicing guardrail messages: `WithCueVoice` and `WithTTS`

`sdk.WithTTS` is the VAD-mode reply voice only. A realtime session's guardrail messages are voiced by `sdk.WithCueVoice(svc, ...sdk.CueOption)`, with `sdk.CueClipCache(cache)` choosing the clip cache (by default one process-wide in-memory cache). `sdk.PrewarmCueClips(ctx, packPath, voice, opts...)` renders a pack's messages ahead of time with the service and cache from `WithCueVoice`. Clips live in a `cueclip.Cache` from `runtime/audio/cueclip` (`NewMemoryCache`, `NewDirCache`, `ReadOnly`).

| If you | You will see | Change |
|---|---|---|
| Set `WithTTS`, `WithTTSProvider` or a `role: tts` provider on a realtime session | it voices nothing there, and `Open` logs a warning once per process | pass the service to `WithCueVoice` |
| Built against `main` between releases with `sdk.WithCueClipCache` | it is removed | pass `sdk.CueClipCache(cache)` to `WithCueVoice` |
| Built against `main` with `PrewarmCueClips` taking a TTS service | it takes `(ctx, packPath, voice, opts...)` and reads the service and cache from `WithCueVoice` in `opts` | pass `WithCueVoice` |
| Built against `main` with `stage.NewCueStage`, `NewCueStageFor`, `CueConfig`, `CueStage`, `EarconPCM` or `ClipKeyFor` | they are unexported; `CueBackchannel` and `CueClip.Video` are gone | build the stage with `stage.NewDuplexCueStage(duplex, stage.CueOptions{...})`, and use `cueclip.Cache`, `cueclip.KeyFor` and `cueclip.NewDirCache`. Clip keys are unchanged, so existing clip directories still play |

#### A spoken reply is stored once its audio has played

With an audio session (`OpenVoice`, or `WithAudioSession` on a provider that takes streaming input), a realtime reply with audio reaches the state store, the recording, memory and `message.created` once its audio has played or been cut. A cut reply keeps its text and finish reason. `Close` and `Complete` wait for held replies before the evals run.

| If you | You will see | Change |
|---|---|---|
| Follow `message.created` or the store during a voice session | a reply arrives when its audio ends, not when the model finishes it | read `Response()` for live text |
| Read a reply the caller barged in on, or that a guardrail cut while it played | `Message.Meta` holds `types.MetaHeardMs`, `types.MetaAudioMs`, `types.MetaPlayedFraction` and `types.MetaPlayoutCut` (`types.PlayoutCutBargeIn`, `PlayoutCutSessionClosed` or `PlayoutCutGuardrail`) | nothing |
| Play a realtime session's `Response()` audio to a listener yourself | nothing is held | pass `sdk.WithPlayoutTracking()` |
| Pass `WithPlayoutTracking` to `Open`, or to a duplex session in VAD mode or with `WithIngestion` | an error: only a realtime session has playout | drop the option |
| Built against `main` with the playout keys on `stage` (`stage.MetaHeardMs` and the rest) | they are in `runtime/types` | import them from `types` |

#### Realtime events and cost

| If you | You will see | Change |
|---|---|---|
| Follow `message.created` on a realtime conversation | the model's replies and tool-calling rounds, at the index each holds in the stored transcript, where only user turns were published | nothing |
| Total cost from `provider.call.completed` | each call completes once. Gemini Live reports usage after a call ends, so its completion waits up to 300 ms for it (`providers.LateUsageReporter`); usage later still is a `provider.call.usage` event with the call's `CallID` | add `provider.call.usage` costs to the call, or use `hooks.OnProviderUsage` |
| Count calls on `provider.call.completed` | a call that produced no message has `NoMessage` set | skip those when counting replies |
| Read a realtime reply's text while it streams | text-only OpenAI Realtime replies stream as deltas, where they arrived in one chunk at the end | nothing |

#### Duplex sessions end without hanging

| If you | You will see | Change |
|---|---|---|
| Call `Close` on a duplex session | the turn in flight finishes and is saved; `Close` waits at most `session.DefaultDrainTimeout` (30 s) for the pipeline to take the end of input, then a short grace, then cancels. A `Response()` channel left unread no longer stalls it | nothing |
| Cancel the context passed to `Start` | the session stops at once: a model call or TTS synthesis is canceled, queued audio is dropped, and `Done()` closes | call `Close` afterwards to run the session evals |
| Send input after the session ended (idle timeout, provider error, provider closed it) | the error that ended it, wrapping `stage.ErrIdleTimeout`, or `sdk.ErrSessionClosed`, where the send returned nil and then blocked | handle the error; match with `errors.Is` |
| Call `SessionError()` | the error that ended the session, where it returned nil | nothing |
| Wait on `Done()` in VAD mode after the idle timeout | `Done()` closes on its own, and `SessionError()` wraps `stage.ErrIdleTimeout` | nothing |
| Listen to a VAD reply longer than the idle timeout | caller audio, silence included, resets the idle timer, as in ASM mode | nothing |
| Hear a long Gemini Live reply | the idle timer is held while a provider turn is open, up to 5 minutes | nothing |
| Resume a realtime conversation with history on OpenAI Realtime (`ResumeDuplex`) | the new session is given the history, where it failed with "session cannot take the conversation's N prior messages" | nothing |

#### VAD mode asks the TTS service for its own default voice

`DefaultVADModeConfig().Voice` is `""`, where it was `"alloy"`. An empty voice asks the TTS service for its own default (`alloy` on OpenAI TTS). ElevenLabs, Cartesia and Gemini TTS refuse `alloy`, so a VAD session on them with no voice set spoke nothing. `stage.DefaultTTSStageWithInterruptionConfig()` still sets `"alloy"`.

| If you | You will see | Change |
|---|---|---|
| Run VAD mode on ElevenLabs, Cartesia or Gemini TTS without a voice | replies in the service's default voice | nothing |
| Read `DefaultVADModeConfig().Voice` | `""` | set `Voice` to name one |
| Build a TTS stage from `stage.DefaultTTSStageWithInterruptionConfig()` with a non-OpenAI service | `alloy`, which that service refuses | set `Voice` |

### Tools

#### Settled tool results reach the model as in-turn results do

| If you | You will see | Change |
|---|---|---|
| Approve a held call (`ResolveTool`, `ResolveToolWithArgs`) and `Continue` | the result formatted as a text turn formats it (strings unquoted, objects indented, capped at `MaxToolResultSize`), where `Continue` sent raw JSON | nothing |
| Approve a call whose result does not match the tool's output schema | the model gets the validation error, and `ToolResolution.Error` carries it | fix the handler |
| Match a failed approved call's text | `Tool execution failed: <error>`, where it was `Tool error: <error>` | match the new text |
| Configure a `ToolResultRewriter` | it runs on results delivered by `Continue`, `ContinueDuplex` and `Resume` too; a rewriter error turns the result into a tool error | nothing |
| Register an executor with `OnToolExecutor` or `WithToolExecutor` that implements `tools.MultimodalExecutor`, `tools.AsyncCallExecutor` or `tools.AsyncToolExecutor` | its content parts reach the model, and a pending result suspends the call | nothing |
| Set `MaxCallsPerMinute` or a registry rate limit | a deferred client call counts when its answer is used, not when the model makes it | nothing |
| Have the repeated-call breaker nudge a round | its other calls count toward `max_tool_calls_per_turn` and `termination.tool_called`, and a text turn whose last round is nudged ends with a `MaxRoundsError` | nothing |
| Call `memory__recall` or `memory__list` with no match | `"memories": []`, where `null` failed the tool's own output schema | nothing |
| Built against `main` with `Conversation.SettleClientTool`, `SettleClientToolRejection`, `HasToolResultRewriters` or `session.DuplexSessionConfig.PipelineRunsTools` | they are not exported | nothing; the AG-UI adapter uses them internally |

### Streaming and AG-UI

#### A streamed turn reports its tool rounds, and the AG-UI adapter streams text

`Stream` and `ResumeStream` now send each tool round as it happens: a `ChunkToolCall` for each call once the round's reply has streamed, and a new `ChunkToolResult` for each result the model is given. Both carry the new `StreamChunk.Round`, the round they belong to. `ChunkToolCall` was documented but never sent. `RunSend` and `RunResume` on an `*sdk.Conversation` drive the turn through them, so text reaches an AG-UI client as the model streams it.

| If you | You will see | Change |
|---|---|---|
| Switch on `StreamChunk.Type` | `ChunkToolCall` chunks, and `ChunkToolResult` chunks with `ToolResult` set; neither carries `Text` | handle or skip them |
| Forward `Stream` chunks to a browser or another untrusted client as they come | `ChunkToolResult` puts what your server-side tools returned on the stream, where only text went before | drop `ChunkToolResult` (and `ChunkToolCall`, if call arguments are private) before forwarding |
| Stream a turn whose tool an `OnToolAsync` check holds | the hold in the `ChunkDone` response's `PendingTools()`, as `Send` reports it, and no `ChunkClientTool` for it | check `PendingTools()` on the `ChunkDone` response, and resolve it with `ResolveTool` or `RejectTool` and `Continue`, not `SendToolResult` |
| Handle `StreamWithCallback` events for a held call | a new `ToolApprovalEvent` for each held call, before `StreamDoneEvent`, where a `ClientToolRequestEvent` came | handle `ToolApprovalEvent`; a `StreamDoneEvent` after one is a paused turn |
| Serve a held turn over A2A `message/stream` | the task ends `input-required`, as `message/send` leaves it | nothing |
| Stream with a `SkillSelector` | the skills index narrowed to this turn's text, as `Send` narrows it | nothing |
| Render AG-UI `TEXT_MESSAGE_CONTENT` | several per message, one per chunk the model streamed; their `delta` fields joined are the message's text | append each `delta` |
| Render AG-UI text an output guardrail cut | the streamed message ends, and the guardrail's message follows as a new text message | nothing |
| Read `stage.ElementMetadata` or `providers.StreamChunk` from a pipeline yourself | elements and chunks carrying only `RoundMessages` during a tool loop with a streaming provider, on the `ExecuteSync` / `Send` path as well as the streamed one (`StreamPipeline.ExecuteSync` ignores them) | skip them, or use them to report the rounds |

#### AG-UI runs carry state deltas, usage and pending calls, and reasoning on request

The AG-UI adapter now sends state changes as `STATE_DELTA`, the run's token usage on `RUN_FINISHED` and `RUN_ERROR`, and the frontend tool calls a run left unanswered as `pendingToolCallIds` on a `success` outcome. With `agui.WithReasoning(true)` it sends the model's reasoning as `REASONING_*` events; `agui.WithMessagesSnapshot(true)` adds a `MESSAGES_SNAPSHOT` before `RUN_FINISHED`. `sdk.WithStreamReasoning()` makes `Stream` and `ResumeStream` send the model's reasoning as new `ChunkReasoning` chunks, which the adapter streams from. A reply an output guardrail blocks now keeps no reasoning, on every path. The AG-UI Go SDK is bumped to a newer `main` pseudo-version that carries `pendingToolCallIds` and `cacheWriteInputTokens`.

| If you | You will see | Change |
|---|---|---|
| Read the `Response` or history of a reply an output guardrail blocked, on a provider that returns reasoning | no `Reasoning` on the blocked reply, on text and realtime turns alike, where the model's reasoning toward the blocked answer was kept | nothing; read the reasoning before the guardrail if you need it, from a provider hook |
| Want an AG-UI client to see the model's reasoning | no reasoning events by default | pass `agui.WithReasoning(true)`; open the conversation with `sdk.WithStreamReasoning()` for it to stream, knowing streamed reasoning goes out before the output guardrails judge the reply |
| Forward runtime events with `agui.WithRuntimeEvents` | no `reasoning.delta` or `reasoning.completed` RAW events, and no reasoning on `message.created`, unless `WithReasoning(true)`; never the opaque reasoning tokens | nothing, or pass `WithReasoning(true)` |
| Configure a `StateProvider` | `Snapshot` called again once the turn has run, and a `STATE_DELTA` for the change; a provider that returns nil sends no `STATE_SNAPSHOT`, where it sent an invalid one | nothing; pass `agui.WithStateAfterEachTool(true)` for a delta after each tool result, with a `Snapshot` safe to call while tools run |
| Read `RUN_FINISHED` for a run that stopped on a frontend tool call | a `success` outcome with `pendingToolCallIds`, where the outcome was absent | nothing; both mean the run completed |
| Read `RUN_FINISHED` or `RUN_ERROR` | `usage`, one entry per provider and model, input including cache reads and writes, output including reasoning | sum `totalTokens` for a total |
| Use `agui.WithMessagesSnapshot(true)` | each message keyed by its stable id (see the next entry), or by the thread id and its position for a message stored without one | set `agui.WithThreadID` from the input's `threadId`, so those positional ids stay stable across runs |
| Read `types.CostInfo` | a new `Model` field naming the model a call was served with, set on text turns | nothing |
| Switch on `StreamChunk.Type` for a conversation opened with `sdk.WithStreamReasoning()` | `ChunkReasoning` chunks, with the delta in `Reasoning` and no `Text` | handle or skip them; `StreamRaw` and the A2A server drop them |
| Stream from a `session.UnarySession` yourself | reasoning-only `providers.StreamChunk`s only with the new `UnarySessionConfig.StreamReasoning` | nothing |
| Script `mock.Turn` values in tests | a new `ReasoningChunks` field the mock providers stream as reasoning | nothing |

#### Stored messages carry stable ids, and AG-UI runs can read the client's history, context and tools

Every message a conversation stores now carries a stable id, under the new `types.MetaMessageID` Meta key, read with the new `Message.ID()` and assigned by the new `types.EnsureMessageID`. The id is kept by every shipped state store and reads back the same on every load. `agui.MessageToAGUI` and `MESSAGES_SNAPSHOT` use it, and `agui.MessageFromAGUI` keeps an AG-UI message's id as the message's stable id. Messages stored before the upgrade have no id and are not rewritten: a snapshot keys each by its thread and position, as before, and `MessageToAGUI` mints an id for it on each call.

Three new adapter options read the rest of `RunAgentInput`, each off by default. `agui.WithClientHistory` replaces the conversation's history with the client's, dropping its system, developer and reasoning messages, through the new `sdk.WithHistory` send option (also `Conversation.ReplaceHistory`). The conversation's own copy of each message it holds wins over the client's, so blocked input turns and interrupted replies stay out of model context and stored metadata survives the echo. `agui.WithInputContext` opens the new message with a delimited context block. `agui.WithFrontendTools` offers the client's tools to the model for the turn through the new `sdk.WithClientTools` send option, refusing a name that is already a tool's with the new `sdk.ErrToolNameConflict`; the tools are registered with the new `tools.Registry.RegisterLocal`, on the conversation's own registry only. `sdk.WithMaxHistorySize` caps what a replaced history adds beyond the stored messages, and the new `stage.ModelContextMask` reports which messages `stage.ExcludeFromModelContext` leaves out, including each one marked with the new `types.MetaExcludedFromModel`. A client tool's name must be 1 to 64 of `A-Z`, `a-z`, `0-9`, `_` and `-`. A streamed round carries its stored id on its text, reasoning and tool-call chunks (the new `StreamChunk.MessageID` in `sdk` and `providers`, `stage.ElementMetadata.MessageID`), and the AG-UI adapter streams each message under it. `types.NewMessageID` mints an id, and `types.MetaInjectedContext` marks a context block a host put in front of a user message.

| If you | You will see | Change |
|---|---|---|
| Use `agui.WithMessagesSnapshot(true)` | each message keyed by its stable id, the same in every run whatever the thread id; `<thread>-<position>` only for a message stored before the upgrade | nothing |
| Read `Message.Meta` of stored or reported messages | a `_message_id` key holding a string | ignore it, or read it with `Message.ID()` |
| Compare stored messages field by field in tests | the `_message_id` Meta key on each | compare without it |
| Convert AG-UI messages with `MessageFromAGUI` | the message's `id` under `Meta["_message_id"]` | nothing |
| Want the model to see the client's history, as AG-UI's whole-history `messages` intend | the conversation's own history, as before | pass `agui.WithClientHistory(input.Messages[:len(input.Messages)-1])` to `RunSend`, only for a client entitled to edit its conversation |
| Want the model to see `RunAgentInput.context` | nothing from it, as before | pass `agui.WithInputContext(input.Context)` |
| Declare frontend tools in the pack only so the model is offered them | the same, as before | pass `agui.WithFrontendTools(input.Tools)` on every `RunSend` instead; a name the pack also declares fails the run, so leave pack tools out |
| Set `ProviderConfig.ToolGrants` (or `DuplexToolConfig.ToolGrants`) to entries ending in `*` | each grant is matched as an exact tool name, never as a wildcard | grant the tool names; patterns belong in the prompt's `allowed_tools` |
| Read the AG-UI `TEXT_MESSAGE_START` / `TOOL_CALL_START` parent ids of a streamed turn | the ids the conversation stores the messages under, where each was a new random id | nothing |
| Read `Meta` of a reply an output guardrail cut mid-stream | its `_message_id` kept beside the guardrail's metadata | nothing |
| Send a `*types.Message` that carries tool calls or a tool result, with `WithMaxMessageSize` set | the size limit counts the tool calls' arguments and the tool result's parts too | raise the limit if such a message is now refused |

### Caller identity

#### The caller travels on the request, set by `WithCaller`

`sdk.WithCaller(types.Caller{ID, Claims})` sets who a conversation acts for. Tool executors and hooks, provider hooks, guardrails, evals, memory stores and the workflow transition authorizer receive it on their request. PromptKit never reads a caller from a `context.Context`. `OpenDuplexContext`, `OpenVoiceContext` and `ResumeDuplexContext` take a context whose values the session's tool calls, session-start hooks and forks see; it bounds the open, not the session.

| If you | You will see | Change |
|---|---|---|
| Put caller identity on the context of `Send` or `OpenDuplexContext` | it reaches no tool, hook or authorizer as the caller | pass `sdk.WithCaller` |
| Compare `hooks.OutputRequest`, `workflow.TransitionRequest` or `providers.ToolResponse` with `==`, or use one as a map key | a compile error: `Caller.Claims` (a map) and `ToolResponse.Parts` (a slice) make them non-comparable | compare the fields you need |
| Use exec hooks or exec evals with `Claims` | the caller in the stdin JSON; `Claims` must be JSON-encodable | keep `Claims` to plain data |

### Providers

#### OpenAI and Gemini speech and tool media no longer depend on the model's name

Which request shape a model gets was read from its name. It now comes from the provider's configuration or from the API's own answer, so a snapshot, an alias such as `gemini-flash-latest`, or an Azure or Vertex deployment name behaves like the model it serves.

| If you | You will see | Change |
|---|---|---|
| Send a tool result's media to a Gemini model whose function responses take none (Gemini 2.x) | the first request carries the media in the function response and is refused; the provider sends it again with the media after the round, and sends later requests that way | set `additional_config.function_response_media: false` to skip the refused request |
| Send a tool result's media to a Gemini model whose name carries no version (`gemini-flash-latest`) | the media in the function response, where it went in a user message after the round | nothing |
| Run a Gemini 2.x Live model with tools that return images | the images go in the function response, which the model ignores, where they were described in the result's text | set `additional_config.function_response_media: false` |
| Transcribe with `whisper-1` on OpenAI STT | the request asks for `json`, and the cost uses the billed seconds the response reports, where it asked for `verbose_json` and used the audio's exact length | nothing |
| Use OpenAI TTS on `tts-1` or `tts-1-hd` with markup tags | the tags' directives are sent as `instructions`, which those models ignore, and `PersonaRubric` offers the markup rubric | set `additional_config.instructions: false`, or call `SetInstructions(false)`, to drop the directives and the rubric |
| List voices with `SupportedVoices` on OpenAI TTS | all 13 voices on every model; the four `tts-1` lacks say so in their description | pick a voice the model has |
| Transcribe with a `gpt-4o-*-transcribe` model on OpenAI STT | the call succeeds and is priced from the usage the response reports, where the `verbose_json` request failed | nothing |
| Use OpenAI TTS on a `gpt-4o-mini-tts` snapshot or an Azure deployment | markup directives are sent as `instructions`, where only the exact name `gpt-4o-mini-tts` got them | nothing |
| Want another realtime input transcription model on OpenAI Realtime | `whisper-1` by default | set the streaming config's `metadata["input_transcription_model"]` |

#### OpenAI refuses a strict `response_format` strict mode cannot express

A `json_schema` response format with `Strict` set was rewritten for strict mode whatever it held. A schema with a keyword strict mode refuses (`minProperties`, `oneOf`, ...) was refused by OpenAI, and a map-typed object was closed so the model could return no keys. The provider now refuses the request before building it, on Chat Completions, the Responses API and Azure OpenAI. Tools are decided per tool: a tool strict mode cannot express is sent non-strict, with its schema as written, and the tools beside it stay strict.

| If you | You will see | Change |
|---|---|---|
| Set `Strict` on a response format whose schema strict mode cannot express | an error naming the keyword and its JSON path, such as `"minProperties" at $.properties.tags`, before any request | change the schema, or send it with `strict: false` |
| Declare a tool whose schema strict mode cannot express | the request succeeds; that tool is sent non-strict | nothing |
| Force a tool by name on the Responses API (`api_mode: responses`) | the choice is sent as a function object, where every request failed | nothing |

#### Tool turns in history keep their linkage on every request path

The plain request path (`Predict`, `PredictStream`) of OpenAI, Claude, Ollama and Gemini serialized a history holding tool turns without the link between a call and its result, and Claude's was refused outright. They now serialize history with their tool path's code.

| If you | You will see | Change |
|---|---|---|
| Send a history with tool turns on the plain path | `tool_calls`/`tool_call_id` (OpenAI, Ollama), `tool_use`/`tool_result` (Claude) and `functionCall`/`functionResponse` (Gemini) | nothing |
| Hold a Claude tool call with no result after it (held for approval, cut short) | the assistant's text is sent without the `tool_use`, where the request failed | nothing |
| Run Gemini tool rounds followed by a user message | the function responses go in their own user turn | nothing |
| Resume a voice conversation on an OpenAI text model or Claude | each spoken reply is sent as its transcript, where the request failed on its audio | nothing |
| Declare no vision capability on an OpenAI provider | a tool result's image is described, not sent | declare vision on a model that has it |

#### Claude learns the thinking shape from the API

The thinking wire shape (adaptive, or enabled with `budget_tokens`) was chosen from the model's name, so a Bedrock model ID, inference profile, ARN or Azure deployment of a 4.5-or-older model got adaptive and every request failed. When the API refuses the shape, the provider now retries once with the other and keeps it.

| If you | You will see | Change |
|---|---|---|
| Use thinking on Claude behind Bedrock, Vertex or Azure | requests succeed, the first after one refused attempt | set `additional_config.thinking_mode` to `adaptive` or `budget` to skip that attempt |
| Set `thinking_mode` to another value | provider construction fails | use `adaptive` or `budget` |

#### Providers take what the model name cannot say from config or the API

Several providers decided behavior from the model name alone, which fails for an Azure deployment, a Bedrock inference profile or ARN, a proxy alias, or a model released after the name rule was written. Each now takes a declaration, or learns from the API, and keeps the name rule only when nothing is declared.

| If you | You will see | Change |
|---|---|---|
| Use an OpenAI realtime model under a name without `realtime` (an Azure deployment, a proxy alias) | no stream input, as before | set `additional_config.realtime: true` |
| Use a Responses-only OpenAI model behind a proxy under a name not ending in `-pro` (`gpt-5-pro-2025-10-06`, `gpt-5.3-codex`, an alias) | requests succeed, the first after one refused Chat Completions request; once a resend to the Responses API succeeds, later requests go there | set `additional_config.api_mode: responses` to skip that request |
| Call a legacy completions model (`davinci-002`) through the `openai` provider behind a proxy | the request is resent to the Responses API and fails there too; you get Chat Completions' error with the Responses error after it, and the provider does not switch | nothing |
| Use a Bedrock embedding inference profile or ARN | provider construction fails, as before, with an error that names `embedding_format` | set `additional_config.embedding_format` to `titan` or `cohere` |
| Use a Vertex embedding model outside `text-embedding`, `textembedding-gecko` and `gemini-embedding` | the provider builds, sends one text per request and takes the size from the first response, where construction failed | set `dimensions` and `batch_size` if the defaults do not fit |
| Use an ElevenLabs model that takes inline tags under an ID not starting with `eleven_v3` | tags stripped, as before | set `additional_config.inline_tags: true` |
| Give a Vertex embedding provider a full resource name (`projects/…/publishers/google/models/<m>` or `projects/…/endpoints/<id>`) | requests go to `POST /v1/<name>:predict` on the configured host, where the name was appended to the publisher's models collection; a publisher model's family defaults apply | nothing |
| Give a Vertex embedding provider a model-registry name (`projects/…/models/<id>`) | provider construction fails: that resource has no `:predict` method | deploy the model and use its endpoint name |
| Match the error text of a failed tool call on OpenAI Chat Completions (non-streaming) or Ollama | `*providers.ProviderHTTPError`, reading `API request to <url> failed with status <n>: <body>`, where it was a plain `API error (status <n>): <body>`. `Send` now returns it as a `*sdk.ProviderError` with `StatusCode` set, and `providers.IsTransient` reports a 429 or 5xx as transient, as on every other request path | match with `errors.As` instead of on the text |

#### The OpenAI Responses API reports why a reply finished

The Responses API sends no `finish_reason`. A streamed reply always finished as `stop`, a reply that called a tool included, and a unary reply reported no finish reason at all. The provider now reads it from the response: a completed response with function calls is `tool_use`, an incomplete one is `max_output_tokens` (or `safety` for a content filter), and any other completed response is `stop`. `api_mode: responses` is the default on `api.openai.com`.

| If you | You will see | Change |
|---|---|---|
| Run a tool-calling turn on the Responses API | `tool_use` on the final stream chunk and the stored assistant message, where it was `stop` | compare against `types.FinishReasonToolUse` |
| Call `Predict` or `PredictWithTools` on the Responses API | `stop`, `tool_use`, `max_output_tokens` or `safety`, where `FinishReason` was empty | compare against the `types.FinishReason*` constants |

#### Gemini applies a response schema only to the answering round

A response schema with tools was routed to the Interactions API, where the model called the tool on every round. The provider now sends a round that can call tools without the schema and, when that round answers, asks for the answer again with the schema and no tools, on whichever API `api_mode` chooses. A stream holds the tool round's chunks back, so the unconstrained draft is never delivered.

| If you | You will see | Change |
|---|---|---|
| Use `response_format` with tools on Gemini | two billed calls for an answering round, with both calls' usage on the answer; `every_round` behaves as `final_turn` | budget for the second call |

#### Speech services

| If you | You will see | Change |
|---|---|---|
| Want speech-to-text from ElevenLabs or Cartesia | `stt.NewElevenLabs` (model `scribe_v3`) and `stt.NewCartesia` (model `ink-whisper`), registered as `elevenlabs` and `cartesia` for `role: stt` | nothing |
| Want Gemini TTS | `tts.NewGemini` and `tts.NewGeminiOnVertex`, registered as `gemini`; the model defaults to `tts.GeminiModelFlashTTS` | nothing |
| Create a Cartesia TTS service without a voice | "Katie - Friendly Fixer" (`f786b574-daa5-4673-aa0c-cbe3e8534c02`), where the withdrawn default spoke nothing | nothing |
| Pass Cartesia a voice it rejects | an error naming the voice, `tts.ErrInvalidVoice` on the websocket too | nothing |
| Set a Gemini Live voice | the streaming config's `metadata["voice"]` picks it; unset keeps `Puck` | nothing |
| Front Gemini Live with a gateway | Live stays on Google whatever `base_url` says | set `additional_config.live_url` (`ws://` or `wss://`) |
| Built against `main` with `tts.DefaultGeminiTTSModel`, `tts.GeminiDefaultVoice` or `gemini.DefaultLiveVoice` | the first is `tts.GeminiModelFlashTTS`; the voices are unexported | rename, or drop the reference |

### Retries

#### Every retrying call shares one loop

Provider HTTP requests, A2A agent calls, TTS and STT requests, MCP connects and the OpenAI Realtime and Gemini Live WebSocket connects now retry through the same loop. Each keeps its own policy type, defaults, attempt count and choice of which failures to retry.

| If you | You will see | Change |
|---|---|---|
| Count `provider_retries_total` | retries from A2A (`provider="a2a"`), TTS (`tts/<provider>`), STT (`stt/<provider>`), MCP (`mcp`) and realtime connects (`openai-realtime`, `gemini-live`) as well as LLM providers, and a new outcome, `retry_after_too_long`; a policy of one attempt and a call canceled mid-retry record nothing | filter on `provider` to keep the old series |
| Get a 429 or 503 with `Retry-After` from an A2A agent, a TTS or STT service or a realtime handshake | the retry waits what the server asked for when that is within the policy's maximum delay (A2A `MaxDelay`, TTS/STT `MaxDelay`, realtime `RetryBackoffMax`); `a2a.HTTPStatusError`, `tts.SynthesisError` and `stt.TranscriptionError` carry it as `RetryAfter` | nothing |
| Get a `Retry-After` longer than that maximum, or longer than a minute from an LLM provider (`DoWithRetry`) | the call fails at once with the last error, where `DoWithRetry` waited however long the header said | retry later yourself if the wait is acceptable |
| Set `A2AConfig.RetryPolicy` (or the SDK's `A2AAgentBuilder.WithRetryPolicy`) | each non-zero field replaces the executor's value for that agent and a zero field keeps it, where the block had no effect; a negative `MaxRetries` turns retries off; an executor built `WithNoRetry` ignores the block | check the values you set |
| Retry MCP connects | each wait is lengthened by up to half at random and never exceeds a minute, where it was exactly `RetryDelay` doubled per retry | nothing |
| Cancel an MCP connect while it waits to retry | `Initialize` returns the context's error | match `context.Canceled` with `errors.Is` |
| Configure `DoWithRetry` with a delay that reaches its 60-second cap | the wait at the cap is 60 seconds, where jitter could take it to 90 | nothing |

### A2A server

#### The A2A server reads a request's version from `A2A-Version` alone, and checks the JSON-RPC envelope

A request is answered in the version its `A2A-Version` header or query parameter names, and a request that names none is A2A 0.3 (A2A 1.0 §3.6.2). The server used to answer a 1.0 method name sent without the header in 1.0, and answered a method name from the other version in the header's version. The official 1.0 clients for Python, JavaScript and Go send the header, and so does the runtime's `a2a.Client`. Their servers refuse a 1.0 method without it, as PromptKit's now does. The envelope is checked as JSON-RPC 2.0 requires.

| If you | You will see | Change |
|---|---|---|
| Test against `runtime/a2a/mock` | the mock applies these rules too: it reads requests with `a2a.ReadServerRequest`, the server's own code | send requests a conforming client would |
| Send a blank `A2A-Version`, or several header lines | a blank value counts as no header, and the query parameter is read next; lines naming different versions get `-32009` | send one version |
| Call a 1.0 method (`SendMessage`, `GetTask`, ...) without `A2A-Version`, for example from `curl` or a hand-written client | `-32009` VersionNotSupportedError, whose message says to send `A2A-Version: 1.0` | send `A2A-Version: 1.0`, or the `A2A-Version=1.0` query parameter |
| Send `A2A-Version: 0.3` with a 1.0 method name | `-32009`, where the call was answered in 0.3 shapes | use the 0.3 name (`message/send`), or send `1.0` |
| Send `A2A-Version: 1.0` with a 0.3 method name (`message/send`, `tasks/get`, ...) | `-32601` Method not found, naming the 1.0 method, where the call was answered in 1.0 shapes | use the 1.0 name (`SendMessage`) |
| Send a request without `"jsonrpc": "2.0"`, with a `method` that is not a non-empty string, or with an `id` that is an object, array or boolean | `-32600` Invalid Request, where a missing `jsonrpc` or such an `id` was served, a missing `method` failed with `-32601`, and a `method` that is not a string failed with `-32700` | send a JSON-RPC 2.0 Request object |
| Send a JSON-RPC batch (an array) | `-32600`, where it failed with `-32700` | send one request per call |
| Use a numeric `id` larger than 2^53 | the response echoes it exactly, where it came back rounded | nothing |
| Read `error.data` on an A2A error (`-32001` to `-32009`) answered in 1.0 | an array holding one `google.rpc.ErrorInfo`: `reason` (`TASK_NOT_FOUND`, ...), `domain` `a2a-protocol.org`, and `metadata.taskId` on errors about a task, where `data` was unset (A2A 1.0 §9.5). `-32009` carries it in 0.3 too. Other 0.3 errors still have none | read `reason` instead of matching message text; `a2a.RPCError.Data` holds the array |
| Read a status `timestamp` from a stream or subscription | every status update has one, where the update that ended a stream had none. It is the time the task store recorded, the one `GetTask` returns, where updates carried a time of their own | nothing |
| Keep tasks in a custom `TaskStore` with timestamps in a zone other than UTC | `timestamp` sent in UTC with a `Z` suffix and 0, 3, 6 or 9 fractional digits, where it carried the store's offset (A2A 1.0 §5.6.1). Every `a2a.TaskStatus` the runtime marshals is written the same way | nothing |

#### Tasks carry history, waiting tasks are evicted, and tenants partition tasks

The server records task history, evicts tasks left waiting for the caller, and keeps each tenant's tasks to itself. It can turn streaming off and serve an extended agent card, and it refuses inline push notification configs. The client decodes unknown task states and gains `SubscribeToTask` and `GetExtendedAgentCard`. See [A2A](/concepts/a2a/) for each.

| If you | You will see | Change |
|---|---|---|
| Read a task from `GetTask`, `ListTasks` or `SendMessage` | `history` holds the caller's messages and the agent's status messages, oldest first, up to the last 100 per task, where it was always empty. The responses are larger | send `historyLength: 0` to leave it out; set `a2aserver.WithMaxTaskHistory` to change the cap |
| Send a negative `historyLength` | `-32602`, where it was treated as `0` | send `0` or more |
| Leave a task `input-required` or `auth-required` for more than 24 hours | the task is evicted, and `GetTask` reports `-32001`, where it was kept for good | set `a2aserver.WithInterruptedTaskTTL`, or `0` to keep waiting tasks |
| Keep tasks in a custom `TaskStore` that implements none of `TaskHistoryStore`, `InterruptedTaskEvictor` and `TenantTaskStore` | tasks have no history, waiting tasks are not evicted, and a request that names a `tenant` fails with `-32004` | implement them to take part |
| Embed `*InMemoryTaskStore` in your own store | its `CreateInTenant`, `Tenant`, `AppendHistory` and `EvictInterrupted` are promoted and act on the embedded store's tasks: a task created by a request that names a tenant bypasses an overridden `Create`, and eviction removes tasks from the embedded store only | override those methods too, or embed the `TaskStore` interface instead of the struct |
| Send A2A 1.0 requests that name a `tenant` | a task is visible only to requests in its tenant, and a context only to the tenant that first used it; others get `-32001` or `-32602`, where `tenant` was ignored | send the same tenant on every request for a task |
| Send a message with `configuration.taskPushNotificationConfig` (0.3: `pushNotificationConfig`) | `-32003` PushNotificationNotSupportedError, and no task starts, where the config was dropped and the message ran | drop the config and poll `GetTask` or subscribe |
| Decode a task state the runtime does not know, in `a2a.TaskState` or a client response | `a2a.TaskStateUnknown`, where decoding failed and a stream event carrying it was dropped | use `a2a.ParseTaskState` to refuse an unknown state |
| Run behind something that cannot carry Server-Sent Events | `a2aserver.WithoutStreaming()` declares `streaming: false` and refuses both streaming methods with `-32004` | opt in if you need it |
| Wait on a task with `Client.WaitForTask` (the A2A tool executor and `a2a_eval` do) that is in a state the client does not know | the wait stops with an error matching `a2a.ErrUnknownTaskState`; the executor cancels the task and fails the call | nothing |
| Serve a stateless handler to several tenants | tasks are partitioned by tenant, but contexts are the handler's: two tenants may send the same `contextId` | key sessions by `MessageRequest.Tenant` and `ContextID` together |
| Construct a server with every TTL but the new one set to `0` | a background eviction goroutine runs for waiting tasks | call `Shutdown`, or set `WithInterruptedTaskTTL(0)` |

### VAD mode speaks a reply sentence by sentence as it streams

A VAD session used to synthesize a reply only once the model had finished it, so the caller's wait grew with the length of the reply. It now speaks each sentence as soon as the model has streamed it.

| If you | You will see | Change |
|---|---|---|
| Read the reply audio from `Response()` in VAD mode | the audio of each sentence, in pieces of about 100ms, where one chunk carried the whole reply | nothing, unless your player expects one chunk per reply |
| Read `Metadata["spoken_text"]` | it on the first chunk of each sentence, with that sentence's text, where it came once with the whole reply's | join the values to get the whole reply |
| Guard VAD replies with an output guardrail that judges only the complete reply (an `AfterCall` hook, or a validator with no streaming evaluation) | the caller hears each sentence before that guardrail runs, as a text caller already sees it. If the guardrail then replaces the reply, the rest is not spoken and its message is | use a guardrail that judges streamed chunks; text it holds back is not spoken until it clears it |
| Read the stored reply of a VAD turn an output guardrail cut after some of it was spoken | the sentences the caller heard, then the guardrail's message, where it was the message alone | nothing |
| Use `ElevenLabsService` | requests go to `/text-to-speech/{voice}/stream`, which returns the audio as it is generated | nothing |
| Read the stored reply of a VAD turn a guardrail truncated or rewrote after some of it was spoken | the truncation or the rewrite, once; when it begins with what was spoken, only the rest is spoken, and when it ends inside it, nothing more is | nothing |
| Speak a short turn while a VAD reply is pending or playing | the turn barges in on the reply once it transcribes to words, where an utterance the voice activity detector never reported as speech was queued after it. Noise the detector never reports as speech (a cough, a door) and that transcribes to nothing leaves the reply alone. A reply canceled before the model wrote any of it ends with the `Interrupted` chunk alone, with no `assistant_turn_complete` | stop waiting for a turn's `assistant_turn_complete` once `Interrupted` arrives |
| Read the stored reply of a VAD turn the caller barged in on | what the caller heard: the sentences that had played, then the share of the words of the one playing that its audio had played, with `FinishReason` `interrupted` and `Meta` `heard_ms`, `audio_ms`, `played_fraction` and `playout_cut` (`barge-in`), as a realtime session with playout tracking records them. It was the whole reply the model wrote. The session assumes your player plays each audio chunk as it arrives | nothing |
| Read the next turn's model request after a barge-in in VAD mode | the cut reply as the caller heard it, where it held the whole reply the model wrote, or nothing when the model was still writing it. A text reply cut to what was heard has `FinishReason` `interrupted` and `Meta` `_heard_prefix` (`types.MetaHeardPrefix`, `Message.IsHeardPrefix`), and stays in model context. `types.ExcludeInterrupted`, `ModelContextMask`/`ExcludeFromModelContext`, summaries, the retrieval index and topic-policy history drop only the other interrupted replies (`Message.LeavesModelContext`), a realtime reply cut with its whole text among them. A round that spoke and called a tool keeps its calls and its own finish reason, with its text cut to what was heard, so the call and its result stay a pair. Each round of a tool-calling turn is spoken apart (its last sentence ends when the next round begins) and each round's message records only its own audio. A reply the caller heard none of goes back to the model as nothing | nothing |
| Run a VAD turn whose model speaks before calling a tool ("Let me look that up.") | the preamble spoken while the tool runs: the provider stage marks the end of a round's text when its stream finishes (`ElementMetadata.RoundTextEnd`, a control element the TTS stage consumes and the session never outputs), and the TTS stage speaks the round's last sentence then, where it waited for the next round's text | nothing; a custom pipeline stage after a streaming provider stage passes the marker on or ignores it |
| Barge in on a VAD tool turn while the tool runs, or after its result came back and before the model said more | the round the tool ran in kept: its call and result reach the transcript as a pair, its spoken text as heard, and the model's next request carries them, where the whole round was dropped as if the tool never ran | nothing |
| Play VAD reply audio later or slower than it arrives, or not at all (a text UI, a selfplay or test client) | the session treats the audio as heard as it arrives, so a barge-in or a close stores more of the reply as heard than was | set `VADModeConfig.NoPlaybackPacing`: the reply is synthesized as fast as the TTS service allows, its turn ends once it is synthesized, all audio handed on counts as heard, and no playback window keeps the bot interruptible after its turn |
| Pass `WithMessageLog` in VAD mode | each turn persisted by the save stage after the TTS stage, as heard, as a realtime session's is, where the provider stage wrote each tool-loop round through the log as the model wrote it | nothing; a crash mid-turn now loses that turn's rounds, not only its unfinished reply |
| Close a VAD session while a reply plays | the reply stored as far as it had played, with `Meta` `playout_cut` `session-closed` and its own `FinishReason`, as a realtime session with playout tracking stores it; and the elements queued behind it still reach the save stage. It was stored whole | nothing |
| Wait for a VAD turn's `assistant_turn_complete` | it once the reply's audio would have finished playing, where it came once the last audio was synthesized. The reply's sentences are synthesized one ahead of the one playing, so a reply's audio arrives at about the pace it plays | wait for the audio, not the turn end, if you measure time to first audio |
| Implement `tts.Service` | `SynthesisConfig.PreviousText` and `NextText` set to the text spoken around each sentence of a VAD reply; `ElevenLabsService` sends them as `previous_text` and `next_text` | nothing; use them for continuity if your service can |

## v2.21.0

### Voice runs on Azure, and Gemini Live on Vertex

An OpenAI provider on `platform: azure` served chat and embeddings, but a Realtime session dialed `wss://api.openai.com` with the provider's empty key, and TTS and STT providers dropped the platform and called `api.openai.com` the same way. A Gemini Live session on a Vertex provider dialed the Gemini API host. Both now use the platform's endpoint and credential, fetched again at every request and every session handshake.

| If you | You will see | Change |
|---|---|---|
| Run an OpenAI Realtime session on an Azure provider | the session connects to `/openai/v1/realtime?model=<deployment>` on the provider's host, authenticated by the platform credential | set `model` to the deployment name |
| Declare an `openai` `tts` or `stt` provider with `platform: azure` | requests go to the deployment's `/audio/speech` or `/audio/transcriptions` route with `api-version` from `additional_config.api_version` (default `2024-12-01-preview`) | set `model` to the deployment name; give the identity `Cognitive Services OpenAI User` on the account |
| Run a Gemini Live session on a Vertex provider | the session connects to the location's Vertex Live endpoint and names the model `projects/<project>/locations/<location>/publishers/google/models/<model>` | use a location where Vertex offers the Live model |

### A platform that cannot host a provider fails or warns

A `platform:` block was read only where a provider type implemented it; elsewhere it was dropped without a word. Self-hosted types were also given the platform's credential in place of their own key. A combination no platform hosts now fails when it is built or opened, or is built against its own endpoint with its own credential and a warning.

| If you | You will see | Change |
|---|---|---|
| Open an OpenAI Realtime session on `bedrock` or `vertex` | an error naming the platform, before anything is dialed | use `azure` or no platform |
| Open a Gemini Live session on a platform other than `vertex` | an error naming the platform | use `vertex` or no platform |
| Build an `imagen` provider on `vertex` | an `UnsupportedProviderPlatformError` (Imagen is retired on Vertex) | use a Gemini image model |
| Name a platform on `ollama`, `vllm`, `elevenlabs`, `cartesia`, an inference provider or an Ollama embedding provider | the provider's own credential and endpoint, and a warning that the platform is ignored | drop the `platform:` block |
| Check `SupportsStreamInput()` on a provider whose sessions are refused | an empty list | nothing |

### Vertex serves the global and multi-region locations

Vertex URLs were built as `https://<location>-aiplatform.googleapis.com`, which does not resolve for the `global` location or the `us` and `eu` multi-regions. Those now use `aiplatform.googleapis.com` and `aiplatform.<us|eu>.rep.googleapis.com`. The location is matched without regard to case.

| If you | You will see | Change |
|---|---|---|
| Set `region: global`, `us` or `eu` on a Vertex provider | requests reach Vertex | nothing |
| Pointed `base_url` at one of those hosts to work around it | `base_url` still wins | nothing, or remove the override |

## v2.20.0

### A mock provider fails when its `mock_config` does not load

A relative `mock_config` resolved against the process's working directory, so a kit loaded from another directory could not find its mock file. The provider then logged a warning and answered with generic replies, and runs passed on the wrong answers. A relative `mock_config` now resolves against `ProviderSpec.BaseDir` when it is set, and `CreateProviderFromSpec` returns an error naming the path when the file does not load.

| If you | You will see | Change |
|---|---|---|
| Build provider specs from config files | relative `mock_config` paths still resolve against the working directory | set `BaseDir` to the directory of the config that declared the provider |
| Set a `mock_config` that does not exist or does not parse | an error from `CreateProviderFromSpec` | fix the path or the file |
| Call `mock.NewToolProvider` directly | a warning and generic replies, as before | nothing |

### Claude sends tools over Anthropic's strict limits without strict

Anthropic refuses a request whose strict schemas, the strict tools and the JSON output schema between them, carry more than 20 strict tools, 24 optional parameters or 16 parameters with union types. Strict tool use is on by default, so a valid tool set over those limits failed every request with a 400. Tools now stay strict in the order they are declared while the request fits; a tool that would go over is sent without strict, with the schema as written, and the provider logs one warning naming those tools.

| If you | You will see | Change |
|---|---|---|
| Give Claude tools with more than 24 optional parameters between them, more than 20 tools, or more than 16 union-typed parameters | the request succeeds; the tools past the limit are not strict, and a warning names them | declare the tools whose arguments matter most first |
| Stay within the limits | every tool strict, as before | nothing |

### The A2A server can complete a conversation it evicts

The A2A server only closed the conversations it evicted, so a pack's `on_conversation_complete` evals never ran for a conversation it hosted. `a2aserver.WithCompleteOnEviction()` makes idle eviction call `Complete()` on a conversation that has it (`a2aserver.CompletableConversation`); SDK conversations from `sdk.A2AOpener` do. Shutdown still closes.

| If you | You will see | Change |
|---|---|---|
| Host SDK conversations on `NewServer` with an opener that starts a fresh conversation per context ID, and declare `on_conversation_complete` evals | they still do not run | pass `a2aserver.WithCompleteOnEviction()` |
| Resume stored state in your opener | eviction still closes | nothing |

### Tool results carry their media on every provider

A tool that returned an image failed the request on OpenAI Chat Completions, which takes only text in a tool message. The OpenAI Responses API reduced a tool's media to text. Claude rejected an image a tool returned by URL. Claude, vLLM and Ollama dropped media they could not send without a word. A binary-returning tool that declared an object output schema failed validation of its text stand-in, or of the summary the HTTP executor returns.

| If you | You will see | Change |
|---|---|---|
| Return an image from a tool on OpenAI Chat Completions | the tool message carries the text and a note; the image follows the round in a user message | nothing |
| Return images or PDFs from a tool on the OpenAI Responses API | `input_image` and `input_file` items in the `function_call_output` | nothing |
| Return media a provider cannot send in a tool result: a kind it does not take, an unlisted format, or a size over its limit | a note in the result naming the media, its reference and why it was not sent | nothing, or send the media another way |
| Return media from a tool on vLLM or Ollama | a note in the tool message for each media part | nothing |
| Declare an output schema on a tool that returns media | the result is not validated against it | nothing |

### The A2A server logs through PromptKit's logger

`server/a2a` wrote its diagnostics with Go's standard `log` package, so an embedder could not level, filter or attach fields to them. It now writes structured records to PromptKit's logger, or to the logger given with `a2aserver.WithLogger`. A store whose policy refuses a state change can wrap `a2aserver.ErrTransitionRefused` to have the refusal logged at debug level.

| If you | You will see | Change |
|---|---|---|
| Read the A2A server's lines from stderr (`a2a: task ...: failed to set ... state`) | structured records on PromptKit's logger, such as `a2a: failed to set task state` with `task_id`, `state` and `error` | configure `logger.SetLogger`/`logger.Configure`, or pass `WithLogger` |
| Refuse task state changes by policy in your store | a warning for each refusal | wrap `a2aserver.ErrTransitionRefused` to log it at debug |

### `Close()` ends a session; `Complete()` ends the conversation

`Close()` ran the `on_conversation_complete` evals as well as the
`on_session_complete` ones, so a conversation that paused for a human's
approval and was resumed scored its whole transcript at every pause, and the
two triggers could not be told apart. `Close()` now ends a session and runs the
session evals only. `Conversation.Complete()`, `WorkflowConversation.Complete()`
and `MultiAgentSession.Complete()` end the conversation (a workflow's state change completes the outgoing state's conversation, which is never resumed): they run the session
evals, then the conversation evals, and close it. Completing an already closed
conversation runs nothing and returns `sdk.ErrConversationClosed`
(`sdk.ErrWorkflowClosed` for a workflow). `evals.EvalWorker` also runs
conversation events published on `eval.conversation.*`; it dropped them before.

| If you | You will see | Change |
|---|---|---|
| Declare `on_conversation_complete` evals and end conversations with `Close()` | they no longer run | call `Complete()` where the conversation is finished, and keep `Close()` where it is suspended or handed to another process |
| Close and resume one conversation, for approvals or per message | session evals at each close, conversation evals only on `Complete()` | nothing |
| Run a workflow with `on_conversation_complete` evals | a state change still runs the outgoing state's; `WorkflowConversation.Close()` no longer runs the active state's | call `WorkflowConversation.Complete()` when the workflow is finished |
| Publish conversation events to an `evals.EvalWorker` | they run, on `eval.conversation.*` | publish them there, once per finished conversation |
| Run an `evals.EvalWorker` on a subscriber that accepts only the subjects it knew | `Start` returns `conversation subscription: ...` and stops | let the subscriber accept `eval.conversation.*` (`evals.SubjectConversationEvals`) |
| Close conversations from a `ShutdownManager` | session evals only, since a shutdown suspends a conversation rather than finishing it | nothing, or `Complete()` the conversations that are finished before shutdown |
| Use `WithEvalTriggers` to run close-time evals out of band | nothing | nothing |

### `Continue` answers a round in one request, and a hold fails closed

`Continue` called the model once for each resolved call, so the first request
answered one call of the round and left the others unanswered. Providers that
require every call to be answered rejected that request. An answer for a call
the conversation had stopped waiting on was accepted, its handler ran, and the
result was dropped with a log line. When the pending
store could not record a hold, the call ran unapproved.

| If you | You will see | Change |
|---|---|---|
| Resolve several held calls, then `Continue` | one model request with every result, and one reply | nothing |
| Resolve, reject or answer a call after `Continue` carried on without it, or `Send` started a new turn | `sdk.ErrToolCallNotAwaited`; `ResolveTool` does not run the handler, and the call leaves the store | nothing, or stop answering calls of abandoned turns |
| Use a pending store whose `Add` can fail | the call does not run, and the model gets an approval error | nothing |
| Need the caller's context or the call id in an approval check | `OnToolAsyncWithContext` | register the check with it |
| Call `Continue` on a duplex conversation | an error naming `Continue()`, where it used to panic; the resolution is kept | call `ContinueDuplex` |
| Set `stage.ProviderConfig.ApprovalChecker` directly | it still works | set `ApprovalGate` to be able to refuse a call |

## v2.19.0

### A pack's fragments render, from the SDK and from the registry

The PromptPack spec defines a pack's `fragments` as content any of its prompts
can reference. The SDK registered them but never gave a prompt a reference to
them, so a `{{name}}` placeholder for a pack fragment failed every turn. Every
prompt in a pack can now use every one of the pack's fragments by name. The new
`Pack.PromptConfig(taskType)` returns a prompt's `Config` with those references,
for code that builds its own registry from a pack. A fragment the compiler wrote
as its own `{{name}}` reference, because a variable chooses it at runtime, is
not in the pack, so its placeholder still needs a value from the caller.

| If you | You will see | Change |
|---|---|---|
| Open a pack whose prompt uses one of the pack's fragments | the fragment's content in the system prompt, where the turn failed with an unresolved placeholder | nothing |
| Build a registry from a pack with `prompt.ToConfig` | the prompt's fragment placeholders stay unresolved, as before | use `pack.PromptConfig(taskType)`, and register `pack.Fragments` with the repository by name |
| Mark a fragment `required: false` and leave its file missing | its placeholder renders empty, where every render failed, and the compiled pack holds the fragment as `""` | nothing |
| Mark a fragment `required: false` whose file exists but does not parse | the compile fails naming the fragment, and the render fails on its placeholder, where the compile left it out | fix the file, or remove the reference |
| Declare a prompt variable with the same name as a pack fragment | the variable fills the placeholder; the fragment is not offered to that prompt | nothing |
| Write a pack fragment whose name holds a `{{variable}}` | it is not offered to any prompt, since a render resolves the name to one the pack does not hold | supply the resolved name as a variable |
| Validate a pack whose prompt uses fragment placeholders and declares no variables | no "references variables, but none are declared" error, unless a fragment it uses holds a variable | nothing |
| Read `FragmentsUsed` on `prompt.template.rendered` | only the fragments the render substituted, not every fragment the prompt could use | nothing |
| Call `Registry.GetCachedFragments`, `GetLoadedFragments` or `GetAvailableRegions` | the fragments loaded so far, sorted, where the list was always empty. A key is `name:<name>` for a reference with no path and `path:<file path>` for one with a path. `GetAvailableRegions` matches a `_us`, `_uk` or `_au` suffix on the fragment's name | nothing |

### Fragment repositories follow one contract

`runtime/persistence/repotest` holds `RunFragmentRepositoryContract`, the checks
the fragment resolver depends on. A repository written outside PromptKit can run
it in its own tests. The YAML, JSON and in-memory repositories now pass it, and
one `Registry` is safe to share between concurrent turns that load fragments or
prompts for the first time.

| If you | You will see | Change |
|---|---|---|
| Ask the in-memory repository for a fragment by `path` | an error wrapping `persistence.ErrFragmentPathUnsupported`, where it answered by name and ignored the path | reference the fragment by name only, or load it from a file repository |
| Match the in-memory repository's not-found error | it satisfies `errors.Is(err, fs.ErrNotExist)` and reads `fragment "<name>" not found: file does not exist` | match with `errors.Is` |
| Pass an absolute `path` straight to the YAML or JSON repository's `LoadFragment` with a base directory | that file, where the base directory was prepended to it | nothing |
| Implement `prompt.FragmentRepository` yourself | the resolver falls back to the working directory only on an error satisfying `fs.ErrNotExist` | run `repotest.RunFragmentRepositoryContract` against it |

### `WithEvalTriggers` keeps close-time evals out of `Close()`

`Close()` runs the `on_session_complete` and `on_conversation_complete` evals, so
a host that closes and resumes one conversation for each message scored the
whole, growing history on every message, and no group filter could separate
those evals from cheap per-turn ones. `sdk.WithEvalTriggers(triggers...)` names
the triggers a conversation's inline evals run. Unset, every trigger runs, as
before. It composes with `WithEvalGroups`; an unknown trigger, or none, fails
`Open`.

| If you | You will see | Change |
|---|---|---|
| Close and resume one conversation per message or request | nothing, unless you opt in | pass `sdk.WithEvalTriggers(evals.TriggerEveryTurn)` to `Open` and `Resume` (add `evals.TriggerSampleTurns` if the pack samples turns: triggers match exactly), and run the session and conversation evals with `sdk.Evaluate` when the conversation ends |
| Run a `WorkflowConversation` | each state's conversation takes the filter, so a state change runs no close-time evals the filter leaves out | nothing |

### Evals and validation

| If you | You will see | Change |
|---|---|---|
| Pass `WithEvalRunner` without `WithEvalRegistry` | the eval middleware checks the pack's evals against the runner's own registry (`EvalRunner.Registry`) and skips a def it rejects, as it already did with the SDK's own runner. An eval with a misspelled param such as `critera:` ran, ignoring the key | fix the def |
| Call `sdk.Evaluate` on a pack or defs | a def `evals.ValidateEvals` rejects (a duplicate id, an unknown or missing trigger, a `sample_percentage` outside 0–100, a metric Prometheus cannot register) is skipped with a warning, as `Open` skips it; a duplicate ran twice. A def its handler cannot run as written (an unknown type or param, an unusable `threshold`) is returned as a result whose `Error` names the problem and emitted as `eval.failed` (`evals.WithRejectedDefs`), and its handler is not called; a misspelled param ran, ignoring the key | fix the def |
| Set `max: 1.5` on `sentence_count` or `max_sentences` | the eval or validator is rejected at load: `max` must be an integer. It was read as `1` | write an integer |
| Set the retired `classifier_id` on `text_sentiment`, `text_toxicity`, `audio_emotion` or `image_moderation` | load reports `classifier_id is no longer supported: name a provider …`, where it reported an unknown param | name a `provider` |
| Have a `max_length` guardrail truncate a reply | a cut that would split a multi-byte character backs off to the character's start, so the reply stays valid UTF-8 and can be a few bytes under the limit. It could end in half a character | nothing |
| Read `sdk.ValidatePack` issues | prompts in name order, the same on every run, and an eval issue from a handler check at the index of the first def with the id it names, where it was always 0 | nothing |

### A prompt that cannot be loaded fails the turn

`PromptAssemblyStage` replaced a prompt it could not load with `You are a
helpful AI assistant.` and the turn ran on it. That hid a missing prompt, a
broken prompt config and a required fragment that could not load. It also
hid a working prompt: the required-variable check ran before `SetVar` and
variable-provider values joined the turn, so a prompt whose `required`
variable came from either was always replaced. The stage now returns the load
error and the turn fails. Required variables are checked by `TemplateStage`,
against every variable the turn has, and a missing one fails the turn with an
error wrapping `stage.ErrTemplateUnresolved`. A stage with no registry or no
task type still runs on the default prompt. `prompt.Registry.LoadTemplateDeferRequired`
loads a template without the required-variable check, for a caller that adds
variables before it renders.

A mid-turn workflow handoff rendered the destination prompt with the artifact
and context variables only, and sent the raw template when that render
failed, so a prompt using a `WithVariables`, `SetVar` or variable-provider
value reached the model with literal `{{name}}` in it, on every round of a
workflow conversation. It now renders with the turn's variables and fails the
round as `TemplateStage` does, with one exception: every artifact a state of
the workflow declares is bound, so `{{artifacts.name}}` renders empty until a
state sets it, as a missing optional fragment renders empty, and renders the
value from the next round on. That holds on the
handoff round, on later turns, on the entry state and after `ResumeWorkflow`,
which before bound no artifacts at all, set or not. A placeholder for an
artifact no state declares still fails the round.

| If you | You will see | Change |
|---|---|---|
| Declare a `required` variable and set it with `SetVar` or a variable provider | the pack's prompt, rendered, where the turn ran on the default prompt | nothing |
| Send to a prompt whose required variable no source sets | `Send` returns an error wrapping `stage.ErrTemplateUnresolved` that names the variable, where the turn ran on the default prompt | supply the variable |
| Open a task type the registry cannot load, or a prompt whose required fragment cannot load | `Send` returns the load error | fix the prompt or the fragment |
| Use variables in the prompts of a workflow's states | every round, including the one after a mid-turn handoff, renders them | nothing |
| Hand off to, or open, a workflow state whose prompt uses `{{artifacts.name}}` before any state has set that declared artifact | the placeholder renders empty, and the turn runs. Before, the handoff round sent the model the literal `{{artifacts.name}}`, and the next `Send` failed with `stage.ErrTemplateUnresolved` | nothing; to tell the model an artifact is missing, say so in the prompt around the placeholder |
| Resume a workflow with `ResumeWorkflow` in a state whose prompt uses `{{artifacts.name}}` | the artifact's persisted value, or empty when it was never set, where the first `Send` failed with `stage.ErrTemplateUnresolved` | nothing |
| Use `{{artifacts.name}}` for an artifact no state declares | the round fails with an error wrapping `stage.ErrTemplateUnresolved` that names the placeholder | declare the artifact on a state, or fix the name |
| Bind a variable for one `Send` (`WithJSONInput`) | it applies to that turn only, where it stayed on every later turn. `TurnState.BeginTurn` now clears `Variables` | pass it on each `Send` that needs it |

### Variables set with `SetVar` follow a workflow, and choose fragments

A `WorkflowConversation` opens a new conversation for each state it enters.
The variables the application set with `SetVar`, `SetVars` or `SetVarsFromEnv`
were left behind on the previous state's conversation, so the next state's
prompt failed when it required one. They now carry into every state's
conversation, by `Transition` or a mid-turn handoff. The variables a state is
opened with (`WithVariables`, prompt defaults, the carried context and
artifacts) are not copied.

A fragment whose name or path a variable chooses (`persona_{{region}}`)
resolved only from the variables passed at load time, so one chosen by a
`SetVar` or variable-provider value failed to load. `PromptAssemblyStage` now
loads the prompt when the turn's first element arrives, with the turn's
variables. `PackCompiler` writes every candidate of such a fragment into the
pack when its name carries every variable of its path and each of them
declares `validation.enum`, so a pack renders it from the variable at run
time, as the sources do. A pack's `fragments` hold static content keyed by
name, so a fixed name over a variable path (`name: persona`,
`path: personas/{{region}}.md`) cannot carry a candidate per value, and
without `enum` the values are unknown when the pack is compiled. In those
cases the compiler writes the `{{name}}` reference as before and logs a
warning, and a render fails on it unless the caller supplies the chosen
fragment's content.

A variable's value that fills a fragment's path must be a plain name: one
with a path separator, `..`, a drive colon or a `{{placeholder}}`, or values
that together make a `..` segment (`{{a}}{{b}}` with two `.`), fail the turn
with `prompt.ErrUnsafeFragmentValue`, from any source. Paths are filled in one
pass, so a value is never expanded again. A segment that empty values leave
empty names no file: an optional fragment renders empty, as a missing one
does. A fragment's name is a key and is not checked: a pack's fragments may
hold any key. A declared `validation.enum` is enforced when a prompt loads: a
value outside it fails with `prompt.ErrVariableNotInEnum`.

The YAML and JSON repositories let a relative fragment path climb only as far
as its own leading `../` segments, which the config wrote:
`../fragments/x.yaml` from a prompts directory reaches its sibling
`fragments/`. A `..` after a name, which climbs back past where the path
started (`x/../../secret.yaml`), is refused with
`persistence.ErrFragmentPathOutsideBase`; since a variable cannot add `..`, a
variable cannot move that point. The check holds on the file the path resolves
to, and the read goes through an `os.Root` opened at that point, so a symlink
pointing past it is refused the same way, while one that stays inside is
followed. That point's real path is resolved once per lookup, when the root is
opened: a pack directory that is itself a symlink, or sits under one, is read
where it really is. Every read of the lookup goes through that one root, so a
fragment link or a parent directory swapped for a symlink afterwards cannot
move it. An absolute path written in the config is used as it is. A name-only
lookup stays in the repository's `fragments/`. From the working-directory
fallback, a symlink pointing past that point reads as not found, as a `..`
after a name does there.

| If you | You will see | Change |
|---|---|---|
| `SetVar` on a workflow's active conversation, then move to another state | the value in the next state's prompt | nothing |
| Choose a fragment with a variable set by `SetVar` or a variable provider, from a source registry | the fragment loads and renders | nothing |
| Compile a prompt whose fragment name carries a variable with `validation.enum` | the pack holds each candidate fragment by its resolved name, and no `{{name}}` reference. A required candidate that cannot load fails the compile | nothing, or add the missing candidate |
| Choose a fragment's path with a variable whose value holds `/`, `..` or an absolute path | the turn fails with `prompt.ErrUnsafeFragmentValue` | pass a plain name |
| Leave a path variable of an optional fragment empty | the fragment renders empty, as a missing one does | nothing |
| Set a variable outside its declared `validation.enum` | loading the prompt fails with `prompt.ErrVariableNotInEnum`, naming the allowed values | pass an allowed value, or widen the enum |
| Reference a fragment by a relative path whose `..` comes after a name (`x/../../y.yaml`), or through a symlink that points past where the path may reach | `persistence.ErrFragmentPathOutsideBase` | write the climb as leading `../`, or give an absolute path |
| Build a `PromptAssemblyStage` pipeline yourself | the template is loaded when the first element arrives | nothing |

### Custom headers reach every request

The OpenAI Responses API path (`api_mode: responses`, unary and streaming),
Gemini's streaming path and Gemini's cached-content cleanup did not send a
provider's `headers`. They now do, as every other path did.

| If you | You will see | Change |
|---|---|---|
| Set `headers` on an OpenAI provider on the Responses API, or on a Gemini provider | the headers on every request | nothing, unless a gateway rejected requests without them |

### Realtime text turns are saved and recorded

A user turn sent with `SendText` in a realtime session never became a user
message, so the state store, a recording and the hooks' history held the
assistant's replies alone. The provider stage now emits it as a user message,
between assistant turns: text typed while a reply is streaming follows that
reply, and an input a guardrail blocks comes right before its blocked turn.
The SDK does not echo it on `Response()`; a pipeline you build yourself sees
it as an element whose `Source` is `stage.TextInputTurnSource`. A recording also records an element without a
timestamp at the time it sees it, and `recording.Export` keeps events with
equal timestamps in the order they were stored. Before, a realtime
recording's turns could come back in any order.

| If you | You will see | Change |
|---|---|---|
| Record or save a realtime session that takes text input | each user turn once, in order, before its reply | nothing |
| Read the output of a `DuplexProviderStage` you build yourself | one extra element per text input, carrying the user message | skip elements whose `Source` is `stage.TextInputTurnSource` if you only want model output |

### `OpenWorkflow` names the same finding on every run

A workflow or `agents` section with several findings was checked in map
order, so the error `OpenWorkflow` and `Open` return named a different first
finding from run to run. The findings now come in state, event and member
name order.

### Providers report the same finish reason and usage for the same turn

Every provider now reports a finished turn's `FinishReason` in the canonical
values in `runtime/types`: `stop` for an answer, `tool_use` for a turn that
calls tools. Some paths reported nothing, or a vendor value, so the same turn
read differently depending on the provider, the request path and, for Gemini,
which Gemini API the request was routed to.

| If you | You will see | Change |
|---|---|---|
| Call `PredictWithTools` on ollama or vllm | `FinishReason` set, where it was empty | nothing |
| Run a Gemini turn that calls a function | `tool_use`, where generateContent reported `stop` | compare against `types.FinishReasonToolUse` |
| Run a Gemini turn through the Interactions API | `tool_use` / `stop`, where a stream reported `tool_calls` and a unary call reported nothing | compare against `types.FinishReasonToolUse` and `types.FinishReasonStop` |
| Use the mock provider | `stop` on `Predict` and `PredictWithTools`, and `tool_use` on a tool-call turn, where it reported nothing or `tool_calls` | compare against the `types.FinishReason*` constants |
| Stream from vllm | a final chunk with the finish reason and, when the server sends usage, its `CostInfo`; `PredictStream` sent no final chunk and tool streams no cost | nothing |
| Call vllm `PredictWithTools` on a turn that only calls tools | the tool calls, where the provider panicked on the response's null content | nothing |

### TTS honors the requested PCM sample rate

`base.TTSRequest.SampleRate` reaches the wire for raw PCM on Cartesia
(`output_format.sample_rate`) and ElevenLabs (`output_format=pcm_<rate>`).
Both synthesized 24 kHz whatever was asked, and raw PCM carries no header naming
its rate, so audio requested at another rate played at the wrong pitch. The
OpenAI speech API takes no sample rate and always returns 24 kHz PCM, so
PromptKit resamples it to the rate asked for as the audio streams, with linear
interpolation. MP3 and WAV carry their rate in the file and keep each service's
fixed rate.

| If you | You will see | Change |
|---|---|---|
| Request PCM at a rate the vendor takes (Cartesia: 8000, 16000, 22050, 24000, 44100; ElevenLabs: those and 48000) | audio at that rate, where it came back at 24 kHz | nothing |
| Request PCM at any other rate from Cartesia or ElevenLabs | an error from `SynthesizeTTS` naming the supported rates, where the call returned 24 kHz audio | request a supported rate, or resample |
| Request PCM at any rate other than 24000 from OpenAI | audio resampled to that rate, where it came back at 24 kHz | nothing |

### Imagen honors the requested size and count

`imagen.Provider.Generate`, which the `image__generate` tool calls, now sends
`base.ImageRequest.Size` and `Count`. Imagen takes an aspect ratio rather than
pixel dimensions, so a `WIDTHxHEIGHT` size is sent as the supported ratio
nearest its width/height ratio: 1:1, 3:4, 4:3, 9:16 or 16:9. Imagen has no
quality setting, so `Quality` is not sent; the model you configure (for example
an ultra or fast variant) decides quality.

| If you | You will see | Change |
|---|---|---|
| Ask for a size such as `1792x1024` | an image at the nearest aspect ratio (16:9), where every image was 1:1 | nothing |
| Ask for a `Count` from 1 to 4 | that many images in `ImageResponse.Images`, and cost for each, where you got one | nothing |
| Ask for a `Count` above 4, or a size that is not `WIDTHxHEIGHT` | an error before any request is sent, where the call returned one 1:1 image | ask for at most 4 images per call, and give a size such as `1024x1024` |

### Resumed tool results name their tool

A tool result that `Resume`, `ResumeStream` or `Continue` gave the model had an
empty tool name, so a provider that matches a result to its call by name
(Gemini's `functionResponse`) got an invalid result. The result now carries the
name of the tool the model called, as a result produced inside the turn does.
Nothing changes for callers.

### Skill selectors see a skill's `allowed-tools`

A `skills.SkillSelector` (`WithSkillSelectorOption`, `WithSkillSelector`,
`ExecutorConfig.Selector`) got each skill's name, description and metadata, and
an empty `AllowedTools`, so it could not choose skills by the tools they grant.
`skills.AdaptSkillSelector` now hands it the skill's `allowed-tools` as well.
They reach a `selection.Selector` as the new `selection.Candidate.AllowedTools`
field, empty for a tool candidate, and an exec selector receives them as
`allowed_tools` in each skill candidate's JSON when the skill declares any.

| If you | You will see | Change |
|---|---|---|
| Read `SkillMetadata.AllowedTools` in a `SkillSelector` | the skill's `allowed-tools`, where it was empty | nothing |
| Write a `selection.Selector` or an exec selector | skill candidates carry `AllowedTools` | nothing; read it to choose by the tools a skill grants |

### Exec selectors receive the documented lowercase keys

An exec selector (`spec.selectors.<name>.command`) was documented to read
lowercase keys on stdin (`query.text`, `candidates[].id`), and the bundled
`rerank.py` reads them, but PromptKit sent the Go field names (`Text`, `ID`,
`Description`). The example saw an empty query, returned no IDs, and every
skill stayed listed. `selection.Query` and `selection.Candidate` now carry
json tags, and the exec selector's stdin uses them: `text`, `kind`,
`pack_id`, `agent_id` (left out when empty) and `k` in `query`; `id`, `name`,
`description`, `metadata` and `allowed_tools` (the last two left out when
empty) in each candidate. The response is read with keys matched
case-insensitively, so `{"Selected": [...]}` still works. `rerank.py`'s
local fallback ranker now leaves out a candidate that shares no word with the
query, so it narrows the list instead of returning every candidate.

| If you | You will see | Change |
|---|---|---|
| Wrote an exec selector against the documented wire, or use `rerank.py` | it reads the query and candidates, and narrows the index | nothing |
| Wrote an exec selector that reads the PascalCase keys PromptKit sent (`Text`, `ID`, `Description`) | it gets no query and no IDs | read the lowercase keys |
| Marshal `selection.Query` or `selection.Candidate` to JSON yourself | lowercase keys | read the lowercase keys |

### Conversation behavior fixes

| If you | You will see | Change |
|---|---|---|
| Pass `WithAPIKey` (or `WithCredential`) without `WithProvider` | requests authenticate with that key, where they used the vendor's environment variable or no key | nothing |
| Pass `WithAPIKey` with a Claude or Gemini `WithModel` and no environment key for that vendor | an Anthropic or Gemini provider, where the SDK built an OpenAI one asking for that model | nothing |
| Call `Send`, `Stream`, `Resume`, `ResumeStream` or `Continue` on one conversation from several goroutines | the turns run one at a time; a waiting turn gives up when its context ends | nothing |
| Stop reading a `Stream`, `ResumeStream` or `StreamRaw` channel before it closes | the conversation's next turn waits until the stream's context ends, since the stream holds the turn | drain the channel, or cancel the context you passed; `stream.Process` and `stream.CollectText` do this for you |
| Start a turn on a conversation from inside one of its own turns, with the turn's context | `ErrReentrantTurn`, where the call shared the running turn's state | run the nested work on another conversation, or after the turn |
| Start a turn on a conversation from an `OnTool` handler of that conversation, or with an unrelated context | the call waits for the running turn, which waits for the handler | use `OnToolCtx` and pass its context, or run the work after the turn |
| `Fork` a conversation opened without `WithStateStore` | the fork continues from the parent's history and records its own turns, where it started empty every turn | nothing |
| Use `WithContextRetrieval`, or an embedding provider in a runtime config | older messages outside the window reach the request, once each and in conversation order, where retrieval never ran | nothing |
| Use `WithContextRetrieval` and send a message | the older messages retrieved are the ones relevant to that message, where retrieval searched with the previous turn's | nothing |
| Check `errors.Is(err, sdk.ErrConversationNotFound)` after `Resume` or `ResumeDuplex` of an unknown ID | it holds for the memory and Redis stores, where they returned only `statestore.ErrNotFound` | nothing |
| Pass `WithUserID` with a state store | a new conversation's persisted state carries the user ID, where the ID reached events only; a resumed conversation's state is left as it was | nothing |
| Call `SetVar` from another goroutine while a turn runs | the call returns and the next turn renders it, where the two could deadlock | nothing |
| Set `WithTokenBudget` | what users said counts against the budget, so long conversations are truncated by `WithTruncation`'s strategy (or fail with `"fail"`), where user text was counted as zero tokens and the budget was rarely reached | check that the budget leaves room for the history you want sent |
| Set `WithTokenBudget` and the history is truncated | the kept history starts at a user message and keeps each tool call with all of its results, where a cut could leave a tool result without its call or open on an assistant reply, which providers reject | nothing |
| Set `WithTokenBudget` lower than the current turn needs | `token budget exceeded: budget N is too small for the current turn`, where the request went out empty or without the user's message | raise the budget |
| Build a pipeline with `stage.NewTokenBudgetStage` | the same whole-turn truncation, and the same error when no user message fits, where it kept the newest messages regardless of turns and could send only the system prompt | raise the budget if you see the error |
| Hold a realtime (ASM) or VAD-mode duplex conversation with a state store | each turn is stored when it completes, so `Messages`, `Fork` and `ResumeDuplex` see the turns of a session still in progress (or lost to a crash), where nothing was stored until `Close`, and nothing at all for a realtime session with `WithMessageLog` | nothing |
| `ResumeDuplex` a realtime conversation | the new provider session is given the persisted user and assistant turns before your first input, where it started with no memory of them | nothing |
| `ResumeDuplex` a realtime conversation that has history, on a provider that cannot replay it | `ErrResumeHistoryUnsupported`, where the conversation continued without its history | use a provider that implements `providers.StreamHistorySupport` (OpenAI Realtime, Gemini Live) |

## v2.18.0

### Config errors, workflow findings and capability matching

| If you | You will see | Change |
|---|---|---|
| Match a `config.ValidationError` message from a field outside `logging` | it starts `config validation error:`, where it started `logging config validation error:`. Logging errors read as before, and carry `Section: "logging"` | match on `Field` and `Message`, or on the new prefix |
| Compare `workflow.Validate` findings between runs | errors and warnings come in state-name, then event-name order, and a cycle is worded the same way on every run | nothing |
| Select providers by their `capabilities` list | `providers.MissingCapabilities` and `providers.HasCapabilities` do the matching. `tools` is a matching label only: tools are still offered to a provider whose list omits it | use them in place of your own matcher |

### The pack compiler resolves fragments and settles one template engine

`PackCompiler` wrote each fragment a prompt referenced into the pack as the
literal `{{name}}`, though the PromptPack spec defines a pack's `fragments` as
content resolved at compile time. It now loads each reference through the
registry's repository and writes the fragment's content. A required fragment
that cannot be loaded fails the compile; an optional one is left out. A
reference whose name or path contains a `{{variable}}` is chosen at runtime, so
the compiler still writes it as `{{name}}`. A compiler built with
`NewPackCompilerWithDeps` on a `Loader` that does not implement
`FragmentLoader` cannot load fragments, so it writes every reference as
`{{name}}`, as before, and logs a warning.

A relative fragment `path` now resolves against the directory of the prompt
config that references it first, when the repository loads prompts from files
(the YAML and JSON repositories do). When no file exists there, it resolves
against the process's working directory, as it did before. An absolute `path`
is used as it is.

`CompileFromRegistry` took the pack's `template_engine` from whichever prompt
it reached first, in map order. It now compiles prompts in sorted task-type
order and uses the engine they declare. A prompt left on the registry's default
engine does not count as declaring one. Prompts that declare different engines
are an error, since a pack has one engine. The single-prompt `Compile` now sets
`$schema` to `PromptPackSchemaURL`, as `CompileFromRegistry` already did.

| If you | You will see | Change |
|---|---|---|
| Compile a prompt that requires a fragment the repository cannot load | the compile fails, naming the prompt and the fragment, where it wrote `{{name}}` | register the fragment with the repository, or mark it `required: false` |
| Read `pack.Fragments` from a compiled pack | the fragment's content, not `{{name}}` | nothing |
| Reference a fragment by a `path` relative to the working directory, and a file of that path also exists under the prompt config's directory | the file under the prompt config's directory | give the path as an absolute path, or rename one of the files |
| Compile two prompts whose fragments of one name resolve to different content | the compile fails, naming the fragment, since a pack holds one fragment per name | give the fragments distinct names |
| Compile prompts that declare different `template_engine`s | the compile fails, naming the prompts | give every prompt that sets one the same engine |

### `Pack.ValidateDetailed` splits findings by severity

`Pack.Validate` returns every finding in one list, so a caller could not tell a
schema violation from a finding a valid pack may have. The new
`ValidateDetailed` returns the findings as `Errors` and `Warnings`. A missing
`compilation` block, which the PromptPack schema does not require, is a
warning. `ValidateDetailed` also checks each prompt's `tools` against the pack's
`tools` and warns about a tool the pack does not define: the schema asks for
one, and a host may still supply tools a pack does not carry. `Validate` keeps
its findings, without the tool warnings, and now lists errors before warnings.
`ListPrompts`, `ListTools` and the prompt findings are sorted.

| If you | You will see | Change |
|---|---|---|
| Block on every `Validate` finding | `missing compilation metadata` among them, as before | call `ValidateDetailed` and block on `Errors` only |
| Want undeclared prompt tools reported | `prompt '<name>': tool '<tool>' is not defined in the pack's tools` in `ValidateDetailed().Warnings` | call `ValidateDetailed` |
| Depend on the order of `Validate`, `ListPrompts` or `ListTools` | a sorted, stable order | nothing |

### Eval thresholds, groups and histogram buckets

| If you | You will see | Change |
|---|---|---|
| Declare `threshold: {operator, value}` on an eval of `type: assertion` or `type: guardrail` | it sets the pass line in place of `min_score`/`max_score`. `gt` and `lt` are strict. An unknown operator, a missing value, or `threshold` alongside `min_score` or `max_score` makes the eval an error result. `ValidateEvalTypes` reports it at load, so the SDK's eval middleware skips that eval with a warning | check the threshold says what you meant, and declare the line once |
| Declare `threshold` on a plain eval | no change: it is carried, not applied, and `Passed` stays nil | wrap the eval to gate on it |
| Filter evals by group without explicit `groups` | every judge- and classifier-backed check beyond the `llm_judge` family (`faithfulness`, `bias`, `toxicity`, `pii_leakage`, `role_violation`, `text_sentiment`, `topic_policy` and the rest), and `tool_exec`, is `long-running` and `external`, where it was `fast-running`. An alias takes its target's groups, and an `assertion` or `guardrail` wrapper takes the groups of the eval it wraps | set `groups` on the eval if you want it in the fast-running set |
| Write a custom eval handler | implement `evals.GroupDeclarer` to state its groups; without it the handler is `fast-running` | add `EvalGroups()` to a handler that calls a model or service |
| Record an eval metric of type `histogram` with `range.min` and `range.max` | ten equal-width buckets across its range, where it had `prometheus.DefBuckets`. A score histogram with `range: {min: 0, max: 1}` gets 0, 0.1 … 1.0 | check dashboards that read `le`, since the series' bucket bounds change |
| Record an eval metric of type `histogram` with no range | `prometheus.DefBuckets`, as before | declare a range on a score histogram, or set `CollectorOpts.EvalHistogramBuckets` to choose buckets for every histogram with no range |

### Realtime duplex sessions run guardrails

The realtime provider stage (`DuplexProviderStage`, used by `OpenDuplex` with a
streaming config) ran no provider hooks, so a pack's `validators:`,
`WithGuardrail` and `WithProviderHook` did nothing in a realtime session. It
now takes the conversation's hook registry (`SetHookRegistry`) and runs input
hooks on text before it is sent, streaming checks on each chunk of assistant
text, and output hooks on each completed turn. An enforced turn is marked
`safety`, its tool calls are dropped, and the rest of a turn a streaming check
blocked is not forwarded. Spoken input is not checked before the model hears
it, and what was streamed before a check fired has already played; see
[The Hook System](/sdk/explanation/hooks/). The SDK's realtime path also hands
the stage the conversation's event emitter.

| If you | You will see | Change |
|---|---|---|
| Run a realtime conversation under a pack with `validators:`, or with `WithGuardrail` | turns blocked where the guardrails say so, with `finish_reason: safety` on the turn-complete chunk, and `validation.*` events | review what the guardrails do to voice turns; they were never exercised there |
| Rely on a realtime turn's tool calls being executed | an enforced turn's tool calls are dropped, as on the text path | nothing, unless a tool must run whatever the guardrail decides |
| Subscribe to the event bus of a realtime conversation | `reasoning.delta` events for a reasoning model, as on the text path | nothing |
| Build a `DuplexProviderStage` yourself | no hooks run unless you call `SetHookRegistry` | call it with your registry |
| Have an input guardrail block text while the model is still answering, even if the answer so far is only reasoning | the blocked turn after that reply and ahead of the next one: in the output, in the history later hooks see, and in its `validation.*` event's turn index. The `BeforeCall` itself reports that turn index too | nothing |
| Check whether a `hooks.Registry` has provider hooks | `Registry.HasProviderHooks` | use it in place of `IsEmpty`, which also counts tool and session hooks |

### Recordings keep the content of control elements

`RecordingStage` skipped every element with a control flag (`EndOfStream`,
`EndOfTurn`, `Interrupt`). The realtime provider stage puts each assistant
turn's `Message`, and its last audio chunk, on the element that ends the turn,
so a recording of a realtime session held no assistant turns. It also recorded
only the first content field of an element. It now records every content field
an element carries, streaming content first and the `Message` last. A guardrail
firing on a streamed text turn is also no longer recorded twice on the message
when an output guardrail then enforces too.

| If you | You will see | Change |
|---|---|---|
| Record a realtime session | `message.created` for each assistant turn, and the turn's last `audio.output` | nothing |
| Put several content fields on one element ahead of a `RecordingStage` | one event per field, where only the first was recorded | nothing |

### `on_conversation_complete` evals run when a conversation closes

The SDK never ran an eval with the `on_conversation_complete` trigger: only
`sdk.Evaluate` with `Trigger: evals.TriggerOnConversationComplete` did.
`Conversation.Close()` now runs them, synchronously, right after the
`on_session_complete` evals, against the whole transcript. They run on every
`Close()`, as session evals already did: a host that closes and resumes the
same conversation scores the reloaded history again on each close, and a
workflow runs them each time it moves to another state, because that closes
the state's conversation. A conversation closed with an empty transcript, such
as one whose open failed, runs none. Session and conversation evals run without
the conversation's lock held, so a slow judge delays `Close()` only: a call
made on the conversation meanwhile sees it closed and returns at once, where
it waited for the evals.

| If you | You will see | Change |
|---|---|---|
| Declare an `on_conversation_complete` eval in a pack you open with the SDK | it runs on `Close()`, and its result reaches your event bus, hooks and metrics | nothing, or remove the eval if you only meant it for Arena |
| Run inline evals on a group filter (`WithEvalGroups`) | only the conversation evals in those groups run | nothing |
| Close and resume the same conversation | the conversation evals run on each close, over the full history | from v2.19.0, pass `sdk.WithEvalTriggers(evals.TriggerEveryTurn)` so `Close()` runs no session or conversation evals, and run those with `sdk.Evaluate` where the conversation really ends |

### Eval definitions are checked with `evals.ValidateEvals`

`evals.ValidateEvals` checks what the eval handlers don't: a missing or
duplicate id, a missing type or trigger, an unknown trigger, a
`sample_percentage` outside 0–100, and a metric name or label Prometheus cannot
register. Nothing in the runtime or the SDK called it. Now `Open()` runs it
over the pack's evals and over the prompt's evals, each on its own, and skips
each def it rejects with a warning, as it already skipped an unknown type.
`sdk.ValidatePack` reports the same defs as `eval` issues, and
`prompt.Pack.ValidateDetailed` adds the messages to its errors (and so
`Pack.Validate` to its findings). A trigger this
runtime does not know is the exception: the schema allows any trigger, so
`ValidatePack` and `Pack.ValidateDetailed` report it as a warning and
`Pack.Validate` leaves it out,
although `Open()` still skips the eval. A prompt eval that shares a pack
eval's id still overrides it and is not a duplicate. Only the unknown-trigger
finding is a warning: any other finding on the same eval is an error.
`evals.ValidateEvalsDetailed` returns the same findings with the def's index
and id, a code and a severity, so a caller can classify them without matching
their text.

| If you | You will see | Change |
|---|---|---|
| Open a pack with two evals of the same id in one scope | the later one is skipped, with a `Skipping unusable pack eval` warning | rename one |
| Open a pack whose eval has a metric name such as `my-score`, or an unusable `when` | that eval is skipped with a warning, where it ran | fix the metric name or the `when` |
| Open a pack whose eval has a trigger this runtime does not know | that eval is skipped with a warning, where it silently never ran | nothing, or fix the trigger |
| Run `sdk.ValidatePack` or `prompt.Pack.Validate` in CI | these findings, where there were none | fix the defs |
| Classify `evals.ValidateEvals` messages | the same messages as before | use `evals.ValidateEvalsDetailed` and its `Code` or `Severity` |

### Check params are checked against what the handler reads

Every built-in check type now lists the params it reads (`evals.ParamNamer`,
`EvalTypeRegistry.ParamNames`), and a key it does not read is rejected when the
pack loads, naming the key and the valid ones. Such a key used to be ignored,
so the check that ran was not the one written. `min_score` and `max_score` are
never reported, and a guardrail also accepts `direction` and `message`. See
[Unknown params](/reference/checks/#unknown-params).

| If you | You will see | Change |
|---|---|---|
| Declare a pack eval or validator with a key its handler does not read (`critera:` on `llm_judge`, `wrods:` on `banned_words`, `model:` on a judge) | `ValidateEvalTypes` reports `unknown param "critera" for llm_judge; valid params: …`, so the SDK's eval middleware skips the eval with a warning; a validator makes `Open()` fail with `ErrInvalidGuardrailParams` | fix or remove the key |
| Put such a key inside a wrapper's `eval_params` | the same error, from the wrapper's `ValidateParams` at load. `Eval` does not check, so a host that adds its own keys at run time keeps working | fix or remove the key |
| Write a custom eval handler | it is not checked unless it implements `ParamNames() []string` | implement it to have typos caught |
| Keep a table of each check's params | `EvalTypeRegistry.ParamNames(type)` returns them | read them from the registry |
| Load a mock provider config (`mock.NewFileMockRepository`) with a key it does not read, such as `default_response:` | the load fails, naming the key and the valid ones, at the top level, in a scenario, or in a structured turn, step, tool call or content part | fix the key; `default_response` is `defaultResponse` |

### `sentence_count` scores against `max`; `content_excludes` takes `case_sensitive`; judges read `judge_defaults`

| If you | You will see | Change |
|---|---|---|
| Set `max` (or `max_sentences`) on `sentence_count` / `max_sentences` | the score is 1 up to `max` and `max / count` above it, where it was always 1. A `max_sentences` guardrail now blocks a reply over `max` with its message. Without `max` the score is still 1. A `max` that is not a positive integer is rejected at load | nothing, unless you relied on the guardrail never firing |
| Set `case_sensitive: true` on `content_excludes` / `banned_words` | a pattern matches only in its own case, in both match modes and while streaming, where the key was ignored | nothing, unless you relied on the key being ignored |
| Supply `Metadata["judge_defaults"]["prompt"]` and a `*prompt.Registry` in `Metadata["prompt_registry"]` (Arena does, from `judge_defaults`) | `llm_judge`, `llm_judge_session` and `llm_judge_tool_calls` without a `system_prompt` grade with that prompt's rendered system template, where they used the built-in prompt. A named prompt that cannot be loaded makes the check an error | check the named prompt renders what you want the judge to see |

## v2.17.0

### A failing stage ends the turn, and `Send` returns its error

A pipeline stage that returned an error did not fail the turn. `ExecuteSync`
and `Conversation.Send` returned nil, and the stages after it kept running on
the failed stage's closed output. A template stage that could not resolve the
system prompt, for example, let the provider stage call the model with no
system prompt at all.

The first failing stage's error now reaches the caller: `ExecuteSync` and
`Send` return it, `Stream` delivers it as its last element, and the completion
event reports it. `errors.Is(err, stage.ErrTemplateUnresolved)` holds for an
unresolved system-prompt variable. The provider stage does not call the model
once an upstream stage has failed. Nothing is canceled: the other stages drain
and persist as before. A context error that follows the caller's own cancel is
not a failure. Error elements that a stage reports and continues past, such as
those from `FramesToMessageStage` and `MediaComposeStage`, do not stop the
call.

| If you | You will see | Change |
|---|---|---|
| Send to a prompt whose system prompt has an unresolved variable | `Send` returns an error wrapping `stage.ErrTemplateUnresolved`, where it returned a reply from a model that never saw the prompt | supply the variable |
| Run a pipeline whose custom stage returns an error | `ExecuteSync`, `Send` and `Stream` return that error | handle it, or have the stage report and continue with an error element |

### A workflow state's `skills` filter applies, and the skill index follows it

A workflow state's `skills` field was parsed and never read, so every skill
could activate in every state. `WorkflowConversation` now sets the
conversation's own skill filter from the state: when the conversation opens for
a state, and when a transition the model makes mid-turn commits, before the
destination state's next round. A state without the field clears the filter.
Narrowing deactivates active skills the new filter excludes, and their tool
grants go with them.

The field takes `none` (case-insensitive), a glob, or, as the PromptPack spec
defines it, a path to a skill directory or `SKILL.md` file, which admits the
skills at or under it. Globs match as before. An inline skill has no path and
matches no path filter.

The `skill__activate` description, which lists the skills the model can
activate, follows the filter. It lists only the skills the conversation's
filter admits and is withheld when the filter admits none. A conversation
with no filter still lists every skill. The skills selector, which narrows the
list further, now runs once per `Send` instead of once per round, and falls
back to every admitted skill if a mid-turn transition leaves it nothing.

| If you | You will see | Change |
|---|---|---|
| Declare `skills` on a workflow state | only the matching skills activate in that state, and the model is offered only those | check each state's value; `none` now means no skills |
| Leave `skills` off a state | every skill, as before | nothing |
| Call `SkillsCapability.RefreshSkillIndex` on a tool registry yourself | nothing: it stays for hosts that drive a registry directly, and conversations no longer call it | nothing |

### Tool policy limits reach the SDK, and `MaxCallsPerMinute` is enforced

The SDK never set the caller's `pipeline.ToolPolicy`, and the provider stage never read `MaxCallsPerMinute`. A pack cannot declare these limits: the spec's `tool_policy` is closed. `WithToolPolicy` now sets the policy for every tool loop a conversation runs: text, voice in VAD mode, each workflow state, and the prompt and agent steps of a composition. A realtime duplex session runs its tools outside the loop and is not covered. Each turn merges the policy with the prompt's `tool_policy` under `pipeline.MergeToolPolicy`: `MaxRounds` and `MaxToolCallsPerTurn` take the lower set value, blocklists combine, and the host's `ToolChoice` wins when set. With `WithSkipSchemaValidation`, a `tool_policy` key the spec does not define is logged as a warning naming the prompt and the key.

`MaxCallsPerMinute` counts each tool's calls in one sliding window per conversation. The window spans the conversation's turns, its composition steps and every state of a `WorkflowConversation`. A call counts when it is admitted to run, not when the model makes it:

- a call the tool loop runs counts as it runs;
- a call held for approval counts when `ResolveTool` or `ResolveToolWithArgs` approves it;
- a deferred client call counts when `Resume`, `Continue` or `ContinueDuplex` uses the result.

A rejected, abandoned or unanswered call never counts. A call over the limit does not run, and the model is told `Tool X not executed: its limit of N calls per minute is reached`. The call emits `tool.call.started` and then `tool.call.failed`. An approval over the limit returns a resolution whose `Error` wraps `*stage.CallRateLimitError`, and the handler is not called. A held call is checked when a person approves it, so an approver can approve a call that then hits the limit.

| If you | You will see | Change |
|---|---|---|
| Need a per-minute cap on a tool | `WithToolPolicy(&pipeline.ToolPolicy{MaxCallsPerMinute: n})` limits it per conversation | nothing, or set the option |
| Set `MaxCallsPerMinute` on a stage you build | the limit is read for the first time | share one window across stages with `ProviderConfig.CallRate` |
| Use `tools.WithRateLimit` as well | the registry limit still counts every conversation's calls to a tool together | nothing |

### A provider's `rate_limit` is enforced on every request

`rate_limit` (`rps`, `burst`) on a provider config was loaded and then dropped. `ApplyProviderTuning` now copies it, `ProviderSpec.RateLimit` carries it, and `CreateProviderFromSpec` applies it, so `Predict` and `PredictStream` share one bucket per provider instance. The limit lives in `base.RateLimitTransport`, which waits for a token before each request. A zero `rps` is unlimited, and a `burst` below 1 counts as 1.

- A canceled context returns `context.Canceled`. A turn that falls past the context deadline fails at once with `base.ErrRateLimitPastDeadline`, which wraps `context.DeadlineExceeded` and is not retried. A limited request is never sent.
- The wait does not count against `request_timeout`.
- The credential is applied once per attempt, after the wait, so a SigV4 signature or a Vertex or Azure token is never older than the wait. A same-host `307` or `308` redirect is re-authorized. A redirect to another host is not.
- A credential that cannot be applied fails with `base.ErrRequestAuthorization`. Retry loops do not retry it and `providers.IsTransient` reports false.

| If you | You will see | Change |
|---|---|---|
| Set `rate_limit` on a provider | requests wait for a token, where they ran unlimited | check `rps` and `burst` |
| Write a provider on `BaseProvider` | `NewBaseProviderWithCredential` stores the credential and `SetCredential` replaces it; call `AuthorizeRequest` where you applied auth | pass a header-key credential with `HeaderKeyCredential` if you send something other than the standard header |
| Implement `credentials.Credential` | the optional `credentials.HeaderNamer` lets the custom-header collision check run before the credential is applied | implement it if your credential sets headers |

### Credentials and custom headers stay on their origin

`http.Client` copies every header except `Authorization` and cookies to a redirect hop, so a provider's `X-Api-Key`, `X-Goog-Api-Key`, Azure `Api-Key`, SigV4 headers and custom `headers` followed a redirect to any host. A redirect to another scheme or host now drops the well-known credential headers, the headers the credential names through `credentials.HeaderNamer`, and every custom header, before the hop is sent. A same-origin redirect keeps them all. The provider clients, the embedding, rerank, TTS, STT, classify and inference default clients, and a client you pass in all get the policy. `httputil.WithCredentialRedirectPolicy` adds it to a copy of your client and never modifies the original.

vLLM, Ollama and Imagen now use a resolved `credential:`. vLLM falls back to `additional_config.api_key`, and Imagen falls back to `GOOGLE_API_KEY`, then `GEMINI_API_KEY`. Ollama sends the credential to a proxy in front of it.

| If you | You will see | Change |
|---|---|---|
| Sit a provider behind a gateway that redirects to another host | the redirected request carries no key or custom header | point the provider at the final host |
| Set `credential:` on a vLLM, Ollama or Imagen provider | it is sent, where it was ignored | check the credential is the one you want sent |
| Set both `GOOGLE_API_KEY` and `GEMINI_API_KEY` for Imagen | `GOOGLE_API_KEY` is used | nothing, or unset the one you don't want |

### Pipeline timeouts, shutdown and concurrency report what happened

| If you | You will see | Change |
|---|---|---|
| Hit the pipeline's `ExecutionTimeout` or `IdleTimeout` | the turn fails with an error wrapping `stage.ErrExecutionTimeout` and `context.DeadlineExceeded`, or `stage.ErrIdleTimeout`, where `ExecuteSync` returned a truncated result with a nil error | handle the error |
| Stream into a pipeline and an upstream stage fails | the streaming provider stage drops the turn in progress instead of sending a half-received turn to the model | nothing |
| Call `Shutdown` while a reader is slow | a reader that reads within the grace period gets every element and the error. About half of the elements were dropped before | nothing |
| Stop reading an execution's output during `Shutdown` | the execution drops the rest of its output and its stream ends with `stage.ErrOutputTruncated`, which also wraps the turn's own error. `ExecuteSync` returns it | treat it as "the turn ran but its output was cut"; do not retry as for `ErrPipelineShuttingDown`, which means the turn never ran |
| Set `PipelineConfig.MaxConcurrentPipelines` | `Execute` waits for a slot, returning the context's error if the context ends first and `ErrPipelineShuttingDown` if `Shutdown` is called first | nothing |
| Build `TemplateStage` or `PromptAssemblyStage` without a shared `TurnState` | `Build` fails with `ErrInvalidPipeline` wrapping `ErrTurnStateRequired`, naming the stage | pass the `TurnState` |
| Read `Execute`'s output | the caller must read the output to its end or cancel the context | nothing |

### Request media is checked against the provider's declared capabilities

`providers.ValidateMultimodalRequest` had no production caller, so an unsupported part reached the adapter. The adapter either failed with its own error or dropped the part and sent the text alone. The provider stage now validates every round, streaming and non-streaming, including composition steps, before the provider-call event. An unsupported part fails the round with `*providers.UnsupportedContentError`.

- Only providers that implement `providers.MultimodalCapabilityProvider` are checked. The mock, replay and imagen providers are not.
- Validation uses what the adapter can send. A provider config's `capabilities` list never narrows it, through `providers.SendableContentProvider`.
- Document parts are checked against `SupportsDocuments` and `DocumentFormats`. Assistant messages are skipped. A part with no MIME type is checked for its modality only, and a format entry ending in `/*` admits every subtype.
- Gemini declares more audio, video and document formats (Office and OpenDocument types, `text/*`, JSON, XML, RTF). OpenAI declares `audio/x-wav` and declares audio only on the Chat Completions API.
- Duplex sessions are not covered.

| If you | You will see | Change |
|---|---|---|
| Send a part the provider cannot send | `Send` returns `*providers.UnsupportedContentError` where the text went alone or the adapter failed | send supported media, or switch provider |
| Run a custom provider whose declarations are narrower than what it sends | the same error | widen the declarations, or pass `sdk.WithoutContentValidation()` (`stage.ProviderConfig.SkipContentValidation`) |

### The media item and size caps apply to each message

The caps of 20 media items and 100 MB in total were counted per loader, and Claude, Gemini, OpenAI and Ollama made a new loader for every part, so the caps never applied on them. vLLM counted them across the whole request. Every provider and path now counts them per message, including each tool-result message. An image sent as a URL that the provider passes on is not loaded and does not count. A message over a cap fails before any request is sent, with an error wrapping `providers.ErrMediaItemLimitExceeded` or `providers.ErrAggregateMediaSizeExceeded`.

The tool-calling paths of Claude, Gemini, OpenAI and Ollama sent a message's text alone when its media failed to load. They now fail the request, as the other paths already did.

| If you | You will see | Change |
|---|---|---|
| Send more than 20 media items, or more than 100 MB of media, in one message | `Send` returns one of the cap errors, where the request was sent | split the media across messages |
| Set `MaxFramesPerMessage` above 20 on `FramesToMessageStage` | the composed message fails the item cap. The default is now 20 | set it to 20 or fewer |
| Send a tool result or tool-turn message whose media cannot load | the request fails, where the text went alone | fix the media source |

### Media retention honors `ExpiresAt`, and deduplicated media stays reachable

`storage.MediaMetadata` gains an optional `ExpiresAt` (`expires_at`). `TimeBasedPolicyHandler` honors it with or without a `PolicyName`. Media with neither never expires.

With `EnableDeduplication`:

- A store of content whose shared file a policy already deleted writes the content again. It returned a reference to the missing file before, and `RetrieveMedia` failed.
- A hit that asks for longer retention extends the shared file's retention and changes only `PolicyName` and `ExpiresAt`. The first store's run, conversation, session and message fields stay. A shorter-lived hit never cuts the retention short, and a hit that never expires keeps the file.
- Metadata and the dedup index are written atomically, and enforcement skips an item while a store is writing it.

`local.FileStore` calls `TrackMedia` after each store, so media stored after `StartEnforcement` expires without a restart. Without `BuildExpiryIndex`, `EnforcePolicy` walks `baseDir`. `FileStoreConfig.PolicyHandler` and `WithPolicyHandler` apply a `storage.MediaPolicyHandler` to each item before it is written, and a rejected policy fails `StoreMedia`. `storage.PolicyHandler` keeps its signatures.

### Eval checks stop ignoring params and report failures as failures

| If you | You will see | Change |
|---|---|---|
| Use `tool_efficiency` with `max_calls` | the within-limits score is `(max_calls + 1 - calls) / (max_calls + 1)`: 1 with no calls, `1 / (max_calls + 1)` at the cap, 0 only once a limit is broken. It was `1 - calls / max_calls`, so a run at the cap scored 0 | none for most thresholds: `max_calls: 5` with `min_score: 0.4` still means at most 3 calls |
| Set `threshold` on `directional` | an error result; the score for `similar_content` is the word-overlap ratio | put the pass line on a `type: assertion` wrapper as `min_score` |
| Set `model` on a judge check (`llm_judge`, `llm_judge_session`, `llm_judge_tool_calls`, RAG, safety, `role_violation`) | an error result, where the param filled a field nothing read | bind another provider with `provider` or `sdk.WithNamedProvider` |
| Set `min_similarity` on `cosine_similarity`, or `min_score` or `max_score` on `rest_eval` or `a2a_eval` | an error result; the endpoint is not called. `cosine_similarity` reports the raw similarity as its score and no longer requires `min_similarity` | wrap the check in `type: assertion` and set `min_score` there |
| Set `extra` on a judge check | the value reaches `JudgeOpts.Extra`; the built-in judges send it as the prediction request's metadata | nothing |
| Run a safety or RAG judge that cannot resolve its judge, whose call fails, or that lacks its input | the result carries an `Error` and the runner emits `eval.failed`, where it emitted `eval.completed` with a score of 0. The score stays 0, so guardrails and assertions still fail closed | handle `eval.failed` |
| Declare `trigger: on_workflow_step` | the eval runs after each committed workflow state transition, against the turn that produced it. `sdk.Evaluate` dispatches the trigger to `EvalRunner.RunWorkflowStepEvals` | nothing |
| Set `on_deny: respond` on `topic_policy` | a denied turn is answered with the policy's response, `I can only help with: <allowed subjects>.`, unless the validator sets a message | nothing |

### Skills: preloaded instructions, merged sources and selectors take effect

- `preload: true` skills now put their instructions in the system prompt on every `Send`, under `## Skill: <name>`. Before, preload applied only the tool grants. A skill that is deactivated or dropped by a workflow filter leaves the prompt.
- `WithSkillsDir` and `WithSkillSource` merge with the skills the pack declares. They were dropped when the pack declared `skills`. A name collision keeps the first source listed.
- A `skills.SkillSelector` (`WithSkillSelectorOption`, `WithSkillSelector`, `ExecutorConfig.Selector`, `TagSelector`) narrows the `skill__activate` index. It was stored and never read. It runs once per turn with the user's message, is skipped for an empty message, and an error or an empty choice lists every admitted skill. A selector bound in the runtime config takes precedence. `skills.AdaptSkillSelector` adapts one onto `selection.Selector`.
- The SDK never modifies a `SkillsCapability` you pass with `WithCapability` to several `Open` calls. An open that adds sources, `WithSkillSelectorOption`, `WithMaxActiveSkillsOption` or a runtime-config skills selector runs on its own copy.

| If you | You will see | Change |
|---|---|---|
| Pass a `SkillSelector` | the index shrinks to what it chooses | check the selector |
| Mark a skill `preload: true` | its instructions add to every turn's context | keep preloaded skills short |

### Client tools: bindings route, declined calls follow `decline_strategy`

| If you | You will see | Change |
|---|---|---|
| Call `OnClientTool` for a pack tool | the tool is a client tool in that conversation: the call reaches your handler, or suspends when the handler is nil. It failed with `no handler registered` before, because the pack loader registers every pack tool as local | remove a `WithToolDescriptorOverride` that set `Mode` only to work around this |
| Bind a tool with no handler | `Conversation.DeferClientTool(name)` does it; `OnClientTool(name, nil)` is the same and no longer panics at call time | nothing |
| Reject a deferred client tool with `RejectClientTool`, and its descriptor declares a consent `decline_strategy` | the model sees `tools.ConsentDeclinedMessage` text for the strategy, followed by `Reason: <reason>` when you give one. `fallback` gives `Tool X unavailable: user declined consent. Use alternative approach.`, `retry` gives `Tool X denied: user declined consent. Try a different approach.`, and any other value gives `Tool X denied: user declined consent`. With no strategy the text stays `Tool rejected: <reason>` | declare the strategy on the descriptor (a host tool registry or `WithToolDescriptorOverride`); a pack cannot carry it |
| Set `validate_output` in a tool's client config | `Resume` and `ResumeStream` check a `SendToolResult` value against the output schema; a mismatch becomes a failed call. Multimodal results are not checked | nothing |
| Run an exec tool that answers `{"pending": ...}` | the call is held like a deferred client tool and appears in `resp.ClientTools()`. The synchronous `Execute` returns an error for a pending answer | resolve it with `SendToolResult` or `RejectClientTool` and `Resume` |

`tools.ConsentConfig.Required` and `DeclineStrategy` are declared settings the SDK passes through on the descriptor. `Required` enforces nothing in the SDK.

### SDK options, errors and conversation behavior

| If you | You will see | Change |
|---|---|---|
| Match `Open` errors with `errors.Is` | `ErrPackNotFound`, `ErrPromptNotFound` and `ErrProviderNotDetected` now match; they did not before | nothing |
| Type-switch on SDK errors | `*sdk.PackError` for a missing, unreadable or invalid pack, `*sdk.ProviderError` for a provider HTTP or transport failure, `*sdk.ValidationError` (`AsValidationError`) for a hook or validator denial, and `*sdk.ToolError` on a failed `ResolveTool` resolution. Each wraps the runtime error, so `errors.As` and `errors.Is` on that error keep working. `sdk.ErrToolNotRegistered` and `sdk.ErrToolNotInPack` mark the no-handler errors | nothing |
| Call `Fork` | the fork keeps tools registered with `OnToolCtx`, `OnToolHTTP` and `OnToolExecutor` | nothing |
| Call `WithA2ATools` twice | every bridge registers its tools. Two bridges that register the same tool name fail `Open`, naming both | rename a tool |
| Pass `WithVADMode` a nil TTS or STT service | an earlier `WithTTS`, `WithTTSProvider` or `WithSTTProvider` stays | nothing |
| Set `WithAudioMonitor` | when the mode resolves to on, `Conversation.AudioMonitor()` delivers `AudioMonitorFrame` values resampled to `Rate`. The option rejects an unknown `Mode` and a `Rate` other than 16000, 24000 or 48000 (0 means 24000) | read the channel; a full buffer drops frames |
| Set `EnableResize` with `MaxWidth` or `MaxHeight` in a `VideoStreamConfig` | each kept image frame is scaled to fit and re-encoded at `Quality` | nothing |
| Validate a pack that relies on the host's default judge | `sdk.ValidatePackWithOptions(path, skip, opts...)` applies the options you pass to `Open`, including `WithJudgeProvider` and `WithEvalRegistry` | use it in place of `ValidatePack` |
| Declare an eval at pack level | `Open` checks the providers of the pack-level and prompt-level evals that run on the prompt's turns, with prompt-level evals overriding by ID | declare the provider |
| Declare a retrieval provider in a runtime config or with `WithEmbeddingProvider` | retrieval runs, with a default top-k of 3 unless `WithContextRetrieval` sets one | nothing |
| Set `RelevanceConfig.CacheEmbeddings` | the context builder embeds only texts it has no vector for, keyed on the text and the embedding provider and model. `sdk.RelevanceConfig` now carries the field | nothing |
| Rely on an empty relevance `QuerySource` | it means `QuerySourceLastUser`; it fell back to oldest-first truncation | nothing |

### HTTP tools apply their request configuration on every call

| If you | You will see | Change |
|---|---|---|
| Use `WithPreRequest` | the hook runs on every built request, after the URL and headers are applied. An error fails the call with `pre-request hook failed: ...` and nothing is sent. It never ran before | check that the hook is safe to run |
| Put `{{...}}` in a tool URL with no `request` block | the template renders from the arguments, and the arguments it consumes are not sent again as query parameters or body fields. A template in the scheme, host or port fails with `ErrTemplatedURLHost` | nothing |
| Call a tool with a `request` block and no arguments | `static_query`, `static_headers` and `header_params` apply. For `POST`, `PUT` and `PATCH`, `static_body` is sent as the JSON body | nothing |
| Pass a value containing `/`, `?` or `#` to a URL template | every action is escaped with `url.PathEscape` before the first literal `?` or `#` in the template and `url.QueryEscape` after it. A rendered path with a `.` or `..` segment fails with `ErrURLDotSegment` | nothing |
| Use a bad `body_mapping` expression | the call fails before a request is sent | fix the expression |
| Match `call_args` in a file tool response | numbers compare by value, so a YAML `42` matches a JSON `42` | nothing |

### Provider adapters

| If you | You will see | Change |
|---|---|---|
| Use Claude, OpenAI or Gemini with the compactor | `MaxContextTokens()` reports the model's context window from a per-model table, so compaction sizes itself instead of using the 128K default. `additional_config.context_window` overrides the table and is the only source for Ollama and vLLM. An unknown model reports 0 | set `context_window` for a model outside the table |
| Send tools to Gemini | `safetySettings` are sent on the tools request as on the plain one | nothing |
| Send tools to vLLM with `guided_grammar` | the grammar is sent | nothing |
| Register a provider under an ID already registered | the last registration wins for `Get`, `GetByType` and `GetAll`; the replaced provider's typed entry is dropped | nothing |
| Embed with OpenAI or Voyage AI | a response without exactly one embedding per input returns an error from `ExtractOrderedEmbeddings`, not nil vectors | handle the error |
| Read a canceled OpenAI chat stream | the finish reason is `canceled`, as for Claude, Gemini and Ollama, where it was `cancelled` | match `canceled` |
| Observe streams | `BaseProvider.SetStreamObserver` or `ProviderSpec.StreamObserver` receives every chunk; an observer that implements `providers.StreamEventObserver` also receives a `StreamEvent` per callback. The final chunk of a clean stream carries a `*PredictionResponse` in `FinalResult` unless the provider set one | nothing |
| Resize a GIF or WebP | the image is labeled `image/jpeg`, the format it is encoded to | nothing |
| Call a TTS service when the account's quota or credit is exhausted | the error wraps `tts.ErrQuotaExceeded` (HTTP 402 and the quota error codes), apart from `tts.ErrRateLimited`. A `429` stays rate-limited and retryable | nothing |

### Runtime packages

| If you | You will see | Change |
|---|---|---|
| Use `variables.StateProvider` | any state store error other than `statestore.ErrNotFound` is returned, wrapped | handle the error |
| Summarize or index messages built from `Parts` | `LLMSummarizer` and `InMemoryIndex` read the content through `GetContent()` | nothing |
| Use the logger | `Bearer` tokens redact whole, including JWT segments. `Configure` records its level, format, common fields and modules, so `SetLevel`, `SetVerbose`, `SetOutput` and `SetLogger(nil)` keep them. `ModuleLoggingSpec.Fields` apply | nothing |
| Search non-Latin text with `memory/corpus` | tokenization splits on runes that are neither letters nor digits | nothing |
| Register session hooks | every session hook runs for the event, each failure is logged at WARN with the hook's name, the event and the session ID, and the failures return joined | nothing |
| Round-trip a prompt through `ToConfig` | the prompt's `Extensions` are deep-copied, not dropped | nothing |
| Read A2A SSE streams with `ReadSSEWithIdleTimeout` or `SendMessageStream` | an idle stream ends with a last event whose `Error` is `a2a.ErrSSEIdleTimeout`, where it looked like a clean end | handle the error |
| Set `server/a2a` `WithWriteTimeout` | `ListenAndServe` and `Serve` set `http.Server.WriteTimeout`; SSE streams lift the deadline for their own connection | nothing |
| Retry A2A requests | the backoff delay is capped at `MaxDelay` first, then shortened by up to 50% jitter, so it never exceeds `MaxDelay`. It could reach 1.5 times `MaxDelay` | nothing |
| Adapt runtime events to AG-UI | `agui.WithRuntimeEvents(types...)` forwards the bus's events as `RAW` events with source `promptkit`. It is off by default | nothing |
| Marshal a provider config | the fields of `pricing`, `rate_limit` and `defaults` are `omitempty`: zeros are left out, and a generated schema marks them optional | nothing |

### New seams for hosts and custom pipelines

These additions need no action.

| Addition | Purpose |
|---|---|
| `hooks.ToolResultRewriter` | A `hooks.ToolHook` that also implements it receives the text of each successful tool result the provider stage runs, and returns the text the model sees. Rewriters chain in registration order, run before the result size limit, and run once per text part of a multimodal result. An error fails the call: the model gets an error result and `tool.call.failed` is emitted. Client tools, calls resumed after approval and duplex sessions are not rewritten. `RewriteResult` must be safe for concurrent use |
| `stage.ProviderConfig.DescribeTool` | Returns the descriptor to offer the model for each registered tool on every tools build: a copy when it changes the tool, or nil to withhold it. The registry's descriptor is never changed. The stage rebuilds the tools array when the result changes between rounds, including after a mid-turn workflow handoff |
| `skills.Executor.SkillIndexIn`, `skills.Executor.SelectSkillsIn` | The `skill__activate` index for one `ActiveSet`, and the selector's choice over the skills that set admits |
| `sdk.WithToolPolicy`, `stage.CallRateWindow`, `stage.CallRateLimitError`, `stage.ProviderConfig.CallRate`, `tools.Registry.ExecuteAsyncAdmitted` | Set the tool policy from the SDK, and share one tool-call window across stages. `CallRateWindow.Admit` checks and counts a call |
| `stage.ErrOutputTruncated`, `stage.ErrExecutionTimeout`, `stage.ErrTurnStateRequired`, `PipelineConfig.PrioritizeElements` | Sentinels for truncated output, the execution timeout and a stage built without a `TurnState`. `PrioritizeElements` (`WithPrioritizeElements`) relays each stage's input in element priority order; it is off by default because an `EndOfTurn` can then overtake its turn's messages |
| `base.RateLimitTransport`, `base.ErrRateLimitPastDeadline`, `base.ErrRequestAuthorization`, `providers.BaseProvider.AuthorizeRequest`, `credentials.HeaderNamer` | The provider rate limit and credential-after-wait path |
| `providers.SendableContentProvider`, `stage.ProviderConfig.SkipContentValidation`, `sdk.WithoutContentValidation` | Request media validation and its opt-out |
| `storage.MediaMetadata.ExpiresAt`, `storage.MediaPolicyHandler`, `storage.MediaPolicyTracker`, `storage.MediaExpiryReporter`, `storage.MediaWriteGuard`, `local.WithPolicyHandler` | Media retention and deduplication |
| `evals.EvalRunner.RunWorkflowStepEvals`, `evals.ResultDetailReplacement`, `evals.ResultReplacement` | Run `on_workflow_step` evals, and let a check name the text that replaces a denied turn |
| `skills.AdaptSkillSelector`, `skills.ActiveSet.Instructions` | Run a `SkillSelector` on the `selection.Selector` path, and read an active skill's instructions without activating it |
| `Conversation.DeferClientTool`, `tools.ConsentDeclinedMessage`, `sdk.ValidatePackWithOptions`, `Conversation.AudioMonitor` | Client tool binding without a handler, the decline text, pack validation with `Open`'s options, and tapped audio |
| `providers.StreamObserver`, `providers.StreamEventObserver`, `BaseProvider.SetStreamObserver`, `providers.NewBaseProviderWithCredential`, `providers.HeaderKeyCredential` | Observe streamed chunks, and construct a provider with its credential |
| `agui.WithRuntimeEvents`, `logger.ModuleConfig.SetModuleFields` | Forward runtime events as AG-UI `RAW` events, and set per-module log fields |

## v2.13.0

### Media options stay with the conversation that set them

`WithMediaStorage` and `WithUnsafePrivateNetworkMedia` were written onto the
provider, so conversations sharing one provider shared them: media resolved
through whichever store was set last, and one conversation that allowed
private-network URLs allowed them for all. Each conversation now runs on its
own view of the provider carrying its own settings. A provider of your own
that does not implement `providers.MediaScoped` is still configured in place.

Claude and Gemini also sent a message as text alone when one of its media
parts could not be loaded, so the model answered about media it never saw.
They now return the error, as OpenAI and Ollama do.

| If you | You will see | Change |
|---|---|---|
| Share one provider across conversations with different media options | each conversation uses its own | nothing |
| Send media that cannot be loaded (a refused private-network URL, a missing storage reference) to Claude or Gemini | an error from `Send` | fix the media, or allow the network with `WithUnsafePrivateNetworkMedia` |

### Realtime sessions use the prompt's parameters and the configured model

An `OpenDuplex` session in ASM mode ignored the prompt's `parameters`, so the
same prompt ran at the realtime API's defaults. The session now carries them:
Gemini Live sends `temperature`, `top_p`, `top_k` and `max_tokens`, and OpenAI
Realtime sends `max_tokens` (its session takes no other sampling parameter).
The rest are logged as not sent. Gemini Live does not answer below a
temperature of 0.5, so a lower one is not sent, with a warning.

An OpenAI realtime session also ran a built-in model whenever the configured
one lacked "realtime" in its name, and that model has been retired. It now
runs the configured model. A Gemini Live provider with no model uses
`gemini-3.8-live`.

| If you | You will see | Change |
|---|---|---|
| Open a duplex session on a prompt that sets `parameters` | the session uses them | check the values, which were never sent before |
| Set a temperature below 0.5 on a Gemini Live prompt | a warning, and the model's default temperature | raise it to 0.5 or more |

### OpenAI uses the Responses API by default

An OpenAI provider with no `api_mode` used Chat Completions unless its model
name ended in `-pro`. Current OpenAI models refuse function tools on Chat
Completions, so every tool call on them failed. Against OpenAI's own API (no
`base_url`, or `api.openai.com`) the default is now the Responses API. A
provider with any other `base_url`, which is usually an OpenAI-compatible
server, still defaults to Chat Completions, as do Azure and Bedrock.

The Responses path now loads images and rejects parts the same way as Chat
Completions. Before, it read only an image's URL or inline data and dropped
an image held in media storage or a local file, and any audio or video part,
without an error.

| If you | You will see | Change |
|---|---|---|
| Use OpenAI without `api_mode` | requests to `/v1/responses` | nothing, or set `api_mode: completions` to keep Chat Completions |
| Send audio input to an audio model (`gpt-audio`) without `api_mode` | an error that audio needs Chat Completions | set `api_mode: completions` |
| Point `base_url` at an OpenAI-compatible server | nothing | nothing |

### Providers stop sending sampling parameters the model rejects

Current OpenAI models (`gpt-5.5`, `gpt-6`) and Claude 5.x models answer
`temperature`, `top_p`, `top_k` or a penalty with a 400, and Gemini models
answer penalties with one. Unless the provider config listed the parameter
in `unsupported_params`, a prompt that set it failed every call. The OpenAI,
Claude and Gemini providers now read the rejection, stop sending that
parameter for the rest of the provider's life, log a warning naming it, and
retry the call, which then succeeds with the model's own default.

| If you | You will see | Change |
|---|---|---|
| Set a sampling parameter a model rejects | the call succeeds after a retry for each rejected parameter, and a warning | remove the parameter, or list it in `unsupported_params` to skip the retry |
| List parameters in `unsupported_params` | nothing | nothing |

### A prompt's `top_k` reaches the provider

A prompt's `parameters.top_k`, and a `model_overrides` entry's, was read from
the pack and then dropped before the call. It now reaches Claude (including
Bedrock and Vertex), Gemini and vLLM, whose APIs take it, on an opened prompt,
a composition step and a mid-turn workflow handoff. Claude omits it when
extended thinking is on, since the API rejects the two together. OpenAI and
Ollama's OpenAI-compatible endpoint have no top-k parameter, so they drop it
and log a warning once per provider. A model that rejects it is configured
with `unsupported_params: [top_k]`, which Claude and Gemini honor.

`PredictionRequest` and `prompt.CallParams` gain `TopK *int`, where nil sends
nothing. `providers.WarnUnsentParams` logs, once per provider and parameter,
a `top_k` or penalty a provider does not send. Claude, Gemini and the OpenAI
Responses API now log it for penalties too, which they drop.

| If you | You will see | Change |
|---|---|---|
| Set `top_k` on a prompt served by Claude, Gemini or vLLM | it on the request | check the value, which was never sent before |
| Set `top_k` on a prompt served by OpenAI or Ollama | a warning that it is not sent | remove it, or set it through `extra_body` for a compatible backend that takes it |
| Write a custom provider | `PredictionRequest.TopK` set when the prompt asks for it | send it, or call `providers.WarnUnsentParams` |

### Context compaction leaves room for the system prompt and tools, and keeps the message log whole

The tool-loop compactor measured only the transcript against its budget, so a
large system prompt or many tool definitions could still overflow the window
after compaction. It now compacts against the round's whole input, so the
system prompt and tool definitions are counted, and warns when they alone fill
the window. Separately, a compaction rule that removes messages
(`CollapsePairs`), or a store holding more history than the turn loaded, could
make the message log skip new messages; the log now records every new message
in order.

| If you | You will see | Change |
|---|---|---|
| Use the default compactor with a large system prompt or many tools | compaction starts earlier, so the request fits | nothing |
| Supply your own `CompactionStrategy` | `Compact` receives the round's input tokens (messages, system prompt and tool definitions) as `lastInputTokens` | compare that, not the messages alone, to your budget |
| Use `CompactionRules` with `CollapsePairs` and a `MessageLog` | new messages reach the log after a compaction removes old ones | nothing |

### A prompt's `temperature: 0`, `top_p` and penalties reach the provider

A prompt's `parameters` reached the provider only partly. Every provider read
a temperature of `0` as unset and replaced it with its own default, so
`temperature: 0` never took effect, and `top_p`, `frequency_penalty` and
`presence_penalty` were never sent at all. They now apply to an opened prompt,
a composition step and a mid-turn workflow handoff, and a `model_overrides`
entry can set them too. A realtime duplex session (ASM) takes them as well; see
the realtime section above.

`PredictionRequest` gains `TemperatureSet`, `FrequencyPenalty` and
`PresencePenalty`. A zero `Temperature` with `TemperatureSet` is sent as zero;
`providers.ResolveTemperature` applies the rule for custom providers.
Penalties are sent to OpenAI Chat Completions, vLLM and Ollama, whose APIs
take them. They are not sent to the OpenAI Responses API or Claude, whose APIs
do not, nor to Gemini, whose current models answer them with a 400.
`top_p` is not sent to Claude, which rejects it alongside a temperature. A model
that rejects a parameter is configured with `unsupported_params`. OpenAI o-series models withhold the penalties by
default, as they already did temperature and `top_p`.

| If you | You will see | Change |
|---|---|---|
| Set `temperature: 0` in a prompt's `parameters` | temperature 0 on the request, not your provider's default | nothing; this is what the prompt asked for |
| Set `top_p`, `frequency_penalty` or `presence_penalty` | them on the request, where the API takes them | check the values, which were never sent before |
| Build `PredictionRequest` yourself | nothing: a zero `Temperature` without `TemperatureSet` is still unset | set `TemperatureSet` to send an explicit zero |
| Read `prompt.Registry.CallParameters` | it returns a `prompt.CallParams` struct | read the fields from it |

### `PackTemplate.Open` honors every option `sdk.Open` does

A conversation opened from a `PackTemplate` was built by a separate copy of
`sdk.Open`'s setup, and the copy had drifted. `WithMediaStorage`,
`WithUnsafePrivateNetworkMedia`, `WithToolExecutor`, `WithToolRegistry`,
`WithShutdownManager` and a custom logger did nothing on it, and its
`OpenDuplex` refused `WithVADMode` with a provider that does not stream. Both
paths now run the same code once the prompt is found.

| If you | You will see | Change |
|---|---|---|
| Open conversations from a `PackTemplate` with any of those options | they take effect | remove any workaround that wired them by hand |

### Composition steps run with their prompt's `parameters`

A composition `prompt` or `agent` step sent no `max_tokens` or `temperature`,
whatever its prompt set, so the provider's defaults applied. A step now sends
its prompt's `max_tokens` and `temperature`, and those of the prompt's
`model_overrides` entry for the step provider's model, as an opened prompt does.

| If you | You will see | Change |
|---|---|---|
| Run a composition whose step prompts set `max_tokens` or `temperature` | those values on each step's request | check them, since steps used the provider's defaults before |

### `PackTemplate.Open` runs the same provider checks as `sdk.Open`

A conversation opened from a `PackTemplate` skipped two load-time gates that
`sdk.Open` runs: a check's `provider` key must be declared and bound to the
right kind of provider, and a `required` provider in `requires.providers` must
be bound. Such a pack opened cleanly from a template and failed at the first
request that needed the missing provider.

| If you | You will see | Change |
|---|---|---|
| Open a template whose pack has a check naming an undeclared or unbound key, or a required provider you do not bind | `Open` fails with the error `sdk.Open` gives | bind the provider, or fix the pack |

### A mid-turn workflow handoff runs on the destination prompt's provider

When the model transitions a workflow mid-turn, the destination state speaks in
the same turn. That round used to stay on the origin conversation's provider,
`max_tokens` and `temperature`, with only the system prompt and tools swapped.
It now runs on what opening the destination state would give it: the provider
its prompt's `provider` key is bound to (RFC 0017), its `parameters`, and the
`model_overrides` entry for that provider's model.

| If you | You will see | Change |
|---|---|---|
| Run a workflow whose states' prompts name different `provider` keys or set different `parameters` | the destination's provider and parameters from the round after the transition | nothing |
| Implement `stage.WorkflowStateResolver` | nothing: `Handoff.Call` is optional, and nil keeps the turn's provider and parameters | set `Call` to switch them |
| Rely on the default context compactor | after a handoff to a provider with a smaller context window, the transcript is compacted for that window before its first round | nothing. A `CompactionStrategy` you supply keeps its own budget unless it implements `stage.ProviderBudgetedCompaction`; a wrapper around a `stage.ContextCompactor` must forward `ForProvider` to keep following the provider |
| Bind a key that a workflow state's prompt names to a provider without tool support | `Open` fails: the transition tool is offered to every state's prompt | bind a provider that implements `providers.ToolSupport` |

### A prompt's `model_overrides` apply

A prompt's `model_overrides` entry for a model never applied: the runtime loaded
every template without a model, and the SDK dropped the overrides when it built
its prompt registry. The entry for the model of the provider that runs the call
now applies. Its `system_template`, `system_template_prefix` and
`system_template_suffix` shape the system prompt of an opened prompt, a
composition step and a mid-turn workflow handoff. Its `parameters` replace the
prompt's `max_tokens` and `temperature` in all three. The model is the bound provider's when the
prompt or step names a `provider` key, and an override key must match it
exactly.

| If you | You will see | Change |
|---|---|---|
| Ship a pack with `model_overrides` | the override for the running provider's model in the system prompt and parameters | check the override text and parameters, which have not run before |
| Ship a pack without `model_overrides` | nothing | nothing |

### A prompt or composition step runs on the provider its `provider` key names

PromptKit implements PromptPack v1.9.0, which adds RFC 0017. A prompt, or a
composition `prompt` or `agent` step, may name a `requires.providers` key in
`provider`, and that call runs on whatever the host bound to the key. A step's
key wins over its prompt's; with neither, the call runs on the agent provider as
before. `sdk.Open` checks every prompt and step in the pack, not only the one it
opens.

| If you | You will see | Change |
|---|---|---|
| Open a pack whose prompts and steps name no `provider` | nothing: every call runs on the agent provider | nothing |
| Open a pack where a prompt or step names a key the pack does not declare in `requires.providers`, or declares with a role other than `llm` | `Open` fails, naming the prompt or step and the key | declare the key with `role: llm`, or fix the name |
| Open such a pack without binding the key | `Open` fails, even when the requirement is optional | bind it with `WithNamedProvider`, or declare it in a provider file after the agent (`WithProviderFile`, `WithProvidersDir`) |
| Bind an inference provider to such a key, or a provider without tool support to a key that an `agent` step or a prompt with `tools` names | `Open` fails, saying what the key is bound to | bind an LLM provider, with tool support where the call uses tools |
| Open a pack in which every prompt names a key, binding only those keys | `Open` succeeds with no agent provider, and does not auto-detect one from the environment | nothing; pass an agent provider, API key or model only if a call should run on it |
| Open a prompt naming a key with `OpenDuplex` | the session streams on the bound provider, which must support duplex streaming in ASM mode | bind a provider that implements `providers.StreamInputSupport` |
| Build `stage.CompositionExecutorDeps` yourself | nothing: the new `ResolveProvider` is optional, and without it every step runs on `Provider` | set `ResolveProvider` to route steps by key |

### Packs carry RFC 0016 governance, and dangling governance references fail `Open`

PromptKit implements PromptPack v1.8.1. A pack may declare `obligations`,
`reviews` and `independent_of` in its governance, an `id` on a validator, and
`extensions` on prompts, validators, evals, agents, workflow states and
compositions. Each host-facing seam hands you the matching declaration; see
[Apply Governance Policy in Your Host](/sdk/how-to/hooks/apply-governance-policy/).

| If you | You will see | Change |
|---|---|---|
| Open a pack whose obligation or review names an eval, validator id or obligation id that does not exist, or a governance field its governance does not declare, or whose review `cadence` is not an ISO 8601 duration | `sdk.Open` fails with a `governance validation failed` error | fix the reference; `sdk.ValidatePack` returns the same error before you open |
| Use a CURIE prefix in `obligation`, `applies_to` or a review `type` that is neither well-known nor in `vocabularies` | a logged warning, and a `warning` issue from `sdk.ValidatePack`; the pack still opens | declare the prefix in `vocabularies`, or ignore it |
| Give two validators the same `id`, or two obligations or two reviews in one governance object the same `id` | `sdk.Open` fails | make validator ids unique across the pack, and obligation and review ids unique within each governance object |
| Build `tools.ToolDescriptor`, `hooks.ToolRequest`, `hooks.ProviderRequest` or `events.ValidationEventData` literals | nothing: the new `Declaration`, `Agent`, `Prompt` and `Validator` fields are optional, and nil means undeclared | nothing |
| Want to decide workflow transitions | every transition is allowed unless you install a policy | implement `workflow.TransitionAuthorizer` and pass it to `sdk.WithTransitionAuthorizer` |

## v2.12.0

### Conversations use your provider's `max_tokens` and `temperature`, and send no output-token limit by default

A conversation sent `max_tokens: 4096` and `temperature: 0.7` on every turn
whose prompt set no `parameters`, so a provider's own defaults never applied.
It now sends neither, and the provider's defaults decide. With no output-token
limit from the prompt or the provider, none is sent and the model's own maximum
applies. Claude requires a limit, so it falls back to 4096. Providers that
`sdk.Open` builds for you keep `temperature: 0.7` and no longer set a limit.

| If you | You will see | Change |
|---|---|---|
| Pass a provider with `Defaults.MaxTokens` or `Defaults.Temperature` to `sdk.WithProvider` | those values on the wire | nothing; remove any workaround that rewrote 4096 / 0.7 |
| Pass a provider with no `Defaults` | no output-token limit (Claude: 4096); temperature `0` on OpenAI, Gemini, Ollama and vLLM, and the API's own default on Claude | set `Defaults`, or set `parameters` on the prompt |
| Rely on the SDK capping replies at 4096 tokens | longer replies, and output cost up to the model's maximum | set `max_tokens` in the prompt's `parameters` or in the provider's defaults |
| Want no limit and set a large number to get it | nothing breaks | set the provider's `max_tokens` to `-1` (`providers.MaxTokensUnlimited`), or leave it unset; a prompt's `max_tokens` must be at least 1 |
| Set `max_tokens: -1` on a Claude provider | `CreateProviderFromSpec` fails: Claude requires a limit | set a positive limit, or leave it unset for 4096 |

## v2.8.0

### The A2A server and client speak A2A 1.0 and 0.3, not a mix of both

The A2A server used to answer in a shape of its own: 0.3's method names, 1.0's
parts, and state names from neither. Standard A2A clients could not parse it.
It now answers each request in the version the request asks for. That
is the `A2A-Version` header, or else the version of the method name used; a
request with no version is 0.3, as the spec says. The runtime client sends
`A2A-Version: 1.0` and falls back to 0.3 on its own.

| If you | You will see | Change |
|---|---|---|
| Parse server responses by hand | a 1.0 `SendMessage` result is `{"task": {...}}`; stream results are wrapped (`{"statusUpdate": ...}`); states are `TASK_STATE_*` and roles `ROLE_*` | use `a2a.Client`, which reads every version, or send 0.3 method names and parse 0.3 |
| Call `SendMessage` (1.0) and expect it to return at once | it now waits for the task to finish or need input, as 1.0 requires | set `configuration.returnImmediately: true` |
| Serve slow agents behind a proxy or load balancer with an idle timeout | a blocking `SendMessage` holds its request for the whole turn (a caller that disconnects releases it; the turn runs on) | set `WithMaxBlockingWait` below the proxy's timeout: past it the caller gets the working task and polls `GetTask` |
| Read a stream's first event as a `working` status | the first event is the Task | read `StreamEvent.Task` |
| Expect one artifact per streamed chunk (`artifact-0`, `artifact-1`, ...) | a text run is one artifact, extended with `append` and closed with `lastChunk`; the stored task holds it once, whole | key on `ArtifactID` and concatenate appended chunks |
| Call `tasks/list` or `ListTasks` without a `contextId` | `-32602`: without caller scoping it listed every caller's tasks | pass the `contextId`, or set `WithTaskOwner` to list the caller's own |
| Call `ListTasks` (1.0) without `pageSize`, or with one over 100 | at most 50 tasks when it is unset and 100 when it is larger, as the 1.0 proto says | page with `nextPageToken`; `Client.ListTasksPage` returns it, where `Client.ListTasks` returns one page of tasks |
| Filter `ListTasks` on `statusTimestampAfter`, or implement `TaskStore.Query` | a task whose status timestamp equals the cutoff is included: `StatusAfter` means at or after | match equal timestamps in your own `Query` |
| Filter `ListTasks` (1.0) on `TASK_STATE_UNSPECIFIED` | every task in the context: the unspecified state means no filter; 0.3's `unknown` still filters | nothing |
| Subscribe to a task that has finished | `-32004` UnsupportedOperation | read it with `GetTask` |
| Use `a2a.MethodSendMessage` and friends | nothing: they keep their old values (`"message/send"`, ...) and the server still answers them, but they are deprecated | use `a2a.MethodV1*` for 1.0 names, `a2a.MethodV03*` for 0.3, or `a2a.LookupMethod` |
| Match error codes | cancel of a finished task is `-32002`; an unsupported operation is `-32004`; push notification config is `-32003`; an internal failure is `-32603` without the cause | update the codes you match |
| Run PromptKit A2A clients from before this release against an upgraded server | a task that stops for client tools arrives as 0.3's `input-required`, which the old client cannot decode | upgrade clients with (or before) servers; an upgraded client still works with an old server, falling back to 0.3 names and the old card path |
| Serve the agent card from `/.well-known/agent.json` only | the card is also at `/.well-known/agent-card.json`, and the client looks there first | nothing, unless a proxy only forwards the old path |

`a2a.TaskState` constants keep their Go values. Stored tasks written in the old
JSON form still decode.

### A2A tasks can be scoped to their caller, and cancel/subscribe can span replicas

`a2aserver.WithTaskOwner` scopes every task to the caller that created it;
see [Callers and replicas](/sdk/how-to/interop/choose-a2a-server-mode/#callers-and-replicas).

| If you | You will see | Change |
|---|---|---|
| Implement your own `TaskStore` and want caller scoping | `NewServer` panics when `WithTaskOwner` is set | implement `OwnedTaskStore` (`CreateOwned`, `Owner`); if you implement `TaskQuerier`, filter by `TaskQuery.Owner` |
| Run several replicas behind a shared task store | `CancelTask` and `SubscribeToTask` only reached the replica running the task, as before | supply `WithTaskCanceler` and `WithTaskEventBus`, or keep session affinity |

## v2.7.0

### Inference providers share one interface; `runtime/classify` is superseded

`role: inference` providers now implement a single interface,
`inference.Provider` (`Infer`: content in, a probability per label out),
replacing the per-task `runtime/classify` interfaces. Each `type` calls one
vendor API: `huggingface`, `openai` (chat completions read through logprobs)
and `systemone` (TypeSafe's Jev, direct or through the Vercel AI Gateway). Pack
params for every check are unchanged. `runtime/classify` still compiles but
nothing uses it; it is removed in v3.

| If you | You will see | Change |
|---|---|---|
| Pass a custom backend to `sdk.WithClassifier` | `Open()` fails: the value "is not an inference.Provider" | implement `Infer(ctx, inference.Request) (inference.Response, error)`; audio and image arrive as media parts on `Request.Inputs` |
| Implement `evals.ProviderBinding` | a check whose `Classifier(key)` returns a classify backend errors: the bound value "is not an inference provider" | return an `inference.Provider` from `Classifier` |
| Set `stage.PipelineConfig.ClassifyRegistry` | it is ignored | set `InferenceRegistry` |
| Declare several inference providers without naming them in checks | every check now uses the **first** one registered | name the provider a check needs with `params.provider` (a key the pack declares in `requires`) |
| Use HF embeddings through the inference role | they are no longer served there | declare `role: embedding`, `type: huggingface` |
| Use `type: nvidia-topic-control` | no change needed, if the endpoint returns logprobs (NIM does) | it is now an alias for `type: openai` with NemoGuard topic control's model, keeping its 20s call timeout; `topic_policy` records the label's probability as `confidence` |
| Point `type: openai` at a non-OpenAI `base_url` without a `credential` | no key is sent (`OPENAI_API_KEY` is only sent to OpenAI) | add an explicit `credential` for that host |

`topic_policy` now sends every backend NemoGuard topic control's trained prompt
and asks for `on-topic` or `off-topic`, allowing whichever gets the higher
probability.

### Guardrail checks are bounded, and a timed-out guardrail returns its message

A guardrail's check is now bounded by a timeout (default 30s,
`evals.DefaultEvalTimeout`). A check that exceeds it (or whose classifier
errors) is enforced with the validator's `message`, where previously an
output guardrail could release the unchecked response and a timed-out input
guardrail returned empty text.

| If you | You will see | Change |
|---|---|---|
| Run a guardrail whose judge regularly takes over 30s, having raised `WithIdleTimeout` for it | the turn is blocked with the validator's message at 30s | raise the bound with `sdk.WithGuardrailTimeout(d)` |
| Declare a guardrail with `guardrails.InputFunc` / `OutputFunc` whose function can run past the bound | the turn is blocked with the default blocked message, `reason: timeout` | return sooner, or raise the bound with `sdk.WithGuardrailTimeout(d)` — it applies to func guardrails too |

A func guardrail's function receives a context carrying the deadline. One
that ignores it keeps running in the background after the turn moves on; its
result is discarded and it can no longer change the response.

Inference calls also emit `inference_requests_total`,
`inference_request_duration_seconds`, `inference_input_tokens_total` and
`inference_cost_total` (labels `provider`, `model` and `source`;
`inference_requests_total` also carries `status`).

Provider HTTP retries (every LLM and inference provider) are counted in
`provider_retries_total{provider, outcome}` (under the metrics collector's namespace): `retry` per retried
attempt, `success` when a call recovers after retrying, `exhausted` when
every attempt fails. A backend that only answers on its second try is now
visible. The OpenAI inference provider's retries can be configured with
`openai.Config.RetryPolicy`.

### Embedding providers no longer guess a model's vector size

`EmbeddingDimensions()` used to answer with a fixed default for any model a
provider did not recognize (1536 for `openai`, 3072 for `gemini`, 768 for
`ollama`, 1024 for `voyageai`), so a self-hosted model reached as `type: openai`
reported 1536 while returning 768-length vectors. A declared `dimensions` was
also ignored by `openai` and `gemini`, and never sent by `ollama` or Bedrock
Titan v2.

Now the size is the declared `dimensions`, else the known size of the model,
else the length of the first vector returned. A declared size is sent to the
API, and every response is checked against the reported size.

| If you | You will see | Change |
|---|---|---|
| Call `EmbeddingDimensions()` before any `Embed` for a model the provider does not know | `0` | set `dimensions`, or embed one text first and read the size then |
| Declare a `dimensions` the model cannot produce | `Embed` errors, naming both sizes (or the API rejects the field, e.g. `text-embedding-ada-002`) | set it to the model's real size, or remove it |
| Rely on a declared `dimensions` for `openai`/`gemini` that was previously ignored | vectors now come back at that size | size existing vector storage to match, or remove the setting |

## v2.6.0

### `tools_offered` now answers for the turn, not the conversation

`tools_offered` is documented as checking what a turn handed the provider, and
it reported the union over the whole conversation. The pipeline never reset its
record between turns, and the eval context extracted over the full history.
Both are fixed. The set a check sees is the tools offered across the current
turn's rounds. A skill grant that widens the set mid-turn counts; a grant from
three turns earlier does not.

| If you | You will see | Change |
|---|---|---|
| Assert `tool_names: [refund]` | passes only if this turn offered `refund`; it used to pass when `refund` was offered in any earlier turn | check that the grant is active on the turn being asserted |
| Assert `tool_names: [refund], absent: true` | fails when this turn offers `refund`; it could never fail once `refund` had been offered once | check that the grant is not active on the turn being asserted |

### Voice (VAD) sessions now run guardrails

A pack's `validators:` did nothing in a VAD voice session. Guardrails are
provider hooks, and the VAD pipeline built its provider stage with a nil hook
registry, so every hook path returned early: no `validation.started` /
`passed` / `failed` events, and no enforcement. Guardrails now run in VAD mode
(`OpenVoice` and the VAD topology) exactly as they do in text. Native realtime
and duplex sessions are a separate stage that had no hook support in this
release, so guardrails did not run there until v2.18.0.

| If you | You will see | Change |
|---|---|---|
| Run a voice conversation under a pack with `validators:` | the conversation is blocked where the validators say so, and `validation.*` events appear | review what the validators do to the voice path before upgrading; they were never exercised there |
| Run native realtime or duplex sessions | no change in this release; guardrails run there from v2.18.0 | none |

### Provider adapters now rewrite your JSON Schema for the vendor

A schema written for a pack went to the wire as written, and the three
providers enforce rules that contradict each other:

| | Anthropic | Gemini | OpenAI (strict) |
|---|---|---|---|
| `additionalProperties: false` on every object | required | rejected | required |
| every property in `required` | not required | n/a | required |
| `minimum` / `maxItems` / `uniqueItems` / … | rejected | accepted | accepted |

No schema was portable, and a valid one failed on whichever provider had not
been tried. The adapters now rewrite the schema on the way out, for output
schemas and tool schemas alike. On Anthropic, constraints its grammar cannot
carry (`minimum`, `maxItems`, `uniqueItems`, …) are removed from the wire and
restated in the node's `description`, so the model still sees them. Tool
arguments stay validated against the schema as written; the rewrite never
touches the descriptor.

| If you | You will see | Change |
|---|---|---|
| Wrote a schema that a provider rejected with a 400 naming a JSON path | the schema works | none |
| Use constraints such as `minimum` or `maxItems` with Anthropic | the constraints restated in the node's `description`, not on the wire | none |
| Use the `not` keyword | the request still fails, with an error naming the keyword; it has no equivalent in the accepted subset | remove the keyword |

### Claude tools use Anthropic's native strict tool use

Tool definitions carry `strict: true`, so the API constrains decoding to the
schema and `tool_use.input` is guaranteed to validate.

| If you | You will see | Change |
|---|---|---|
| Define tools for Claude | better-formed tool arguments, and the schema adapted as above, because strict mode enforces the same rules | none |
| Need a schema sent exactly as written | the adapted schema | set `strict_tools: false` per provider, as below |

```yaml
providers:
  - id: claude
    type: claude
    model: claude-sonnet-5
    additional_config:
      strict_tools: false
```

## v2.5.0

### Checks name the provider they need, and the host binds it

Checks that need a model they do not own (a judge for `toxicity`, a classifier
for `text_sentiment`) name it by a logical key the pack declares in `requires`.
The host binds that name to a concrete provider and can rebind it without the
pack changing.

```yaml
requires:
  providers:
    - key: grader          # a name this pack invented
      role: llm
      description: grades the toxicity guardrail
      required: true

prompts:
  chat:
    validators:
      - type: toxicity
        params:
          provider: grader
```

```go
conv, _ := sdk.Open("./app.pack.json", "chat",
    sdk.WithProvider(agent),
    sdk.WithNamedProvider(sdk.ProviderSpec{
        ID: "grader", Type: "openai", Model: "gpt-4.1-mini",
    }),
)
```

| If you | You will see | Change |
|---|---|---|
| Use `classifier_id` in a pack | the check errors, naming the replacement | rename it to `provider`, and declare that name in `requires` |
| Declare a judge-backed check (`toxicity`, `bias`, `pii_leakage`, `role_violation`, `llm_judge`, the RAG primitives) that names no provider | `Open()` fails | add `params.provider` naming a key from `requires`, or pass `sdk.WithJudgeProvider` as a host default |
| Bind an ancillary provider with `sdk.WithLLMProvider` | it silently became your agent | use `sdk.WithNamedProvider`, which registers it for a pack to name without touching the agent |
| Run `sdk.Evaluate` on a pack whose checks name providers | the checks error | pass `EvaluateOpts.ProviderBinding` (or `JudgeTargets`, which is treated as one) |

A judge-backed guardrail with no judge used to build and then block every turn,
reporting a content violation as the reason, or, for `pii_leakage`, run only its
regex layer. Both now fail at `Open()`, naming the check and whether the fix
belongs in the pack or in the host wiring. A check that names a provider it
cannot get is an error, because a skip scores 1.0 and passes.

## v2.4.0

### A tool call slower than 30s no longer kills its own turn

No action needed. The pipeline's idle timer is held open while tools run, so a
slow tool is bounded by its own `TimeoutMs` rather than by `IdleTimeout`. If you
raised `IdleTimeout` to work around this, you can put it back.

### A2A `message/send` keeps caller context values

No action needed. Values your HTTP middleware puts on the request context
(identity, tenant, request-scoped config) now reach the conversation on
`message/send` as they always did on `message/stream`. If you worked around this
by keying state on `contextID`, that workaround can go.
