---
title: PackC GitHub Action
description: Compile and publish prompt packs to OCI registries in CI/CD pipelines
sidebar:
  order: 7
---

The PackC GitHub Action enables teams to compile prompt packs and publish them to OCI-compliant registries directly from CI/CD pipelines.

## Overview

The action:
- Downloads and caches PackC and ORAS binaries automatically
- Compiles prompt configurations into distributable packs
- Publishes packs to any OCI-compliant registry (GHCR, Docker Hub, etc.)
- Supports artifact signing with Cosign for supply chain security
- Outputs structured results for downstream workflow steps

## Quick Start

```yaml
name: Build and Publish Pack

on:
  push:
    branches: [main]
  release:
    types: [published]

jobs:
  build-pack:
    runs-on: ubuntu-latest

    steps:
      - uses: actions/checkout@v4

      - name: Build and publish pack
        uses: AltairaLabs/PromptKit/.github/actions/packc-action@v1
        with:
          config-file: config.arena.yaml
          registry: ghcr.io
          repository: ${{ github.repository }}/prompts
          username: ${{ github.actor }}
          password: ${{ secrets.GITHUB_TOKEN }}
          version: ${{ github.ref_name }}
```

---

## Inputs

| Input | Description | Required | Default |
|-------|-------------|----------|---------|
| `config-file` | Path to Arena YAML configuration file | **Yes** | - |
| `pack-id` | Unique identifier for the pack | No | Derived from config |
| `version` | Pack version for publishing | No | `latest` |
| `packc-version` | PackC binary version | No | `latest` |
| `output` | Output file path for compiled pack | No | `{pack-id}.pack.json` |
| `validate` | Run validation after compile | No | `true` |
| `registry` | OCI registry URL (e.g., ghcr.io) | No | - |
| `repository` | Repository path within registry | No | - |
| `username` | Registry username | No | - |
| `password` | Registry password/token | No | - |
| `sign` | Sign with Cosign | No | `false` |
| `cosign-key` | Cosign private key (path or content) | No | - |
| `cosign-password` | Cosign key password | No | - |
| `working-directory` | Working directory | No | `.` |

---

## Outputs

| Output | Description |
|--------|-------------|
| `pack-file` | Path to compiled pack file |
| `pack-id` | Pack identifier |
| `prompts` | Number of prompts in pack |
| `tools` | Number of tools in pack |
| `registry-url` | Full OCI registry URL (if published) |
| `digest` | OCI content digest (if published) |
| `signature` | Cosign signature reference (if signed) |

---

## Usage Examples

### Compile Only

Compile a pack without publishing:

```yaml
- name: Build pack
  uses: AltairaLabs/PromptKit/.github/actions/packc-action@v1
  with:
    config-file: config.arena.yaml
    pack-id: my-prompts

- name: Upload pack artifact
  uses: actions/upload-artifact@v4
  with:
    name: prompt-pack
    path: my-prompts.pack.json
```

### Publish to GitHub Container Registry

```yaml
- name: Build and publish pack
  uses: AltairaLabs/PromptKit/.github/actions/packc-action@v1
  with:
    config-file: config.arena.yaml
    registry: ghcr.io
    repository: ${{ github.repository }}/prompts
    username: ${{ github.actor }}
    password: ${{ secrets.GITHUB_TOKEN }}
    version: ${{ github.sha }}
```

### Publish with Semantic Versioning

Use release tags for versioning:

```yaml
name: Publish Pack

on:
  release:
    types: [published]

jobs:
  publish:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4

      - name: Publish pack
        uses: AltairaLabs/PromptKit/.github/actions/packc-action@v1
        with:
          config-file: config.arena.yaml
          registry: ghcr.io
          repository: ${{ github.repository }}/prompts
          username: ${{ github.actor }}
          password: ${{ secrets.GITHUB_TOKEN }}
          version: ${{ github.event.release.tag_name }}
```

### Sign with Cosign

Add supply chain security with artifact signing:

