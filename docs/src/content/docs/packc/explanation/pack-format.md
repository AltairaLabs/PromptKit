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
  "apiVersion": "promptkit.altairalabs.ai/v1alpha1",
  "kind": "PromptPack",
  "metadata": {
    "name": "customer-support",
    "version": "1.0.0"
  },
  "prompts": [
    {
      "id": "greeting",
      "system": "You are a helpful support agent.",
      "template": "Help the user with: {{.query}}"
    }
  ],
  "tools": [...],
  "fragments": {...}
}
```

### Top-Level Fields

| Field | Description |
|-------|-------------|
| `apiVersion` | PromptPack schema version |
| `kind` | Always `"PromptPack"` |
| `metadata` | Name, version, description |
| `prompts` | Array of prompt definitions |
| `tools` | Optional tool specifications |
| `fragments` | Reusable prompt components |

### Prompt Structure

Each prompt in the `prompts` array:

```json
{
  "id": "task-id",
  "name": "Display Name",
  "description": "What this prompt does",
  "system": "System prompt text",
  "template": "User message template with {{.variables}}",
  "parameters": {
    "temperature": 0.7,
    "max_tokens": 1000
  },
  "tools": ["tool1", "tool2"]
}
```

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
    "company-info": {
      "content": "You work for Acme Corp, a leader in...",
      "description": "Standard company information"
    }
  },
  "prompts": [
    {
      "id": "support",
      "system": "{{fragment:company-info}}\n\nYou are a support agent...",
      "fragments": ["company-info"]
    }
  ]
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

The PromptPack format itself is versioned via `apiVersion`:

```json
{
  "apiVersion": "promptkit.altairalabs.ai/v1alpha1"
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
  "apiVersion": "promptkit.altairalabs.ai/v1alpha1",
  "kind": "PromptPack",
  "metadata": { "name": "...", "version": "..." }
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
packc validate pack.json --strict
```

## Learn More

- **PromptPack Specification**: [promptpack.org](https://promptpack.org)
- **PackC Reference**: [Compile Command](/packc/reference/compile/)
- **Validation**: [Validate Command](/packc/reference/validate/)
