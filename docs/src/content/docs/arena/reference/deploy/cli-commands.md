---
title: 'Deploy: CLI Commands'
---

## Synopsis

```bash
promptarena deploy [subcommand] [flags]
```

## Description

The `deploy` command manages prompt pack deployments to cloud providers through adapter plugins. It supports planning, applying, monitoring, and destroying deployments across multiple environments.

## Global Flags

| Flag | Short | Default | Description |
|------|-------|---------|-------------|
| `--env` | `-e` | `"default"` | Target environment name |
| `--config` | | `arena.yaml` | Path to config file |
| `--pack` | | Auto-detected | Path to `.pack.json` file |

Pack auto-detection searches the current directory for `*.pack.json` files.

## Commands

### deploy

Run plan and apply sequentially. This is the default command.

```bash
promptarena deploy [flags]
```

**Examples:**

```bash
# Deploy to default environment
promptarena deploy

# Deploy to production
promptarena deploy --env production

# Deploy with explicit pack file
promptarena deploy --pack dist/app.pack.json

# Deploy with custom config
promptarena deploy --config deploy.yaml --env staging
```

**Process:**

1. Load arena.yaml deploy config
2. Resolve pack file (auto-detect or `--pack`)
3. Merge base config with environment overrides
4. Load prior state (if exists)
5. Refresh state from live environment (pre-plan state refresh)
6. Call adapter `Plan`
7. Call adapter `Apply`
8. Save state with adapter state, pack checksum, and timestamps

---

### deploy plan

Preview deployment changes without modifying any resources.

```bash
promptarena deploy plan [flags]
```

**Examples:**

```bash
# Plan for default environment
promptarena deploy plan

# Plan for production
promptarena deploy plan --env production
```

**Output format:**

```
Planning deployment (env: production)...
Provider: agentcore v0.2.0

Changes:
  + agent_runtime.greeting    Create agent runtime
  ~ a2a_endpoint.greeting     Update A2A endpoint
  - old_resource.legacy       Delete legacy resource
    unchanged.resource         No change

Summary: 1 to create, 1 to update, 1 to delete
```

**Change symbols:**

| Symbol | Action |
|--------|--------|
| `+` | CREATE |
| `~` | UPDATE |
| `-` | DELETE |
| `!` | DRIFT |
| ` ` | NO_CHANGE |

---

### deploy apply

Apply deployment changes (called automatically by `deploy`).

```bash
promptarena deploy apply [flags]
```

**Examples:**

```bash
# Apply to staging
promptarena deploy apply --env staging
```

---

### deploy status

Show current deployment status and resource health.

```bash
promptarena deploy status [flags]
```

**Examples:**

```bash
# Status for default environment
promptarena deploy status

# Status for production
promptarena deploy status --env production
```

**Output format:**

```
Status: deployed
Last deployed: 2026-02-16T10:30:00Z
Pack checksum: sha256:abc123def456...

Resources:
  agent_runtime.greeting: healthy
  a2a_endpoint.greeting: healthy — serving at https://example.com/a2a
```

**Status values:**

| Status | Meaning |
|--------|---------|
| `deployed` | All resources running |
| `not_deployed` | No resources found |
| `degraded` | Some resources unhealthy |
| `unknown` | Unable to determine |

**Resource health:**

| Health | Meaning |
|--------|---------|
| `healthy` | Running normally |
| `unhealthy` | Exists but has issues |
| `missing` | Expected but not found |

**Requires:** Prior deployment state (exits with error if no state exists).

---

### deploy destroy

Tear down all managed resources.

```bash
promptarena deploy destroy [flags]
```

**Examples:**

```bash
# Destroy staging deployment
promptarena deploy destroy --env staging
```

**Process:**

1. Load prior state (exit if none exists)
2. Call adapter `Destroy` with prior state
3. Delete local state file
4. Display completion message

**Requires:** Prior deployment state.

---

### deploy refresh

