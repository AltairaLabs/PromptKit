# PackC GitHub Action

Compile and publish prompt packs to OCI-compliant registries in your CI/CD pipelines.

## Quick Start

```yaml
- name: Build and publish pack
  uses: AltairaLabs/PromptKit/.github/actions/packc-action@v1
  with:
    config-file: config.arena.yaml
    registry: ghcr.io
    repository: ${{ github.repository }}/prompts
    username: ${{ github.actor }}
    password: ${{ secrets.GITHUB_TOKEN }}
```

## Features

- Downloads and caches PackC, ORAS, and Cosign binaries automatically
- Compiles prompt configurations into distributable packs
- Publishes to any OCI-compliant registry (GHCR, Docker Hub, ECR, etc.)
- Supports artifact signing with Cosign for supply chain security
- Multi-platform support (Linux, macOS, Windows)

## Inputs

| Input | Required | Default | Description |
|-------|----------|---------|-------------|
| `config-file` | **Yes** | - | Path to Arena YAML configuration |
| `pack-id` | No | - | Pack identifier |
| `version` | No | `latest` | Pack version for publishing |
| `output` | No | - | Output file path |
| `validate` | No | `true` | Run validation after compile |
| `registry` | No | - | OCI registry URL |
| `repository` | No | - | Repository path in registry |
| `username` | No | - | Registry username |
| `password` | No | - | Registry password/token |
| `sign` | No | `false` | Sign with Cosign |
| `cosign-key` | No | - | Cosign private key |
| `working-directory` | No | `.` | Working directory |

## Outputs

| Output | Description |
|--------|-------------|
| `pack-file` | Path to compiled pack file |
| `pack-id` | Pack identifier |
| `prompts` | Number of prompts in pack |
| `tools` | Number of tools in pack |
| `registry-url` | Full OCI registry URL |
| `digest` | OCI content digest |
| `signature` | Cosign signature reference |

## Examples

### Compile Only

```yaml
- uses: AltairaLabs/PromptKit/.github/actions/packc-action@v1
  with:
    config-file: config.arena.yaml
    pack-id: my-prompts

- uses: actions/upload-artifact@v4
  with:
    name: prompt-pack
    path: my-prompts.pack.json
```

### Publish to GitHub Container Registry

```yaml
- uses: AltairaLabs/PromptKit/.github/actions/packc-action@v1
  with:
    config-file: config.arena.yaml
    registry: ghcr.io
    repository: ${{ github.repository }}/prompts
    username: ${{ github.actor }}
    password: ${{ secrets.GITHUB_TOKEN }}
    version: ${{ github.ref_name }}
```

### Sign with Cosign

```yaml
- uses: AltairaLabs/PromptKit/.github/actions/packc-action@v1
  with:
    config-file: config.arena.yaml
    registry: ghcr.io
    repository: ${{ github.repository }}/prompts
    username: ${{ github.actor }}
    password: ${{ secrets.GITHUB_TOKEN }}
    sign: 'true'
    cosign-key: ${{ secrets.COSIGN_PRIVATE_KEY }}
    cosign-password: ${{ secrets.COSIGN_PASSWORD }}
```

### Use Outputs

```yaml
- uses: AltairaLabs/PromptKit/.github/actions/packc-action@v1
  id: packc
  with:
    config-file: config.arena.yaml
    registry: ghcr.io
    repository: ${{ github.repository }}/prompts
    username: ${{ github.actor }}
    password: ${{ secrets.GITHUB_TOKEN }}

- run: |
    echo "Published: ${{ steps.packc.outputs.registry-url }}"
    echo "Digest: ${{ steps.packc.outputs.digest }}"
    echo "Prompts: ${{ steps.packc.outputs.prompts }}"
```

## Registry Authentication

### GitHub Container Registry

```yaml
username: ${{ github.actor }}
password: ${{ secrets.GITHUB_TOKEN }}
```

Requires `packages: write` permission in your workflow.

### Docker Hub

```yaml
username: ${{ secrets.DOCKERHUB_USERNAME }}
password: ${{ secrets.DOCKERHUB_TOKEN }}
```

## Documentation

Full documentation with more examples: [PackC Action Docs](https://altairalabs.github.io/PromptKit/devops/packc-action/)

## License

Apache-2.0 - See [LICENSE](../../../LICENSE)
