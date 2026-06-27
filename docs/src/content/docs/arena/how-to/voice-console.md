---
title: Use the Voice Console
---
Run a live, hands-free voice conversation with an Arena agent from the terminal.

## Prerequisites

- The standard `promptarena` binary (voice is built in — no special build needed).
- **PortAudio** installed on the host machine (see [Install PortAudio](#install-portaudio)).
- A pair of **headphones** — the mic stays open the whole session; speaker audio feeds
  straight back into the mic without them, causing echo loops.
- An Arena config with at least one provider and one agent declared.

## Getting the Binary

Voice ships **inside the standard `promptarena` binary** — there is no separate
voice build. The binary stays pure Go (no cgo); it loads PortAudio dynamically
**at runtime**, and only when you actually start a voice session. So you install
`promptarena` the usual way (`npm install -g @altairalabs/promptarena`, a release
download, or `make build-arena`) and then [install PortAudio](#install-portaudio)
to enable voice.

If you run `chat --voice` without PortAudio present, only voice is unavailable —
the command prints a clear install hint and every other feature keeps working.

## Install PortAudio

PortAudio must be present on the machine **at runtime** — the binary loads it on
demand when a voice session starts.

| Platform | Command |
|----------|---------|
| macOS | `brew install portaudio` |
| Debian / Ubuntu | `sudo apt install libportaudio2` |
| Fedora / RHEL | `sudo dnf install portaudio` |
| Windows | place `portaudio.dll` on your `PATH` |

## Start a Voice Session

```bash
promptarena chat --voice --config config.arena.yaml
```

The console opens in full-screen TUI mode. Speak naturally — the agent responds
with synthesized audio. Press `q` or `Ctrl-C` to exit.

## Mode Selection: ASM vs VAD

How the system detects the end of each turn depends on the provider type.

### ASM mode (realtime providers — default)

Providers such as OpenAI Realtime and Gemini Live handle turn detection natively
inside the connection. The voice console uses **ASM** (Automatic Speech Mode) for
these providers: it streams raw PCM from the microphone directly into the provider
connection, and the provider signals when a turn is complete.

No extra flags are needed — ASM is selected automatically for any provider that
supports stream input (`audio_enabled: true` / `response_modalities: [AUDIO]`).

### VAD mode (text / REST providers)

For standard chat-completion providers that do not support streaming audio input,
the voice console falls back to client-side **VAD** (Voice Activity Detection).
The mic is recorded locally until silence is detected, then the audio is transcribed
via an STT provider and sent as a text turn. TTS converts the agent's reply back
to audio for playback.

Supply the STT provider id and an optional TTS voice id:

```bash
promptarena chat --voice \
  --voice-stt openai-whisper \
  --voice-output-voice alloy \
  --config config.arena.yaml
```

`--voice-stt` must match a provider declared under `stt_providers:` in the Arena
config. `--voice-output-voice` must match a voice id declared under `voices:`. The
value is a **voice binding id** (the `id:` field in the `voices:` list), not a raw
vendor voice name — the binding maps the id to the actual synthesis voice for the
configured provider, so set the binding rather than a vendor-specific name unless the
two happen to coincide.

## Echo Guard

When headphones are not available (e.g., laptop speakers), enable `--echo-guard`
to gate the microphone while the agent is speaking:

```bash
promptarena chat --voice --echo-guard --config config.arena.yaml
```

Echo guard is **off by default** because it adds a small latency and is unnecessary
when headphones are used. It is best-effort in v1: it suppresses capture during
playback but does not perform full acoustic echo cancellation.

**Recommendation**: always prefer headphones over echo guard for the lowest latency
and cleanest transcription.

## Full Flag Reference

| Flag | Default | Description |
|------|---------|-------------|
| `--voice` | `false` | Enable hands-free voice mode (requires PortAudio installed) |
| `--voice-stt <id>` | — | STT provider id for VAD mode (text provider path) |
| `--voice-output-voice <id>` | — | TTS voice id the agent speaks in (VAD mode) |
| `--echo-guard` | `false` | Gate mic while agent speaks (best-effort; use headphones instead) |
| `--config <path>` | `config.arena.yaml` | Path to the Arena config file |

## See Also

- [Set Up Voice Testing with Self-Play](/arena/how-to/setup-voice-testing/) — automated voice testing without a live mic
- [Configure Providers](/arena/how-to/configure-providers/) — declare TTS and STT providers
- [Install PromptArena](/arena/how-to/installation/) — standard (non-voice) installation
