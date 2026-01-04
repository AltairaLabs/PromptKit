---
title: PackC Reference
sidebar:
  order: 0
---
Complete command-line reference for the PromptKit Pack Compiler (packc).

## Overview

PackC is the official compiler for PromptKit packs. It transforms YAML prompt configurations into optimized, validated JSON pack files ready for use with the SDK.

## Commands

- **[compile](compile)** - Compile all prompts from arena.yaml into a pack
- **[compile-prompt](compile-prompt)** - Compile a single prompt to pack format
- **[validate](validate)** - Validate a pack file
- **[inspect](inspect)** - Display pack information and structure
- **[version](version)** - Show packc version

## Quick Reference

### Common Usage

```bash
# Compile all prompts into a pack
packc compile --config arena.yaml --output app.pack.json --id my-app

# Validate a pack
packc validate app.pack.json

# Inspect pack contents
packc inspect app.pack.json
```

### Installation

```bash
# Install from source
go install github.com/AltairaLabs/PromptKit/tools/packc@latest

# Or use pre-built binary
# Download from GitHub releases
```

### Pack File Format

PackC produces `.pack.json` files with this structure:

```json
{
  "id": "my-app",
  "name": "My Application",
  "version": "1.0.0",
  "template_engine": "go",
  "prompts": {
    "task_type": {
      "system": "System prompt...",
      "user_template": "User template...",
      "parameters": {...},
      "tools": [...],
      "variables": {...}
    }
  }
}
```

## See Also

- [PackC How-To Guides](../how-to/) - Task-focused guides
- [PackC Tutorials](../tutorials/) - Learn by building
- [Pack Format Specification](../../sdk/explanation/promptpack-format)