Refresh local state from the live environment. Queries the adapter for the current state of all resources and updates local state to match reality. Use this to detect drift between local state and cloud resources.

```bash
promptarena deploy refresh [flags]
```

**Examples:**

```bash
# Refresh state for default environment
promptarena deploy refresh

# Refresh state for production
promptarena deploy refresh --env production
```

**Process:**

1. Acquire deploy lock
2. Load prior state (exit if none exists)
3. Call adapter `Status` with prior state
4. Update adapter state and `last_refreshed` timestamp
5. Save updated state
6. Display resource status (drift shown with `!`)

**Requires:** Prior deployment state.

---

### deploy import

Import a pre-existing resource into deployment state. This allows PromptKit to manage resources that were created outside of the deploy workflow.

```bash
promptarena deploy import <type> <name> <id> [flags]
```

**Arguments:**

| Argument | Description |
|----------|-------------|
| `type` | Resource type (e.g., `agent_runtime`, `a2a_endpoint`) |
| `name` | Resource name to assign in local state |
| `id` | Provider-specific resource identifier |

**Examples:**

```bash
# Import an agent runtime
promptarena deploy import agent_runtime my-agent container-abc123

# Import an A2A endpoint
promptarena deploy import a2a_endpoint my-ep endpoint-xyz789

# Import into a specific environment
promptarena deploy import agent_runtime my-agent container-abc123 --env production
```

**Process:**

1. Acquire deploy lock
2. Load prior state (or create new state if none exists)
3. Call adapter `Import` with resource type, name, and identifier
4. Update or create local state with adapter response
5. Save updated state with `last_refreshed` timestamp
6. Display imported resource information

---

### deploy adapter install

Install an adapter binary from the registry.

```bash
promptarena deploy adapter install <provider>[@<version>]
```

**Examples:**

```bash
# Install latest version
promptarena deploy adapter install agentcore

# Install specific version
promptarena deploy adapter install agentcore@0.2.0
```

**Process:**

1. Look up provider in the built-in adapter registry
2. Determine version (specified or latest from registry)
3. Download binary from GitHub Releases for current OS/architecture
4. Install to `~/.promptarena/adapters/promptarena-deploy-{provider}`
5. Set executable permissions (0755)

**Download URL format:**

```
https://github.com/{repo}/releases/download/v{version}/promptarena-deploy-{provider}_{os}_{arch}
```

---

### deploy adapter list

List all installed adapters.

```bash
promptarena deploy adapter list
```

**Output format:**

```
Installed adapters:
  agentcore  ~/.promptarena/adapters/promptarena-deploy-agentcore
  custom     .promptarena/adapters/promptarena-deploy-custom
```

Searches:

1. Project-local: `.promptarena/adapters/`
2. User-level: `~/.promptarena/adapters/`

---

### deploy adapter remove

Remove an installed adapter.

```bash
promptarena deploy adapter remove <provider>
```

**Examples:**

```bash
promptarena deploy adapter remove agentcore
```

Removes the binary from `~/.promptarena/adapters/`.

## Configuration

The deploy section in arena.yaml:

```yaml
deploy:
  provider: agentcore          # Required: adapter name
  config:                       # Optional: base provider config
    region: us-west-2
  environments:                 # Optional: per-environment overrides
    production:
      config:
        region: us-east-1
```

## File Paths

| Path | Description |
|------|-------------|
| `arena.yaml` | Default config file |
| `*.pack.json` | Auto-detected pack files |
| `.promptarena/deploy.state` | Deployment state (JSON) |
| `.promptarena/deploy.lock` | Deploy lock file (prevents concurrent access) |
| `.promptarena/adapters/` | Project-local adapters |
| `~/.promptarena/adapters/` | User-level adapters |

## See Also

- [Configure Deploy](../../how-to/deploy/configure) — Configuration guide
- [Plan and Apply](../../how-to/deploy/plan-and-apply) — Deployment workflows
- [Protocol](protocol) — JSON-RPC method details
