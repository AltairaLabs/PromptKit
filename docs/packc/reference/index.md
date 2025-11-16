---
layout: docs
title: PackC Reference
nav_order: 1
parent: PackC
has_children: true
---

# PackC Reference

Complete command-line reference for the PromptKit Pack Compiler (packc).

## Overview

PackC is the official compiler for PromptKit packs. It transforms YAML prompt configurations into optimized, validated JSON pack files ready for use with the SDK.

## Commands

- **[compile](compile.md)** - Compile all prompts from arena.yaml into a pack
- **[compile-prompt](compile-prompt.md)** - Compile a single prompt to pack format
- **[validate](validate.md)** - Validate a pack file
- **[inspect](inspect.md)** - Display pack information and structure
- **[version](version.md)** - Show packc version

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
- [Pack Format Specification](../../sdk/explanation/promptpack-format.md)
