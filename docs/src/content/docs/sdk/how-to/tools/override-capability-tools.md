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
    "github.com/AltairaLabs/PromptKit/runtime/v2/memory"
    "github.com/AltairaLabs/PromptKit/runtime/v2/tools"
    "github.com/AltairaLabs/PromptKit/sdk/v2"
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

```
WARN tool descriptor override skipped: tool not registered  name=memory__remember
```

A renamed tool is not the only thing that produces this. The capability that owns the tool may have declined to register it, in which case the tool name is still correct for your PromptKit version and the override is a symptom rather than the cause. Check the capability's own preconditions first — they are logged at WARN by the capability itself, immediately before this line:

```
WARN memory tools skipped: scope has no subject key
     expected_key=user_id scope_keys=[agent_id,virtual_user_id,workspace_id]
```

Memory registers its tools only when the scope carries a subject; see [`WithMemorySubjectKey`](/sdk/reference/conversation-manager/#WithMemorySubjectKey) when your scope map spells that key differently. `WithMemoryToolsDisabled` suppresses them outright. Skills register nothing without a discovered skill source, and A2A registers nothing when the pack declares no matching agent.

## How it works

`WithToolDescriptorOverride` does not subvert the capability — the capability still registers its tools with their defaults. Once all capabilities have run `RegisterTools`, the SDK iterates the configured overrides and:

1. Looks up each tool by name in the registry. If absent, logs and skips.
2. Clones the descriptor (so mutations don't leak into shared registry state).
3. Calls the patch function on the clone.
4. Re-registers the patched descriptor (`Registry.Register` is last-write-wins, so this replaces the original).

## Getting a new parameter to the host

Adding a field to the input schema makes the model send it. Whether anything downstream can read it depends on the tool: the executor decodes the arguments it knows about, and several route what is left over to the host rather than dropping it.

| Tool | Unknown top-level args arrive in |
|---|---|
| `memory__remember` | `Memory.Metadata` (merged with the typed `metadata` arg; typed keys win) |
| `memory__recall` | `RetrieveOptions.Extras` |
| `memory__list` | `ListOptions.Extras` |
| `memory__forget` | `DeleteOptions.Extras`, for stores implementing `memory.ExtrasDeleter` |
| A2A outgoing tools | `Message.Metadata` on the wire to the remote agent |
| `workflow__transition` | `TransitionResult.HostExtras`, readable on the `workflow.transitioned` event |
| `workflow__set_artifact`, the skills tools | nowhere — the field reaches the model but not you |

[Memory and Grounding](/sdk/how-to/conversations/use-memory/#backend-specific-arguments) works the memory case through end to end.

For the tools in the last row, extending the schema is a documentation change only: the model will populate the field and the value is discarded. File an issue if you need one of them, so the observation point can be designed rather than guessed.

## Does not affect

- The executor that handles the tool. The executor remains the one the capability registered — it gains no new typed behavior, only the passthrough above.
- Other tools — patches operate on a clone of one descriptor.
- Capability lifecycle (Init, Close).
