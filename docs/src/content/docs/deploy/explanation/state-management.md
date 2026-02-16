---
title: State Management
sidebar:
  order: 2
---

## Overview

Deploy tracks deployment state in a local file (`.promptarena/deploy.state`) to enable incremental updates, drift detection, and clean teardown. This page explains what state is stored, when it changes, and why.

## Why State?

Without persistent state, the deploy system would need to:

- Query the cloud provider for every operation to discover existing resources
- Recreate resources that already exist
- Be unable to detect when a pack has changed since the last deployment
- Lack the information needed to tear down resources

State solves these problems by recording what was deployed, when, and what the adapter needs to manage those resources going forward.

## State File Location

```
project/
└── .promptarena/
    └── deploy.state
```

- **Directory permissions**: 0750
- **File permissions**: 0600 (read/write for owner only)
- **Format**: Indented JSON

The `.promptarena/` directory is created automatically when state is first saved.

## State Structure

```json
{
  "version": 1,
  "provider": "agentcore",
  "environment": "production",
  "last_deployed": "2026-02-16T10:30:00Z",
  "pack_version": "v1.0.0",
  "pack_checksum": "sha256:abc123def456...",
  "adapter_version": "0.2.0",
  "state": "<opaque adapter state>"
}
```

| Field | Description |
|-------|-------------|
| `version` | State file format version (currently `1`) |
| `provider` | Name of the adapter that created this state |
| `environment` | Target environment name |
| `last_deployed` | RFC 3339 timestamp of last successful deployment |
| `pack_version` | Version string from the deployed pack |
| `pack_checksum` | SHA-256 checksum of the pack file (`sha256:{hex}`) |
| `adapter_version` | Version of the adapter that performed the deployment |
| `state` | Opaque string from the adapter (resource IDs, metadata, etc.) |

## State Lifecycle

### Creation

State is created after a successful `deploy` or `deploy apply`:

```
deploy → plan → apply → save state
```

The CLI constructs the state from:

1. The provider name and environment from arena.yaml
2. The pack version and checksum from the pack file
3. The adapter version from `GetProviderInfo`
4. The opaque state string returned by `Apply`

### Reading

State is loaded for operations that need prior context:

- **`deploy plan`** — Passes prior state to the adapter so it can compare current vs. desired state
- **`deploy` / `deploy apply`** — Same as plan, plus persists updated state after apply
- **`deploy status`** — Passes prior state so the adapter can look up resource IDs
- **`deploy destroy`** — Passes prior state so the adapter knows what to tear down

### Update

State is overwritten after each successful `deploy` or `deploy apply`. The new state includes:

- Updated timestamp
- Updated pack checksum (if the pack changed)
- Updated adapter state (from the apply response)

### Deletion

State is deleted after a successful `deploy destroy`. This indicates no managed resources exist.

## Pack Checksums

The CLI computes a SHA-256 checksum of the pack file before deployment:

```
sha256:e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855
```

This checksum is stored in state and displayed in `deploy status` output. It helps detect when a pack has changed since the last deployment.

### Checksum Uses

- **Change detection** — The adapter can compare the current pack checksum against the prior state to determine if the pack has changed
- **Audit trail** — Know exactly which pack version is deployed
- **Drift detection** — Verify that what's deployed matches what's in your repository

## Opaque Adapter State

The `state` field is a string whose contents are entirely controlled by the adapter. The CLI:

- Stores it as-is after `Apply`
- Passes it back to the adapter on subsequent operations
- Never interprets or modifies it

Adapters typically encode:

- Cloud resource IDs (instance ARNs, endpoint URLs)
- Configuration hashes for change detection
- Deployment metadata (creation timestamps, tags)
- Internal version identifiers

This design lets each adapter track whatever provider-specific information it needs without the CLI needing to understand resource models across different cloud platforms.

## State and Environments

Each environment shares the same state file location. The environment name is recorded in the state file, so the CLI and adapter can track which environment was last deployed.

## State and CI/CD

In CI/CD environments, state is typically not persisted between workflow runs. See [CI/CD Integration](../how-to/ci-cd-integration) for strategies:

- Committing state to the repository
- Using artifact storage
- Relying on the adapter to discover existing resources

## Tradeoffs

### Benefits

- **Incremental updates** — Only change what's different
- **Clean teardown** — Know exactly what resources to delete
- **Audit trail** — Timestamp, version, and checksum tracking
- **Simple format** — Plain JSON, easy to inspect and debug

### Limitations

- **Local only** — State is stored on disk, not in a remote backend
- **Single writer** — Concurrent deploys can cause state conflicts
- **No locking** — No built-in protection against concurrent access

## See Also

- [Adapter Architecture](adapter-architecture) — How adapters use state
- [Plan and Apply](../how-to/plan-and-apply) — Deployment workflows
- [CLI Commands](../reference/cli-commands) — Status and destroy commands
