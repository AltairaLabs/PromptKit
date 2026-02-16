---
title: Configure Deploy
sidebar:
  order: 2
---

## Goal

Set up the deploy section in arena.yaml to target a cloud provider.

## Prerequisites

- An adapter installed (see [Install Adapters](install-adapters))
- An existing arena.yaml file

## Basic Configuration

Add a `deploy` section to your arena.yaml:

```yaml
deploy:
  provider: agentcore
  config:
    region: us-west-2
```

| Field | Required | Description |
|-------|----------|-------------|
| `provider` | Yes | Adapter name (matches the binary `promptarena-deploy-{provider}`) |
| `config` | No | Provider-specific configuration (passed as JSON to the adapter) |
| `environments` | No | Per-environment config overrides |

## Provider Config

The `config` section is opaque to the CLI — its contents are defined by each adapter. Check your adapter's documentation for supported fields.

Example for the agentcore adapter:

```yaml
deploy:
  provider: agentcore
  config:
    region: us-west-2
    account_id: "123456789012"
    instance_type: t3.medium
    timeout: 300
```

## Environment Overrides

Add environment-specific configuration that merges with the base config:

```yaml
deploy:
  provider: agentcore
  config:
    region: us-west-2
    instance_type: t3.medium
  environments:
    dev:
      config:
        instance_type: t3.small
    staging:
      config:
        instance_type: t3.medium
    production:
      config:
        region: us-east-1
        instance_type: c5.large
        enable_autoscaling: true
```

### How Merging Works

When you deploy with `--env production`, the CLI:

1. Starts with the base `config`
2. Merges in `environments.production.config`
3. Later values override earlier ones
4. New keys are added

**Effective config for `--env production`:**

```json
{
  "region": "us-east-1",
  "instance_type": "c5.large",
  "enable_autoscaling": true
}
```

**Effective config for `--env dev`:**

```json
{
  "region": "us-west-2",
  "instance_type": "t3.small"
}
```

## Validate Configuration

Test that your configuration is valid:

```bash
# Plan will validate config through the adapter
promptarena deploy plan
```

If the adapter supports config validation, it will report errors before planning:

```
Error: invalid config: region "invalid-region" not supported
```

## Specifying the Pack File

By default, the CLI auto-detects `*.pack.json` files in your project directory. To specify explicitly:

```bash
promptarena deploy --pack dist/app.pack.json
```

## Specifying the Config File

By default, the CLI looks for `arena.yaml`. To use a different file:

```bash
promptarena deploy --config deploy.yaml
```

## Minimal Example

The simplest possible deploy configuration:

```yaml
deploy:
  provider: agentcore
```

This uses the adapter's defaults with no custom config and the `"default"` environment.

## Complete Example

A full configuration with multiple environments:

```yaml
# arena.yaml
prompt_configs:
  - id: greeting
    file: prompts/greeting.yaml

deploy:
  provider: agentcore
  config:
    region: us-west-2
    account_id: "123456789012"
    instance_type: t3.medium
    timeout: 300
  environments:
    dev:
      config:
        instance_type: t3.small
        timeout: 60
    staging:
      config:
        timeout: 300
    production:
      config:
        region: us-east-1
        instance_type: c5.large
        timeout: 600
        enable_autoscaling: true
```

## Troubleshooting

### Error: deploy section not found

Ensure your arena.yaml has a `deploy` key at the top level:

```yaml
deploy:
  provider: agentcore
```

### Error: provider required

The `provider` field is mandatory. Add it to your deploy config.

### Error: adapter not found for provider

The CLI can't find a binary named `promptarena-deploy-{provider}`. Install the adapter:

```bash
promptarena deploy adapter install agentcore
```

## See Also

- [Install Adapters](install-adapters) — Install the adapter before configuring
- [Plan and Apply](plan-and-apply) — Use your configuration to deploy
- [Multi-Environment Tutorial](../tutorials/02-multi-environment) — Step-by-step environment setup
