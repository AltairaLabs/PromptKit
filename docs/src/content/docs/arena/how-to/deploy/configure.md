---
title: 'Deploy: Configure'
---

## Goal

Set up the deploy section in arena.yaml to target a cloud provider.

## Prerequisites

- An adapter installed (see [Install Adapters](/arena/how-to/deploy/install-adapters/))
- An existing arena.yaml file

## Basic Configuration

Add a `deploy` section to your arena.yaml. Lead with the Omnia provider (the primary Altaira platform target):

```yaml
deploy:
  provider: omnia
  config:
    api_endpoint: https://omnia.example.com
    workspace: my-workspace
```

| Field | Required | Description |
|-------|----------|-------------|
| `provider` | Yes | Adapter name (matches the binary `promptarena-deploy-{provider}`) |
| `config` | No | Provider-specific configuration (passed as JSON to the adapter) |
| `environments` | No | Per-environment config overrides |

## Provider Config

The `config` section is opaque to the CLI — its contents are defined by each adapter. Check your adapter's documentation for supported fields.

### Omnia

The Omnia adapter targets an Altaira Omnia Kubernetes workspace. It needs the workspace API endpoint, the workspace name, an API token, and the provider bindings to wire into the deployed `AgentRuntime`:

```yaml
deploy:
  provider: omnia
  config:
    api_endpoint: https://omnia.example.com
    workspace: my-workspace
    api_token: ${OMNIA_API_TOKEN}        # or export OMNIA_API_TOKEN
    providers:
      - { name: default,    ref: claude-prod,  role: llm }
      - { name: embeddings, ref: text-embed-3, role: embedding }
    runtime:
      replicas: 2
      autoscaling:
        enabled: true
        type: hpa                        # hpa | keda (keda enables scale-to-zero)
        min_replicas: 2
        max_replicas: 10
    labels:
      team: platform
```

Key Omnia config fields:

| Field | Required | Description |
|-------|----------|-------------|
| `api_endpoint` | Yes | Omnia workspace API base URL |
| `workspace` | Yes | Omnia workspace name (lowercase alphanumeric + hyphens) |
| `api_token` | No | `omnia_sk_` bearer token (or set `OMNIA_API_TOKEN`) |
| `providers` | Yes | Role-aware bindings `[{name, ref, role}]` (roles: `llm`, `embedding`, `tts`, `stt`, `image`, `inference`) |
| `runtime` | No | Replica count, resource requests, and HPA/KEDA autoscaling passthrough |
| `labels` | No | Extra labels merged onto every created resource |

### AgentCore

The AWS Bedrock AgentCore adapter targets an AWS account and region:

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

By default, the CLI looks for `arena.yaml` (no `--config` flag needed). To use a different file:

```bash
promptarena deploy --config staging.arena.yaml
```

## Minimal Example

The simplest possible Omnia deploy configuration:

```yaml
deploy:
  provider: omnia
  config:
    api_endpoint: https://omnia.example.com
    workspace: my-workspace
    providers:
      - { name: default, ref: claude-prod, role: llm }
```

The equivalent for AgentCore, using the adapter's defaults and the `"default"` environment:

```yaml
deploy:
  provider: agentcore
```

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
  provider: omnia
```

### Error: provider required

The `provider` field is mandatory. Add it to your deploy config.

### Error: adapter not found for provider

The CLI can't find a binary named `promptarena-deploy-{provider}`. Install the adapter:

```bash
promptarena deploy adapter install omnia
```

## See Also

- [Install Adapters](/arena/how-to/deploy/install-adapters/) — Install the adapter before configuring
- [Plan and Apply](/arena/how-to/deploy/plan-and-apply/) — Use your configuration to deploy
- [Multi-Environment Tutorial](/arena/tutorials/deploy/multi-environment/) — Step-by-step environment setup