```yaml
- name: Build, publish, and sign
  uses: AltairaLabs/PromptKit/.github/actions/packc-action@v1
  with:
    config-file: config.arena.yaml
    registry: ghcr.io
    repository: ${{ github.repository }}/prompts
    username: ${{ github.actor }}
    password: ${{ secrets.GITHUB_TOKEN }}
    version: v1.0.0
    sign: 'true'
    cosign-key: ${{ secrets.COSIGN_PRIVATE_KEY }}
    cosign-password: ${{ secrets.COSIGN_PASSWORD }}
```

### Publish to Docker Hub

```yaml
- name: Publish to Docker Hub
  uses: AltairaLabs/PromptKit/.github/actions/packc-action@v1
  with:
    config-file: config.arena.yaml
    registry: docker.io
    repository: myorg/prompt-packs
    username: ${{ secrets.DOCKERHUB_USERNAME }}
    password: ${{ secrets.DOCKERHUB_TOKEN }}
    version: latest
```

### Use Outputs for Downstream Steps

```yaml
- name: Build pack
  id: packc
  uses: AltairaLabs/PromptKit/.github/actions/packc-action@v1
  with:
    config-file: config.arena.yaml
    registry: ghcr.io
    repository: ${{ github.repository }}/prompts
    username: ${{ github.actor }}
    password: ${{ secrets.GITHUB_TOKEN }}

- name: Summary
  run: |
    echo "## Pack Published" >> $GITHUB_STEP_SUMMARY
    echo "- Pack ID: ${{ steps.packc.outputs.pack-id }}" >> $GITHUB_STEP_SUMMARY
    echo "- Prompts: ${{ steps.packc.outputs.prompts }}" >> $GITHUB_STEP_SUMMARY
    echo "- Tools: ${{ steps.packc.outputs.tools }}" >> $GITHUB_STEP_SUMMARY
    echo "- Registry: ${{ steps.packc.outputs.registry-url }}" >> $GITHUB_STEP_SUMMARY
    echo "- Digest: ${{ steps.packc.outputs.digest }}" >> $GITHUB_STEP_SUMMARY
```

### Working with Subdirectories

Run from a specific directory:

```yaml
- name: Build pack
  uses: AltairaLabs/PromptKit/.github/actions/packc-action@v1
  with:
    config-file: config.arena.yaml
    working-directory: packages/customer-support
```

### Custom Output Path

```yaml
- name: Build pack with custom output
  uses: AltairaLabs/PromptKit/.github/actions/packc-action@v1
  with:
    config-file: config.arena.yaml
    output: dist/prompts.pack.json
    pack-id: my-pack
```

---

## OCI Media Type

Packs are published with the custom media type:

```
application/vnd.promptkit.pack.v1+json
```

This allows OCI-compliant tools to identify and handle prompt packs appropriately.

---

## Registry Authentication

### GitHub Container Registry (GHCR)

```yaml
username: ${{ github.actor }}
password: ${{ secrets.GITHUB_TOKEN }}
```

Requires `packages: write` permission in your workflow:

```yaml
permissions:
  packages: write
```

### Docker Hub

```yaml
username: ${{ secrets.DOCKERHUB_USERNAME }}
password: ${{ secrets.DOCKERHUB_TOKEN }}
```

Create an access token at: https://hub.docker.com/settings/security

### AWS ECR

Use the AWS ECR login action first:

```yaml
- name: Configure AWS credentials
  uses: aws-actions/configure-aws-credentials@v4
  with:
    aws-access-key-id: ${{ secrets.AWS_ACCESS_KEY_ID }}
    aws-secret-access-key: ${{ secrets.AWS_SECRET_ACCESS_KEY }}
    aws-region: us-east-1

- name: Login to ECR
  id: login-ecr
  uses: aws-actions/amazon-ecr-login@v2

- name: Build and publish
  uses: AltairaLabs/PromptKit/.github/actions/packc-action@v1
  with:
    config-file: config.arena.yaml
    registry: ${{ steps.login-ecr.outputs.registry }}
    repository: prompt-packs
    version: ${{ github.sha }}
```

