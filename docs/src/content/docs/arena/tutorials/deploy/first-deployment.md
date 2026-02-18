---
title: 'Deploy: Your First Deployment'
---

Deploy a compiled prompt pack to a cloud provider in 20 minutes.

## What You'll Build

A fully deployed prompt pack running on a cloud provider, managed through the PromptKit CLI with state tracking and status monitoring.

## Learning Objectives

In this tutorial, you'll learn to:

- Install a deploy adapter
- Configure the deploy section in arena.yaml
- Preview a deployment plan
- Apply the deployment
- Check deployment status
- Tear down resources

## Time Required

**20 minutes**

## Prerequisites

- PromptKit CLI (`promptarena`) installed
- A compiled `.pack.json` file (see [Your First Pack](/packc/tutorials/01-first-pack/))
- Access to AWS (for the agentcore adapter example)
- AWS credentials configured in your environment

## Step 1: Install an Adapter

Adapters are plugin binaries that connect the deploy CLI to specific cloud providers. Install the AgentCore adapter for AWS Bedrock:

```bash
promptarena deploy adapter install agentcore
```

**Expected output:**

```
Installed adapter: agentcore v0.2.0
  Location: ~/.promptarena/adapters/promptarena-deploy-agentcore
```

Verify the installation:

```bash
promptarena deploy adapter list
```

**Expected output:**

```
Installed adapters:
  agentcore  ~/.promptarena/adapters/promptarena-deploy-agentcore
```

**What happened:**

1. The CLI fetched the adapter binary from the GitHub release
2. Downloaded the correct binary for your OS and architecture
3. Installed it to `~/.promptarena/adapters/` with execute permissions

## Step 2: Add Deploy Configuration

Add a `deploy` section to your `arena.yaml`:

```yaml
# arena.yaml (add to existing config)
deploy:
  provider: agentcore
  config:
    region: us-west-2
```

The `provider` field tells the CLI which adapter binary to use. The `config` section is passed directly to the adapter — its contents depend on the provider.

## Step 3: Preview the Plan

Before making any changes, preview what the deployment will do:

```bash
promptarena deploy plan
```

**Expected output:**

```
Planning deployment...
Provider: agentcore v0.2.0

Changes:
  + agent_runtime.greeting    Create agent runtime
  + a2a_endpoint.greeting     Create A2A endpoint

Summary: 2 resources to create, 0 to update, 0 to delete
```

**What happened:**

1. The CLI loaded your arena.yaml and found the deploy config
2. Auto-detected the `.pack.json` file in your project
3. Launched the adapter binary and sent it the pack + config via JSON-RPC
4. The adapter analyzed the pack and reported what resources need to be created

The symbols indicate the action for each resource:

| Symbol | Action |
|--------|--------|
| `+` | Create |
| `~` | Update |
| `-` | Delete |
| ` ` | No change |

## Step 4: Apply the Deployment

Apply the plan to create the resources:

```bash
promptarena deploy
```

**Expected output:**

```
Planning deployment...
Applying...
  Creating agent_runtime.greeting... done
  Creating a2a_endpoint.greeting... done
Deployment complete.
```

**What happened:**

1. The CLI ran a plan (same as Step 3)
2. Sent an apply request to the adapter
3. The adapter created resources on the cloud provider
4. State was saved to `.promptarena/deploy.state`

## Step 5: Check Status

Verify the deployment is healthy:

```bash
promptarena deploy status
```

**Expected output:**

```
Status: deployed
Last deployed: 2026-02-16T10:30:00Z
Pack checksum: sha256:abc123...

Resources:
  agent_runtime.greeting: healthy
  a2a_endpoint.greeting: healthy
```

## Step 6: Clean Up

When you're done, tear down the resources:

```bash
promptarena deploy destroy
```

**Expected output:**

```
Destroying deployment...
  Deleting a2a_endpoint.greeting... done
  Deleting agent_runtime.greeting... done
Deployment destroyed.
```

This removes cloud resources and deletes the local state file.

## What You Learned

Congratulations! You've successfully:

- Installed a deploy adapter plugin
- Configured deployment in arena.yaml
- Previewed changes with `deploy plan`
- Applied a deployment
- Monitored deployment status
- Torn down resources with `deploy destroy`

## Understanding the Workflow

The deploy workflow follows a plan-apply pattern:

1. **Plan** — Adapter analyzes the pack and config, reports what will change
2. **Apply** — Adapter creates, updates, or deletes cloud resources
3. **State** — CLI persists deployment state locally for future operations
4. **Status** — Adapter queries the provider for current resource health
5. **Destroy** — Adapter removes all managed resources

## Common Issues

### Issue: adapter not found

**Solution:** Install the adapter first:

```bash
promptarena deploy adapter install agentcore
```

### Issue: no pack file found

**Solution:** Compile your pack before deploying:

```bash
packc compile --config arena.yaml --output app.pack.json --id my-app
```

Or specify the pack file explicitly:

```bash
promptarena deploy --pack app.pack.json
```

### Issue: provider authentication failed

**Solution:** Ensure your cloud credentials are configured:

```bash
# For AWS
aws configure
# Or set environment variables
export AWS_ACCESS_KEY_ID=...
export AWS_SECRET_ACCESS_KEY=...
export AWS_REGION=us-west-2
```

## Next Steps

Now that you've deployed your first pack, you're ready to:

- **[Tutorial: Multi-Environment](multi-environment)** — Deploy to dev, staging, and production
- **[How-To: Plan and Apply](../../how-to/deploy/plan-and-apply)** — Learn advanced deployment workflows
- **[Reference: CLI Commands](../../reference/deploy/cli-commands)** — Complete command documentation
