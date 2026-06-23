# Voice Console — ASM (native realtime) mode

Talk to a native-realtime agent (OpenAI Realtime) by voice from the terminal.
The provider does STT + LLM + TTS + turn detection server-side, so no STT/TTS
config is needed.

## Run

```bash
# Build the voice binary (needs PortAudio — see the voice docs)
make build-arena-voice

export OPENAI_API_KEY=sk-...

# Run from this directory
../../bin/promptarena-voice chat --voice --config config.arena.yaml
```

Speak naturally; the agent replies in voice. Press `q` or `Ctrl-C` to exit.

## Requirements

- A `-tags voice` binary (`make build-arena-voice` or a `promptarena-voice-*` release binary).
- PortAudio installed at runtime (`brew install portaudio` on macOS).
- `OPENAI_API_KEY` (the agent uses `gpt-realtime`).
- **Headphones** — the mic stays open the whole session; speaker audio feeds
  back into the mic without them. For laptop speakers add `--echo-guard`
  (best-effort).

## How it works

`openai-realtime` is the only LLM provider, so the console selects it and
detects that it supports streaming audio input → **ASM mode**: raw mic PCM is
streamed into the connection and the provider signals end-of-turn. Transcripts
and any tool calls appear in the conversation panel exactly as for a text turn.
