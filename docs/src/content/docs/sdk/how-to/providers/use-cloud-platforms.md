---
title: Run models on Bedrock, Vertex AI or Azure
description: Host a provider on AWS Bedrock, Google Vertex AI or Azure with WithBedrock, WithVertex and WithAzure, and pick the credential each platform uses.
sidebar:
  order: 40
verified:
  commit: 8489bf3d95c7498f9de0f024769db9399c24e884
  sources:
    - runtime/credentials/aws_integration.go
    - runtime/credentials/azure_integration.go
    - runtime/credentials/gcp_integration.go
    - sdk/options.go
    - sdk/platform_credential.go
    - sdk/sdk.go
---

Run a model through a cloud platform and authenticate with the platform's own credentials with no API key.

## Prerequisites

- Platform credentials available to the process: an AWS credential chain, Application Default Credentials, or an Azure identity.
- A pack and prompt to open (see [Open a conversation](/sdk/how-to/conversations/initialize/)).

## Steps

### 1. Pick the platform option

Each option takes the provider type that handles messages for the model, such as `claude`, `gemini` or `openai`, and the model name.

| Option | Signature | Default credential |
|---|---|---|
| `sdk.WithBedrock` | `(region, providerType, model string, opts ...PlatformOption)` | The AWS default chain: IRSA, instance profile, environment variables. |
| `sdk.WithVertex` | `(region, project, providerType, model string, opts ...PlatformOption)` | Application Default Credentials. |
| `sdk.WithAzure` | `(endpoint, providerType, model string, opts ...PlatformOption)` | The Azure default chain: managed identity, Azure CLI and others. |

```go
conv, err := sdk.Open("./chat.pack.json", "assistant",
	sdk.WithBedrock("us-west-2", "claude", "claude-sonnet-4-20250514"),
)
if err != nil {
	return err
}
defer conv.Close()
```

The Azure endpoint is your account host, such as `https://my-resource.openai.azure.com`.

### 2. Override the credential

Pass a platform option as the last arguments when the default chain is not the credential you want.

| Platform | Option | Credential used |
|---|---|---|
| Bedrock | `sdk.WithAWSRoleARN(arn)` | STS role assumption. |
| Bedrock | `sdk.WithAWSProfile(profile)` | The named profile in `~/.aws/credentials` or `~/.aws/config`. |
| Vertex | `sdk.WithGCPServiceAccount(keyPath)` | The service account key file. |
| Azure | `sdk.WithAzureClientSecret(tenantID, clientID, secret)` | A service principal secret. |
| Azure | `sdk.WithAzureManagedIdentity(clientID)` | The user-assigned managed identity with that client ID. |

```go
sdk.WithBedrock("us-west-2", "claude", "claude-sonnet-4-20250514",
	sdk.WithAWSRoleARN("arn:aws:iam::123456789012:role/BedrockAccess"),
)
```

When you set more than one override for a platform, one wins:

- Bedrock: the role ARN beats the profile.
- Azure: the client secret beats the managed identity.
- Azure: an empty `clientID` for `WithAzureManagedIdentity` selects nothing, so the default chain applies.

### 3. Adjust region, project or endpoint

`sdk.WithPlatformRegion`, `sdk.WithPlatformProject` and `sdk.WithPlatformEndpoint` set the same values the constructor takes. For Bedrock and Vertex, an endpoint replaces the derived base URL. For Azure, the endpoint stays the account host and the `openai` provider builds the deployment path under it.

## Verify

`Open` fails when the credential cannot be created. The error starts with `bedrock credentials:`, `vertex credentials:` or `azure credentials:`, followed by the cloud SDK's reason.

## Behavior to know

- On Bedrock, a model name that appears in the SDK's Bedrock model mapping is replaced with the Bedrock model ID.
- Vertex derives a different base URL for `claude`, `gemini` and other provider types.

## Related

- [Configure embedding providers declaratively](/sdk/how-to/providers/declarative-embedding-providers/)
- [Providers](/concepts/providers/)
