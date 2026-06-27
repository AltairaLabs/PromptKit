# Voice Console — VAD (composed) mode

> **Status: experimental** — voice runs inside the interactive hub console
> (`promptarena chat --voice`). Composed VAD (voice over text agents) is
> experimental: speech detection and transcription work, but turn-by-turn
> conversation and barge-in are tracked in issue
> [#1469](https://github.com/AltairaLabs/PromptKit/issues/1469) and not yet
> complete. For a fully working voice path, see the ASM example.

Talk to a plain **text** agent (gpt-4o here; swap for Claude, etc.) by voice.
The console records the mic until silence (voice-activity detection),
transcribes it (STT), sends a normal text turn, then speaks the reply (TTS).
Evals, guardrails, and conversation history work exactly as for typed text.

## Run

```bash
# Voice is built into promptarena; install PortAudio to use it (see voice docs)

export OPENAI_API_KEY=sk-...   # used by the agent, STT (whisper) and TTS

# Run from this directory
../../bin/promptarena chat --voice \
  --voice-stt openai-stt \
  --voice-output-voice agent-voice \
  --config config.arena.yaml
```

Speak naturally; pause to let VAD end your turn. Press `q` or `Ctrl-C` to exit.

## Requirements

- PortAudio installed at runtime (`brew install portaudio` on macOS).
- `OPENAI_API_KEY` — the agent (`gpt-4o`), STT (`whisper-1`), and TTS all use it.
  To use a Claude agent instead, swap `providers/openai-gpt4o.provider.yaml` for a
  `type: claude` provider and export `ANTHROPIC_API_KEY` (STT/TTS still need `OPENAI_API_KEY`).
- **Headphones** — the mic stays open the whole session. For laptop speakers add
  `--echo-guard` (best-effort; headphones are cleaner).

## Flags

- `--voice-stt openai-stt` — the `role: stt` provider id used to transcribe the mic.
- `--voice-output-voice agent-voice` — a `voices:` binding **id** (not a raw vendor
  voice name); the binding maps it to the `openai-tts` provider, which speaks in `alloy`.

## How it works

`openai-gpt4o` is the only `role: llm` provider, so the console selects it and —
because it does not support streaming audio — uses **VAD mode**:
`mic → VAD → STT → text turn → TTS → speaker`. The `openai-stt` (role stt) and
`openai-tts` (role tts) providers are routed by their roles, not treated as agents.