---

## Signing with Cosign

The action supports signing artifacts with [Cosign](https://github.com/sigstore/cosign) for supply chain security.

### Generate Keys

```bash
cosign generate-key-pair
# Creates cosign.key (private) and cosign.pub (public)
```

### Store Keys as Secrets

1. Add `COSIGN_PRIVATE_KEY` (contents of cosign.key)
2. Add `COSIGN_PASSWORD` (key password)

### Verify Signed Packs

```bash
cosign verify --key cosign.pub ghcr.io/myorg/prompts:v1.0.0
```

---

## Caching

The action uses GitHub's tool cache to store downloaded binaries:

- PackC binary (from PromptKit releases)
- ORAS CLI (for OCI publishing)
- Cosign (for signing, when enabled)

Cache keys:
- `packc-{version}-{platform}-{arch}`
- `oras-{version}-{platform}-{arch}`
- `cosign-{version}-{platform}-{arch}`

---

## Platform Support

| Platform | Architecture | Status |
|----------|--------------|--------|
| Linux | x64 | ✅ Supported |
| Linux | arm64 | ✅ Supported |
| macOS | x64 | ✅ Supported |
| macOS | arm64 | ✅ Supported |
| Windows | x64 | ✅ Supported |
| Windows | arm64 | ✅ Supported |

---

## Troubleshooting

### Authentication failures

Ensure proper permissions and secrets:

```yaml
permissions:
  contents: read
  packages: write

jobs:
  build:
    steps:
      - name: Publish pack
        uses: AltairaLabs/PromptKit/.github/actions/packc-action@v1
        with:
          # ...
          username: ${{ github.actor }}
          password: ${{ secrets.GITHUB_TOKEN }}
```

### Config file not found

Ensure `config-file` path is relative to `working-directory`:

```yaml
- uses: AltairaLabs/PromptKit/.github/actions/packc-action@v1
  with:
    config-file: config.arena.yaml  # Not packages/my-pack/config.arena.yaml
    working-directory: packages/my-pack
```

### Validation warnings

Pack validation may show warnings for non-critical issues. The action continues unless validation fails completely.

```yaml
- uses: AltairaLabs/PromptKit/.github/actions/packc-action@v1
  with:
    config-file: config.arena.yaml
    validate: 'false'  # Skip validation if needed
```

### ORAS push fails

Check registry connectivity and authentication:

```bash
# Test manually
oras login ghcr.io -u $GITHUB_ACTOR
oras push ghcr.io/myorg/test:latest ./test.json
```

---

## Version Compatibility

The action is released alongside PromptKit and uses the same version numbers:

| Reference | Description |
|-----------|-------------|
| `@v1.0.0` | Specific version (recommended for reproducibility) |
| `@v1` | Latest v1.x.x release (auto-updated) |
| `@main` | Development branch (may be unstable) |

**Example references:**
```yaml
# Specific version (most stable)
uses: AltairaLabs/PromptKit/.github/actions/packc-action@v1.0.0

# Major version (gets patch updates automatically)
uses: AltairaLabs/PromptKit/.github/actions/packc-action@v1

# Latest development (not recommended for production)
uses: AltairaLabs/PromptKit/.github/actions/packc-action@main
```

---

## Related Documentation

- [PackC CLI Reference](../tools/packc) - Full CLI documentation
- [Pack Format](../tools/pack-format) - Pack file specification
- [CI/CD Pipelines](./ci-cd-pipelines) - PromptKit CI/CD overview
- [PromptArena Action](./promptarena-action) - Run prompt tests in CI

---

## Contributing

The action source code is located at:
```
.github/actions/packc-action/
├── action.yml          # Action metadata
├── src/                # TypeScript source
├── dist/               # Compiled bundle
└── README.md           # Quick reference
```

To modify the action:

```bash
cd .github/actions/packc-action
npm install
npm run lint
npm run test
npm run build
```

---

*Last Updated: January 2026*
