# PromptArena GitHub Action

Run PromptArena tests in your CI/CD pipelines with native GitHub integration.

## Usage

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
          version: 'latest'
        env:
          OPENAI_API_KEY: ${{ secrets.OPENAI_API_KEY }}
          ANTHROPIC_API_KEY: ${{ secrets.ANTHROPIC_API_KEY }}
```

## Inputs

| Input | Description | Required | Default |
|-------|-------------|----------|---------|
| `config-file` | Path to Arena YAML configuration file | Yes | - |
| `version` | PromptArena version to use (e.g., `v1.1.6` or `latest`) | No | `latest` |
| `scenarios` | Comma-separated list of scenarios to run | No | - |
| `providers` | Comma-separated list of providers to use | No | - |
| `regions` | Comma-separated list of regions to run | No | - |
| `output-dir` | Directory for test results | No | `out` |
| `junit-output` | Path for JUnit XML output file | No | - |
| `fail-on-error` | Fail the action if tests fail | No | `true` |
| `working-directory` | Working directory for running tests | No | `.` |

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

## Examples

### Basic Usage

```yaml
- name: Run Arena tests
  uses: AltairaLabs/PromptKit/.github/actions/promptarena-action@v1
  with:
    config-file: arena.yaml
```

### With Filters

```yaml
- name: Run specific scenarios
  uses: AltairaLabs/PromptKit/.github/actions/promptarena-action@v1
  with:
    config-file: arena.yaml
    scenarios: 'scenario1,scenario2'
    providers: 'openai-gpt4'
```

### Continue on Failure

```yaml
- name: Run Arena tests (continue on failure)
  id: arena
  uses: AltairaLabs/PromptKit/.github/actions/promptarena-action@v1
  with:
    config-file: arena.yaml
    fail-on-error: 'false'

- name: Check results
  run: |
    if [ "${{ steps.arena.outputs.success }}" == "false" ]; then
      echo "Tests failed but continuing..."
    fi
```

### Upload Reports as Artifacts

```yaml
- name: Run Arena tests
  id: arena
  uses: AltairaLabs/PromptKit/.github/actions/promptarena-action@v1
  with:
    config-file: arena.yaml

- name: Upload test reports
  uses: actions/upload-artifact@v4
  with:
    name: arena-reports
    path: out/
```

### Use with Test Reporter

```yaml
- name: Run Arena tests
  uses: AltairaLabs/PromptKit/.github/actions/promptarena-action@v1
  with:
    config-file: arena.yaml
    junit-output: test-results/junit.xml

- name: Publish Test Results
  uses: dorny/test-reporter@v2
  with:
    name: Arena Tests
    path: test-results/junit.xml
    reporter: java-junit
```

### Pin to Specific Version

```yaml
- name: Run Arena tests
  uses: AltairaLabs/PromptKit/.github/actions/promptarena-action@v1
  with:
    config-file: arena.yaml
    version: 'v1.1.6'
```

## Platform Support

The action supports:
- Linux (x64, arm64)
- macOS (x64, arm64)
- Windows (x64, arm64)

## Caching

The action uses GitHub's tool cache to store downloaded binaries. Subsequent runs with the same version will use the cached binary for faster execution.

## License

Apache-2.0
