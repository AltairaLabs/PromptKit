---
title: Cloud Provider Examples
sidebar:
  order: 15
---

Examples of configuring PromptKit with cloud hyperscaler platforms.

## Overview

PromptKit supports running LLMs on cloud platforms in addition to direct API access. This provides enterprise-grade security, managed authentication, and unified billing.

## AWS Bedrock

Use Claude models via AWS Bedrock with IAM-based authentication.

### Provider Configuration

```yaml
# providers/claude-bedrock.provider.yaml
apiVersion: promptkit.altairalabs.ai/v1alpha1
kind: Provider
metadata:
  name: claude-bedrock
  labels:
    platform: aws
    environment: production

spec:
  type: claude
  model: claude-3-5-sonnet-20241022
  platform:
    type: bedrock
    region: us-west-2
  defaults:
    temperature: 0.7
    max_tokens: 1000
```

### Authentication

Bedrock uses the AWS SDK credential chain automatically:

**For EKS with IRSA (recommended):**
```yaml
# ServiceAccount with IAM role
apiVersion: v1
kind: ServiceAccount
metadata:
  name: promptkit
  annotations:
    eks.amazonaws.com/role-arn: arn:aws:iam::123456789012:role/bedrock-access
```

**For EC2/ECS:**
- Uses instance/task IAM roles automatically

**For local development:**
```bash
export AWS_ACCESS_KEY_ID="AKIA..."
export AWS_SECRET_ACCESS_KEY="..."
export AWS_REGION="us-west-2"
```

### Required IAM Permissions

```json
{
  "Version": "2012-10-17",
  "Statement": [
    {
      "Effect": "Allow",
      "Action": [
        "bedrock:InvokeModel",
        "bedrock:InvokeModelWithResponseStream"
      ],
      "Resource": "arn:aws:bedrock:us-west-2::foundation-model/anthropic.claude-*"
    }
  ]
}
```

### Model Mapping

PromptKit automatically maps model names:

| PromptKit Model | Bedrock Model ID |
|-----------------|------------------|
| `claude-3-5-sonnet-20241022` | `anthropic.claude-3-5-sonnet-20241022-v2:0` |
| `claude-3-opus-20240229` | `anthropic.claude-3-opus-20240229-v1:0` |
| `claude-3-haiku-20240307` | `anthropic.claude-3-haiku-20240307-v1:0` |

## GCP Vertex AI

Use Claude or Gemini models via Vertex AI with GCP authentication.

### Provider Configuration

```yaml
# providers/claude-vertex.provider.yaml
apiVersion: promptkit.altairalabs.ai/v1alpha1
kind: Provider
metadata:
  name: claude-vertex
  labels:
    platform: gcp
    environment: production

spec:
  type: claude
  model: claude-3-5-sonnet-20241022
  platform:
    type: vertex
    region: us-central1
    project: my-gcp-project
  defaults:
    temperature: 0.7
    max_tokens: 1000
```

### Authentication

Vertex uses GCP Application Default Credentials:

**For GKE with Workload Identity (recommended):**
```yaml
# ServiceAccount with Workload Identity
apiVersion: v1
kind: ServiceAccount
metadata:
  name: promptkit
  annotations:
    iam.gke.io/gcp-service-account: promptkit@my-project.iam.gserviceaccount.com
```

**For Compute Engine/Cloud Run:**
- Uses attached service account automatically

**For local development:**
```bash
export GOOGLE_APPLICATION_CREDENTIALS="/path/to/service-account.json"
# or
gcloud auth application-default login
```

### Required IAM Roles

- `roles/aiplatform.user` - For Vertex AI API access

## Azure AI Foundry

Use OpenAI models via Azure with Azure AD authentication.

### Provider Configuration

```yaml
# providers/gpt4-azure.provider.yaml
apiVersion: promptkit.altairalabs.ai/v1alpha1
kind: Provider
metadata:
  name: gpt4-azure
  labels:
    platform: azure
    environment: production

spec:
  type: openai
  model: gpt-4o
  platform:
    type: azure
    endpoint: https://my-resource.openai.azure.com
  defaults:
    temperature: 0.7
    max_tokens: 1000
```

