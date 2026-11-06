---
title: Runtime Reference
sidebar:
  order: 0
verified:
  commit: 8489bf3d95c7498f9de0f024769db9399c24e884
  sources:
    - runtime/evals/types.go
    - runtime/hooks/guardrails/adapter.go
    - runtime/logger/logger.go
    - runtime/mcp/registry.go
    - runtime/mcp/types.go
    - runtime/memory/types.go
    - runtime/pipeline/stage/pipeline.go
    - runtime/pipeline/stage/stage.go
    - runtime/pipeline/stage/stages_provider.go
    - runtime/pipeline/stage/tool_call_rate.go
    - runtime/pipeline/types.go
    - runtime/providers/claude/claude.go
    - runtime/providers/gemini/gemini.go
    - runtime/providers/ollama/ollama.go
    - runtime/providers/openai/openai.go
    - runtime/providers/provider.go
    - runtime/providers/streaming.go
    - runtime/providers/vllm/vllm.go
    - runtime/telemetry/listener.go
---
Index of the PromptKit runtime API reference. The runtime is the Go module `github.com/AltairaLabs/PromptKit/runtime/v2`: a stage-based streaming pipeline, provider integrations (OpenAI, Claude, Gemini, Vertex, Bedrock, Ollama, vLLM and others), tool execution with MCP, state persistence, hooks and guardrails, and observability.

## Components

| Component | Description | Reference |
|-----------|-------------|-----------|
| Pipeline | Stage-based execution engine | [Pipeline](/runtime/reference/pipeline/) |
| Providers | LLM provider implementations | [Providers](/runtime/reference/providers/) |
| Tools | Function calling | [Tools](/runtime/reference/tools/) |
| MCP | Model Context Protocol integration | [MCP](/runtime/reference/mcp/) |
| Hooks | Provider, tool and session hooks, and guardrails | [Hooks](/runtime/reference/hooks/) |
| State store | Conversation persistence | [State Store](/runtime/reference/statestore/) |
| Storage | Media storage backends | [Storage](/runtime/reference/storage/) |
| Types | Core data structures | [Types](/runtime/reference/types/) |
| A2A | Client, types, tool bridge, mock | [A2A](/runtime/reference/a2a/) |
| Evals | Evaluation framework for PromptPack | [Evals](/runtime/reference/evals/) |
| Memory | Cross-session agentic memory | [Memory](/runtime/reference/memory/) |
| Memory corpus | Retriever over a fixed document set | [Memory Corpus](/runtime/reference/memory-corpus/) |
| Logging | Structured logging with context | [Logging](/runtime/reference/logging/) |
| Metrics | Prometheus metrics collection | [Metrics](/runtime/reference/metrics/) |
| Telemetry | OpenTelemetry trace export | [Telemetry](/runtime/reference/telemetry/) |
| Streaming | Duplex response state machine, audio streaming, tool execution | [Streaming](/runtime/reference/streaming/) |
| TTS | Text-to-speech service interface and providers | [TTS](/runtime/reference/tts/) |
| Audio | VAD, turn detection, interruption handling | [Audio](/runtime/reference/audio/) |
| Variables | Dynamic variable resolution for prompt templates | [Variables](/runtime/reference/variables/) |

## Import paths

```go
import (
    "github.com/AltairaLabs/PromptKit/runtime/v2/pipeline"
    "github.com/AltairaLabs/PromptKit/runtime/v2/pipeline/stage"
    "github.com/AltairaLabs/PromptKit/runtime/v2/providers"
    "github.com/AltairaLabs/PromptKit/runtime/v2/tools"
    "github.com/AltairaLabs/PromptKit/runtime/v2/mcp"
    "github.com/AltairaLabs/PromptKit/runtime/v2/statestore"
    "github.com/AltairaLabs/PromptKit/runtime/v2/hooks"
    "github.com/AltairaLabs/PromptKit/runtime/v2/hooks/guardrails"
    "github.com/AltairaLabs/PromptKit/runtime/v2/types"
    "github.com/AltairaLabs/PromptKit/runtime/v2/logger"
    "github.com/AltairaLabs/PromptKit/runtime/v2/telemetry"
)
```

## Provider calls

`Predict` returns a complete response. `PredictStream` returns a channel of `StreamChunk` values.

