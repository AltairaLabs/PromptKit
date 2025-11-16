---
title: compile-prompt
docType: reference
order: 2
---
# packc compile-prompt

Compile a single prompt YAML file into pack format.

## Synopsis

```bash
packc compile-prompt --prompt <yaml-file> --output <json-file> [options]
```

## Description

The `compile-prompt` command compiles a single prompt YAML configuration file into a standalone pack JSON file. This is useful for:

- Testing individual prompts during development
- Creating single-prompt packs for specialized use cases
- Compiling prompts independently of the main arena.yaml
- Quick iteration on prompt development

Unlike the `compile` command which processes all prompts from arena.yaml, `compile-prompt` focuses on a single prompt file and produces a minimal pack containing only that prompt.

## Options

### Required Options

**`--prompt <path>`**
- Path to the prompt YAML file to compile
- The YAML must contain a valid PromptConfig structure
- Relative paths are resolved from the current directory

**`--output <path>`**
- Output pack file path
- Must end with `.json` or `.pack.json`
- Parent directory will be created if it doesn't exist

### Optional Options

**`--config-dir <path>`**
- Base directory for resolving config files
- Default: current directory
- Used for resolving relative paths in media references

## Examples

### Basic Single Prompt Compilation

```bash
packc compile-prompt \
  --prompt prompts/support.yaml \
  --output packs/support.pack.json
```

### With Custom Config Directory

```bash
packc compile-prompt \
  --prompt prompts/marketing/email.yaml \
  --output packs/marketing-email.pack.json \
  --config-dir ./config
```

### Compile for Testing

```bash
# Compile to temporary location for testing
packc compile-prompt \
  --prompt prompts/experimental/new-feature.yaml \
  --output /tmp/test-feature.pack.json
```

### Multiple Single Prompts

```bash
# Compile several prompts individually
for prompt in prompts/*.yaml; do
  name=$(basename "$prompt" .yaml)
  packc compile-prompt \
    --prompt "$prompt" \
    --output "packs/${name}.pack.json"
done
```

## Input Format

The prompt YAML file must contain a valid PromptConfig structure:

```yaml
# prompts/support.yaml
apiVersion: promptkit/v1
kind: PromptConfig
spec:
  task_type: customer-support
  name: Customer Support Agent
  description: Handles customer inquiries
  system_prompt: |
    You are a helpful customer support agent.
    You assist customers with their questions.
  
  user_template: |
    Customer: 
  
  template_engine: go
  
  parameters:
    temperature: 0.7
    max_tokens: 1000
    top_p: 0.9
  
  tools:
    - name: search_knowledge_base
      description: Search the knowledge base
```

## Output Format

The command produces a JSON pack file with this structure:

```json
{
  "id": "customer-support",
  "version": "1.0.0",
  "compiler_version": "packc-v0.1.0",
  "prompts": {
    "customer-support": {
      "id": "customer-support",
      "name": "Customer Support Agent",
      "description": "Handles customer inquiries",
      "system": "You are a helpful customer support agent.\nYou assist customers with their questions.",
      "user_template": "Customer: ",
      "template_engine": "go",
      "parameters": {
        "temperature": 0.7,
        "max_tokens": 1000,
        "top_p": 0.9
      },
      "tools": ["search_knowledge_base"]
    }
  },
  "metadata": {
    "source_file": "prompts/support.yaml",
    "compiled_at": "2025-01-16T10:30:00Z"
  }
}
```

## Compilation Process

The compile-prompt command performs these steps:

1. **Read YAML File** - Load and parse the prompt YAML
2. **Validate Structure** - Ensure PromptConfig is valid
3. **Check Media References** - Validate any media file paths
4. **Create Registry** - Build in-memory prompt registry
5. **Compile Pack** - Generate optimized pack JSON
6. **Write Output** - Save pack to specified file

## Exit Codes

- **0** - Successful compilation
- **1** - Error occurred (see error message)

## Common Errors

### Invalid YAML Syntax

```
Error parsing prompt config: yaml: line 5: mapping values are not allowed in this context
```

**Solution**: Check YAML indentation and syntax. Use a YAML validator.

### Missing Required Fields

```
Error parsing prompt config: missing required field: task_type
```

**Solution**: Ensure the prompt YAML includes all required fields (task_type, system_prompt).

### Media File Not Found

```
⚠ Media validation warnings:
  - Image file not found: images/logo.png
```

**Solution**: Ensure media files exist at specified paths. Use `--config-dir` to set base directory.

### Invalid Template Syntax

```
Compilation failed: template parse error: unexpected "}" in operand
```

**Solution**: Check Go template syntax in user_template or system_prompt. Ensure `` are balanced.

### Output Directory Missing

```
Failed to write pack file: no such file or directory
```

**Solution**: Create output directory first or use mkdir:

```bash
mkdir -p packs
packc compile-prompt --prompt prompts/support.yaml --output packs/support.pack.json
```

## Warnings

