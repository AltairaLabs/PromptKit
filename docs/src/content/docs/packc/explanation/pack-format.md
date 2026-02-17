---
title: Pack Format
description: Understanding the PromptPack specification and .pack.json structure
sidebar:
  order: 1
---

Understanding the [PromptPack](https://promptpack.org) specification and `.pack.json` structure.

## The PromptPack Standard

[PromptPack](https://promptpack.org) is an **open specification** for packaging AI prompts in a vendor-neutral, framework-agnostic format. PackC compiles source files into packs that conform to this standard.

### Why a Standard?

Today's AI prompt development is fragmented:

- Each framework uses its own format
- Switching providers means rebuilding prompts
- No consistent way to version or test prompts
- Prompts are treated as disposable, not engineered assets

[PromptPack](https://promptpack.org) solves this with a universal JSON format that works across **any** runtime or provider.

### Core Principles

From the [PromptPack specification](https://promptpack.org):

1. **Vendor Neutrality**: Works with any AI framework or provider
2. **Completeness**: Everything needed in a single file—prompts, tools, guardrails
3. **Discipline**: Treat prompts as version-controlled, testable engineering artifacts

## Pack Structure

A PromptPack-compliant `.pack.json` file:

```json
{
  "$schema": "https://promptpack.org/schema/latest/promptpack.schema.json",
  "id": "customer-support",
  "name": "Customer Support Pack",
  "version": "v1.0.0",
  "template_engine": {
    "version": "v1",
    "syntax": "{{variable}}"
  },
  "prompts": {
    "greeting": {
      "id": "greeting",
      "name": "Greeting",
      "system_template": "You are a helpful support agent. Help the user with: {{query}}",
      "version": "v1.0.0"
    }
  }
}
```

### Top-Level Fields

| Field | Description |
|-------|-------------|
| `id` | Unique pack identifier |
| `name` | Human-readable pack name |
| `version` | Pack version (semver) |
| `prompts` | Map of prompt definitions |

### Prompt Structure

Each prompt in the `prompts` map (keyed by task type):

```json
{
  "id": "task-id",
  "name": "Display Name",
  "description": "What this prompt does",
  "version": "v1.0.0",
  "system_template": "System prompt text with {{variables}}",
  "variables": [
    {
      "name": "variable_name",
      "type": "string",
      "required": true
    }
  ],
  "tools": ["tool1", "tool2"]
}
```

## Workflows

Packs can include a `workflow` section that defines an event-driven state machine over prompts. Each state references a prompt task type, and transitions are triggered by named events.

```json
{
  "workflow": {
    "version": 1,
    "entry": "intake",
    "states": {
      "intake": {
        "prompt_task": "greeting",
        "description": "Initial customer contact",
        "on_event": {
          "Escalate": "specialist",
          "Resolve": "closed"
        },
        "orchestration": "internal"
      },
      "specialist": {
        "prompt_task": "specialist",
        "description": "Specialized support handling",
        "on_event": {
          "Resolve": "closed"
        },
        "persistence": "persistent",
        "orchestration": "external"
      },
      "closed": {
        "prompt_task": "farewell",
        "description": "Conversation complete"
      }
    }
  }
}
```

### Workflow Fields

| Field | Description |
|-------|-------------|
| `version` | Workflow spec version (currently `1`) |
| `entry` | Name of the initial state |
| `states` | Map of state definitions |

### State Fields

| Field | Description |
|-------|-------------|
| `prompt_task` | References a prompt in the `prompts` map |
| `description` | Human-readable state description |
| `on_event` | Map of event name to target state |
| `persistence` | `"transient"` or `"persistent"` (storage hint) |
| `orchestration` | `"internal"`, `"external"`, or `"hybrid"` (control mode) |

A state with no `on_event` entries is a **terminal state** — the workflow is complete when it reaches one.

### Orchestration Modes

- **`internal`** (default) — The agent drives transitions automatically
- **`external`** — External callers (HTTP, queues) drive transitions via the SDK
- **`hybrid`** — Mixed control; both agent and external callers can trigger transitions

## Portability

The key benefit of PromptPack is **portability**. A pack compiled with PackC works with:

- **PromptKit SDK** (Go) — The reference implementation
- **Other PromptPack runtimes** — Any language that reads the spec
- **Custom integrations** — Parse the JSON directly

```
                    ┌─────────────────────────────────────────────┐
                    │           .pack.json (PromptPack)           │
                    │         Vendor-Neutral, Portable            │
                    └─────────────────┬───────────────────────────┘
                                      │
          ┌───────────────────────────┼───────────────────────────┐
          ▼                           ▼                           ▼
   ┌─────────────┐            ┌─────────────┐            ┌─────────────┐
   │ PromptKit   │            │ Python      │            │ Your Custom │
   │ (Go)        │            │ Runtime     │            │ Integration │
   └─────────────┘            └─────────────┘            └─────────────┘
```

No vendor lock-in. Build once, deploy everywhere.

## Why JSON?

The [PromptPack spec](https://promptpack.org) uses JSON because:

1. **Universal** — Supported by all programming languages
2. **Human-readable** — Easy to inspect and debug
3. **Fast** — Native parsing in most runtimes
4. **Standard** — Well-defined (RFC 8259)
5. **Tooling** — jq, validators, formatters

### Alternatives Considered

| Format | Why Not |
|--------|---------|
| YAML | Slower parsing, more edge cases |
| Binary (Protobuf) | Not human-readable, requires codegen |
| TOML | Limited nesting support |

## Fragments

Reusable prompt components, defined once and referenced by multiple prompts:

```json
{
  "fragments": {
    "company-info": "You work for Acme Corp, a leader in..."
  },
  "prompts": {
    "support": {
      "id": "support",
      "system_template": "{{company-info}}\n\nYou are a support agent...",
      "version": "v1.0.0"
    }
  }
}
```

Benefits:
- **DRY** — Define once, use many times
- **Consistency** — Same content across prompts
- **Maintainability** — Update in one place

## Versioning

### Semantic Versioning

Packs use semantic versioning:

```
MAJOR.MINOR.PATCH

2.1.3
│ │ │
│ │ └─ Patch: Bug fixes, no API changes
│ └─── Minor: New features, backward compatible
└───── Major: Breaking changes
```

### Schema Versions

The PromptPack format itself is versioned via `$schema` and `compilation.schema`:

```json
{
  "$schema": "https://promptpack.org/schema/latest/promptpack.schema.json",
  "compilation": {
    "schema": "v1"
  }
}
```

This allows the specification to evolve while maintaining compatibility.

## Comparison with Other Formats

### vs. Framework-Specific Prompts

| PromptPack | Framework-Specific |
|------------|-------------------|
| ✅ Portable — works anywhere | ❌ Locked to one framework |
| ✅ Standard format | ❌ Proprietary format |
| ✅ Versioned | ❌ Often unversioned |
| ✅ Compiled & validated | ❌ Runtime parsing |

### vs. Raw YAML/JSON Files

| PromptPack | Raw Files |
|------------|-----------|
| ✅ Compiled, optimized | ❌ Requires parsing at runtime |
| ✅ Single file | ❌ Multiple files to manage |
| ✅ Schema validated | ❌ May have errors |
| ✅ Versioned | ❌ No version info |

### vs. Langchain Templates

| PromptPack | Langchain |
|------------|-----------|
| ✅ Language agnostic | ❌ Python-specific |
| ✅ Multi-prompt bundles | ❌ Single templates |
| ✅ Self-contained | ❌ Code dependencies |

## Best Practices

### 1. Follow the Specification

Use the [PromptPack spec](https://promptpack.org) for maximum portability:

```json
{
  "$schema": "https://promptpack.org/schema/latest/promptpack.schema.json",
  "id": "my-pack",
  "name": "My Pack",
  "version": "v1.0.0"
}
```

### 2. Keep Packs Focused

One pack per application or feature:

```
✅ customer-support.pack.json
✅ sales-automation.pack.json

❌ all-prompts.pack.json
```

### 3. Use Semantic Versioning

```
1.0.0 → 1.0.1  // Bug fix
1.0.1 → 1.1.0  // New prompt added
1.1.0 → 2.0.0  // Breaking change
```

### 4. Validate Before Deploy

```bash
packc validate pack.json
```

## Learn More

- **PromptPack Specification**: [promptpack.org](https://promptpack.org)
- **PackC Reference**: [Compile Command](/packc/reference/compile/)
- **Validation**: [Validate Command](/packc/reference/validate/)
