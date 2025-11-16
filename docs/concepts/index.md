---
layout: docs
title: Concepts
nav_order: 7
has_children: true
description: "Fundamental concepts that apply across all PromptKit components"
keywords: "prompt engineering, LLM concepts, template systems, provider abstraction, validation, guardrails, state management, tool integration, MCP"
---

# Core Concepts

Fundamental concepts that apply across all PromptKit components.

## Overview

These concepts are building blocks used throughout PromptKit. Understanding them helps you work effectively with Runtime, SDK, PromptArena, and PackC.

## Available Concepts

### [Prompts](prompts.md)
Learn about prompts, prompt engineering, and how PromptKit handles prompts across components.

### [Templates](templates.md)
Understand template systems, variable substitution, and template management with PackC.

### [Providers](providers.md)
Learn about LLM providers, how PromptKit abstracts them, and multi-provider strategies.

### [Validation](validation.md)
Understand content validation, guardrails, and safety measures across the platform.

### [State Management](state-management.md)
Learn about conversation state, session management, and persistence strategies.

### [Tools & MCP](tools-mcp.md)
Understand function calling, tool integration, and the Model Context Protocol.

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
1. Start with [Prompts](prompts.md) - The foundation
2. Read [Providers](providers.md) - How LLMs work
3. Explore [Templates](templates.md) - Organizing prompts
4. Learn [State Management](state-management.md) - Multi-turn conversations

**If you're building production apps:**
1. [Validation](validation.md) - Safety and guardrails
2. [State Management](state-management.md) - Scalable conversations
3. [Tools & MCP](tools-mcp.md) - Extended capabilities
4. [Providers](providers.md) - Multi-provider strategies

## Relationship to Other Documentation

**Concepts** explain *what* things are and *why* they exist.

**[Reference](../runtime/reference/index.md)** shows *how* to use APIs.

**[How-To Guides](../runtime/how-to/index.md)** show *how* to accomplish tasks.

**[Tutorials](../runtime/tutorials/index.md)** provide *step-by-step* learning.

## Related Documentation

- **[Getting Started](../guides/getting-started.md)**: Quick introduction to PromptKit
- **[Architecture](../architecture/index.md)**: System design and structure
- **[Glossary](../glossary.md)**: Term definitions
- **[Workflows](../workflows/index.md)**: End-to-end examples
