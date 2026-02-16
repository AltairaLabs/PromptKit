---
title: Adapter Architecture
sidebar:
  order: 1
---

## Overview

Deploy uses a **plugin architecture** where adapter binaries handle provider-specific logic. The CLI communicates with adapters via JSON-RPC 2.0 over stdio, creating a clean separation between the deployment workflow and cloud provider integration.

## Why Plugins?

Cloud providers have vastly different APIs, resource models, authentication mechanisms, and deployment patterns. Embedding all provider logic in the CLI would create:

- **Dependency bloat** — Every provider SDK linked into one binary
- **Release coupling** — Provider changes require CLI updates
- **Testing complexity** — Integration tests for every cloud provider

The plugin pattern avoids these problems. Each adapter is an independent binary with its own dependencies, release cycle, and test suite.

## Communication Model

```d2
direction: right

cli: CLI Process {
  shape: rectangle
  label: "promptarena deploy\n(parent process)"
}

stdin: stdin {
  shape: rectangle
  label: "JSON-RPC\nrequest"
}

stdout: stdout {
  shape: rectangle
  label: "JSON-RPC\nresponse"
}

adapter: Adapter Process {
  shape: rectangle
  label: "promptarena-deploy-{provider}\n(child process)"
}

cli -> stdin -> adapter
adapter -> stdout -> cli
```

The CLI launches the adapter binary as a child process. Communication flows through:

- **stdin** — CLI sends JSON-RPC requests
- **stdout** — Adapter sends JSON-RPC responses
- **stderr** — Available for adapter debug logging (not captured by CLI)

This model is simple, portable, and requires no network configuration. The adapter binary is a standalone executable with no shared memory or sockets.

## Adapter Discovery

When the CLI needs an adapter, it searches three locations in order:

1. **Project-local**: `.promptarena/adapters/promptarena-deploy-{provider}`
2. **User-level**: `~/.promptarena/adapters/promptarena-deploy-{provider}`
3. **System PATH**: Standard `$PATH` lookup

This precedence lets you:

- Pin a project to a specific adapter version (project-local)
- Install adapters once for all projects (user-level)
- Use system-installed adapters (PATH)

## Binary Naming Convention

All adapter binaries follow the naming pattern:

```
promptarena-deploy-{provider}
```

For example:

- `promptarena-deploy-agentcore` — AWS Bedrock AgentCore
- `promptarena-deploy-cloudrun` — Google Cloud Run
- `promptarena-deploy-lambda` — AWS Lambda

During installation, platform-specific suffixes (`_darwin_arm64`, `_linux_amd64`) are stripped to the canonical name.

## The Provider Interface

Every adapter implements the `Provider` interface:

```go
type Provider interface {
    GetProviderInfo(ctx context.Context) (*ProviderInfo, error)
    ValidateConfig(ctx context.Context, req *ValidateRequest) (*ValidateResponse, error)
    Plan(ctx context.Context, req *PlanRequest) (*PlanResponse, error)
    Apply(ctx context.Context, req *PlanRequest, callback ApplyCallback) (adapterState string, err error)
    Destroy(ctx context.Context, req *DestroyRequest, callback DestroyCallback) error
    Status(ctx context.Context, req *StatusRequest) (*StatusResponse, error)
}
```

### Method Responsibilities

| Method | Purpose |
|--------|---------|
| `GetProviderInfo` | Return adapter name, version, and capabilities |
| `ValidateConfig` | Check that provider config is valid before planning |
| `Plan` | Analyze pack + config and report what resources will change |
| `Apply` | Create, update, or delete resources on the cloud provider |
| `Destroy` | Remove all managed resources |
| `Status` | Query current resource health from the cloud provider |

### Data Flow

Each method receives structured input and returns structured output:

1. **Plan** receives the serialized pack, deployment config, environment name, and prior adapter state. It returns a list of resource changes with actions (CREATE, UPDATE, DELETE, NO_CHANGE).

2. **Apply** receives the same input as Plan. It performs the actual resource operations and returns an opaque state string that the CLI persists for future operations.

3. **Destroy** receives the deployment config, environment, and prior state. It removes all resources and returns no state.

4. **Status** receives the deployment config, environment, and prior state. It returns the overall deployment status and per-resource health.

## Adapter State

The state string returned by `Apply` is **opaque** to the CLI. The CLI stores it in the state file and passes it back to the adapter on subsequent operations. The adapter can encode whatever it needs:

- Resource IDs and ARNs
- Configuration hashes
- Deployment metadata
- Version information

This design lets each adapter track state in whatever format suits its provider, without the CLI needing to understand provider-specific resource models.

## Adapter Lifecycle

```
CLI starts adapter binary
  ↓
Adapter reads JSON-RPC requests from stdin
  ↓
Adapter calls Provider methods
  ↓
Adapter writes JSON-RPC responses to stdout
  ↓
CLI closes stdin → adapter exits
```

The adapter process lives only for the duration of a single CLI command. It is not a long-running daemon. This keeps the model simple and avoids issues with stale connections or process management.

## Buffer Management

The JSON-RPC communication uses buffered I/O with:

- **Initial buffer**: 64 KB
- **Max line size**: 10 MB

The large max size accommodates pack payloads, which can be substantial for projects with many prompts, tools, and resources.

## Tradeoffs

### Benefits

- **Independent releases** — Adapter bugs don't require CLI updates
- **Language flexibility** — Adapters can be written in any language (though the Go SDK is provided)
- **Simple protocol** — JSON-RPC over stdio is easy to debug and test
- **No network dependencies** — Local IPC, no ports or TLS needed

### Limitations

- **Process overhead** — Each command spawns a new process
- **No streaming** — Current protocol collects events in final response rather than true streaming
- **Binary distribution** — Users must download platform-specific binaries

## See Also

- [Protocol](../reference/protocol) — JSON-RPC method specifications
- [Adapter SDK](../reference/adapter-sdk) — Build your own adapter
- [State Management](state-management) — How state is persisted
