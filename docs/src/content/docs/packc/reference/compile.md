---
title: compile
sidebar:
  order: 1
---
Compile all prompts from an arena.yaml configuration into a single pack file.

## Synopsis

```bash
packc compile [-c <arena.yaml>] [-o <pack-file>] [--id <pack-id>]
```

## Description

The `compile` command reads an Arena configuration file (`arena.yaml`) that references multiple prompt YAML files and compiles them all into a single optimized `.pack.json` file.

This is the primary command for building production packs that contain multiple prompts for your application.

## Options

### Optional (all flags have smart defaults)

**`-c, --config <path>`**
- Path to the arena.yaml configuration file
- This file lists all prompts to include in the pack
- Default: `config.arena.yaml`

**`-o, --output <path>`**
- Path where the compiled pack file will be written
- Should end in `.pack.json`
- Default: `{id}.pack.json`
- Example: `packs/customer-support.pack.json`

**`--id <string>`**
- Unique identifier for this pack
- Used by the SDK to reference the pack
- Should be kebab-case (e.g., `customer-support`)
- Default: current folder name (sanitized to lowercase alphanumeric with hyphens)

## Examples

### Basic Compilation

```bash
packc compile \
  --config arena.yaml \
  --output packs/app.pack.json \
  --id my-app
```

### Production Pack

```bash
packc compile \
  --config configs/production/arena.yaml \
  --output dist/production.pack.json \
  --id prod-assistant
```

### Multiple Environments

```bash
# Development pack
packc compile \
  --config arena.dev.yaml \
  --output packs/app.dev.pack.json \
  --id app-dev

# Production pack
packc compile \
  --config arena.prod.yaml \
  --output packs/app.prod.pack.json \
  --id app-prod
```

## Input: arena.yaml Structure

The arena.yaml file references prompt configurations:

```yaml
prompt_configs:
  - file: prompts/support.yaml
  - file: prompts/sales.yaml
  - file: prompts/technical.yaml

tools:
  - file: tools/search_kb.yaml
  - file: tools/create_ticket.yaml
```

## Output: Pack File

The command produces a `.pack.json` file:

```json
{
  "$schema": "https://promptpack.org/schema/latest/promptpack.schema.json",
  "id": "customer-support",
  "name": "customer-support",
  "version": "v1.0.0",
  "template_engine": {
    "version": "v1",
    "syntax": "{{variable}}"
  },
  "prompts": {
    "customer-support": {
      "id": "customer-support",
      "name": "Customer Support Agent",
      "system_template": "You are a customer support agent...",
      "version": "v1.0.0"
    },
    "sales-assistant": {
      "id": "sales-assistant",
      "name": "Sales Assistant",
      "system_template": "You are a sales assistant...",
      "version": "v1.0.0"
    }
  },
  "compilation": {
    "compiled_with": "packc-v0.1.0",
    "created_at": "2025-01-15T10:30:00Z",
    "schema": "v1"
  }
}
```

## Compilation Process

The compile command performs these steps:

1. **Load Configuration** - Read arena.yaml
2. **Parse Prompts** - Load all referenced YAML files
3. **Validate** - Check for errors and warnings
4. **Compile** - Transform to pack format
5. **Write** - Save to output file

## Exit Codes

- `0` - Success
- `1` - Error (invalid config, compilation failure, etc.)

## Common Errors

### Using Smart Defaults

All flags have smart defaults, so you can run `packc compile` with no arguments:

```bash
# Uses config.arena.yaml, folder name as ID, {id}.pack.json as output
packc compile

# Or override specific options
packc compile --config arena.yaml --output app.pack.json --id my-app
```

### Config File Not Found

```bash
Error loading arena config: open arena.yaml: no such file or directory
```

**Solution:** Check that the config file exists:

```bash
ls -la arena.yaml
```

### Invalid YAML Syntax

```bash
Error loading arena config: yaml: line 5: mapping values are not allowed
```

**Solution:** Fix YAML syntax errors in arena.yaml or prompt files.

### Missing Prompt Files

```bash
Error: prompt file not found: prompts/support.yaml
```

**Solution:** Ensure all referenced prompt files exist.

## Validation Warnings

The compiler may emit warnings for non-fatal issues:

```bash
⚠ Media validation warnings for customer-support:
  - Referenced image not found: images/logo.png
  - Template variable 'user_name' not defined
```

These warnings don't stop compilation but should be addressed.

## Performance

Compilation is fast:

- Small packs (1-5 prompts): <100ms
- Medium packs (10-20 prompts): <500ms
- Large packs (50+ prompts): <2s

## Best Practices

### 1. Organize by Environment

```
configs/
├── arena.dev.yaml
├── arena.staging.yaml
└── arena.prod.yaml

packs/
├── app.dev.pack.json
├── app.staging.pack.json
└── app.prod.pack.json
```

### 2. Use Descriptive Pack IDs

```bash
# Good
--id customer-support-v2
--id sales-assistant-prod
--id technical-docs-staging

# Bad
--id pack1
--id app
--id test
```

### 3. Version Your Packs

Include version in pack metadata:

```yaml
# arena.yaml
version: "1.0"
metadata:
  pack_version: "2.1.0"
```

### 4. Validate After Compilation

Always validate after compiling:

```bash
packc compile --config arena.yaml --output app.pack.json --id my-app
packc validate app.pack.json
```

### 5. Store Packs in Version Control

Commit compiled packs to track changes:

```bash
git add packs/app.pack.json
git commit -m "chore: update customer support pack"
```

## Integration with CI/CD

### GitHub Actions

```yaml
name: Compile Packs

on: [push]

jobs:
  compile:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v3
      
      - name: Install packc
        run: go install github.com/AltairaLabs/PromptKit/tools/packc@latest
      
      - name: Compile packs
        run: |
          packc compile --config arena.yaml --output packs/app.pack.json --id my-app
          packc validate packs/app.pack.json
      
      - name: Upload packs
        uses: actions/upload-artifact@v3
        with:
          name: compiled-packs
          path: packs/
```

### Makefile

```makefile
.PHONY: compile-packs
compile-packs:
	packc compile --config arena.yaml --output packs/app.pack.json --id my-app
	packc validate packs/app.pack.json

.PHONY: compile-all
compile-all:
	packc compile --config arena.dev.yaml --output packs/app.dev.pack.json --id app-dev
	packc compile --config arena.prod.yaml --output packs/app.prod.pack.json --id app-prod
```

## See Also

- [compile-prompt](compile-prompt) - Compile single prompt
- [validate](validate) - Validate pack file
- [inspect](inspect) - Inspect pack contents
- [How to Compile Packs](../how-to/compile-packs)
