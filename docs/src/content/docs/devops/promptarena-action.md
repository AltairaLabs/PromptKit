---
title: PromptArena GitHub Action
description: Run PromptArena tests in CI/CD pipelines with native GitHub integration
sidebar:
  order: 6
---

The PromptArena GitHub Action enables teams to run prompt tests in their CI/CD pipelines without manual installation or configuration.

## Overview

The action:
- Downloads and caches PromptArena binaries automatically
- Supports all platforms (Linux, macOS, Windows) and architectures (x64, arm64)
- Provides native GitHub test reporting via JUnit XML output
- Outputs structured results for downstream workflow steps

## Quick Start

```yaml
name: Arena Tests

on:
  pull_request:
  push:
    branches: [main]

jobs:
  arena-tests:
    runs-on: ubuntu-latest

    steps:
      - uses: actions/checkout@v4

      - name: Run Arena tests
        uses: AltairaLabs/PromptKit/.github/actions/promptarena-action@v1
        with:
          config-file: config.arena.yaml
        env:
          OPENAI_API_KEY: ${{ secrets.OPENAI_API_KEY }}
          ANTHROPIC_API_KEY: ${{ secrets.ANTHROPIC_API_KEY }}
```

---

## Inputs

| Input | Description | Required | Default |
|-------|-------------|----------|---------|
| `config-file` | Path to Arena YAML configuration file | **Yes** | - |
| `version` | PromptArena version (`latest` or `vX.Y.Z`) | No | `latest` |
| `scenarios` | Comma-separated list of scenarios to run | No | - |
| `providers` | Comma-separated list of providers to use | No | - |
| `regions` | Comma-separated list of regions to run | No | - |
| `output-dir` | Directory for test results | No | `out` |
| `junit-output` | Path for JUnit XML output file | No | - |
| `fail-on-error` | Fail the action if tests fail | No | `true` |
| `working-directory` | Working directory for running tests | No | `.` |

---

## Outputs

| Output | Description |
|--------|-------------|
| `passed` | Number of passed tests |
| `failed` | Number of failed tests |
| `errors` | Number of errors |
| `total` | Total number of tests |
| `total-cost` | Total cost in dollars |
| `success` | Whether all tests passed (`true`/`false`) |
| `junit-path` | Path to generated JUnit XML file |
| `html-path` | Path to generated HTML report |

---

## Usage Examples

### Basic Usage

Run all tests from a configuration file:

```yaml
- name: Run Arena tests
  uses: AltairaLabs/PromptKit/.github/actions/promptarena-action@v1
  with:
    config-file: arena.yaml
```

### Filter by Scenario and Provider

Run specific scenarios against specific providers:

```yaml
- name: Run filtered tests
  uses: AltairaLabs/PromptKit/.github/actions/promptarena-action@v1
  with:
    config-file: arena.yaml
    scenarios: 'customer-support,edge-cases'
    providers: 'openai-gpt4,anthropic-claude'
```

### Pin to Specific Version

Use a specific PromptArena version for reproducibility:

```yaml
- name: Run Arena tests
  uses: AltairaLabs/PromptKit/.github/actions/promptarena-action@v1
  with:
    config-file: arena.yaml
    version: 'v1.1.6'
```

### Continue on Test Failure

Run tests but don't fail the workflow if tests fail:

```yaml
- name: Run Arena tests
  id: arena
  uses: AltairaLabs/PromptKit/.github/actions/promptarena-action@v1
  with:
    config-file: arena.yaml
    fail-on-error: 'false'

- name: Check results
  run: |
    echo "Tests passed: ${{ steps.arena.outputs.passed }}"
    echo "Tests failed: ${{ steps.arena.outputs.failed }}"
    if [ "${{ steps.arena.outputs.success }}" == "false" ]; then
      echo "::warning::Some tests failed, review results"
    fi
```

### Upload Reports as Artifacts

Save HTML and JUnit reports for later analysis:

```yaml
- name: Run Arena tests
  uses: AltairaLabs/PromptKit/.github/actions/promptarena-action@v1
  with:
    config-file: arena.yaml
    output-dir: test-results

- name: Upload test reports
  uses: actions/upload-artifact@v4
  with:
    name: arena-reports
    path: test-results/
    retention-days: 30
```

### Integrate with Test Reporter

Display test results in GitHub's native test reporting UI:

```yaml
- name: Run Arena tests
  uses: AltairaLabs/PromptKit/.github/actions/promptarena-action@v1
  with:
    config-file: arena.yaml
    junit-output: test-results/junit.xml

- name: Publish Test Results
  uses: dorny/test-reporter@v1
  if: always()
  with:
    name: Arena Tests
    path: test-results/junit.xml
    reporter: java-junit
```

### Matrix Testing Across Providers

Test prompts against multiple providers in parallel:

