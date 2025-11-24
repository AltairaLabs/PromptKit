---
title: Configuration Schema Validation
description: How to use JSON schemas for validating PromptKit configuration files
sidebar:
  order: 5
---

PromptKit provides JSON schemas for all configuration file types, enabling automatic validation and IDE support.

## Overview

JSON schemas are automatically generated from the PromptKit codebase and provide:

- **Validation**: Ensure your config files match the expected structure
- **IDE Support**: Get autocomplete, inline documentation, and error checking
- **CI/CD Integration**: Validate configs automatically in your pipelines
- **Automatic Loading**: Configs are validated against schemas when loaded by PromptKit tools

### Automatic Validation

As of version 1.0, PromptKit automatically validates all configuration files against their schemas during loading:

- **Arena configs**: Validated when `config.LoadConfig()` is called
- **Scenarios**: Validated when loading individual scenario files
- **Providers**: Validated when loading provider configurations
- **PromptConfigs**: Validated when loading prompt configuration files
- **Tools**: Validated when loading tool definitions
- **Personas**: Validated when loading persona files

This means you'll get immediate, detailed error messages if your configs don't match the expected structure, making it easier to catch typos and structural issues early.

## Available Schemas

All schemas are available at `https://promptkit.altairalabs.ai/schemas/`.

### Using Latest Schemas (Recommended)

The `/latest/` path automatically points to the current stable schema version:

| Config Type | Latest Schema URL |
|------------|-------------------|
| Arena | `https://promptkit.altairalabs.ai/schemas/latest/arena.json` |
| Scenario | `https://promptkit.altairalabs.ai/schemas/latest/scenario.json` |
| Provider | `https://promptkit.altairalabs.ai/schemas/latest/provider.json` |
| PromptConfig | `https://promptkit.altairalabs.ai/schemas/latest/promptconfig.json` |
| Tool | `https://promptkit.altairalabs.ai/schemas/latest/tool.json` |
| Persona | `https://promptkit.altairalabs.ai/schemas/latest/persona.json` |

**Benefits of using `/latest/`:**

- Automatic updates when new schema versions are released
- No need to update URLs in your config files
- Always get the latest validation rules and field definitions

### Versioned Schemas

For stability in production or when you need a specific schema version:

| Config Type | Versioned Schema URL |
|------------|----------------------|
| Arena | `https://promptkit.altairalabs.ai/schemas/v1alpha1/arena.json` |
| Scenario | `https://promptkit.altairalabs.ai/schemas/v1alpha1/scenario.json` |
| Provider | `https://promptkit.altairalabs.ai/schemas/v1alpha1/provider.json` |
| PromptConfig | `https://promptkit.altairalabs.ai/schemas/v1alpha1/promptconfig.json` |
| Tool | `https://promptkit.altairalabs.ai/schemas/v1alpha1/tool.json` |
| Persona | `https://promptkit.altairalabs.ai/schemas/v1alpha1/persona.json` |

### Common Schemas

Shared configuration structures:

- **Metadata**: `https://promptkit.altairalabs.ai/schemas/v1alpha1/common/metadata.json`
- **Assertions**: `https://promptkit.altairalabs.ai/schemas/v1alpha1/common/assertions.json`
- **Media**: `https://promptkit.altairalabs.ai/schemas/v1alpha1/common/media.json`

## Using Schemas in Configuration Files

Add a `$schema` field at the top of your YAML config:

```yaml
$schema: https://promptkit.altairalabs.ai/schemas/latest/arena.json
apiVersion: promptkit.altairalabs.ai/v1alpha1
kind: Arena
metadata:
  name: my-arena
spec:
  # ... rest of config
```

**Tip:** Use `/latest/` in the `$schema` URL to automatically get schema updates, while `apiVersion` remains stable for runtime compatibility.

### Arena Configuration

Recommended file name: `config.arena.yaml`

```yaml
$schema: https://promptkit.altairalabs.ai/schemas/latest/arena.json
apiVersion: promptkit.altairalabs.ai/v1alpha1
kind: Arena
metadata:
  name: customer-support-arena
spec:
  scenarios:
    - file: scenarios/support-ticket.scenario.yaml
    - file: scenarios/product-inquiry.scenario.yaml
  
  providers:
    - file: providers/openai.provider.yaml
    - file: providers/anthropic.provider.yaml
```

