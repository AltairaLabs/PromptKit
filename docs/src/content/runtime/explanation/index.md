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
- [Pipeline Architecture](pipeline-architecture) - How the middleware-based pipeline works
- [Provider System](provider-system) - LLM provider abstraction and implementation

### Integration
- [Middleware Design](middleware-design) - Composable middleware patterns
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

### Middleware Pattern

Runtime uses a middleware-based architecture where each middleware layer processes requests in sequence. This provides:

- **Composability**: Mix and match middleware
- **Separation of concerns**: Each middleware has one job
- **Flexibility**: Add custom middleware easily
- **Testability**: Test middleware in isolation

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
Request
  ↓
Pipeline
  ├── State Middleware (load conversation)
  ├── Template Middleware (apply templates)
  ├── Validator Middleware (check content)
  └── Provider Middleware (call LLM)
      ├── Tool Registry (available tools)
      └── Provider (OpenAI/Claude/Gemini)
  ↓
Response
```

## Design Principles

**1. Simplicity**
- Easy to use for common cases
- Sensible defaults
- Clear error messages

**2. Composability**
- Middleware stack is flexible
- Mix components as needed
- Build custom pipelines

**3. Extensibility**
- Custom middleware supported
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
