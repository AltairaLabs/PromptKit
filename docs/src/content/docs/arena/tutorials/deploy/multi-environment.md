---
title: 'Deploy: Multi-Environment Deployments'
---

Deploy the same prompt pack to dev, staging, and production environments from a single configuration.

## What You'll Build

A multi-environment deployment setup where each environment has its own configuration overrides, independent state tracking, and separate cloud resources.

## Learning Objectives

In this tutorial, you'll learn to:

- Configure multiple environments in arena.yaml
- Override base config per environment
- Deploy to a specific environment
- Manage independent environment state
- Promote packs across environments

## Time Required

**25 minutes**

## Prerequisites

- Completed [Tutorial: First Deployment](first-deployment)
- An installed deploy adapter
- A compiled `.pack.json` file

## Step 1: Configure Environments

Update your `arena.yaml` deploy section with environment-specific overrides:

```yaml
deploy:
  provider: agentcore
  config:
    region: us-west-2
    instance_type: t3.medium
    timeout: 300
  environments:
    dev:
      config:
        instance_type: t3.small
        timeout: 60
    staging:
      config:
        instance_type: t3.medium
        timeout: 300
    production:
      config:
        region: us-east-1
        instance_type: c5.large
        timeout: 600
        enable_autoscaling: true
```

**How config merging works:**

The base `config` provides defaults. Each environment's `config` is merged on top, overriding matching keys and adding new ones.

For the `production` environment, the effective config is:

```json
{
  "region": "us-east-1",
  "instance_type": "c5.large",
  "timeout": 600,
  "enable_autoscaling": true
}
```

## Step 2: Deploy to Dev

Start by deploying to the dev environment:

```bash
promptarena deploy plan --env dev
```

**Expected output:**

```
Planning deployment (env: dev)...
Provider: agentcore v0.2.0

Changes:
  + agent_runtime.greeting    Create agent runtime
  + a2a_endpoint.greeting     Create A2A endpoint

Summary: 2 resources to create, 0 to update, 0 to delete
```

Apply the dev deployment:

```bash
promptarena deploy --env dev
```

## Step 3: Deploy to Staging

Deploy the same pack to staging with different configuration:

```bash
promptarena deploy --env staging
```

Each environment maintains its own state independently. Deploying to staging does not affect the dev deployment.

## Step 4: Check Environment Status

Check the status of each environment:

```bash
# Check dev
promptarena deploy status --env dev

# Check staging
promptarena deploy status --env staging
```

**Expected output (dev):**

```
Status: deployed
Last deployed: 2026-02-16T10:30:00Z
Pack checksum: sha256:abc123...

Resources:
  agent_runtime.greeting: healthy
  a2a_endpoint.greeting: healthy
```

## Step 5: Promote to Production

After validating in dev and staging, deploy to production:

```bash
# Preview production changes
promptarena deploy plan --env production

# Apply to production
promptarena deploy --env production
```

Production uses different instance types and enables autoscaling as configured in Step 1.

## Step 6: Update a Single Environment

When you update your pack, you can redeploy to a single environment:

```bash
# Recompile after prompt changes
packc compile --config arena.yaml --output app.pack.json --id my-app

# Deploy updated pack to dev first
promptarena deploy --env dev

# Plan will show updates instead of creates
promptarena deploy plan --env dev
```

**Expected output:**

```
Planning deployment (env: dev)...

Changes:
  ~ agent_runtime.greeting    Update agent runtime
    a2a_endpoint.greeting     No change

Summary: 0 to create, 1 to update, 0 to delete
```

## Step 7: Clean Up Environments

Tear down environments you no longer need:

```bash
# Destroy dev
promptarena deploy destroy --env dev

# Destroy staging
promptarena deploy destroy --env staging

# Destroy production
promptarena deploy destroy --env production
```

Each destroy only affects the targeted environment.

## What You Learned

Congratulations! You've successfully:

- Configured multiple environments with overrides
- Deployed independently to dev, staging, and production
- Managed per-environment state
- Promoted packs across environments
- Updated and torn down individual environments

## Environment Strategy Tips

### Keep Base Config Minimal

Put shared defaults in the base `config`. Only override what differs per environment:

```yaml
deploy:
  provider: agentcore
  config:
    region: us-west-2          # shared default
  environments:
    production:
      config:
        region: us-east-1      # only override what's different
```

### Use Consistent Naming

The `--env` flag value is passed directly to the adapter as the environment name. Use consistent, lowercase names:

```bash
# Good
promptarena deploy --env production
promptarena deploy --env staging

# Avoid
promptarena deploy --env PROD
promptarena deploy --env Staging-v2
```

### Default Environment

When no `--env` flag is provided, the environment defaults to `"default"`. This is fine for single-environment projects.

## Common Issues

### Issue: wrong environment deployed

**Solution:** Always use `deploy plan` before `deploy` to preview the target environment:

```bash
promptarena deploy plan --env production
```

### Issue: environments sharing state

Each environment is tracked independently. If you see unexpected state, check that you're using the correct `--env` flag consistently.

## Next Steps

Now that you understand multi-environment deployments, explore:

- **[How-To: Configure Deploy](../../how-to/deploy/configure)** — Advanced configuration patterns
- **[How-To: CI/CD Integration](../../how-to/deploy/ci-cd)** — Automate multi-environment deployments
- **[Explanation: State Management](../../explanation/deploy/state-management)** — How state tracking works per environment