The compile-prompt command may display warnings that don't prevent compilation:

### Media Reference Warnings

```
⚠ Media validation warnings:
  - Image file not found: assets/banner.jpg
  - Video file missing: media/intro.mp4
```

These warnings indicate missing media files referenced in the prompt. The pack will compile, but media content may not work at runtime.

## Integration

### Pre-commit Hook

```bash
#!/bin/bash
# .git/hooks/pre-commit
# Compile modified prompts before commit

for file in $(git diff --cached --name-only | grep '^prompts/.*\.yaml$'); do
  name=$(basename "$file" .yaml)
  echo "Compiling $file..."
  packc compile-prompt --prompt "$file" --output "packs/${name}.pack.json"
  
  if [ $? -ne 0 ]; then
    echo "❌ Failed to compile $file"
    exit 1
  fi
  
  git add "packs/${name}.pack.json"
done
```

### Makefile Target

```makefile
.PHONY: compile-prompts
compile-prompts:
	@mkdir -p packs
	@for prompt in prompts/*.yaml; do \
		name=$$(basename $$prompt .yaml); \
		echo "Compiling $$prompt..."; \
		packc compile-prompt --prompt $$prompt --output packs/$$name.pack.json; \
	done

.PHONY: compile-single
compile-single:
	@if [ -z "$(PROMPT)" ]; then \
		echo "Usage: make compile-single PROMPT=prompts/support.yaml"; \
		exit 1; \
	fi
	@packc compile-prompt --prompt $(PROMPT) --output $(OUTPUT)
```

Usage:

```bash
# Compile all prompts individually
make compile-prompts

# Compile specific prompt
make compile-single PROMPT=prompts/support.yaml OUTPUT=packs/support.pack.json
```

### CI/CD Pipeline

```yaml
# .github/workflows/compile-single-prompts.yml
name: Compile Modified Prompts

on:
  pull_request:
    paths:
      - 'prompts/*.yaml'

jobs:
  compile:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v3
      
      - name: Install packc
        run: go install github.com/AltairaLabs/PromptKit/tools/packc@latest
      
      - name: Detect modified prompts
        id: changed-files
        uses: tj-actions/changed-files@v40
        with:
          files: prompts/*.yaml
      
      - name: Compile modified prompts
        if: steps.changed-files.outputs.any_changed == 'true'
        run: |
          mkdir -p packs
          for file in $; do
            name=$(basename "$file" .yaml)
            echo "Compiling $file..."
            packc compile-prompt --prompt "$file" --output "packs/${name}.pack.json"
          done
      
      - name: Upload packs
        uses: actions/upload-artifact@v3
        with:
          name: compiled-packs
          path: packs/*.pack.json
```

## Development Workflow

### Quick Test Cycle

```bash
# Edit prompt
vim prompts/support.yaml

# Compile for testing
packc compile-prompt \
  --prompt prompts/support.yaml \
  --output /tmp/test-support.pack.json

# Test with SDK
go run cmd/test/main.go --pack /tmp/test-support.pack.json
```

### Organize by Environment

```bash
# Development
packc compile-prompt \
  --prompt prompts/dev/support.yaml \
  --output packs/dev/support.pack.json

# Staging
packc compile-prompt \
  --prompt prompts/staging/support.yaml \
  --output packs/staging/support.pack.json

# Production
packc compile-prompt \
  --prompt prompts/prod/support.yaml \
  --output packs/prod/support.pack.json
```

## Best Practices

### 1. Use Descriptive Filenames

```bash
# Good
packc compile-prompt \
  --prompt prompts/customer-support-v2.yaml \
  --output packs/customer-support-v2.pack.json

# Avoid
packc compile-prompt \
  --prompt prompts/temp.yaml \
  --output packs/out.json
```

### 2. Validate After Compilation

```bash
packc compile-prompt --prompt prompts/support.yaml --output packs/support.pack.json
packc validate packs/support.pack.json
```

### 3. Version Individual Prompts

```bash
# Include version in filename
packc compile-prompt \
  --prompt prompts/support.yaml \
  --output "packs/support-$(date +%Y%m%d).pack.json"
```

### 4. Use Scripts for Batch Operations

```bash
#!/bin/bash
# compile-all-prompts.sh

for prompt in prompts/*.yaml; do
  name=$(basename "$prompt" .yaml)
  echo "Compiling: $name"
  
  packc compile-prompt \
    --prompt "$prompt" \
    --output "packs/${name}.pack.json"
  
  if [ $? -eq 0 ]; then
    echo "✓ Compiled: packs/${name}.pack.json"
  else
    echo "✗ Failed: $prompt"
    exit 1
  fi
done

echo "✓ All prompts compiled successfully"
```

## See Also

- [compile command](compile) - Compile all prompts from arena.yaml
- [validate command](validate) - Validate compiled packs
- [inspect command](inspect) - Inspect pack contents
- [Pack Format](../../sdk/explanation/promptpack-format) - Pack structure details