### Authentication

Azure uses the Azure SDK credential chain:

**For AKS with Managed Identity (recommended):**
```yaml
# Pod identity binding
apiVersion: aadpodidentity.k8s.io/v1
kind: AzureIdentityBinding
metadata:
  name: promptkit-binding
spec:
  azureIdentity: promptkit-identity
  selector: promptkit
```

**For Azure VMs:**
- Uses system-assigned managed identity automatically

**For local development:**
```bash
export AZURE_CLIENT_ID="..."
export AZURE_TENANT_ID="..."
export AZURE_CLIENT_SECRET="..."
# or
az login
```

### Required Azure Roles

- `Cognitive Services OpenAI User` - For Azure OpenAI API access

## Multi-Cloud Arena Configuration

Use providers from multiple clouds in a single Arena:

```yaml
# arena.yaml
apiVersion: promptkit.altairalabs.ai/v1alpha1
kind: Arena
metadata:
  name: multi-cloud-test

spec:
  providers:
    - file: providers/claude-bedrock.provider.yaml
    - file: providers/claude-vertex.provider.yaml
    - file: providers/gpt4-azure.provider.yaml
    - file: providers/openai-direct.provider.yaml

  prompt_configs:
    - id: assistant
      file: prompts/assistant.yaml

  scenarios:
    - file: scenarios/comparison-test.yaml

  defaults:
    concurrency: 4
    output:
      formats: [json, html]
```

## Per-Provider Credential Example

Use different API keys for the same provider type:

```yaml
# arena.yaml
spec:
  providers:
    # Production OpenAI
    - file: providers/openai-prod.provider.yaml

    # Development OpenAI (different key, cheaper model)
    - file: providers/openai-dev.provider.yaml
```

```yaml
# providers/openai-prod.provider.yaml
spec:
  id: openai-prod
  type: openai
  model: gpt-4o
  credential:
    credential_env: OPENAI_PROD_KEY  # Uses OPENAI_PROD_KEY env var
```

```yaml
# providers/openai-dev.provider.yaml
spec:
  id: openai-dev
  type: openai
  model: gpt-4o-mini
  credential:
    credential_env: OPENAI_DEV_KEY   # Uses OPENAI_DEV_KEY env var
```

## Credential File Example

Store API keys in files (useful with Kubernetes secrets):

```yaml
# providers/openai-secret.provider.yaml
spec:
  type: openai
  model: gpt-4o
  credential:
    credential_file: /run/secrets/openai-api-key
```

```yaml
# Kubernetes Secret mount
apiVersion: v1
kind: Pod
spec:
  containers:
    - name: promptkit
      volumeMounts:
        - name: openai-secret
          mountPath: /run/secrets
  volumes:
    - name: openai-secret
      secret:
        secretName: openai-api-key
```

## Running the Examples

```bash
# AWS Bedrock (requires AWS credentials)
promptarena run -c examples/cloud-providers/bedrock-arena.yaml

# GCP Vertex (requires GCP credentials)
promptarena run -c examples/cloud-providers/vertex-arena.yaml

# Azure (requires Azure credentials)
promptarena run -c examples/cloud-providers/azure-arena.yaml

# Multi-cloud comparison
promptarena run -c examples/cloud-providers/multi-cloud-arena.yaml
```

## Best Practices

1. **Use managed identities** - Avoid static credentials when running in cloud environments
2. **Use credential_env for multi-tenant** - Keep different environments isolated
3. **Use credential_file with secrets managers** - Integrate with Kubernetes secrets, Vault, etc.
4. **Set appropriate regions** - Choose regions close to your workloads for lower latency
5. **Monitor costs** - Cloud provider pricing may differ from direct API access

## See Also

- [Provider Configuration](../reference/config-schema#provider) - Full provider schema
- [Provider Concepts](/concepts/providers) - Provider architecture
- [Runtime Provider Reference](/runtime/reference/providers) - API documentation
