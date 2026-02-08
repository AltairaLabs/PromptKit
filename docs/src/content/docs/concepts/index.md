---
title: Concepts
description: Fundamental concepts that apply across all PromptKit components
sidebar:
  order: 0
---
Fundamental concepts that apply across all PromptKit components.

## Overview

These concepts are building blocks used throughout PromptKit. Understanding them helps you work effectively with Runtime, SDK, PromptArena, and PackC.

## Available Concepts

### [Prompts](prompts)
Learn about prompts, prompt engineering, and how PromptKit handles prompts across components.

### [Templates](templates)
Understand template systems, variable substitution, and template management with PackC.

### [Providers](providers)
Learn about LLM providers, how PromptKit abstracts them, and multi-provider strategies.

### [Validation](validation)
Understand content validation, guardrails, and safety measures across the platform.

### [State Management](state-management)
Learn about conversation state, session management, and persistence strategies.

### [Tools & MCP](tools-mcp)
Understand function calling, tool integration, and the Model Context Protocol.

### [A2A (Agent-to-Agent)](a2a)
Learn about the Agent-to-Agent protocol for inter-agent communication, task lifecycle, and discovery.

## How Concepts Work Together

```
User Input
    ↓
[Prompt Template] → Combine system prompt + user message
    ↓
[Validation] → Check content safety
    ↓
[State Management] → Load conversation history
    ↓
[Provider] → Send to LLM (with Tools if needed)
    ↓
[Validation] → Check response safety
    ↓
[State Management] → Save conversation
    ↓
Response
```

## Why Learn Concepts?

Understanding these concepts helps you:

- **Make better design decisions** - Know when to use each feature
- **Debug effectively** - Understand what's happening under the hood
- **Build efficiently** - Leverage existing patterns and best practices
- **Extend PromptKit** - Create custom components that fit the architecture

## Component-Specific vs Universal

Some concepts are universal across PromptKit:
- ✅ **Prompts** - Used by all components
- ✅ **Providers** - Abstracted consistently
- ✅ **Validation** - Available everywhere

Some concepts are component-specific:
- **Templates** - Mainly Runtime and PackC
- **State Management** - Mainly Runtime and SDK
- **Tools/MCP** - Mainly Runtime

## Learning Path

**If you're new to PromptKit:**
1. Start with [Prompts](prompts) - The foundation
2. Read [Providers](providers) - How LLMs work
3. Explore [Templates](templates) - Organizing prompts
4. Learn [State Management](state-management) - Multi-turn conversations

**If you're building production apps:**
1. [Validation](validation) - Safety and guardrails
2. [State Management](state-management) - Scalable conversations
3. [Tools & MCP](tools-mcp) - Extended capabilities
4. [Providers](providers) - Multi-provider strategies

## Relationship to Other Documentation

**Concepts** explain *what* things are and *why* they exist.

**[Reference](../runtime/reference/index)** shows *how* to use APIs.

**[How-To Guides](../runtime/how-to/index)** show *how* to accomplish tasks.

**[Tutorials](../runtime/tutorials/index)** provide *step-by-step* learning.

## Related Documentation

- **[Getting Started](../guides/getting-started)**: Quick introduction to PromptKit
- **[Architecture](../architecture/index)**: System design and structure
- **[Glossary](../glossary)**: Term definitions
- **[Workflows](../workflows/index)**: End-to-end examples