```go
package main

import (
    "context"
    "fmt"
    "log"

    "github.com/AltairaLabs/PromptKit/runtime/v2/providers"
    "github.com/AltairaLabs/PromptKit/runtime/v2/providers/openai"
    "github.com/AltairaLabs/PromptKit/runtime/v2/types"
)

func main() {
    provider := openai.NewProvider(
        "openai",
        "gpt-4o-mini",
        "", // default base URL; the API key comes from the environment
        providers.ProviderDefaults{Temperature: 0.7, MaxTokens: 1500},
        false, // includeRawOutput
    )
    defer provider.Close()

    ctx := context.Background()
    req := providers.PredictionRequest{
        Messages: []types.Message{{Role: "user", Content: "Hello!"}},
    }

    resp, err := provider.Predict(ctx, req)
    if err != nil {
        log.Fatal(err)
    }
    fmt.Println(resp.Content)

    streamChan, err := provider.PredictStream(ctx, req)
    if err != nil {
        log.Fatal(err)
    }
    for chunk := range streamChan {
        if chunk.Error != nil {
            log.Printf("Error: %v\n", chunk.Error)
            break
        }
        if chunk.Delta != "" {
            fmt.Print(chunk.Delta)
        }
        if chunk.FinishReason != nil {
            fmt.Printf("\nfinish reason: %s\n", *chunk.FinishReason)
        }
    }
}
```

### Provider defaults

`providers.ProviderDefaults` sets request parameters that apply when a request leaves the field at zero.

| Field | Type | Description |
|-------|------|-------------|
| `Temperature` | `float32` | Sampling temperature |
| `TopP` | `float32` | Nucleus sampling threshold |
| `MaxTokens` | `int` | Output-token limit. Zero or `providers.MaxTokensUnlimited` sends no limit, except on Claude, where zero falls back to 4096 |
| `Pricing` | `providers.Pricing` | `InputCostPer1K` and `OutputCostPer1K` for cost calculation |
| `DisablePromptCaching` | `bool` | Disables Anthropic prompt caching |

## MCP servers

```go
mcpRegistry := mcp.NewRegistry()
defer mcpRegistry.Close()

err := mcpRegistry.RegisterServer(mcp.ServerConfig{
    Name:    "filesystem",
    Command: "npx",
    Args:    []string{"-y", "@modelcontextprotocol/server-filesystem", "/allowed"},
})
if err != nil {
    log.Fatal(err)
}

serverTools, err := mcpRegistry.ListAllTools(ctx)
if err != nil {
    log.Fatal(err)
}
for serverName, tools := range serverTools {
    log.Printf("Server %s has %d tools\n", serverName, len(tools))
}
```

## Tool policy

`pipeline.ToolPolicy` constrains tool use in the provider stage. Pass it as the third argument to `stage.NewProviderStage`.

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `ToolChoice` | `string` | `"auto"` | `"auto"`, `"required"`, `"none"` or a tool name |
| `MaxRounds` | `int` | 50 | Maximum LLM-to-tool rounds per turn |
| `MaxToolCallsPerTurn` | `int` | 0 (no cap) | Maximum tool calls across all rounds of a turn |
| `MaxParallelToolCalls` | `int` | 10 | Maximum concurrent tool executions |
| `MaxCallsPerMinute` | `int` | 0 (unlimited) | Maximum calls to each tool in any one-minute window of a conversation; a call over the limit is not executed, and the model is told the tool's limit is reached |
| `MaxCostUSD` | `float64` | 0 (unlimited) | Cumulative cost budget; the turn fails with an error once exceeded |
| `Blocklist` | `[]string` | none | Tool names the stage refuses to execute; the model receives a "blocked by policy" result |
| `MaxIdenticalToolCalls` | `int` | 3 | The call that repeats the same tool and arguments this many times is skipped; the model receives a stop-repeating result; a second loop after that fails the turn |
| `StopOnTool` | `string` | none | Ends the tool loop after the round in which this tool was called |

A prompt's `tool_policy` narrows these limits. `MaxRounds` and `MaxToolCallsPerTurn` take the smaller non-zero value, `Blocklist` is the union, and the caller's `ToolChoice` wins when set.

```go
policy := &pipeline.ToolPolicy{
    ToolChoice:           "auto",
    MaxRounds:            5,
    MaxToolCallsPerTurn:  10,
    MaxParallelToolCalls: 5,
    MaxCostUSD:           1.00,
    Blocklist:            []string{"dangerous_tool"},
}
```

## Related

- [Pipeline](/runtime/reference/pipeline/): stage and pipeline API
- [Providers](/runtime/reference/providers/): provider implementations
- [Tools](/runtime/reference/tools/): tool registry and execution
- [MCP](/runtime/reference/mcp/): Model Context Protocol integration
- [Types](/runtime/reference/types/): core data structures
- [How-to guides](/runtime/how-to/), [tutorials](/runtime/tutorials/) and [explanation](/runtime/explanation/)
