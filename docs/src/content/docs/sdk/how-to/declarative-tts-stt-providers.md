---
title: Declarative TTS / STT Providers
description: Configure speech synthesis and transcription providers from RuntimeConfig YAML
sidebar:
  order: 20
---

The chat-provider and embedding-provider declarative pattern (#979) extends to text-to-speech and speech-to-text. Voice-mode applications no longer need to hard-code provider construction in Go.

## Quick Start

```yaml
spec:
  tts_providers:
    - id: voice
      type: elevenlabs
      model: eleven_turbo_v2
      credential:
        credential_env: ELEVEN_API_KEY
    - id: cart
      type: cartesia
      credential:
        credential_env: CARTESIA_API_KEY
      additional_config:
        ws_url: wss://api.cartesia.ai/tts/websocket

  stt_providers:
    - id: whisper
      type: openai
      model: whisper-1
      credential:
        credential_env: OPENAI_API_KEY
```

```go
conv, _ := sdk.Open("./pack.json", "chat",
    sdk.WithRuntimeConfig("./runtime.yaml"),
)
```

The first declared TTS entry becomes the default `ttsService` unless `WithTTS` (or `WithVADMode`) wired one programmatically. Same for STT and `sttService`.

## Supported Types

| Block | `type` value | Underlying package |
|---|---|---|
| `tts_providers` | `openai` | `runtime/tts` (OpenAI TTS) |
| `tts_providers` | `elevenlabs` | `runtime/tts` (ElevenLabs) |
| `tts_providers` | `cartesia` | `runtime/tts` (Cartesia) |
| `stt_providers` | `openai` | `runtime/stt` (OpenAI Whisper) |

`additional_config` honored extras:

- **Cartesia** — `ws_url` (string) for the websocket streaming endpoint.

## Programmatic Path Still Works

`WithTTS(service)` and `WithVADMode(stt, tts, ...)` are unchanged. When set, they win over the YAML default — same precedence rule as chat and embedding providers.

## Validation

`LoadRuntimeConfig` rejects:

- Missing `type`.
- A `type` outside the supported set.
- Two entries with the same effective ID (explicit ID, or `type` when ID is omitted).

## Adding a New Provider

The factory pattern is open: a per-provider package can self-register via `init()`:

```go
package mytts

import "github.com/AltairaLabs/PromptKit/runtime/tts"

func init() {
    tts.RegisterFactory("my_tts", func(spec tts.ProviderSpec) (tts.Service, error) {
        return NewMyTTS(spec.Model, tts.APIKeyFromCredential(spec.Credential)), nil
    })
}
```

Side-effect-import the package from your application and the new `type:` value works in YAML immediately.

## Related

- [Declarative Embedding Providers](/sdk/how-to/declarative-embedding-providers/)
- [Use a RuntimeConfig](/sdk/how-to/use-runtime-config/)
