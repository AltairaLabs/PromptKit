---
layout: default
title: "PackC CLI User Guide"
parent: Guides
nav_order: 3
---

## PromptKit PackC - CLI User Guide

PackC is the PromptKit compiler that transforms source files into optimized prompt packs. It supports multiple input formats, advanced compilation features, and generates portable prompt packages for various deployment targets.

## Quick Start

### Installation

```bash
# Install from source
go install github.com/AltairaLabs/PromptKit/tools/packc/cmd/packc@latest

# Or build locally
make build-packc
./bin/packc --help
```

### Basic Usage

```bash
# Compile a single prompt file
packc compile prompt.yaml -o mypack.json

# Compile entire directory
packc compile ./prompts/ -o output/

# Validate pack format
packc validate mypack.json

# Inspect pack contents
packc inspect mypack.json
```

## Command Reference

### `packc compile`

Compile source files into prompt packs.

**Syntax:**

```bash
packc compile <input> [flags]
```

**Flags:**

- `--output, -o` - Output file or directory (required)
- `--format` - Output format: json, yaml, binary (default: json)
- `--optimize` - Enable optimization passes
- `--minify` - Minify output (removes comments and whitespace)
- `--validate` - Validate pack after compilation
- `--include-source` - Include source code in pack for debugging
- `--target` - Target runtime: arena, sdk, generic (default: generic)
- `--schema-version` - Pack schema version (default: latest)

**Examples:**

```bash
# Basic compilation
packc compile prompt.yaml -o output.json

# Optimized binary pack
packc compile ./src/ -o release.pack --format binary --optimize

# Development pack with source
packc compile ./dev/ -o debug.json --include-source --validate

# Arena-specific compilation
packc compile scenarios/ -o arena-tests.json --target arena
```

### `packc validate`

Validate prompt pack format and content.

**Syntax:**

```bash
packc validate <pack-file> [flags]
```

**Flags:**

- `--strict` - Enable strict validation mode
- `--schema` - Custom schema file for validation
- `--output-format` - Validation report format: text, json, junit

**Examples:**

```bash
# Basic validation
packc validate mypack.json

# Strict validation with JUnit report
packc validate mypack.json --strict --output-format junit
```

### `packc inspect`

Inspect prompt pack contents and metadata.

**Syntax:**

```bash
packc inspect <pack-file> [flags]
```

**Flags:**

- `--format` - Output format: table, json, yaml
- `--show-content` - Display actual prompt content
- `--show-metadata` - Display pack metadata
- `--filter` - Filter by prompt name or tag

**Examples:**

```bash
# Basic inspection
packc inspect mypack.json

# Detailed content view
packc inspect mypack.json --show-content --format yaml

# Filter by tag
packc inspect mypack.json --filter "tag:customer-support"
```

### `packc extract`

Extract source files from a compiled pack.

**Syntax:**

```bash
packc extract <pack-file> <output-dir> [flags]
```

**Flags:**

- `--format` - Extract format: original, yaml, json
- `--include-metadata` - Include pack metadata files

**Examples:**

```bash
# Extract to YAML format
packc extract mypack.json ./extracted/ --format yaml

# Extract with metadata
packc extract mypack.json ./src/ --include-metadata
```

## Pack Format Specification

### Source File Formats

PackC supports multiple input formats for maximum flexibility:

#### YAML Format

```yaml
# prompt.yaml
name: "customer-greeting"
version: "1.0.0"
description: "Friendly customer service greeting"
tags: ["customer-support", "greeting"]

metadata:
  author: "support-team"
  created: "2024-01-15"
  category: "customer-service"

prompt:
  system: |
    You are a friendly customer service representative.
    Always greet customers warmly and professionally.
  
  user: |
    Hello, I need help with {{issue_type}}.
    
    Details: {{customer_details}}

variables:
  issue_type:
    type: "string"
    description: "Type of customer issue"
    required: true
    
  customer_details:
    type: "string"
    description: "Additional customer context"
    required: false
    default: "No additional details provided"

examples:
  - name: "billing-inquiry"
    variables:
      issue_type: "billing"
      customer_details: "Question about last month's charge"
    
  - name: "technical-support"
    variables:
      issue_type: "technical issue"
      customer_details: "App won't load on mobile device"
```

#### JSON Format

```json
{
  "name": "customer-greeting",
  "version": "1.0.0",
  "description": "Friendly customer service greeting",
  "tags": ["customer-support", "greeting"],
  "prompt": {
    "system": "You are a friendly customer service representative...",
    "user": "Hello, I need help with {{issue_type}}..."
  },
  "variables": {
    "issue_type": {
      "type": "string",
      "description": "Type of customer issue",
      "required": true
    }
  }
}
```

#### Markdown Format

```markdown
<!-- prompt.md -->
---
name: customer-greeting
version: 1.0.0
tags: [customer-support, greeting]
---

# Customer Service Greeting

## System Prompt

You are a friendly customer service representative.
Always greet customers warmly and professionally.

## User Prompt

Hello, I need help with {{issue_type}}.

Details: {{customer_details}}

## Variables

- **issue_type** (string, required): Type of customer issue
- **customer_details** (string, optional): Additional customer context
```

### Compiled Pack Format

PackC generates standardized prompt packs:

