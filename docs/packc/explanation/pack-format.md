---
layout: docs
title: Pack Format
parent: Explanation
grand_parent: PackC
nav_order: 1
---

# Pack Format

Understanding the structure and design of `.pack.json` files.

## Overview

A pack is a compiled, optimized JSON file containing one or more prompts ready for production use with the PromptKit SDK.

## Design Goals

### 1. Self-Contained

Packs include everything needed to execute prompts:

```json
{
  "id": "customer-support",
  "version": "1.0.0",
  "compiler_version": "packc-v0.1.0",
  "prompts": { ... },
  "fragments": { ... },
  "metadata": { ... }
}
```

No external dependencies required at runtime.

### 2. Optimized for Loading

- **Single file** - No directory scanning
- **JSON format** - Fast parsing
- **Minimal size** - Quick downloads
- **Indexed access** - O(1) prompt lookup by ID

### 3. Version Trackable

```json
{
  "version": "1.2.0",
  "compiler_version": "packc-v0.1.0",
  "metadata": {
    "compiled_at": "2025-01-16T10:30:00Z"
  }
}
```

Know exactly what version and when it was compiled.

## Pack Structure

### Top-Level Fields

```json
{
  "id": "string",              // Pack identifier
  "version": "string",         // Semantic version
  "name": "string",            // Optional display name
  "description": "string",     // Optional description
  "compiler_version": "string", // PackC version used
  "template_engine": "string", // go | python | jinja2
  "prompts": {},               // Prompt definitions (map)
  "fragments": {},             // Reusable fragments (map)
  "metadata": {}               // Additional metadata
}
```

### Prompt Structure

Each prompt in the `prompts` map:

```json
{
  "prompts": {
    "task-id": {
      "id": "task-id",
      "name": "Display Name",
      "description": "What this prompt does",
      "system": "System prompt text",
      "user_template": "User: {{.input}}",
      "template_engine": "go",
      "parameters": {
        "temperature": 0.7,
        "max_tokens": 1000
      },
      "tools": ["tool1", "tool2"],
      "fragments": ["fragment-id"]
    }
  }
}
```

## Why JSON?

### Advantages

1. **Universal** - Supported by all languages
2. **Human-readable** - Easy to inspect
3. **Fast** - Native parsing in most runtimes
4. **Standard** - Well-defined spec (RFC 8259)
5. **Tooling** - jq, validators, formatters

### Alternatives Considered

**YAML** - More readable but slower to parse, more parsing edge cases

**Binary (Protobuf/MessagePack)** - Faster but not human-readable, requires codegen

**TOML** - Good for config but limited nesting support

**Decision**: JSON balances readability, performance, and compatibility.

## Template Engine Support

Packs specify template engine at pack level:

```json
{
  "template_engine": "go"  // Default for all prompts
}
```

Individual prompts can override:

```json
{
  "prompts": {
    "special-prompt": {
      "template_engine": "jinja2"  // Override for this prompt
    }
  }
}
```

Supported engines:
- **go** - Go templates (default)
- **python** - Python string formatting
- **jinja2** - Jinja2 templates

## Fragments

Reusable prompt components:

```json
{
  "fragments": {
    "company-info": {
      "content": "Company: {{.company_name}}...",
      "description": "Standard company information"
    }
  },
  "prompts": {
    "support": {
      "system": "{{.fragments.company-info}}\n\nYou are support...",
      "fragments": ["company-info"]
    }
  }
}
```

Benefits:
- **DRY** - Define once, use many times
- **Consistency** - Same content across prompts
- **Maintainability** - Update in one place

## Metadata

Pack metadata for tracking:

```json
{
  "metadata": {
    "compiled_at": "2025-01-16T10:30:00Z",
    "compiler_version": "packc-v0.1.0",
    "source_files": ["prompts/support.yaml"],
    "git_commit": "abc123",
    "build_number": "42",
    "environment": "production"
  }
}
```

Custom metadata is allowed for tracking builds, deployments, etc.

## Pack Versioning

### Semantic Versioning

```
MAJOR.MINOR.PATCH

2.1.3
│ │ │
│ │ └─ Patch: Bug fixes, no API changes
│ └─── Minor: New features, backward compatible
└───── Major: Breaking changes
```

### Version Constraints

Packs should declare SDK compatibility:

```json
{
  "version": "2.1.0",
  "sdk_version": ">=0.1.0 <2.0.0"
}
```

