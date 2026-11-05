---
title: Override capability tool definitions
description: Patch the description and schemas of tools that memory, workflow, A2A and skills register.
sidebar:
  order: 4
verified:
  commit: 004b9cbbd4d40f770a2e3c76053b600a55189355
  sources:
    - runtime/memory/store.go
    - runtime/memory/tools.go
    - sdk/capability_memory.go
    - sdk/conversation.go
    - sdk/conversation_tools.go
    - sdk/options.go
    - sdk/tool_descriptor_override.go
---

Capabilities such as memory, workflow and A2A register their tools with built-in descriptors (description, input schema, output schema). Use `WithToolDescriptorOverride` to patch a descriptor after the capability registers it, for example to add a domain-specific parameter or constrain a value with an `enum`.

## Prerequisites

- A conversation that uses a capability whose tool you want to change, such as `sdk.WithMemory`.
- The tool's registered name, such as `memory__remember` (the constant `memory.RememberToolName`).

## 1. Patch the description

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

The patch function receives a clone of the registered descriptor. Mutate its fields in place.

## 2. Replace the input schema

Assign a new `json.RawMessage` to `InputSchema`:

```go
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

## 3. Compose overrides

Overrides for the same tool apply in the order you pass them. The second sees the descriptor the first mutated:

```go
sdk.WithToolDescriptorOverride("memory__remember",
    func(d *tools.ToolDescriptor) { d.Description = "step 1" }),
sdk.WithToolDescriptorOverride("memory__remember",
    func(d *tools.ToolDescriptor) { d.Description += " | step 2" }),
// Final description: "step 1 | step 2"
```

## 4. Read the new parameter in your host

Adding a field to the input schema makes the model send it. Whether your code can read it depends on the tool. The executor decodes the arguments it knows about, and several route the remaining top-level arguments to the host:

| Tool | Unknown top-level args arrive in |
|---|---|
| `memory__remember` | `Memory.Metadata` (merged with the typed `metadata` arg; typed keys win) |
| `memory__recall` | `RetrieveOptions.Extras` |
| `memory__list` | `ListOptions.Extras` |
| `memory__forget` | `DeleteOptions.Extras`, for stores implementing `memory.ExtrasDeleter` |
| A2A outgoing tools | `Message.Metadata` on the wire to the remote agent |
| `workflow__transition` | `TransitionResult.HostExtras`, readable on the `workflow.transitioned` event |
| `workflow__set_artifact`, the skills tools | nowhere: the model sees the field but your code does not |

[Memory and grounding](/sdk/how-to/conversations/use-memory/#backend-specific-arguments) walks through the memory case.

Overrides change the descriptor only. The executor stays the one the capability registered, and other tools and the capability lifecycle are unaffected.

## Verify

Print the descriptor the conversation registered:

```go
registry := conv.ToolRegistry()
fmt.Println(registry.Get(memory.RememberToolName).Description)
```

The output is your patched description.

## Troubleshoot

An override for a tool name that is not in the registry is skipped with a warning. Other overrides still apply, so an override list survives upgrades that rename or remove a tool:

```text
WARN tool descriptor override skipped: tool not registered  name=memory__remember
```

The name can also be correct while the capability declined to register the tool. The capability logs its own reason at WARN, immediately before the override warning:

```text
WARN memory tools skipped: scope has no subject key
     expected_key=user_id scope_keys=[agent_id,virtual_user_id,workspace_id]
```

Memory registers its tools only when the scope carries a subject. If your scope spells that key differently, declare it with [`WithMemorySubjectKey`](/sdk/reference/conversation-manager/#WithMemorySubjectKey). Pass `WithMemoryToolsDisabled` to `WithMemory` to suppress the tools. Skills register nothing without a discovered skill source, and A2A registers nothing when the pack declares no matching agent.

## Related

- [Memory and grounding](/sdk/how-to/conversations/use-memory/)
- [Register tools](/sdk/how-to/tools/register-tools/)
