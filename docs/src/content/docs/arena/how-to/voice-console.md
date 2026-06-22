---
title: Use the Voice Console
---
Run a live, hands-free voice conversation with an Arena agent from the terminal.

## Prerequisites

- A voice-enabled `promptarena-voice` binary (see [Getting the Binary](#getting-the-binary)).
- PortAudio installed on the host machine (see [Install PortAudio](#install-portaudio)).
- A pair of **headphones** — the mic stays open the whole session; speaker audio feeds
  straight back into the mic without them, causing echo loops.
- An Arena config with at least one provider and one agent declared.

## Getting the Binary

The standard `promptarena` binary is pure Go and does not include audio support.
Voice requires a separate binary compiled with `-tags voice` and `CGO_ENABLED=1`.

### Option 1: Download a pre-built voice binary

Each release publishes `promptarena-voice-<OS>-<arch>` archives alongside the
standard binaries on the [PromptKit Releases](https://github.com/AltairaLabs/PromptKit/releases)
page. Download the archive for your platform and put the binary on your `PATH`:

```bash
# macOS (Apple Silicon) — adjust version and arch as needed
curl -LO https://github.com/AltairaLabs/PromptKit/releases/latest/download/promptarena-voice_Darwin_arm64.tar.gz
tar -xf promptarena-voice_Darwin_arm64.tar.gz
sudo mv promptarena-voice /usr/local/bin/
```

### Option 2: Build from source

```bash
# macOS
brew install portaudio
make build-arena-voice

# Linux (Debian/Ubuntu)
sudo apt-get install -y portaudio19-dev
make build-arena-voice

# Output: bin/promptarena-voice
```

The `make build-arena-voice` target sets `CGO_ENABLED=1 -tags voice` automatically.

## Install PortAudio

PortAudio must be present on the machine **at runtime** (not just at build time) because
the voice binary links against it dynamically.

| Platform | Command |
|----------|---------|
| macOS | `brew install portaudio` |
| Debian / Ubuntu | `sudo apt-get install -y portaudio19-dev` |
| Fedora / RHEL | `sudo dnf install -y portaudio-devel` |
| Windows | `vcpkg install portaudio:x64-windows` |

## Start a Voice Session

```bash
promptarena-voice chat --voice --config config.arena.yaml
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
promptarena-voice chat --voice \
  --voice-stt openai-whisper \
  --voice-output-voice alloy \
  --config config.arena.yaml
```

`--voice-stt` must match a provider declared under `stt_providers:` in the Arena
config. `--voice-output-voice` must match a voice id declared under `voices:`.

## Echo Guard

When headphones are not available (e.g., laptop speakers), enable `--echo-guard`
to gate the microphone while the agent is speaking:

```bash
promptarena-voice chat --voice --echo-guard --config config.arena.yaml
```

Echo guard is **off by default** because it adds a small latency and is unnecessary
when headphones are used. It is best-effort in v1: it suppresses capture during
playback but does not perform full acoustic echo cancellation.

**Recommendation**: always prefer headphones over echo guard for the lowest latency
and cleanest transcription.

## Full Flag Reference

| Flag | Default | Description |
|------|---------|-------------|
| `--voice` | `false` | Enable hands-free voice mode (requires a `-tags voice` binary) |
| `--voice-stt <id>` | — | STT provider id for VAD mode (text provider path) |
| `--voice-output-voice <id>` | — | TTS voice id the agent speaks in (VAD mode) |
| `--echo-guard` | `false` | Gate mic while agent speaks (best-effort; use headphones instead) |
| `--config <path>` | `config.arena.yaml` | Path to the Arena config file |

## See Also

- [Set Up Voice Testing with Self-Play](/arena/how-to/setup-voice-testing/) — automated voice testing without a live mic
- [Configure Providers](/arena/how-to/configure-providers/) — declare TTS and STT providers
- [Install PromptArena](/arena/how-to/installation/) — standard (non-voice) installation