### Scenario Configuration

Recommended file name: `*.scenario.yaml` (e.g., `support-ticket.scenario.yaml`)

```yaml
$schema: https://promptkit.altairalabs.ai/schemas/latest/scenario.json
apiVersion: promptkit.altairalabs.ai/v1alpha1
kind: Scenario
metadata:
  name: support-ticket
spec:
  id: "support-ticket"
  task_type: "assistant"
  description: "Test handling of customer support tickets"
  
  turns:
    - role: user
      content: "I need help with my order"
      assertions:
        - type: content_includes
          params:
            patterns: ["ticket number", "order"]
```

### Provider Configuration

Recommended file name: `*.provider.yaml` (e.g., `openai.provider.yaml`)

```yaml
$schema: https://promptkit.altairalabs.ai/schemas/latest/provider.json
apiVersion: promptkit.altairalabs.ai/v1alpha1
kind: Provider
metadata:
  name: openai-gpt4
spec:
  id: "openai-gpt4"
  type: openai
  model: gpt-4
  defaults:
    temperature: 0.7
    max_tokens: 2000
```

### Tool Configuration

Recommended file name: `*.tool.yaml` (e.g., `search.tool.yaml`)

```yaml
$schema: https://promptkit.altairalabs.ai/schemas/latest/tool.json
apiVersion: promptkit.altairalabs.ai/v1alpha1
kind: Tool
metadata:
  name: search
spec:
  id: "search"
  description: "Search for information"
  input_schema:
    type: object
    properties:
      query:
        type: string
        description: "Search query"
```

### Persona Configuration

Recommended file name: `*.persona.yaml` (e.g., `customer.persona.yaml`)

```yaml
$schema: https://promptkit.altairalabs.ai/schemas/latest/persona.json
apiVersion: promptkit.altairalabs.ai/v1alpha1
kind: Persona
metadata:
  name: curious-customer
spec:
  id: "curious-customer"
  description: "A curious customer asking detailed questions"
  traits:
    - inquisitive
    - detail-oriented
```

## VS Code Integration

### Automatic Setup

If you're using the PromptKit repository, schema validation is already configured in `.vscode/settings.json`.

### Manual Setup

Add to your workspace or user settings:

```json
{
  "yaml.schemas": {
    "https://promptkit.altairalabs.ai/schemas/latest/arena.json": [
      "config.arena.yaml",
      "*.arena.yaml",
      "**/arena.yaml"
    ],
    "https://promptkit.altairalabs.ai/schemas/latest/scenario.json": [
      "*.scenario.yaml",
      "**/scenarios/*.yaml"
    ],
    "https://promptkit.altairalabs.ai/schemas/latest/provider.json": [
      "*.provider.yaml",
      "**/providers/*.yaml"
    ],
    "https://promptkit.altairalabs.ai/schemas/latest/promptconfig.json": [
      "*.prompt.yaml",
      "**/prompts/*.yaml"
    ],
    "https://promptkit.altairalabs.ai/schemas/latest/tool.json": [
      "*.tool.yaml",
      "**/tools/*.yaml"
    ],
    "https://promptkit.altairalabs.ai/schemas/latest/persona.json": [
      "*.persona.yaml",
      "**/personas/*.yaml"
    ]
  },
  "yaml.schemaStore.enable": true,
  "yaml.validate": true
}
```

**File Naming Convention:** Use typed file extensions for better IDE integration:

- Arena: `config.arena.yaml`
- Scenarios: `*.scenario.yaml`
- Providers: `*.provider.yaml`
- Tools: `*.tool.yaml`
- Personas: `*.persona.yaml`
- PromptConfigs: `*.prompt.yaml`

### Features

Once configured, VS Code provides:

- **Autocomplete**: Press `Ctrl+Space` to see available fields
- **Inline Documentation**: Hover over fields to see descriptions
- **Error Checking**: Real-time validation with red squiggles
- **Quick Fixes**: Suggestions for fixing validation errors

## Other Editors

### JetBrains IDEs (IntelliJ, GoLand, PyCharm)

1. Open **Settings** → **Languages & Frameworks** → **Schemas and DTDs** → **JSON Schema Mappings**
2. Click **+** to add a new mapping
3. Enter schema URL: `https://promptkit.altairalabs.ai/schemas/latest/arena.json`
4. Add file patterns: `config.arena.yaml`, `*.arena.yaml`
5. Repeat for other config types (scenario, provider, tool, persona)

