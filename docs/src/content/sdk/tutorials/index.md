---
title: SDK Tutorials
docType: tutorial
order: 3
---
# SDK v2 Tutorials

Step-by-step tutorials to learn the PromptKit SDK v2 pack-first architecture.

## Learning Path

Follow these tutorials in order for a structured learning experience:

### Getting Started

1. **[Your First Conversation](01-first-conversation)**  
   Build a chatbot in 5 lines of code. Learn `sdk.Open()` and basic message sending.

2. **[Streaming Responses](02-streaming-responses)**  
   Implement real-time streaming with `conv.Stream()`. Display incremental results.

### Building Features

3. **[Tool Integration](03-tool-integration)**  
   Add function calling with `OnTool()`. Register handlers and build a weather assistant.

4. **[Variables and Templates](04-state-management)**  
   Use `SetVar()`/`GetVar()` for template variables. Manage conversation context.

### Advanced Topics

5. **[Human-in-the-Loop](05-custom-pipelines)**  
   Implement approval workflows with `OnToolAsync()`. Build safe AI agents.

6. **[Working with Media](06-media-storage)**  
   Handle images and multimodal content. Optimize memory with storage.

## Prerequisites

- Go 1.21 or higher
- Basic Go programming knowledge
- API key for OpenAI, Anthropic, or Google

## What You'll Build

By completing these tutorials, you'll:

- Create conversational AI applications with minimal code
- Implement streaming and tool calling
- Build approval workflows for sensitive operations
- Understand SDK v2 architecture and patterns

## Getting Help

- Check the [How-To Guides](../how-to/) for specific tasks
- See the [Reference Documentation](../reference/) for API details
- Review [SDK Examples](/sdk/examples/) for complete applications

Start with [Your First Conversation](01-first-conversation) →
