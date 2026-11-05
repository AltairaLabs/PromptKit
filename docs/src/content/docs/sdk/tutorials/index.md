---
title: SDK Tutorials
sidebar:
  order: 0
verified:
  commit: 885cafa787a84bfba66f158fe51cda15a077ec23
  sources:
    - runtime/credentials/resolver.go
    - sdk/conversation.go
    - sdk/conversation_tools.go
---
Step-by-step tutorials that teach the PromptKit SDK, starting from a pack file.

## Learning Path

Follow the tutorials in order.

### Getting Started

1. **[Your First Conversation](/sdk/tutorials/01-first-conversation/)**  
   Open a pack with `sdk.Open()` and send messages.

2. **[Streaming Responses](/sdk/tutorials/02-streaming-responses/)**  
   Stream replies with `conv.Stream()` and display incremental results.

### Building Features

3. **[Tool Integration](/sdk/tutorials/03-tool-integration/)**  
   Register handlers with `OnTool()` and build a weather assistant.

4. **[Variables and Templates](/sdk/tutorials/04-state-management/)**  
   Use `SetVar()` and `GetVar()` for template variables.

### Advanced Topics

5. **[Human-in-the-Loop](/sdk/tutorials/05-custom-pipelines/)**
   Gate sensitive tools behind approval with `OnToolAsync()`.

6. **[Observability](/sdk/tutorials/06-media-storage/)**
   Subscribe to conversation events with `hooks.On()` and `hooks.OnEvent()`.

### Voice & Audio

7. **[Audio Sessions](/sdk/tutorials/07-audio-sessions/)**
   Build voice conversations with VAD and ASM modes.

8. **[TTS Integration](/sdk/tutorials/08-tts-integration/)**
   Add text-to-speech to responses.

9. **[Variable Providers](/sdk/tutorials/09-variable-providers/)**
   Inject dynamic context from retrieval, databases and session state.

### A2A

10. **[A2A Server](/sdk/tutorials/10-a2a-server/)**
    Expose a PromptKit conversation as an A2A agent server.

11. **[AG-UI Integration](/sdk/tutorials/11-ag-ui-integration/)**
    Serve a PromptKit agent to frontend applications over the AG-UI protocol.

## Prerequisites

- Go 1.26 or later
- An API key for OpenAI, Anthropic, or Google

## What you will build

By the end you have built:

- A conversational application
- Streaming replies and tool calling
- Approval workflows for sensitive operations

## Related pages

- Check the [How-To Guides](/sdk/how-to/) for specific tasks
- See the [Reference Documentation](/sdk/reference/) for API details
- Review [SDK Examples](/sdk/examples/) for complete applications

Start with [Your First Conversation](/sdk/tutorials/01-first-conversation/).
