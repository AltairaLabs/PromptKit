---
title: Pre-warm guardrail message clips
description: Render a pack's guardrail messages into the cue clip cache at startup or deploy, so realtime sessions speak them with no TTS call.
sidebar:
  order: 6
---
Render a pack's guardrail messages before any session needs them, so a realtime voice session speaks a blocked reply's message at once and makes no TTS call for it.

## Prerequisites

- A realtime voice conversation (`sdk.OpenDuplex` or `sdk.OpenVoice` in ASM mode) whose guardrail messages are spoken, as described in [Choose a voice mode](/sdk/how-to/voice/choose-voice-mode/).
- A `tts.Service` that renders PCM (see [Configure text-to-speech](/sdk/how-to/voice/configure-tts/)).

## Step 1: Pre-warm the pack's messages

Call `sdk.PrewarmCueClips` with the pack, the TTS service and the session's voice name:

```go
import (
	"context"
	"log"
	"os"

	"github.com/AltairaLabs/PromptKit/runtime/v2/tts"
	"github.com/AltairaLabs/PromptKit/sdk/v2"
)

func warm(ctx context.Context) {
	speech := tts.NewOpenAI(os.Getenv("OPENAI_API_KEY"))
	if err := sdk.PrewarmCueClips(ctx, "./assistant.pack.json", speech, "alloy"); err != nil {
		log.Printf("pre-warm: %v", err)
	}
}
```

It renders every prompt's validator messages, the messages of guardrails you pass with `sdk.WithGuardrail`, and the default blocked message. Each renders once, as PCM16 mono at 24 kHz, the rate OpenAI Realtime and Gemini Live speak at. The voice is matched to the service's voices the way a session matches its own voice: by ID or name, ignoring case. Pre-warm one TTS service per voice: a message already pre-warmed for the voice with another service's audio is refused with `stage.ErrVoicePrewarmedByAnotherService`.

A message the service fails to render is named in the returned error, and the other messages are still cached. A failed pre-warm does not stop a session from working: a message not in the cache is rendered when the session starts.

`PrewarmCueClips` takes the same options as `sdk.Open`. Pass the ones that shape the guardrails (`sdk.WithEvalRegistry`, `sdk.WithGuardrail`, `sdk.WithJudgeProvider`, `sdk.WithSkipSchemaValidation`) as you pass them to `Open`, so the same messages are rendered.

## Step 2: Open conversations on the same cache

Without `sdk.WithCueClipCache`, both the pre-warm and every conversation use the process-wide in-memory cache, so a conversation opened afterwards in the same process plays the pre-warmed clips:

```go
conv, err := sdk.OpenDuplex("./assistant.pack.json", "chat",
	sdk.WithProvider(realtime), sdk.WithTTS(speech))
```

To share clips across processes, pass the same `stage.ClipCache` to the pre-warm and to each conversation with `sdk.WithCueClipCache`.

A clip plays for a session whose voice and output rate match what was pre-warmed. A session with a TTS service plays it when the service is the one that rendered it.

On OpenAI Realtime the session speaks guardrail messages itself, outside the conversation and in its exact voice, and that source comes before the TTS service and the pre-warmed clips. An OpenAI Realtime session plays a pre-warmed clip only for a message its own audio failed to render. Pre-warming pays off on providers without their own out-of-band audio, such as Gemini Live.

## Step 3 (optional): Fill a directory offline

`stage.NewDirClipCache` keeps clips as files, so you can render them once and ship or check in the directory. Pre-warm into a writable directory:

```go
cache, err := stage.NewDirClipCache("./cues")
if err != nil {
	log.Fatal(err)
}
err = sdk.PrewarmCueClips(ctx, "./assistant.pack.json", speech, "alloy", sdk.WithCueClipCache(cache))
```

Open conversations on it read-only. A session with no TTS service configured plays the clips pre-warmed for its voice and makes no TTS call. A session whose TTS service renders no PCM does not use them. When the pre-warming service has no voice matching the one you name, it renders in its default voice and those clips are not kept for sessions without a TTS service; a warning names the voice:

```go
cues, err := stage.NewDirClipCache("./cues", stage.DirCacheReadOnly())
if err != nil {
	log.Fatal(err)
}
conv, err := sdk.OpenDuplex("./assistant.pack.json", "chat",
	sdk.WithProvider(realtime), sdk.WithCueClipCache(cues))
```

A message the directory does not hold plays the earcon, a short tone.

## Verify

Count your TTS service's calls (wrap its `Synthesize`). After the pre-warm, open a conversation, trigger a guardrail, and read `Response()`: the chunks whose `Metadata["cue"]` is `guardrail_message` carry the pre-warmed audio, and the service is not called.

## Troubleshooting

- `the clip source is session-bound`: you passed a nil service or a live session. Only a TTS service can be pre-warmed; audio a provider renders inside its own session cannot.
- `renders no PCM`: the service lists no `pcm` format. Use a service that does.
- The session still calls the TTS service: its voice or output rate differs from the pre-warm's. Pre-warm with the voice name the session uses.

## Related

- [Choose a voice mode](/sdk/how-to/voice/choose-voice-mode/)
- [Configure text-to-speech](/sdk/how-to/voice/configure-tts/)
- [`stage` reference](/runtime/reference/pipeline/)
