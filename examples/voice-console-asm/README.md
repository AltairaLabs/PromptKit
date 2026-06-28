# Voice Console — ASM (native realtime) mode

> **Status: experimental** — voice runs inside the interactive hub console
> (`promptarena chat --voice`). ASM (native realtime) is the working path and
> is what this example exercises. Composed VAD (voice over text agents) is
> experimental; turn-by-turn conversation and barge-in are tracked in issue
> [#1469](https://github.com/AltairaLabs/PromptKit/issues/1469) and not yet
> complete.

Talk to a native-realtime agent (OpenAI Realtime) by voice from the terminal.
The provider does STT + LLM + TTS + turn detection server-side, so no STT/TTS
config is needed.

## Run

```bash
# Voice is built into promptarena; install PortAudio to use it (see voice docs)

export OPENAI_API_KEY=sk-...

# Run from this directory
../../bin/promptarena chat --voice --config config.arena.yaml
```

Speak naturally; the agent replies in voice. Press `q` or `Ctrl-C` to exit.

## Requirements

- PortAudio installed at runtime (`brew install portaudio` on macOS).
- `OPENAI_API_KEY` (the agent uses `gpt-realtime`).
- **Headphones** — the mic stays open the whole session; speaker audio feeds
  back into the mic without them. For laptop speakers add `--echo-guard`
  (best-effort).

## Barge-in

`--barge-in` now stops in-flight speaker playback the moment you interrupt: the
audio sink is flushed so the agent goes quiet immediately instead of finishing
its buffered sentence ([#1485](https://github.com/AltairaLabs/PromptKit/issues/1485)).

Genuine open-speaker barge-in (talking over the agent without headphones) still
needs acoustic echo cancellation so the open mic doesn't hear the agent and
interrupt it. AEC is in progress — Speex
([#1506](https://github.com/AltairaLabs/PromptKit/issues/1506)) and WebRTC AEC3
([#1507](https://github.com/AltairaLabs/PromptKit/issues/1507)) — so until it
lands, **use headphones for reliable barge-in**.

### Same-device requirement (duplex / AEC)

Low-latency duplex audio and (later) AEC need the microphone and speaker to be
the **same device** sharing one clock — e.g. a single headset or the built-in
mic+speakers. When capture and playback are different devices with no shared
clock, the single duplex stream can't open, so the console automatically
degrades to **half-duplex** (separate mic and speaker streams). Voice still
works, but with reduced quality: no open-speaker barge-in and no AEC. A warning
is logged when this fallback happens. For the best experience, select the same
device for both input and output.

## How it works

`openai-realtime` is the only LLM provider, so the console selects it and
detects that it supports streaming audio input → **ASM mode**: raw mic PCM is
streamed into the connection and the provider signals end-of-turn. Transcripts
and any tool calls appear in the conversation panel exactly as for a text turn.
