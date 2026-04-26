---
title: Override Capability Tool Definitions
sidebar:
  order: 4
---

Capabilities like memory, workflow, and A2A register their tools with hard-coded descriptors (description, input/output schema). When you need to customize those — for example, to add a domain-specific parameter that downstream consumers rely on — you don't need to fork PromptKit. Use `WithToolDescriptorOverride` to patch the descriptor after the capability registers it.

## When to use this

- The default tool description doesn't fit your deployment's wording or policy.
- You need to add or constrain an input parameter (e.g. an `enum` validating allowed values).
- You want to relabel the namespace or augment the output schema.
- Consumer code (Omnia, internal services) reads a non-standard parameter and the LLM should know to set it.

## Basic example

```go
import (
    "github.com/AltairaLabs/PromptKit/runtime/memory"
    "github.com/AltairaLabs/PromptKit/runtime/tools"
    "github.com/AltairaLabs/PromptKit/sdk"
)

conv, err := sdk.Open(packPath, "chat",
    sdk.WithMemory(store, scope),
    sdk.WithToolDescriptorOverride(memory.RememberToolName,
        func(d *tools.ToolDescriptor) {
            d.Description = "Store something in memory; tag it with a category."
        }),
)
```

The patch function receives a clone of the descriptor that the capability registered. Mutate fields in place; the SDK re-registers the patched descriptor before the first `Send()`.

## Replacing the input schema

```go
import "encoding/json"

categorySchema := json.RawMessage(`{
    "type": "object",
    "properties": {
        "content":  {"type": "string"},
        "category": {
            "type": "string",
            "enum": ["memory:health", "memory:identity", "memory:preferences"]
        }
    },
    "required": ["content", "category"]
}`)

sdk.WithToolDescriptorOverride(memory.RememberToolName,
    func(d *tools.ToolDescriptor) {
        d.InputSchema = categorySchema
    })
```

## Composing overrides

Multiple overrides for the same tool compose in registration order — the second sees the descriptor already mutated by the first:

```go
sdk.WithToolDescriptorOverride("memory__remember",
    func(d *tools.ToolDescriptor) { d.Description = "step 1" }),
sdk.WithToolDescriptorOverride("memory__remember",
    func(d *tools.ToolDescriptor) { d.Description += " | step 2" }),
// Final description: "step 1 | step 2"
```

## Tolerance to version skew

If you reference a tool name that doesn't exist in the registry (for example, a tool that was renamed or removed in a newer PromptKit release), the override is logged at WARN level and skipped. Other overrides still apply. This means override lists survive PromptKit upgrades without breaking the consumer build.

## How it works

`WithToolDescriptorOverride` does not subvert the capability — the capability still registers its tools with their defaults. Once all capabilities have run `RegisterTools`, the SDK iterates the configured overrides and:

1. Looks up each tool by name in the registry. If absent, logs and skips.
2. Clones the descriptor (so mutations don't leak into shared registry state).
3. Calls the patch function on the clone.
4. Re-registers the patched descriptor (`Registry.Register` is last-write-wins, so this replaces the original).

## Does not affect

- The executor that handles the tool. The executor remains the one the capability registered. If you need the executor to recognise a new parameter, you must change the capability code (or wrap the capability — see [Capabilities reference](/sdk/reference/capabilities/)).
- Other tools — patches operate on a clone of one descriptor.
- Capability lifecycle (Init, Close).
