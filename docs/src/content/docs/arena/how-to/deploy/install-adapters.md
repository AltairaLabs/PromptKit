---
title: 'Deploy: Install Adapters'
---

## Goal

Install, list, and remove deploy adapter plugins.

## Prerequisites

- PromptKit CLI (`promptarena`) installed

## Install an Adapter

Install an adapter from the default registry:

```bash
promptarena deploy adapter install agentcore
```

Install a specific version:

```bash
promptarena deploy adapter install agentcore@0.2.0
```

**What happens:**

1. The CLI looks up the adapter in the built-in registry
2. Downloads the binary from the adapter's GitHub Releases for your OS and architecture
3. Installs it to `~/.promptarena/adapters/promptarena-deploy-agentcore`
4. Sets executable permissions (0755)

### Download URL Format

Adapter binaries are fetched from GitHub Releases:

```
https://github.com/{repo}/releases/download/v{version}/promptarena-deploy-{provider}_{os}_{arch}
```

For example, on macOS ARM64:

```
https://github.com/AltairaLabs/promptarena-deploy-agentcore/releases/download/v0.2.0/promptarena-deploy-agentcore_darwin_arm64
```

## List Installed Adapters

See all installed adapters:

```bash
promptarena deploy adapter list
```

**Expected output:**

```
Installed adapters:
  agentcore  ~/.promptarena/adapters/promptarena-deploy-agentcore
```

The list command searches two locations:

1. **Project-local**: `.promptarena/adapters/` in your project directory
2. **User-level**: `~/.promptarena/adapters/` in your home directory

## Remove an Adapter

Remove an installed adapter:

```bash
promptarena deploy adapter remove agentcore
```

This removes the binary from `~/.promptarena/adapters/`.

## Adapter Discovery

When you run a deploy command, the CLI locates the adapter binary in this order:

1. **Project-local** — `.promptarena/adapters/promptarena-deploy-{provider}`
2. **User-level** — `~/.promptarena/adapters/promptarena-deploy-{provider}`
3. **System PATH** — Standard `$PATH` lookup

This lets you override a global adapter with a project-specific version.

## Manual Installation

You can also install adapters manually by placing the binary in the right location:

```bash
# Build from source
cd promptarena-deploy-myprovider
go build -o promptarena-deploy-myprovider .

# Install to user-level directory
mkdir -p ~/.promptarena/adapters
cp promptarena-deploy-myprovider ~/.promptarena/adapters/
chmod 755 ~/.promptarena/adapters/promptarena-deploy-myprovider
```

Or install to your project for team-specific versions:

```bash
mkdir -p .promptarena/adapters
cp promptarena-deploy-myprovider .promptarena/adapters/
chmod 755 .promptarena/adapters/promptarena-deploy-myprovider
```

## Available Adapters

| Adapter | Provider | Description |
|---------|----------|-------------|
| `agentcore` | AWS Bedrock AgentCore | Deploy to AWS Bedrock AgentCore |

## Verification

Confirm an adapter is working:

```bash
# List should show the adapter
promptarena deploy adapter list

# Plan should connect to the adapter successfully
promptarena deploy plan
```

## Troubleshooting

### Adapter not found

Ensure the binary exists and is executable:

```bash
ls -la ~/.promptarena/adapters/
```

### Permission denied

Fix executable permissions:

```bash
chmod 755 ~/.promptarena/adapters/promptarena-deploy-agentcore
```

### Wrong architecture

If you see exec format errors, the binary was built for a different OS/architecture. Reinstall:

```bash
promptarena deploy adapter remove agentcore
promptarena deploy adapter install agentcore
```

## See Also

- [Configure Deploy](configure) — Set up arena.yaml after installing an adapter
- [CLI Commands](../../reference/deploy/cli-commands) — Complete adapter command reference
- [Adapter SDK](../../reference/deploy/adapter-sdk) — Build your own adapter
