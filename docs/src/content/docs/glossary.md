---
title: Glossary
description: Definitions of key terms used throughout PromptKit documentation
sidebar:
  order: 100
---

Definitions of key terms used throughout PromptKit documentation.

## Audio & Voice

### ASM
**Audio Streaming Model** - A mode for native bidirectional audio streaming with multimodal LLMs like Gemini Live. Audio streams directly to and from the model without separate STT/TTS stages. Compare with [VAD](#vad).

### ASR
**Automatic Speech Recognition** - Technology that converts spoken audio into text. Also known as [STT](#stt) (Speech-to-Text). Used in [VAD](#vad) mode pipelines.

### Barge-in
The ability for a user to interrupt the AI assistant while it's speaking. Enables natural back-and-forth conversations in voice interactions. Requires real-time [turn detection](#turn-detection).

### Bit Depth
The number of bits used to represent each audio sample. PromptKit typically uses 16-bit audio, which provides good quality for voice while keeping file sizes manageable.

### Channels
The number of audio tracks in a stream. Mono (1 channel) is used for voice conversations; stereo (2 channels) is unnecessary for speech.

### Duplex
**Bidirectional streaming** - A communication pattern where audio or data flows in both directions simultaneously. In PromptKit, duplex sessions enable real-time voice conversations where the user can speak while receiving responses.

### PCM
**Pulse Code Modulation** - An uncompressed audio format used for streaming and storage. PromptKit uses raw PCM (no headers) at 16kHz sample rate for voice input to Gemini Live API.

### Sample Rate
The number of audio samples captured per second, measured in Hz. PromptKit uses 16000 Hz (16 kHz) for voice input, which is standard for speech recognition.

### STT
**Speech-to-Text** - The process of converting spoken audio into written text. Also known as [ASR](#asr). In [VAD](#vad) mode, STT converts user speech before sending to the LLM.

### TTS
**Text-to-Speech** - The process of converting written text into spoken audio. In [VAD](#vad) mode, TTS converts LLM responses into audio for playback.

### Turn
A single exchange in a conversation consisting of user input followed by an AI response. See also [Multi-turn](#multi-turn).

### Turn Detection
The process of determining when a user has finished speaking. Uses [VAD](#vad) to identify speech boundaries based on silence duration and speech patterns. Also called "endpointing."

### VAD
**Voice Activity Detection** - A mode that detects when a user is speaking versus silent. Used to determine turn boundaries in voice conversations. VAD mode pipelines use separate [STT](#stt) and [TTS](#tts) stages. Compare with [ASM](#asm).

## PromptKit Components

### Arena
**PromptArena** - PromptKit's testing framework for running scenarios against prompts. Executes conversations, validates responses, and generates reports.

### Assertion
A test check in Arena that validates expected behavior in a [scenario](#scenario). Assertions run after each turn to verify response content, format, or other criteria. Different from [guardrails](#guardrail), which are runtime checks.

### DAG
**Directed Acyclic Graph** - The structure used by PromptKit's [pipeline](#pipeline) where data flows through [stages](#stage) in a defined order with no cycles, ensuring deterministic execution.

### Event Bus
A publish-subscribe system that distributes execution events to listeners. Used for observability, logging, and monitoring pipeline execution.

### Guardrail
A validator that enforces policies on LLM outputs in real-time. Guardrails can detect and prevent policy violations (banned content, PII exposure, etc.) and abort responses early. Different from [assertions](#assertion), which are test-time checks.

### Middleware
Pluggable processing components that intercept and transform data flowing through the [pipeline](#pipeline). Examples: template rendering, validation, state management.

### Mock Provider
A fake LLM [provider](#provider) that returns pre-configured responses. Used for deterministic testing without calling real APIs or incurring costs.

### Pack
A JSON file (`.pack.json`) that bundles prompts, templates, and configuration together. Packs are the primary way to organize and distribute prompts in PromptKit.

### PackC
**Pack Compiler** - PromptKit's tool for validating, compiling, and managing prompt packs.

### Persona
A configuration that defines how a self-play LLM should behave when simulating a user in Arena tests. Personas have their own system prompts and behavioral guidelines.

### Pipeline
A sequence of processing [stages](#stage) that handle a conversation turn. Pipelines can include stages for audio processing, LLM interaction, validation, and more. Implemented as a [DAG](#dag).

### Provider
An abstraction layer for LLM services (OpenAI, Anthropic, Google, etc.). Providers handle API communication and normalize responses across different services.

### Replay
Re-running a recorded conversation using captured provider responses instead of calling real LLMs. Enables deterministic debugging and regression testing.

### Runtime
PromptKit's core execution engine that loads packs, manages state, executes pipelines, and coordinates providers.

### Scenario
A test definition for Arena that specifies conversations to run, expected behaviors, and [assertions](#assertion) to validate.

### SDK
PromptKit's high-level Go library for building conversational applications. Provides a simplified API over the Runtime.

### Self-play
An Arena testing mode where an LLM generates simulated user responses based on a [persona](#persona). Combined with [TTS](#tts), enables fully automated voice testing without pre-recorded audio.

### Session Recording
Capturing detailed event streams and artifacts (audio, messages, context) from test runs. Used for debugging, [replay](#replay), and analysis.

### Stage
A single processing step within a [pipeline](#pipeline). Examples: LLMStage (calls the model), TTSStage (converts text to speech), VADStage (detects speech boundaries).

### State Store
Persistent storage backend (Redis, in-memory, file) that maintains conversation history and context across sessions.

### Streaming Response
Returning LLM output in real-time as [tokens](#token) are generated, rather than waiting for the complete response. Provides faster perceived latency and enables [barge-in](#barge-in).

### Variable Provider
A pluggable component that dynamically provides values for template variables at runtime. Examples: TimeProvider (current time), RAGProvider (retrieved context), StateProvider (conversation state).

## LLM Concepts

### Context Window
The maximum amount of text (measured in [tokens](#token)) that an LLM can process in a single request, including both input and output.

### Embeddings
Dense vector representations of text that capture semantic meaning. Used for similarity scoring, semantic search, and [RAG](#rag) retrieval.

### Few-shot Learning
Providing an LLM with example input/output pairs in the prompt to guide its behavior, without requiring fine-tuning.

### Grounding
Anchoring LLM responses to factual, verifiable information from external sources. Reduces [hallucinations](#hallucination) by providing relevant context.

### Hallucination
When an LLM generates plausible-sounding but false or fabricated information not supported by its training data or provided context.

### Multi-turn
A conversation with multiple back-and-forth exchanges that maintains context across [turns](#turn). Requires [state management](#state-store) to track history.

### Ollama
An open-source platform for running LLMs locally. PromptKit's Ollama [provider](#provider) enables cost-free local inference using models like Llama, Mistral, and LLaVA. Uses an OpenAI-compatible API.

### Multimodal
Support for multiple content types (text, images, audio, video) in a single interaction. Gemini and GPT-4V are examples of multimodal models.

### Prompt
Instructions and context provided to an LLM to guide its response. In PromptKit, prompts are defined in packs with templates for dynamic content.

### Prompt Engineering
The practice of crafting effective prompts to guide LLM behavior toward desired outputs. Includes techniques like [few-shot learning](#few-shot-learning) and chain-of-thought.

### RAG
**Retrieval-Augmented Generation** - A technique that retrieves relevant context from external data sources (documents, databases) and provides it to the LLM. Improves [grounding](#grounding) and reduces [hallucinations](#hallucination).

### System Prompt
Initial instructions that set the LLM's behavior, personality, and constraints. Defined in the `system_template` field of a prompt.

### Temperature
A parameter controlling LLM output randomness. Lower values (0.0-0.3) produce more deterministic responses; higher values (0.7-1.0) produce more creative/varied responses.

### Token
The basic unit of text processing for LLMs. Roughly 4 characters or 0.75 words in English. Used to measure [context window](#context-window) size and API costs.

### Tool
A function that an LLM can call to perform actions or retrieve information. Also known as function calling. Defined in prompt packs and executed by the runtime.

## Protocols

### HITL
**Human-in-the-Loop** - A workflow pattern where certain decisions or [tool](#tool) executions require human approval before proceeding. Used for sensitive operations or quality control.

### MCP
**Model Context Protocol** - An open standard for connecting LLMs to external [tools](#tool) and data sources. PromptKit supports MCP for tool integration.

### WebSocket
A protocol providing full-duplex communication over a single TCP connection. Used by PromptKit for real-time [duplex](#duplex) streaming with Gemini Live API.

## See Also

- [Concepts](concepts/) - Detailed explanations of PromptKit concepts
- [Architecture](architecture/) - System design and component relationships
