---
title: Runtime Explanation
docType: explanation
order: 4
---
# Runtime Explanation

Understanding the architecture and design of PromptKit Runtime.

## Purpose

These explanations help you understand *why* Runtime works the way it does. They cover architectural decisions, design patterns, and the reasoning behind Runtime's implementation.

## Topics

### Architecture
- [Pipeline Architecture](pipeline-architecture) - How the stage-based streaming pipeline works
- [Provider System](provider-system) - LLM provider abstraction and implementation

### Integration
- [Stage Design](stage-design) - Composable stage patterns
- [State Management](state-management) - Conversation history and persistence

## When to Read These

**Read explanations when you want to:**
- Understand architectural decisions
- Learn design patterns used in Runtime
- Extend Runtime with custom components
- Troubleshoot complex issues
- Contribute to Runtime development

**Don't read these when you need to:**
- Quickly complete a specific task → See [How-To Guides](../how-to/index)
- Learn Runtime from scratch → See [Tutorials](../tutorials/index)
- Look up API details → See [Reference](../reference/index)

## Key Concepts

### Stage-Based Architecture

Runtime uses a stage-based streaming architecture where each stage processes elements in its own goroutine. This provides:

- **True Streaming**: Elements flow through as they're produced
- **Concurrency**: Stages run in parallel
- **Composability**: Mix and match stages
- **Backpressure**: Channel-based flow control
- **Testability**: Test stages in isolation

### Provider Abstraction

Runtime abstracts LLM providers behind a common interface, enabling:

- **Provider independence**: Switch providers without code changes
- **Fallback strategies**: Try multiple providers automatically
- **Consistent API**: Same code for OpenAI, Claude, Gemini
- **Easy testing**: Mock providers for tests

### Tool System

Runtime implements function calling through:

- **Tool descriptors**: Define tools with schemas
- **Executors**: Pluggable tool execution backends
- **MCP integration**: Connect external tool servers
- **Automatic calling**: LLM decides when to use tools

## Architecture Overview

```
Input
  ↓
Pipeline
  ├── StateStoreLoad Stage (load conversation)
  ├── PromptAssembly Stage (apply templates)
  ├── Validation Stage (check content)
  └── Provider Stage (call LLM)
      ├── Tool Registry (available tools)
      └── Provider (OpenAI/Claude/Gemini)
  ↓
Output
```

Each stage runs concurrently in its own goroutine, connected by channels.

## Pipeline Modes

Runtime supports three execution modes:

### Text Mode
Standard HTTP-based LLM interactions for chat and completion.

### VAD Mode
Voice Activity Detection for voice applications using text-based LLMs:
Audio → STT → LLM → TTS

### ASM Mode
Audio Streaming Mode for native multimodal LLMs with real-time audio via WebSocket.

## Design Principles

**1. Streaming First**
- Channel-based data flow
- Concurrent stage execution
- True streaming (not simulated)

**2. Composability**
- Stage pipeline is flexible
- Mix components as needed
- Build custom pipelines

**3. Extensibility**
- Custom stages supported
- Custom providers possible
- Custom tools and executors

**4. Production-Ready**
- Error handling built-in
- Resource cleanup automatic
- Monitoring capabilities

## Related Documentation

- **[Reference](../reference/index)**: Complete API documentation
- **[How-To Guides](../how-to/index)**: Task-focused guides
- **[Tutorials](../tutorials/index)**: Step-by-step learning

## Contributing

Understanding these architectural concepts is valuable when contributing to Runtime. See the [Runtime codebase](https://github.com/AltairaLabs/PromptKit/tree/main/runtime) for implementation details.
