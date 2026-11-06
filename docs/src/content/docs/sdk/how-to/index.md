---
title: SDK How-To Guides
description: Task-focused guides for the PromptKit SDK, grouped by what you want to do.
sidebar:
  order: 0
verified:
  commit: 004b9cbbd4d40f770a2e3c76053b600a55189355
  sources:
    - sdk/conversation.go
    - sdk/conversation_tools.go
    - sdk/hooks/events.go
---
Task-focused guides for the PromptKit SDK. Each page assumes you have a conversation open; if you do not, start with [Open a Conversation](/sdk/how-to/conversations/initialize/).

## Conversations

- [Open a Conversation](/sdk/how-to/conversations/initialize/): start with `sdk.Open()`
- [Send Messages](/sdk/how-to/conversations/send-messages/): send with `Send()` and `Stream()`
- [Manage Variables](/sdk/how-to/conversations/manage-state/): set and read template variables with `SetVar()` and `GetVar()`
- [Manage Context](/sdk/how-to/conversations/manage-context/): configure token budgets and truncation for long conversations
- [Memory and Grounding](/sdk/how-to/conversations/use-memory/): give the model memory tools or inject retrieved content into the prompt
- [Use RuntimeConfig](/sdk/how-to/conversations/use-runtime-config/): load providers, tools, MCP servers, hooks, state store and logging from one YAML file
- [Stream with typed event callbacks](/sdk/how-to/conversations/stream-with-callbacks/): handle text deltas and tool requests with `OnStreamEvent`
- [Shut down conversations gracefully](/sdk/how-to/conversations/shut-down-gracefully/): close every open conversation on SIGTERM with a `ShutdownManager`
- [Handle SDK errors](/sdk/how-to/conversations/handle-errors/): match `ValidationError`, `ProviderError`, `PackError` and the sentinel errors

## Tools

- [Register Tools](/sdk/how-to/tools/register-tools/): add function calling with `OnTool()`
- [Call HTTP Tools](/sdk/how-to/tools/http-tools/): make HTTP API calls with `OnToolHTTP()`
- [Register Client-Side Tools](/sdk/how-to/tools/client-tools/): run tools on the client, such as GPS or camera
- [Define Exec Tools](/sdk/how-to/tools/exec-tools/): bind tools to external subprocesses in any language
- [Configure MCP Servers](/sdk/how-to/tools/configure-mcp/): connect MCP tool servers with `NewMCPServer()` and `WithMCPServer()`
- [Override Capability Tool Definitions](/sdk/how-to/tools/override-capability-tools/): patch the descriptors that memory, workflow and A2A register
- [Resolve MCP Server Endpoints at Runtime](/sdk/how-to/tools/resolve-mcp-endpoints/): declare servers by name and supply URLs with `WithMCPEndpoints`

## Hooks

- [Write Custom Hooks](/sdk/how-to/hooks/custom-hooks/): implement each hook type in Go and register it
- [Run Exec Hooks](/sdk/how-to/hooks/exec-hooks/): add external hooks in any language via subprocesses
- [Sandbox Exec Hooks](/sdk/how-to/hooks/sandbox-hooks/): run exec hook subprocesses in a container, sidecar or custom backend
- [Apply Governance Policy in Your Host](/sdk/how-to/hooks/apply-governance-policy/): enforce your own policy over a pack's declarations

## Providers

- [Bring Your Own Inference Provider](/sdk/how-to/providers/bring-your-own-classifier/): plug a custom classifier into the SDK
- [Configure Embedding Providers Declaratively](/sdk/how-to/providers/declarative-embedding-providers/): set up embeddings from RuntimeConfig YAML
- [Configure TTS / STT Providers Declaratively](/sdk/how-to/providers/declarative-tts-stt-providers/): set up speech synthesis and transcription from RuntimeConfig YAML
- [Rerank Retrieved Candidates](/sdk/how-to/providers/use-rerank-provider/): reorder candidates by relevance with `WithRerankProvider`
- [Run Models on Bedrock, Vertex AI or Azure](/sdk/how-to/providers/use-cloud-platforms/): host a provider on a cloud platform and choose its credential

## Voice

- [Choose a voice mode](/sdk/how-to/voice/choose-voice-mode/): open a duplex session in VAD mode or ASM mode
- [Configure text-to-speech](/sdk/how-to/voice/configure-tts/): pick a TTS service, set the voice and format, and stream synthesis
- [Handle turns, barge-in and transcripts](/sdk/how-to/voice/handle-turns-and-barge-in/): stream live audio, tune turn endings and flush playback on barge-in
- [Monitor audio in a voice session](/sdk/how-to/voice/monitor-audio/): read the user's and the agent's audio with `WithAudioMonitor`
- [Ingest several audio tracks into one agent](/sdk/how-to/voice/ingest-multiple-tracks/): feed named tracks to one agent with `MultiTrackIngestion`

## Multimodal

- [Preprocess Images](/sdk/how-to/multimodal/preprocess-images/): resize and optimize images before sending them to a provider
- [Analyze Documents](/sdk/how-to/multimodal/analyze-documents/): send PDF documents to a provider for analysis

## Interoperability

- [Connect A2A Agents](/sdk/how-to/interop/connect-a2a-agents/): register remote A2A agents with `NewA2AAgent()` and `WithA2AAgent()`
- [Choose an A2A Server Mode](/sdk/how-to/interop/choose-a2a-server-mode/): decide whether the A2A server or your platform owns conversations
- [Run a Multi-Agent Pack in Process](/sdk/how-to/interop/run-multi-agent-pack/): open an entry agent and its members with `OpenMultiAgent`

## Observability and evals

- [Monitor Events](/sdk/how-to/observability/monitor-events/): subscribe to events with `hooks.On()`
- [Run Evals](/sdk/how-to/observability/run-evals/): evaluate conversations with `sdk.Evaluate()`

## Related

- [Tutorials](/sdk/tutorials/): step-by-step learning
- [Reference](/sdk/reference/): API reference
- [Examples](/sdk/examples/): working code examples
