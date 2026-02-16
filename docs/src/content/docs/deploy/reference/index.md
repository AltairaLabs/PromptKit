---
title: Deploy Reference
sidebar:
  order: 0
---
Complete reference documentation for the PromptKit Deploy framework.

## Overview

Deploy provides a plugin-based deployment system for shipping prompt packs to cloud providers. It uses adapter binaries that communicate via JSON-RPC 2.0 over stdio.

## Reference Documents

- **[CLI Commands](cli-commands)** - All deploy and adapter management commands with flags and examples
- **[Adapter SDK](adapter-sdk)** - Go SDK for building custom adapter plugins
- **[Protocol](protocol)** - JSON-RPC methods, request/response types, and error codes

## Quick Reference

### Common Usage

```bash
# Deploy to default environment
promptarena deploy

# Preview changes
promptarena deploy plan --env production

# Check status
promptarena deploy status --env production

# Tear down
promptarena deploy destroy --env staging

# Manage adapters
promptarena deploy adapter install agentcore
promptarena deploy adapter list
promptarena deploy adapter remove agentcore
```

### Key Paths

| Path | Description |
|------|-------------|
| `arena.yaml` | Deploy configuration (deploy section) |
| `.promptarena/deploy.state` | Persistent deployment state |
| `.promptarena/adapters/` | Project-local adapter binaries |
| `~/.promptarena/adapters/` | User-level adapter binaries |

### Adapter Binary Naming

Adapter binaries follow the convention `promptarena-deploy-{provider}`:

```
promptarena-deploy-agentcore
promptarena-deploy-cloudrun
promptarena-deploy-lambda
```

## See Also

- [How-To Guides](../how-to/) - Task-focused guides
- [Tutorials](../tutorials/) - Learn by building
- [Explanation](../explanation/) - Architecture deep dives