```json
{
  "schema_version": "1.1",
  "metadata": {
    "name": "customer-support-pack",
    "version": "1.2.0",
    "description": "Complete customer support prompts",
    "compiled_at": "2024-01-15T10:30:00Z",
    "compiler_version": "1.0.0",
    "source_hash": "abc123...",
    "target": "arena"
  },
  "prompts": [
    {
      "id": "greeting",
      "name": "customer-greeting",
      "version": "1.0.0",
      "tags": ["customer-support", "greeting"],
      "content": {
        "system": "You are a friendly...",
        "user": "Hello, I need help with {{issue_type}}..."
      },
      "variables": {
        "issue_type": {
          "type": "string",
          "required": true,
          "description": "Type of customer issue"
        }
      },
      "examples": [...],
      "metadata": {
        "author": "support-team",
        "category": "customer-service"
      }
    }
  ],
  "dependencies": [],
  "checksums": {
    "sha256": "def456..."
  }
}
```

## Advanced Features

### Template Processing

PackC supports advanced template processing with various engines:

#### Handlebars Templates

```yaml
name: "advanced-template"
template_engine: "handlebars"

prompt:
  system: |
    {{ "{{" }}#if technical_user}}
    You are a technical support specialist.
    {{else}}
    You are a general customer service agent.
    {{ "{{" }}/if}}
  
  user: |
    {{ "{{" }}#each issues}}
    Issue {{ "{{" }}@index}}: {{this.description}}
    {{ "{{" }}/each}}

variables:
  technical_user:
    type: "boolean"
    default: false
  
  issues:
    type: "array"
    items:
      type: "object"
      properties:
        description:
          type: "string"
```

#### Go Template Engine

```yaml
name: "go-template"
template_engine: "go"

prompt:
  user: |
    {{range .items}}
    - {{ "{{" }}.name}}: {{ "{{" }}.value}}
    {{end}}
    
    {{ "{{" }}if gt (len .items) 5}}
    Note: You have many items to process.
    {{end}}
```

### Optimization Passes

PackC includes several optimization passes:

#### Content Optimization

```bash
# Enable all optimizations
packc compile src/ -o optimized.json --optimize

# Specific optimizations
packc compile src/ -o output.json \
  --optimize-templates \
  --dedupe-content \
  --compress-metadata
```

#### Bundle Analysis

```bash
# Analyze pack size and content
packc analyze mypack.json

# Output:
# Pack Analysis Report
# ==================
# Total Size: 45.2 KB
# Prompts: 12
# Variables: 28
# Templates: 8
# Largest Prompt: customer-escalation (8.4 KB)
# Optimization Potential: 12% (5.4 KB)
```

### Multi-Pack Compilation

```bash
# Compile multiple packs with shared dependencies
packc compile-workspace ./workspace/ -o ./dist/

# Directory structure:
# workspace/
# ├── shared/
# │   └── common-variables.yaml
# ├── customer-support/
# │   └── prompts/
# └── technical-docs/
#     └── prompts/
```

## Integration Examples

### Arena Integration

```yaml
# arena.yaml - Using compiled packs
packs:
  - name: "customer-support"
    path: "./packs/customer-support.json"
  
  - name: "technical"
    path: "./packs/technical.json"

scenarios:
  - name: "support-test"
    pack: "customer-support"
    prompt: "greeting"
    variables:
      issue_type: "billing"
```

### SDK Integration

```go
// main.go - Loading packs in SDK
package main

import (
    "github.com/AltairaLabs/PromptKit/sdk"
)

func main() {
    // Load compiled pack
    pack, err := sdk.LoadPack("./customer-support.json")
    if err != nil {
        log.Fatal(err)
    }
    
    // Get prompt from pack
    prompt, err := pack.GetPrompt("greeting")
    if err != nil {
        log.Fatal(err)
    }
    
    // Execute with variables
    result, err := prompt.Execute(map[string]interface{}{
        "issue_type": "technical",
        "customer_details": "App crashes on startup",
    })
    if err != nil {
        log.Fatal(err)
    }
    
    fmt.Println(result.Content)
}
```

## Best Practices

### Project Organization

```text
project/
├── src/                    # Source prompt files
│   ├── shared/            # Shared components
│   ├── customer-support/  # Feature-specific prompts
│   └── technical/
├── packs/                 # Compiled packs
├── tests/                 # Test scenarios
└── packc.yaml            # Pack configuration
```

### Configuration File

```yaml
# packc.yaml
project:
  name: "my-prompts"
  version: "1.0.0"

compilation:
  source_dir: "./src"
  output_dir: "./packs"
  optimize: true
  validate: true

targets:
  - name: "development"
    format: "json"
    include_source: true
  
  - name: "production"
    format: "binary"
    minify: true
    optimize: true

validation:
  strict: true
  custom_schemas:
    - "./schemas/custom.json"
```

### Version Management

```bash
# Semantic versioning for packs
packc compile src/ -o release-1.2.0.json --version 1.2.0

# Generate version manifest
packc manifest ./packs/ -o versions.json

# Version compatibility check
packc check-compatibility old-pack.json new-pack.json
```

## Troubleshooting

### Common Issues

#### Compilation Errors

```bash
# Verbose compilation for debugging
packc compile src/ -o debug.json --verbose

# Validate individual files
packc validate-source src/problematic-prompt.yaml
```

#### Template Syntax Errors

```bash
# Test template rendering
packc test-template prompt.yaml --variables test-vars.json

# Check template syntax
packc lint src/templates/
```

#### Pack Validation Failures

```bash
# Detailed validation report
packc validate mypack.json --strict --output-format json > validation-report.json

# Fix common issues
packc fix-pack mypack.json -o fixed-pack.json
```

#### Performance Issues

```bash
# Profile compilation
packc compile src/ -o output.json --profile

# Optimize large packs
packc optimize large-pack.json -o optimized-pack.json
```

For more examples and detailed specifications, see the `docs/pack-format-spec.md` and `examples/` directory.
