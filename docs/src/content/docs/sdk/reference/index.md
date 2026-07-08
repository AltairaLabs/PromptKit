---
title: SDK Reference
sidebar:
  order: 0
---
Reference documentation for the PromptKit SDK.

The SDK is a pack-first API: open a conversation from a pack file, send messages,
and register tools and hooks. New here? Start with the
[Tutorials](/sdk/tutorials/), or the [How-To Guides](/sdk/how-to/) for
task-focused recipes.

## SDK API

The complete `sdk` package — `Open`, the `Conversation` type (`Send`, `Stream`,
`OnTool`, `Fork`, `EventBus`, …), `Response`, `StreamChunk`, every `With*`
option, and the exported error values — is generated from source, so it never
drifts from the code:

- **[Conversation &amp; SDK API](/sdk/reference/conversation-manager/)** — the full generated `sdk` package reference.

## Integrations

- **[A2A Server](/sdk/reference/a2a-server/)** — `A2AServer`, task store, and conversation opener (`server/a2a`).
- **[AG-UI Integration](/sdk/reference/ag-ui/)** — converters between PromptKit and AG-UI SDK types (`sdk/agui`).

## Configuration &amp; Protocols

- **[RuntimeConfig](/sdk/reference/runtime-config/)** — YAML config schema for exec tools, hooks, sandboxes, and the state store.
- **[Exec Protocol](/sdk/reference/exec-protocol/)** — wire protocol for subprocess tools, evals, and hooks.

## Audio, TTS, Streaming &amp; Variables

These document runtime packages, so they live under the Runtime reference:

- **[Audio API](/runtime/reference/audio/)** — VAD/ASM modes, turn detection, audio streaming (`runtime/audio`).
- **[TTS API](/runtime/reference/tts/)** — text-to-speech services, voices, formats (`runtime/tts`).
- **[Streaming](/runtime/reference/streaming/)** — bidirectional streaming utilities and response collection (`runtime/streaming`).
- **[Variable Providers](/runtime/reference/variables/)** — dynamic variable resolution and built-in providers (`runtime/variables`).

## See Also

- [Tutorials](/sdk/tutorials/)
- [How-To Guides](/sdk/how-to/)
- [Examples](/sdk/examples/)
