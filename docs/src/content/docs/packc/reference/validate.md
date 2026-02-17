---
title: validate
sidebar:
  order: 3
---
Validate a compiled pack file for errors and warnings.

## Synopsis

```bash
packc validate <pack-file>
```

## Description

The `validate` command checks a compiled `.pack.json` file for:

- Schema compliance
- Required fields
- Valid data types
- Tool references
- Template syntax
- Parameter values

Use this command to ensure packs are correctly formatted before deployment.

## Arguments

**`<pack-file>`** (required)
- Path to the pack file to validate
- Must be a `.pack.json` file

## Examples

### Basic Validation

```bash
packc validate packs/app.pack.json
```

**Output (success):**
```
Validating pack: packs/app.pack.json
Validating against PromptPack schema...
✓ Schema validation passed
✓ Pack structure is valid
```

**Output (with warnings):**
```
Validating pack: packs/app.pack.json
Validating against PromptPack schema...
✓ Schema validation passed
⚠ Pack has 2 warnings:
  - prompt 'support': no variables defined
  - prompt 'support': missing version
```

### Validate in CI/CD

```bash
# Validate and fail on warnings
packc validate packs/app.pack.json || exit 1
```

### Validate Multiple Packs

```bash
for pack in packs/*.pack.json; do
  echo "Validating $pack"
  packc validate "$pack"
done
```

## Validation Checks

### Schema Validation

Checks pack structure:

- Pack ID present
- Version format valid
- Prompts section exists
- Required prompt fields present

### Prompt Validation

For each prompt:

- System prompt not empty
- Parameters within valid ranges
- Template syntax correct
- Variables defined

### Tool Validation

- Referenced tools exist
- Tool parameters valid
- Tool names unique

### Workflow Validation

If the pack includes a `workflow` section:

- Entry state exists in the states map
- All `on_event` targets reference valid state names
- Each state's `prompt_task` references a prompt in the pack
- `orchestration` values are one of: `internal`, `external`, `hybrid`
- `persistence` values are one of: `transient`, `persistent`
- No unreachable states (every non-entry state is reachable via at least one transition)
- Terminal states (no `on_event`) have valid prompt tasks

### Parameter Validation

- temperature: 0.0-2.0
- max_tokens: positive integer
- top_p: 0.0-1.0
- top_k: positive integer

## Exit Codes

- `0` - Pack is valid (no warnings)
- `1` - Pack has warnings or errors

## Common Warnings

### Missing Template Variables

```
⚠ Template variable 'customer_name' not defined
```

**Cause:** Template uses `` but variable not in pack

**Solution:** Add variable to pack or remove from template

### Tool Not Defined

```
⚠ Tool 'search_api' referenced but not defined
```

**Cause:** Prompt lists tool that doesn't exist in pack

**Solution:** Add tool to pack or remove from prompt's available_tools

### Parameter Out of Range

```
⚠ Temperature 2.5 exceeds maximum 2.0
```

**Cause:** Invalid model parameter value

**Solution:** Correct parameter in source YAML

### Empty System Prompt

```
⚠ System prompt is empty for prompt 'assistant'
```

**Cause:** Prompt has no system instructions

**Solution:** Add system prompt to YAML

## Integration with Build Process

### Pre-commit Hook

```bash
#!/bin/bash
# .git/hooks/pre-commit

echo "Validating packs..."
for pack in packs/*.pack.json; do
  if ! packc validate "$pack"; then
    echo "❌ Pack validation failed"
    exit 1
  fi
done

echo "✓ All packs valid"
```

### CI Pipeline

```yaml
# .github/workflows/validate-packs.yml
name: Validate Packs

on: [push, pull_request]

jobs:
  validate:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v3
      
      - name: Install packc
        run: go install github.com/AltairaLabs/PromptKit/tools/packc@latest
      
      - name: Validate all packs
        run: |
          EXIT_CODE=0
          for pack in packs/*.pack.json; do
            if ! packc validate "$pack"; then
              EXIT_CODE=1
            fi
          done
          exit $EXIT_CODE
```

## Best Practices

### 1. Validate After Every Compilation

```bash
packc compile --config arena.yaml --output app.pack.json --id my-app
packc validate app.pack.json
```

### 2. Fail CI on Warnings

Treat warnings as errors in production:

```bash
if ! packc validate packs/prod.pack.json; then
  echo "Production pack validation failed"
  exit 1
fi
```

### 3. Validate Before Deployment

```bash
# In deployment script
echo "Validating packs before deployment..."
for pack in packs/*.pack.json; do
  packc validate "$pack" || {
    echo "Deployment aborted: invalid pack"
    exit 1
  }
done
```

### 4. Keep Validation Logs

```bash
packc validate packs/app.pack.json > validation-report.txt 2>&1
```

## See Also

- [compile](compile) - Compile packs
- [inspect](inspect) - Inspect pack contents
- [How to Validate Packs](../how-to/validate-packs)
