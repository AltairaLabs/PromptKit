---
title: Configure TTS / STT Providers Declaratively
description: Configure speech synthesis and transcription providers from RuntimeConfig YAML
sidebar:
  order: 20
---

The chat-provider and embedding-provider declarative pattern extends to text-to-speech and speech-to-text. Voice-mode applications no longer need to hard-code provider construction in Go.

## Quick Start

Declare them under `spec.providers` with `role: tts` / `role: stt`:

```yaml
spec:
  providers:
    - id: voice
      role: tts
      type: elevenlabs
      model: eleven_turbo_v2
      credential:
        credential_env: ELEVEN_API_KEY
    - id: cart
      role: tts
      type: cartesia
      credential:
        credential_env: CARTESIA_API_KEY
      additional_config:
        ws_url: wss://api.cartesia.ai/tts/websocket

    - id: whisper
      role: stt
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

## The `tts_providers:` / `stt_providers:` blocks

Before role routing, each capability had its own top-level block:

```yaml
spec:
  tts_providers:
    - id: voice
      type: elevenlabs
      model: eleven_turbo_v2
      credential:
        credential_env: ELEVEN_API_KEY

  stt_providers:
    - id: whisper
      type: openai
      model: whisper-1
      credential:
        credential_env: OPENAI_API_KEY
```

Both still work and behave identically. They are deprecated in favor of
`role: tts` / `role: stt` and are removed in v3. Declaring the same ID in both
spellings is rejected rather than silently resolved.

## Supported Types

| Role | `type` value | Underlying package |
|---|---|---|
| `tts` | `openai` | `runtime/tts` (OpenAI TTS) |
| `tts` | `elevenlabs` | `runtime/tts` (ElevenLabs) |
| `tts` | `cartesia` | `runtime/tts` (Cartesia) |
| `stt` | `openai` | `runtime/stt` (OpenAI Whisper) |

`additional_config` honored extras:

- **Cartesia** — `ws_url` (string) for the websocket streaming endpoint.

## Programmatic Path Still Works

`WithTTS(service)` and `WithVADMode(stt, tts, ...)` are unchanged. When set, they win over the YAML default — same precedence rule as chat and embedding providers.

## Validation

`LoadRuntimeConfig` rejects:

- Missing `type`.
- An unknown `role`.
- Two entries with the same effective ID *within a role* (explicit ID, or `type`
  when ID is omitted). The same ID under two different roles is allowed, since
  the slots are separate.

`model` is optional for `role: tts` and `role: stt` — it overrides the
provider's own default.

An unsupported `type` is caught when the provider is constructed, not by a
hardcoded list in the config loader, so the error names the type and the
registered alternatives. The deprecated `tts_providers:` / `stt_providers:`
blocks additionally check `type` against a fixed allowlist at load time.

## Adding a New Provider

The factory pattern is open: a per-provider package can self-register via `init()`:

```go
package mytts

import "github.com/AltairaLabs/PromptKit/runtime/v2/tts"

func init() {
    tts.RegisterFactory("my_tts", func(spec tts.ProviderSpec) (tts.Service, error) {
        return NewMyTTS(spec.Model, tts.APIKeyFromCredential(spec.Credential)), nil
    })
}
```

Side-effect-import the package from your application and the new `type:` value works in YAML immediately.

## Related

- [Declarative Embedding Providers](/sdk/how-to/providers/declarative-embedding-providers/)
- [Use a RuntimeConfig](/sdk/how-to/conversations/use-runtime-config/)
