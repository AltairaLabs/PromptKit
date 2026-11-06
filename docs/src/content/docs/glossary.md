---
title: Glossary
description: Definitions of key terms used throughout PromptKit documentation
sidebar:
  order: 100
verified:
  commit: ebf8d92f226c18ca12b790f73b09a762bd8d2512
  sources:
    - pkg/config/role.go
    - runtime/events/bus.go
    - runtime/hooks/hooks.go
    - runtime/inference/factory.go
    - runtime/inference/provider.go
    - runtime/pipeline/stage/stage.go
    - runtime/pipeline/stage/stages_speech.go
    - runtime/prompt/schema/promptpack.schema.json
    - runtime/providers/gemini/audio_encoder.go
    - runtime/providers/replay/provider.go
    - runtime/statestore/file/store.go
    - runtime/statestore/memory.go
    - runtime/statestore/redis.go
    - runtime/variables/state.go
    - runtime/variables/time.go
    - sdk/conversation_tools.go
---

Definitions of terms used across the PromptKit documentation. Each entry links to the page that covers the term in full.

## Audio and voice

### ASM

**Audio Streaming Mode**. A duplex voice mode in which a native audio model (Gemini Live, OpenAI Realtime) takes audio in and returns audio, with no separate [STT](#stt) or [TTS](#tts) stage. The model detects turn boundaries. Compare with [VAD](#vad). See [Choose a voice mode](/sdk/how-to/voice/choose-voice-mode/).

### ASR

**Automatic Speech Recognition**. Converts spoken audio to text. Another name for [STT](#stt).

### Barge-in

A user interrupting the assistant while it speaks. The session stops the assistant's playback and starts a new [turn](#turn). See [Handle turns, barge-in and transcripts](/sdk/how-to/voice/handle-turns-and-barge-in/).

### Bit depth

The number of bits per audio sample. Voice input to Gemini Live is 16-bit PCM. See [Audio](/runtime/reference/audio/).

### Channels

The number of audio tracks in a stream. Voice input is mono (one channel). See [Audio](/runtime/reference/audio/).

### Duplex

A session that sends and receives audio at the same time, so the user can speak while the assistant responds. A duplex session runs in [VAD](#vad) mode or [ASM](#asm) mode. See [Choose a voice mode](/sdk/how-to/voice/choose-voice-mode/).

### PCM

**Pulse Code Modulation**. Uncompressed audio samples without a header. Gemini Live accepts 16 kHz PCM input. See [Audio](/runtime/reference/audio/).

### Sample rate

The number of audio samples per second, in Hz. Gemini Live input must be 16000 Hz, and the default VAD sample rate is 16000 Hz. See [Audio](/runtime/reference/audio/).

### STT

**Speech-to-Text**. Converts spoken audio to text. In [VAD](#vad) mode the `STTStage` transcribes the user's speech before the text reaches the model. Also called [ASR](#asr). See [Choose a voice mode](/sdk/how-to/voice/choose-voice-mode/).

### TTS

**Text-to-Speech**. Converts text to spoken audio. In [VAD](#vad) mode the `TTSStage` turns the model's reply into audio. See [Configure text-to-speech](/sdk/how-to/voice/configure-tts/).

### Turn

One exchange in a conversation: user input followed by the assistant's response. See [Multi-turn](#multi-turn).

### Turn detection

Deciding when a user has finished speaking. In [VAD](#vad) mode the session ends a turn after a configured silence; in [ASM](#asm) mode the model decides. Also called endpointing. See [Handle turns, barge-in and transcripts](/sdk/how-to/voice/handle-turns-and-barge-in/).

### VAD

**Voice Activity Detection**. A duplex voice mode for text models. A voice activity detector finds turn boundaries, [STT](#stt) transcribes the speech, the model replies in text, and [TTS](#tts) speaks the reply. Compare with [ASM](#asm). See [Choose a voice mode](/sdk/how-to/voice/choose-voice-mode/).

## Runtime and SDK

### DAG

**Directed Acyclic Graph**. The shape of a [pipeline](#pipeline): [stages](#stage) connected by channels with no cycles. See [Pipeline architecture](/runtime/explanation/pipeline-architecture/).

### Event bus

The publish-subscribe bus (`events.EventBus`) that delivers runtime events to listeners. Observability, logging, and metrics subscribe to it. See [Runtime event system](/architecture/runtime-events/).

### Hook

An interception point in the runtime. `ProviderHook` wraps provider calls, `ToolHook` wraps tool calls, `ToolResultRewriter` rewrites a tool result before the model sees it, and `SessionHook` observes session lifecycle. [Guardrails](#guardrail) are hooks. See [Hooks](/runtime/reference/hooks/).

### Inference provider

A [provider](#provider) declared with `role: inference` that answers classification requests through one interface, `inference.Provider`: content and optional labels in, a probability per label out. Each type calls one vendor API: `huggingface` (Hugging Face Inference API), `openai` (OpenAI-compatible chat completions, read through logprobs), or `systemone`. Checks such as `audio_emotion`, `text_toxicity`, and `topic_policy` use inference providers. See [Providers](/concepts/providers/).

### Mock provider

A [provider](#provider) that returns configured responses instead of calling a model. Use it for deterministic tests without network calls or cost. See [Providers](/concepts/providers/).

### Pack

A JSON file (`.pack.json`) that bundles prompts, tools, validators, evals, skills, and workflow definitions. The SDK opens a pack with `sdk.Open`. See [Prompts](/concepts/prompts/).

### Pipeline

The [DAG](#dag) of [stages](#stage) that handles one conversation turn. Stages cover prompt assembly, state loading, the provider call, validation, and audio processing. See [Runtime pipeline](/architecture/runtime-pipeline/).

### Provider

The runtime's adapter to a model service such as OpenAI, Anthropic, Google, Ollama, or vLLM. A provider sends requests, reads responses, and normalizes them to common types. See [Providers](/concepts/providers/).

### Replay

Re-running a recorded session through the replay provider, which returns the recorded responses instead of calling a model. See [Providers](/concepts/providers/).

### Runtime

The core Go library (`runtime/`): providers, the [pipeline](#pipeline), tools, [state stores](#state-store), events, and hooks. The [SDK](#sdk) builds on it. See [System overview](/architecture/system-overview/).

### SDK

The high-level Go library (`sdk/`) for building conversational applications. `sdk.Open` returns a `Conversation` that wires a pack, a provider, and the runtime together. See [SDK](/sdk/).

### Session recording

The event stream and media of a session, captured so the session can be inspected, exported, or replayed. See [Runtime event system](/architecture/runtime-events/).

### Skill

A directory with a `SKILL.md` file that a conversation activates on demand through the `skill__activate` tool. Activation returns the skill's instructions and grants the tools it lists. See [Skills](/concepts/skills/) and the [Skills reference](/reference/skills/).

### Stage

One processing step in a [pipeline](#pipeline). Examples: `ProviderStage` calls the model, `STTStage` transcribes audio, `TTSStage` synthesizes speech. See [Stage design](/runtime/explanation/stage-design/).

### State store

The backend that holds conversation history and metadata across turns and sessions. Built-in stores: in-memory, Redis, and file. See [State management](/concepts/state-management/).

### Streaming response

Model output delivered as it is generated rather than after it completes. See [Streaming](/runtime/reference/streaming/).

### Variable provider

A component that supplies template variable values at runtime. Examples: `TimeProvider` (current time) and `StateProvider` (conversation metadata). See [Variables](/runtime/reference/variables/).

## Checks

### Assertion

A [check](/concepts/validation/) in a PromptArena scenario that produces a pass or fail verdict on a response. An assertion wraps an [eval](#eval) handler and applies score thresholds. Compare with [guardrail](#guardrail), which enforces at runtime. See [Checks and validation](/concepts/validation/).

### Eval

A scored check declared in a pack under `evals`. An eval records a score and metrics and never decides pass or fail. See [Checks and validation](/concepts/validation/) and [Evals](/runtime/reference/evals/).

### Guardrail

A validator declared in a pack under `validators` that enforces a policy on model input or output at runtime. A triggered guardrail truncates, replaces, or blocks the message. Compare with [assertion](#assertion), which only reports. See [Checks and validation](/concepts/validation/).

## PromptArena

PromptArena and PackC are separate products in the [promptarena repository](https://github.com/AltairaLabs/promptarena). PromptKit supplies the runtime they run on.

### Arena

**PromptArena**. The testing tool that runs [scenarios](#scenario) against packs, evaluates [assertions](#assertion), and writes reports.

### PackC

**Pack Compiler**. The tool that validates and compiles prompt packs. See [Prompts](/concepts/prompts/).

### Persona

A configuration that tells a [self-play](#self-play) model how to behave as the user in an Arena test.

### Scenario

An Arena test definition: the conversation to run, its inputs, and the [assertions](#assertion) to evaluate.

### Self-play

An Arena mode in which a model plays the user, following a [persona](#persona), to generate the user turns. Combined with [TTS](#tts), it drives voice tests without recorded audio.

## LLM concepts

### Context window

The most text, measured in [tokens](#token), that a model processes in one request, input and output together. See [Manage context](/sdk/how-to/conversations/manage-context/).

### Embeddings

Vector representations of text that capture meaning. Used for similarity scoring, semantic search, and [RAG](#rag). See [Retrieval architecture](/runtime/explanation/retrieval-architecture/).

### Few-shot learning

Giving a model example inputs and outputs in the prompt to steer its behavior without fine-tuning.

### Grounding

Anchoring a response to verifiable information from an external source, which reduces [hallucinations](#hallucination).

### Hallucination

A plausible but false statement that the model's training data or the supplied context does not support.

### Multi-turn

A conversation with several exchanges that keeps context across [turns](#turn). The [state store](#state-store) holds the history. See [State management](/concepts/state-management/).

### Multimodal

Support for several content types (text, images, audio, video) in one interaction. See [Analyze documents](/sdk/how-to/multimodal/analyze-documents/).

### Ollama

An open-source tool for running models locally. The Ollama [provider](#provider) uses its OpenAI-compatible API. See [Providers](/concepts/providers/).

### Prompt

The instructions and context sent to a model. A pack defines prompts with templates for dynamic content. See [Prompts](/concepts/prompts/).

### Prompt engineering

Writing prompts to steer a model toward the output you want, with techniques such as [few-shot learning](#few-shot-learning) and chain-of-thought.

### RAG

**Retrieval-Augmented Generation**. Retrieving relevant content from documents or a database and giving it to the model with the prompt. Improves [grounding](#grounding). See [Retrieval architecture](/runtime/explanation/retrieval-architecture/).

### System prompt

The instructions that set a model's behavior and constraints. A pack prompt defines it in `system_template`. See [Templates](/concepts/templates/).

### Temperature

A sampling parameter for output randomness. Lower values give more deterministic output and higher values give more varied output.

### Token

The unit a model reads and writes: a word fragment of about four characters in English. [Context window](#context-window) size and API cost are measured in tokens.

### Tool

A function the model can call to act or fetch information, also called function calling. A pack declares tools and the runtime executes them. See [Tools and MCP](/concepts/tools-mcp/).

### vLLM

A GPU inference server for high-throughput model serving. The vLLM [provider](#provider) targets a self-hosted vLLM endpoint. See [Providers](/concepts/providers/).

## Protocols

### A2A

**Agent-to-Agent**. A protocol for agents to call each other. PromptKit acts as an A2A client and serves conversations as A2A agents. See [A2A](/concepts/a2a/).

### HITL

**Human-in-the-loop**. A [tool](#tool) call held for human approval before it runs. The SDK exposes held calls through `OnToolAsync`, `PendingTools`, `ResolveTool`, and `RejectTool`. See [Tools and MCP architecture](/architecture/runtime-tools-mcp/).

### MCP

**Model Context Protocol**. An open standard for connecting models to external [tools](#tool) and data. PromptKit includes an MCP client. See [Tools and MCP](/concepts/tools-mcp/).

### WebSocket

A protocol for two-way messaging over one TCP connection. Gemini Live streaming uses it for [duplex](#duplex) sessions. See [Audio](/runtime/reference/audio/).

## See also

- [Concepts](/concepts/)
- [Architecture](/architecture/)