### Vim/Neovim

Use [yaml-language-server](https://github.com/redhat-developer/yaml-language-server) with the following config:

```lua
require('lspconfig').yamlls.setup {
  settings = {
    yaml = {
      schemas = {
        ["https://promptkit.altairalabs.ai/schemas/latest/arena.json"] = { "config.arena.yaml", "*.arena.yaml" },
        ["https://promptkit.altairalabs.ai/schemas/latest/scenario.json"] = { "*.scenario.yaml", "**/scenarios/*.yaml" },
        ["https://promptkit.altairalabs.ai/schemas/latest/provider.json"] = { "*.provider.yaml", "**/providers/*.yaml" },
        ["https://promptkit.altairalabs.ai/schemas/latest/tool.json"] = { "*.tool.yaml", "**/tools/*.yaml" },
        ["https://promptkit.altairalabs.ai/schemas/latest/persona.json"] = { "*.persona.yaml", "**/personas/*.yaml" },
      }
    }
  }
}
```

### Emacs

Use [lsp-mode](https://emacs-lsp.github.io/lsp-mode/) with yaml-language-server:

```elisp
(with-eval-after-load 'lsp-mode
  (add-to-list 'lsp-yaml-schemas
               '("https://promptkit.altairalabs.ai/schemas/latest/arena.json" . ["config.arena.yaml" "*.arena.yaml"]))
  (add-to-list 'lsp-yaml-schemas
               '("https://promptkit.altairalabs.ai/schemas/latest/scenario.json" . ["*.scenario.yaml"])))
```

## File Naming Conventions

PromptKit uses typed file extensions to improve IDE integration and make file purposes clear:

| Type | Pattern | Example |
|------|---------|----------|
| Arena | `config.arena.yaml` | `config.arena.yaml` |
| Scenario | `*.scenario.yaml` | `support-ticket.scenario.yaml` |
| Provider | `*.provider.yaml` | `openai-gpt4.provider.yaml` |
| Tool | `*.tool.yaml` | `search.tool.yaml` |
| Persona | `*.persona.yaml` | `curious-customer.persona.yaml` |
| PromptConfig | `*.prompt.yaml` | `assistant.prompt.yaml` |

### Why Use Typed Extensions?

1. **Better IDE Support**: Schema stores and language servers can match files by pattern
2. **Self-Documenting**: File purpose is clear from the name
3. **Easier Navigation**: Find all providers with `find . -name "*.provider.yaml"`
4. **Backwards Compatible**: Old names like `arena.yaml` still work

### Migration Example

Old structure:

```text
arena.yaml
providers/
  openai.yaml
  anthropic.yaml
scenarios/
  test1.yaml
  test2.yaml
```

New structure:

```text
config.arena.yaml
providers/
  openai.provider.yaml
  anthropic.provider.yaml
scenarios/
  test1.scenario.yaml
  test2.scenario.yaml
```

**Note:** The `kind` field in each manifest remains the authoritative type indicator at runtime. File naming is primarily for IDE and developer experience.

## Command Line Validation

### Using promptarena validate (Recommended)

The `promptarena` CLI includes a built-in validate command that automatically detects config types:

```bash
# Validate any config file (auto-detects type from 'kind' field)
promptarena validate arena.yaml
promptarena validate scenarios/test.yaml
promptarena validate providers/openai.yaml

# Explicit type specification
promptarena validate config.yaml --type arena

# Schema-only validation (skip business logic checks)
promptarena validate arena.yaml --schema-only

# Verbose error output
promptarena validate arena.yaml --verbose
```

**Features:**

- Automatic type detection from `kind` field
- Schema validation with detailed error messages
- Optional business logic validation for arena configs
- Field path highlighting in error messages

**Example output:**

```text
Validating arena.yaml as type 'arena'...
✅ Schema validation passed for arena.yaml

Running business logic validation...
✅ Business logic validation passed

✅ arena.yaml is valid
```

### Using ajv-cli

Install [ajv-cli](https://github.com/ajv-validator/ajv-cli):

```bash
npm install -g ajv-cli ajv-formats
```

Validate a config file:

```bash
ajv validate \
  -s https://promptkit.altairalabs.ai/schemas/v1alpha1/arena.json \
  -d arena.yaml \
  --strict=false
```

### Using make (in PromptKit repo)

The PromptKit repository includes Makefile targets:

```bash
# Check if schemas are up to date
make schemas-check

# Regenerate schemas
make schemas
```

## CI/CD Integration

### GitHub Actions

PromptKit includes a [schema validation workflow](https://github.com/AltairaLabs/PromptKit/blob/main/.github/workflows/schemas.yml) that:

1. Validates schemas are up to date with code
2. Tests all example configs against their schemas
3. Reports validation errors

Example workflow snippet:

```yaml
- name: Validate configs
  run: |
    npm install -g ajv-cli ajv-formats
    
    for file in $(find . -name "arena.yaml"); do
      ajv validate \
        -s https://promptkit.altairalabs.ai/schemas/v1alpha1/arena.json \
        -d "$file" \
        --strict=false
    done
```

### GitLab CI

```yaml
validate:schemas:
  image: node:20
  script:
    - npm install -g ajv-cli ajv-formats
    - |
      for file in $(find . -name "*.yaml"); do
        # Determine schema based on file location
        case "$file" in
          */scenarios/*) schema="scenario" ;;
          */providers/*) schema="provider" ;;
          *arena*) schema="arena" ;;
          *) continue ;;
        esac
        
        ajv validate \
          -s "https://promptkit.altairalabs.ai/schemas/v1alpha1/${schema}.json" \
          -d "$file" \
          --strict=false
      done
```

## Pre-commit Hooks

Create `.pre-commit-config.yaml`:

```yaml
repos:
  - repo: local
    hooks:
      - id: validate-arena-configs
        name: Validate Arena Configs
        entry: bash -c 'for f in $(git diff --cached --name-only | grep arena.yaml); do ajv validate -s https://promptkit.altairalabs.ai/schemas/v1alpha1/arena.json -d "$f" --strict=false; done'
        language: system
        files: 'arena\.yaml$'
        pass_filenames: false
```

## Schema Generation

Schemas are automatically generated from the PromptKit Go codebase using reflection.

### How It Works

1. The `schema-gen` tool analyzes Go struct definitions
2. Generates JSON Schema Draft 7 compatible schemas
3. Adds descriptions from Go doc comments
4. Includes validation rules from struct tags

### Contributing to Schemas

When you modify config structures in the codebase:

1. Update struct tags and doc comments:

```go
// ArenaConfig defines the complete arena configuration
type ArenaConfig struct {
    // Metadata about the arena
    Metadata Metadata `json:"metadata" jsonschema:"required,description=Arena metadata"`
    
    // List of scenario files to include
    Scenarios []ScenarioRef `json:"scenarios" jsonschema:"required,minItems=1,description=Scenarios to test"`
}
```

1. Run `make schemas` to regenerate
1. The CI validates schemas match the code

## Troubleshooting

### Schema Not Found (404)

Ensure you're using the correct schema URL. All schemas are hosted at:

- `https://promptkit.altairalabs.ai/schemas/v1alpha1/`

### Validation Errors

Common issues:

**Missing required fields:**

```text
Error: must have required property 'metadata'
```

→ Add the `metadata` field to your config

**Invalid type:**

```text
Error: must be string
```

→ Check the field type matches the schema (e.g., string vs number)

**Additional properties:**

```text
Error: must NOT have additional properties
```

→ Remove unknown fields or check for typos

### IDE Not Recognizing Schema

1. Ensure the YAML extension is installed (e.g., Red Hat YAML for VS Code)
2. Check the `$schema` field is at the top of the file
3. Verify VS Code settings include the schema mapping
4. Reload the window: `Cmd+Shift+P` → "Developer: Reload Window"

## Best Practices

1. **Always include `$schema`**: Makes configs self-documenting
2. **Use schema validation in CI**: Catch errors before deployment
3. **Keep schemas up to date**: Run `make schemas` after struct changes
4. **Add descriptions**: Document fields in Go struct tags
5. **Test with examples**: Validate example configs in CI

## Related

- [Arena Configuration Reference](/arena/reference/config-schema/)
- [Scenario Format](/arena/reference/scenario-format/)
- [Provider Configuration](/runtime/reference/providers/)
- [Configuration Validation](/packc/explanation/validation/)