### Schema Versions

Pack format itself is versioned:

```json
{
  "schema_version": "1.0"  // Pack format version
}
```

Allows pack format evolution while maintaining compatibility.

## Size Considerations

### Typical Sizes

- **Single prompt**: 1-5 KB
- **Small pack (3-5 prompts)**: 10-20 KB
- **Medium pack (10-20 prompts)**: 50-100 KB
- **Large pack (50+ prompts)**: 200-500 KB

### Optimization Strategies

1. **Remove whitespace** - Minimize JSON
2. **Share fragments** - Reuse common content
3. **Short IDs** - Use concise task_type names
4. **Trim descriptions** - Keep concise
5. **Separate packs** - Split large applications

### Size Limits

Recommended maximum: **1 MB per pack**

Reasons:
- Fast network transfer
- Quick parse time
- Memory efficient
- Easy to cache

## Security Considerations

### Code Injection

Templates can execute code:

```json
{
  "user_template": "{{.input}}"  // Safe
  "user_template": "{{exec .cmd}}"  // Dangerous!
}
```

PackC validates templates for safety.

### Data Exposure

Avoid sensitive data in packs:

```json
{
  "api_key": "secret123"  // DON'T DO THIS
}
```

Use environment variables or secrets management instead.

## Pack Signing (Future)

Planned features for pack integrity:

```json
{
  "signature": {
    "algorithm": "sha256",
    "hash": "abc123...",
    "signed_by": "team@company.com"
  }
}
```

Enables:
- Verify pack authenticity
- Detect tampering
- Trust chain verification

## Evolution

The pack format is designed to evolve:

### Forward Compatibility

Newer packs work with older SDKs (best effort):

```json
{
  "schema_version": "1.1",  // SDK 1.0 ignores unknown fields
  "new_feature": "..."
}
```

### Backward Compatibility

Older packs work with newer SDKs:

```json
{
  "schema_version": "1.0"  // SDK 1.1 supports old format
}
```

### Migration Path

When breaking changes are needed:

1. Announce deprecation
2. Support both versions (1 year)
3. Provide migration tool
4. Release new major version

## Comparison with Other Formats

### vs. Raw YAML/JSON Prompts

| Pack Format | Raw Files |
|-------------|-----------|
| ✅ Compiled, optimized | ❌ Requires parsing |
| ✅ Single file | ❌ Multiple files |
| ✅ Validated | ❌ May have errors |
| ✅ Versioned | ❌ No version info |
| ✅ Production ready | ❌ Dev-time only |

### vs. Langchain Prompt Templates

| Pack Format | Langchain |
|-------------|-----------|
| ✅ Language agnostic | ❌ Python-specific |
| ✅ Multi-prompt | ❌ Single template |
| ✅ Self-contained | ❌ Code dependencies |
| ✅ Compiled | ❌ Runtime parsing |

### vs. OpenAI Prompt Files

| Pack Format | OpenAI Files |
|-------------|--------------|
| ✅ Multi-model | ❌ OpenAI-specific |
| ✅ Tool definitions | ❌ Limited metadata |
| ✅ Fragments | ❌ No reuse mechanism |
| ✅ Versioned | ❌ No versioning |

## Best Practices

### 1. Keep Packs Focused

One pack per application or feature:

```
✅ customer-support.pack.json
✅ sales-automation.pack.json

❌ all-prompts.pack.json
```

### 2. Use Semantic Versioning

```
1.0.0 → 1.0.1  // Bug fix
1.0.1 → 1.1.0  // New prompt added
1.1.0 → 2.0.0  // Removed prompt
```

### 3. Include Metadata

```json
{
  "metadata": {
    "compiled_at": "...",
    "git_commit": "...",
    "environment": "production"
  }
}
```

### 4. Validate Before Deploy

```bash
packc validate pack.json
```

### 5. Version Control Packs

Commit packs to git for traceability:

```bash
git add packs/prod/*.pack.json
git commit -m "Release v1.2.0"
git tag v1.2.0
```

## Summary

The pack format is designed to be:

- **Self-contained** - Everything needed in one file
- **Optimized** - Fast loading and parsing
- **Versioned** - Track changes over time
- **Standard** - JSON for universal support
- **Evolvable** - Forward and backward compatible

This design enables reliable, efficient prompt deployment in production systems.
