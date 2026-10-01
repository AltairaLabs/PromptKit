---
title: SDK Explanation
sidebar:
  order: 0
---
Deep-dive documentation explaining SDK architecture and design.

## Architecture

- **[SDK Architecture](/sdk/explanation/architecture/)** - Pack-first design and components
- **[Observability](/sdk/explanation/observability/)** - Event system and monitoring
- **[How Variables Resolve](/sdk/explanation/variables/)** - Sources, precedence, and the single substitution point

## Design Philosophy

### Pack-First Architecture

The SDK is built around the pack file as the single source of truth:

```d2
direction: down

pack: Pack File {
  label: "Pack File\n← Configuration, prompts, tools"
}

open: sdk.Open() {
  label: "sdk.Open()\n← Load and validate"
}

conv: Conversation {
  label: "Conversation\n← Ready to use"
}

pack -> open -> conv
```

### Why Pack-First?

1. **Reduced Boilerplate** - No manual provider/manager setup
2. **Tested Configuration** - Packs are validated by Arena
3. **Portable** - Same pack works across environments
4. **Versioned** - Pack files are version controlled

### Minimal API

```go
conv, _ := sdk.Open("./pack.json", "chat")
defer conv.Close()
resp, _ := conv.Send(ctx, "Hello")
```

Three lines to a working conversation.

## Key Concepts

### Conversation Lifecycle

1. **Open** - `sdk.Open()` loads pack, creates conversation
2. **Configure** - `SetVar()`, `OnTool()` setup
3. **Use** - `Send()`, `Stream()` interactions
4. **Close** - `Close()` cleanup

### Tool Execution

Tools are registered with handlers:

```mermaid
flowchart TD
  LR["LLM Request"] --> TCD["Tool Call Decision"] --> HL["Handler Lookup"]
  HL --> OT["OnTool handler"] --> EX1["Execute immediately"]
  HL --> OTA["OnToolAsync handler"]
  OTA --> AA["Auto-approve"] --> EX2["Execute"]
  OTA --> PD["Pending"] --> W["Wait for ResolveTool/RejectTool"]
```

### Event System

Events flow through the hooks package:

```mermaid
flowchart TD
  SD["Send()"] --> E1["EventPipelineStarted"]
  SD --> PC["Provider Call"] --> E2["EventProviderCallStarted"]
  PC --> RS["Response"] --> E3["EventProviderCallCompleted"]
  RS --> TC["Tool Call"] --> E4["EventToolCallStarted"]
  TC --> H["Handler"] --> E5["EventToolCallCompleted"]
  RS --> ER["Error"] --> E6["EventProviderCallFailed"]
  RS --> E7["EventPipelineCompleted"]
```

## See Also

- [Tutorials](/sdk/tutorials/)
- [How-To Guides](/sdk/how-to/)
- [API Reference](/sdk/reference/)
