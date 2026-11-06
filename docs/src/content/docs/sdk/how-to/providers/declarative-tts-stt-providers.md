---
title: Configure TTS / STT Providers Declaratively
description: Declare speech synthesis and transcription providers in RuntimeConfig YAML
sidebar:
  order: 20
verified:
  commit: 8489bf3d95c7498f9de0f024769db9399c24e884
  sources:
    - pkg/config/role.go
    - pkg/config/runtime_config.go
    - runtime/credentials/resolver.go
    - runtime/providers/base/factory.go
    - runtime/stt/openai_register.go
    - runtime/tts/cartesia_register.go
    - runtime/tts/elevenlabs_register.go
    - runtime/tts/factory.go
    - runtime/tts/openai_register.go
    - sdk/options.go
    - sdk/provider_file.go
    - sdk/provider_introspection.go
    - sdk/runtime_config.go
---

Declare text-to-speech and speech-to-text providers in a `RuntimeConfig` file so a voice application does not construct them in Go.

## Prerequisites

- A pack and a RuntimeConfig file loaded with `WithRuntimeConfig` (see [Use a RuntimeConfig](/sdk/how-to/conversations/use-runtime-config/)).
- The provider API keys in environment variables.

## Steps

### 1. Declare the providers

Add entries to `spec.providers` with `role: tts` or `role: stt`:

```yaml
apiVersion: promptkit.altairalabs.ai/v1alpha1
kind: RuntimeConfig
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

`model` is optional and overrides the provider's own default.

### 2. Choose a type

| Role | `type` | Provider |
|---|---|---|
| `tts` | `openai` | OpenAI TTS |
| `tts` | `elevenlabs` | ElevenLabs |
| `tts` | `cartesia` | Cartesia |
| `stt` | `openai` | OpenAI Whisper |

Cartesia reads `additional_config.ws_url` as its websocket streaming endpoint.

### 3. Open the conversation

```go
conv, err := sdk.Open("./pack.json", "chat",
    sdk.WithRuntimeConfig("./runtime.yaml"),
)
```

The first declared `tts` entry becomes the conversation's `ttsService`, and the first `stt` entry its `sttService`. `WithTTS` and `WithVADMode(stt, tts, ...)` take precedence: when either has set a service, the declared one does not replace it.

### 4. Add a provider type (optional)

Register a factory from an `init()` function to make a new `type` value work in YAML:

```go
package mytts

import "github.com/AltairaLabs/PromptKit/runtime/v2/tts"

func init() {
    tts.RegisterFactory("my_tts", func(spec tts.ProviderSpec) (tts.Service, error) {
        return NewMyTTS(spec.Model, tts.APIKeyFromCredential(spec.Credential)), nil
    })
}
```

Side-effect import the package from your application. `stt.RegisterFactory` works the same way for speech-to-text.

## Verify

`ValidateOptions` applies the options without opening a conversation, so it builds every declared provider:

```go
err := sdk.ValidateOptions(sdk.WithRuntimeConfig("./runtime.yaml"))
```

A `nil` result means every provider builds. It resolves credentials but does not call the provider, so a wrong key still passes. `sdk.RegisteredProviderTypes()["tts"]` and `["stt"]` list the types your binary can build.

## Troubleshoot

| Error | Cause |
|---|---|
| `unsupported provider type: <type>` | The `type` has no registered factory for its role. `LoadRuntimeConfig` does not check it; the error appears when `Open` or `ValidateOptions` applies the option. |
| `duplicate provider id within role` | Two entries in one role share an effective ID (the `id`, or the `type` when `id` is empty). The same ID under two roles is allowed. |
| `provider type is required` | An entry has no `type`. |
| `TTS provider "<id>": duplicate ID` | The same ID appears under `role: tts` and in the top-level `tts_providers:` block. `stt_providers:` behaves the same. |

`LoadRuntimeConfig` also rejects an unknown `role`.

The top-level `tts_providers:` and `stt_providers:` blocks take the same entries without `role` and behave the same. They are deprecated and removed in v3; move their entries to `spec.providers`. Unlike `role` entries, they check `type` against a fixed list at load time.

## Related

- [Use a RuntimeConfig](/sdk/how-to/conversations/use-runtime-config/)
- [Declarative embedding providers](/sdk/how-to/providers/declarative-embedding-providers/)
