---
title: Compilation Architecture
sidebar:
  order: 2
---
Understanding how PackC compiles prompts into packs.

## Overview

PackC transforms human-friendly YAML configurations into validated JSON packs through a multi-stage compilation pipeline.

## Compilation Pipeline

```
YAML Files → Parser → Validator → Pack Builder → JSON Output
```

### Stage 1: Configuration Loading

**Input**: arena.yaml + prompt YAML files

```yaml
# arena.yaml
prompts:
  - prompts/support.yaml
  - prompts/sales.yaml
```

**Process**:
1. Read arena.yaml
2. Resolve file paths (relative to arena.yaml)
3. Load each prompt YAML
4. Parse YAML to PromptConfig structs

**Output**: In-memory PromptConfig objects

### Stage 2: Prompt Registry

**Purpose**: Central repository of all prompts

```go
repo := memory.NewPromptRepository()
registry := prompt.NewRegistryWithRepository(repo)
```

**Features**:
- Deduplication by task_type
- Fast lookup by ID
- Validation on registration

### Stage 3: Validation

**Checks**:
- Required fields present (task_type, system_template)
- Template syntax valid
- Parameter types correct
- Tool references valid
- Media files exist

**Output**: Validated PromptConfig objects or errors

### Stage 4: Pack Assembly

**Process**:
1. Create pack structure
2. Add each prompt to prompts map
3. Add fragments map
4. Add metadata (compiler version, timestamp)
5. Assign pack ID and version

**Output**: Complete Pack object

### Stage 5: JSON Serialization

**Format**: Indented JSON for readability

```go
json.MarshalIndent(pack, "", "  ")
```

**Options**:
- Indent: 2 spaces
- Escape HTML: false
- Sort keys: consistent ordering

**Output**: .pack.json file

## Compiler Components

### PromptConfig Parser

Parses YAML to PromptConfig:

```go
func ParseConfig(data []byte) (*Config, error)
```

Handles:
- YAML unmarshaling
- Type conversion
- Default values
- Validation

### Pack Compiler

Orchestrates compilation:

```go
compiler := prompt.NewPackCompiler(registry)
pack, err := compiler.CompileFromRegistry(packID, compilerVersion)
```

Responsibilities:
- Iterate through registry
- Transform prompts
- Build pack structure
- Add metadata

### Validator

Validates prompts and packs:

```go
warnings := pack.Validate()
```

Returns list of validation warnings (non-fatal) or errors (fatal).

## Template Processing

### Go Templates

Default template engine:

```yaml
system_template: |
  You are a helpful assistant.
  User: {{user_name}}
  Message: {{message}}
```

**Processing**:
1. Parse template text
2. Check syntax errors
3. Store as string (runtime parsing)

**Why not pre-compile?**
- Templates need runtime data
- Keeps packs language-agnostic
- SDK handles execution

### Template Validation

PackC validates template syntax:

```go
_, err := template.New("test").Parse(tmpl)
```

But doesn't execute (no runtime data available).

## Fragment Handling

### Fragment Definition

```yaml
# fragment.yaml
fragments:
  company-info:
    content: "Company: "
    description: "Standard company info"
```

### Fragment Compilation

Two strategies:

**1. Inline (default)**

Fragment content embedded in prompt:

```json
{
  "prompts": {
    "support": {
      "system_template": "Company: {{company_name}}\n\nYou are support..."
    }
  }
}
```

**2. Reference**

Fragment stored separately:

```json
{
  "fragments": {
    "company-info": "Company: {{company_name}}..."
  },
  "prompts": {
    "support": {
      "system_template": "{{company-info}}\n\nYou are support..."
    }
  }
}
```

**Tradeoff**:
- Inline: Faster execution, larger size
- Reference: Smaller size, runtime lookup

## Error Handling

### Fatal Errors

Stop compilation immediately:

- Invalid YAML syntax
- Missing required fields
- Template parse errors
- File not found

### Warnings

Allow compilation but report issues:

- Missing descriptions
- Undefined tools
- Missing media files
- Large prompt size

### Error Context

Provide helpful error messages:

```
Error parsing prompt config: yaml: line 5: mapping values are not allowed
File: prompts/support.yaml
Line: 5
```

## Memory Management

### Small Footprint

PackC uses minimal memory:

1. **Streaming YAML parsing** - Process files one at a time
2. **Lazy loading** - Load prompts on demand
3. **Garbage collection** - Release after compilation
4. **No caching** - Don't hold data post-compile

### Large Projects

For projects with 100+ prompts:

- Use single-prompt compilation for testing
- Compile subsets during development
- Full compilation only for releases

## Compilation Modes

### Standard Mode

```bash
packc compile --config arena.yaml --output pack.json --id app
```

- Full validation
- Complete metadata

## Deterministic Builds

PackC produces deterministic output:

**Given**:
- Same source files
- Same packc version
- Same compilation flags

**Result**:
- Identical pack.json output
- Same checksums
- Reproducible builds

**Implementation**:
- Sorted keys in JSON
- Fixed timestamp format
- Consistent whitespace

## Debugging Compilation

Use `packc inspect` to view the contents of a compiled pack:

```bash
packc inspect packs/app.pack.json
```

Use `packc validate` to check a pack for errors:

```bash
packc validate packs/app.pack.json
```

## Build Reproducibility

### Version Locking

Lock packc version:

```yaml
# .packc-version
0.1.0
```

### Input Hashing

Track source file changes:

```bash
find prompts/ -type f -exec sha256sum {} \; > prompts.sha256
```

### Build Manifest

Generate build info:

```json
{
  "packc_version": "0.1.0",
  "source_files": ["prompts/support.yaml"],
  "file_hashes": {"prompts/support.yaml": "abc123..."},
  "build_time": "2025-01-16T10:30:00Z",
  "build_machine": "ci-runner-01"
}
```

## Compiler Architecture

### Modular Design

```
PackCompiler
├── Parser (YAML → PromptConfig)
├── Validator (checks)
├── Assembler (build pack)
└── Serializer (write JSON)
```

Each component is:
- Independent
- Testable
- Replaceable

### Extension Points

1. **Custom parsers** - Support other input formats
2. **Custom validators** - Add validation rules
3. **Custom serializers** - Output other formats

## Comparison with Other Compilers

### vs. TypeScript Compiler

| Feature | PackC | tsc |
|---------|-------|-----|
| Input | YAML | TypeScript |
| Output | JSON | JavaScript |
| Type checking | Limited | Full |
| Optimization | Minimal | Extensive |
| Speed | ~100ms | ~1-10s |

### vs. Babel

| Feature | PackC | Babel |
|---------|-------|-------|
| Input | YAML | JavaScript |
| Output | JSON | JavaScript |
| Transformations | Few | Many |
| Plugins | Planned | Extensive |
| Speed | Fast | Moderate |

## Summary

PackC's compilation architecture is:

- **Pipeline-based** - Clear stages from YAML to JSON
- **Validated** - Multiple validation checkpoints
- **Extensible** - Modular components
- **Deterministic** - Reproducible builds
- **Fast** - Milliseconds for typical projects

This design ensures reliable, consistent pack generation for production use.
