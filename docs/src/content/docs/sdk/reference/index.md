---
title: SDK Reference
sidebar:
  order: 0
verified:
  commit: 8489bf3d95c7498f9de0f024769db9399c24e884
  sources:
    - sdk/conversation.go
    - sdk/conversation_tools.go
    - sdk/errors.go
    - sdk/response.go
    - sdk/runtime_config.go
    - sdk/sdk.go
    - sdk/streaming.go
---
Reference documentation for the PromptKit SDK.

The SDK is a pack-first API: open a conversation from a pack file, send messages,
and register tools and hooks. For learning paths and task recipes, see the
[Tutorials](/sdk/tutorials/) and [How-To Guides](/sdk/how-to/).

## SDK API

The `sdk` package reference is generated from source. It covers `Open`, the `Conversation` type (`Send`, `Stream`, `OnTool`,
`Fork`, `EventBus`, …), `Response`, `StreamChunk`, every `With*` option, and the
exported error values:

- [Conversation &amp; SDK API](/sdk/reference/conversation-manager/): the full generated `sdk` package reference.

## Integrations

- [A2A Server](/sdk/reference/a2a-server/): `A2AServer`, task store, and conversation opener (`server/a2a`).
- [AG-UI Integration](/sdk/reference/ag-ui/): converters between PromptKit and AG-UI SDK types (`sdk/agui`).

## Configuration &amp; Protocols

- [RuntimeConfig](/sdk/reference/runtime-config/): YAML config schema for providers, tools, evals, hooks, sandboxes, selectors, skills, MCP servers, the state store, and logging.
- [Exec Protocol](/sdk/reference/exec-protocol/): wire protocol for subprocess tools, evals, and hooks.

## Audio, TTS, Streaming &amp; Variables

These document runtime packages, so they live under the Runtime reference:

- [Audio API](/runtime/reference/audio/): VAD/ASM modes, turn detection, audio streaming (`runtime/audio`).
- [TTS API](/runtime/reference/tts/): text-to-speech services, voices, formats (`runtime/tts`).
- [Streaming](/runtime/reference/streaming/): bidirectional streaming utilities and response collection (`runtime/streaming`).
- [Variable Providers](/runtime/reference/variables/): dynamic variable resolution and built-in providers (`runtime/variables`).

## See Also

- [Tutorials](/sdk/tutorials/)
- [How-To Guides](/sdk/how-to/)
- [Examples](/sdk/examples/)
