---
title: Plan and Apply
sidebar:
  order: 3
---

## Goal

Preview deployment changes, apply them, check status, and tear down resources.

## Prerequisites

- Deploy section configured in arena.yaml (see [Configure Deploy](configure-deploy))
- A compiled `.pack.json` file
- An installed adapter

## Plan a Deployment

Preview what changes will be made without modifying anything:

```bash
promptarena deploy plan
```

Target a specific environment:

```bash
promptarena deploy plan --env production
```

**Example output:**

```
Planning deployment (env: production)...
Provider: agentcore v0.2.0

Changes:
  + agent_runtime.greeting    Create agent runtime
  + a2a_endpoint.greeting     Create A2A endpoint

Summary: 2 resources to create, 0 to update, 0 to delete
```

### Reading the Plan

| Symbol | Action | Description |
|--------|--------|-------------|
| `+` | CREATE | New resource will be created |
| `~` | UPDATE | Existing resource will be modified |
| `-` | DELETE | Resource will be removed |
| ` ` | NO_CHANGE | Resource exists and needs no changes |

## Apply a Deployment

Run both plan and apply in one step:

```bash
promptarena deploy
```

Or target an environment:

```bash
promptarena deploy --env staging
```

**What happens:**

1. The CLI loads arena.yaml and merges environment config
2. Auto-detects or uses the specified pack file
3. Sends a plan request to the adapter
4. Sends an apply request with streaming progress
5. Saves state to `.promptarena/deploy.state`

### Specifying a Pack File

```bash
promptarena deploy --pack dist/app.pack.json
```

### Using a Different Config File

```bash
promptarena deploy --config deploy.yaml
```

## Check Deployment Status

Query the current state of deployed resources:

```bash
promptarena deploy status
```

Or for a specific environment:

```bash
promptarena deploy status --env production
```

**Example output:**

```
Status: deployed
Last deployed: 2026-02-16T10:30:00Z
Pack checksum: sha256:abc123def456...

Resources:
  agent_runtime.greeting: healthy
  a2a_endpoint.greeting: healthy
```

### Status Values

| Status | Meaning |
|--------|---------|
| `deployed` | All resources are running |
| `not_deployed` | No resources found |
| `degraded` | Some resources are unhealthy |
| `unknown` | Unable to determine status |

### Resource Health

| Health | Meaning |
|--------|---------|
| `healthy` | Resource is running normally |
| `unhealthy` | Resource exists but has issues |
| `missing` | Resource was expected but not found |

## Destroy a Deployment

Remove all managed resources:

```bash
promptarena deploy destroy --env staging
```

**What happens:**

1. Loads prior state (exits if no state exists)
2. Sends a destroy request to the adapter
3. The adapter removes all managed resources
4. Deletes the local state file

## Redeployment Workflow

When you update a pack and redeploy, the adapter receives the prior state and can determine what changed:

```bash
# Update your prompt
vim prompts/greeting.yaml

# Recompile
packc compile --config arena.yaml --output app.pack.json --id my-app

# Plan shows updates
promptarena deploy plan --env production

# Apply updates
promptarena deploy --env production
```

The plan will show `~` (UPDATE) for resources that need modification and empty space for resources with no changes.

## Common Patterns

### Deploy to All Environments

```bash
# Deploy in order: dev → staging → production
promptarena deploy --env dev
promptarena deploy --env staging
promptarena deploy --env production
```

### Quick Status Check

```bash
for env in dev staging production; do
  echo "=== $env ==="
  promptarena deploy status --env $env
done
```

### Safe Production Deploy

Always plan before applying to production:

```bash
# Review the plan
promptarena deploy plan --env production

# Apply after review
promptarena deploy apply --env production
```

## Troubleshooting

### Error: no prior state found

The `status` and `destroy` commands require a prior deployment. Deploy first:

```bash
promptarena deploy --env production
```

### Error: no pack file found

Compile your pack first, or specify the path:

```bash
promptarena deploy --pack path/to/app.pack.json
```

### Deployment shows no changes

If plan shows all resources as NO_CHANGE, the pack and config haven't changed since the last deploy. Update your pack or config and try again.

## See Also

- [Configure Deploy](configure-deploy) — Set up arena.yaml
- [CI/CD Integration](ci-cd-integration) — Automate plan and apply
- [CLI Commands](../reference/cli-commands) — Complete flag reference
- [State Management](../explanation/state-management) — How state tracking works