```yaml
jobs:
  arena-tests:
    runs-on: ubuntu-latest
    strategy:
      matrix:
        provider: [openai-gpt4, anthropic-claude, google-gemini]

    steps:
      - uses: actions/checkout@v4

      - name: Run Arena tests - ${{ matrix.provider }}
        uses: AltairaLabs/PromptKit/.github/actions/promptarena-action@v1
        with:
          config-file: arena.yaml
          providers: ${{ matrix.provider }}
        env:
          OPENAI_API_KEY: ${{ secrets.OPENAI_API_KEY }}
          ANTHROPIC_API_KEY: ${{ secrets.ANTHROPIC_API_KEY }}
          GOOGLE_API_KEY: ${{ secrets.GOOGLE_API_KEY }}
```

### Cost Tracking

Monitor API costs across test runs:

```yaml
- name: Run Arena tests
  id: arena
  uses: AltairaLabs/PromptKit/.github/actions/promptarena-action@v1
  with:
    config-file: arena.yaml

- name: Report cost
  run: |
    echo "## Test Cost Report" >> $GITHUB_STEP_SUMMARY
    echo "Total API cost: \$${{ steps.arena.outputs.total-cost }}" >> $GITHUB_STEP_SUMMARY
```

### Working with Subdirectories

Run tests from a specific directory:

```yaml
- name: Run Arena tests
  uses: AltairaLabs/PromptKit/.github/actions/promptarena-action@v1
  with:
    config-file: config.arena.yaml
    working-directory: tests/prompts
```

---

## Environment Variables

The action passes through all environment variables to PromptArena. Common variables include:

| Variable | Purpose |
|----------|---------|
| `OPENAI_API_KEY` | OpenAI API authentication |
| `ANTHROPIC_API_KEY` | Anthropic API authentication |
| `AZURE_OPENAI_API_KEY` | Azure OpenAI authentication |
| `AZURE_OPENAI_ENDPOINT` | Azure OpenAI endpoint URL |
| `GOOGLE_API_KEY` | Google AI API authentication |

**Example:**

```yaml
- name: Run Arena tests
  uses: AltairaLabs/PromptKit/.github/actions/promptarena-action@v1
  with:
    config-file: arena.yaml
  env:
    OPENAI_API_KEY: ${{ secrets.OPENAI_API_KEY }}
    ANTHROPIC_API_KEY: ${{ secrets.ANTHROPIC_API_KEY }}
```

---

## Caching

The action uses GitHub's tool cache to store downloaded binaries. Benefits:
- Subsequent runs with the same version use cached binary
- Significantly faster execution after first run
- Cache persists across workflow runs

Cache key format: `promptarena-{version}-{platform}-{arch}`

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

### Tests fail with "config file not found"

Ensure the `config-file` path is relative to the `working-directory`:

```yaml
# If config is at: my-project/tests/arena.yaml
- uses: AltairaLabs/PromptKit/.github/actions/promptarena-action@v1
  with:
    config-file: arena.yaml
    working-directory: my-project/tests
```

### API key errors

Ensure secrets are properly configured:

1. Go to: Repository Settings → Secrets and variables → Actions
2. Add required API keys as repository secrets
3. Reference them in the workflow:

```yaml
env:
  OPENAI_API_KEY: ${{ secrets.OPENAI_API_KEY }}
```

### Binary download fails

If using a specific version, ensure it exists as a GitHub release:

```bash
# List available versions
gh release list -R AltairaLabs/PromptKit
```

### Tests timeout

For long-running tests, increase the job timeout:

```yaml
jobs:
  arena-tests:
    runs-on: ubuntu-latest
    timeout-minutes: 30  # Default is 6 hours, but set appropriate limit
```

---

## Version Compatibility

The action is released alongside PromptKit and uses the same version numbers:

| Reference | Description |
|-----------|-------------|
| `@v1.1.6` | Specific version (recommended for reproducibility) |
| `@v1` | Latest v1.x.x release (auto-updated) |
| `@main` | Development branch (may be unstable) |

**Example references:**
```yaml
# Specific version (most stable)
uses: AltairaLabs/PromptKit/.github/actions/promptarena-action@v1.1.6

# Major version (gets patch updates automatically)
uses: AltairaLabs/PromptKit/.github/actions/promptarena-action@v1

# Latest development (not recommended for production)
uses: AltairaLabs/PromptKit/.github/actions/promptarena-action@main
```

**Note:** The action version determines which `promptarena` binary versions are available. Using `version: 'latest'` input will download the latest released binary regardless of action version.

---

## Related Documentation

- [Arena CLI Reference](../arena/reference/) - Full CLI documentation
- [Arena Configuration](../arena/reference/configuration) - arena.yaml format
- [CI/CD Pipelines](./ci-cd-pipelines) - PromptKit CI/CD overview
- [Testing Releases](./testing-releases) - Release testing guide

---

## Contributing

The action source code is located at:
```
.github/actions/promptarena-action/
├── action.yml          # Action metadata
├── src/                # TypeScript source
├── dist/               # Compiled bundle
└── README.md           # Quick reference
```

To modify the action:

```bash
cd .github/actions/promptarena-action
npm install
npm run build
```

---

*Last Updated: January 2026*
