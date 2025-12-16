# PromptKit SDK

The PromptKit SDK provides a simple, pack-first API for building LLM-powered applications in Go.

## Overview

SDK v2 is designed around PromptPack files - everything starts from a `.pack.json` file that defines prompts, variables, tools, validators, and pipeline configuration. The SDK loads the pack and provides a minimal API to interact with LLMs.

### Key Features

- **Pack-First Design**: Load prompts directly from pack files - no manual configuration
- **Stage-Based Pipeline**: Built on the streaming stage architecture for true streaming execution
- **Multiple Execution Modes**: Text, VAD (Voice Activity Detection), and ASM (Audio Streaming Mode)
- **Tool System**: Multiple executor types (function, HTTP, MCP)
- **Streaming Support**: Built-in streaming with customizable handlers
- **Observability**: EventBus integration with hooks package for monitoring

## Quick Start

```go
package main

import (
    "context"
    "fmt"
    "log"

    "github.com/AltairaLabs/PromptKit/sdk"
)

func main() {
    // Open a conversation from a pack file
    conv, err := sdk.Open("./assistant.pack.json", "chat")
    if err != nil {
        log.Fatal(err)
    }
    defer conv.Close()

    // Send a message and get a response
    resp, err := conv.Send(context.Background(), "Hello!")
    if err != nil {
        log.Fatal(err)
    }
    fmt.Println(resp.Text())
}
```

## Installation

```bash
go get github.com/AltairaLabs/PromptKit/sdk
```

## Core Concepts

### Opening a Conversation

Use `Open` to load a pack file and create a conversation for a specific prompt:

```go
// Minimal - provider auto-detected from environment
conv, _ := sdk.Open("./demo.pack.json", "troubleshooting")

// With options - override model, provider, etc.
conv, _ := sdk.Open("./demo.pack.json", "troubleshooting",
    sdk.WithModel("gpt-4o"),
    sdk.WithAPIKey(os.Getenv("MY_OPENAI_KEY")),
)
```

### Variables

Variables defined in the pack are populated at runtime:

```go
conv.SetVar("customer_id", "acme-corp")
conv.SetVars(map[string]any{
    "customer_name": "ACME Corporation",
    "tier": "premium",
})
```

### Tools

Tools defined in the pack just need implementation handlers:

```go
conv.OnTool("list_devices", func(args map[string]any) (any, error) {
    return myAPI.ListDevices(args["customer_id"].(string))
})
```

### Streaming

Stream responses chunk by chunk:

```go
for chunk := range conv.Stream(ctx, "Tell me a story") {
    fmt.Print(chunk.Text)
}
```

## Pipeline Architecture

The SDK uses a stage-based pipeline architecture for all execution modes. Pipelines are composed of stages that process streaming elements.

### Pipeline Modes

| Mode | Description | Use Case |
|------|-------------|----------|
| **Text** | Standard HTTP API calls | Chat, completion, text generation |
| **VAD** | Audio → VAD → STT → LLM → TTS | Voice assistants without native audio LLM |
| **ASM** | Direct audio streaming via WebSocket | Native multimodal LLMs (Gemini Live) |

### Text Mode Pipeline

```
StateStoreLoad → VariableProvider → PromptAssembly → Template → Provider → Validation → StateStoreSave
```

### VAD Mode Pipeline

```
StateStoreLoad → VariableProvider → PromptAssembly → Template → AudioTurn → STT → Provider → TTS → Validation → StateStoreSave
```

### ASM Mode Pipeline

```
StateStoreLoad → VariableProvider → PromptAssembly → Template → DuplexProvider → Validation → StateStoreSave
```

## Configuration Options

### Provider Configuration

```go
conv, _ := sdk.Open("./pack.json", "chat",
    sdk.WithProvider(openai.NewProvider(openai.Config{
        APIKey: os.Getenv("OPENAI_API_KEY"),
    })),
    sdk.WithModel("gpt-4o"),
    sdk.WithMaxTokens(1000),
    sdk.WithTemperature(0.7),
)
```

### State Store (Conversation History)

```go
store := statestore.NewMemoryStore()

conv, _ := sdk.Open("./pack.json", "chat",
    sdk.WithStateStore(store),
    sdk.WithConversationID("session-123"),
)
```

### VAD Mode (Voice Activity Detection)

```go
sttService := stt.NewOpenAI(os.Getenv("OPENAI_API_KEY"))
ttsService := tts.NewOpenAI(os.Getenv("OPENAI_API_KEY"))

conv, _ := sdk.OpenDuplex("./pack.json", "voice-chat",
    sdk.WithProvider(openai.NewProvider(...)),
    sdk.WithVADMode(sttService, ttsService, sdk.DefaultVADModeConfig()),
)
```

### ASM Mode (Audio Streaming)

```go
// For providers with native audio streaming (e.g., Gemini Live)
session, _ := gemini.NewStreamSession(ctx, endpoint, apiKey, config)

conv, _ := sdk.OpenDuplex("./pack.json", "voice-chat",
    sdk.WithStreamingConfig(session),
)
```

## Package Structure

```
sdk/
├── doc.go              # Package documentation
├── sdk.go              # Entry points: Open, OpenDuplex
├── conversation.go     # Conversation type
├── options.go          # Configuration options
├── response.go         # Response type
├── hooks/              # Event subscription and lifecycle hooks
├── session/            # Session management (Unary, Duplex)
├── stream/             # Streaming response handling
├── tools/              # Tool handlers and HITL support
└── internal/
    └── pipeline/       # Pipeline builder (stage-based)
```

## Examples

See the `sdk/examples/` directory for complete examples:

- `hello` - Basic conversation
- `streaming` - Token-by-token streaming
- `tools` - Tool registration and execution
- `hitl` - Human-in-the-loop approval
- `duplex-streaming` - WebSocket duplex streaming
- `vad-demo` - Voice activity detection demo
- `voice-interview` - Complete voice interview application

## Design Principles

1. **Pack is the Source of Truth** - The `.pack.json` file defines prompts, tools, validators, and pipeline configuration. The SDK configures itself automatically.

2. **Convention Over Configuration** - API keys from environment, provider auto-detection, model defaults from pack. Override only when needed.

3. **Progressive Disclosure** - Simple things are simple, advanced features available but not required.

4. **Same Runtime, Same Behavior** - SDK v2 uses the same runtime pipeline as Arena. Pack-defined behaviors work identically.

5. **Stage-Based Streaming** - Built on the stage architecture for true streaming execution with concurrent processing.

## Runtime Types

The SDK uses runtime types directly - no duplication:

```go
import "github.com/AltairaLabs/PromptKit/runtime/types"

msg := &types.Message{Role: "user"}
msg.AddTextPart("Hello")
```

Key runtime types: `types.Message`, `types.ContentPart`, `types.MediaContent`, `types.CostInfo`, `types.ValidationResult`.

## Schema Reference

All pack examples conform to the PromptPack Specification v1.1.0. See the [PromptPack Schema](../runtime/prompt/schema/promptpack.schema.json) for the complete specification.

## Related Documentation

- [Pipeline Stage Architecture](../runtime/pipeline/stage/README.md) - Stage-based pipeline system
- [SDK Migration Guide](../docs/MIGRATION_GUIDE.md) - Migration from middleware to stages
- [Breaking Changes](../docs/BREAKING_CHANGES.md) - Breaking changes documentation
