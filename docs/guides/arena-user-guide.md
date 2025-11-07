---
layout: page
title: "Arena CLI Reference"
permalink: /docs/guides/arena-user-guide/
nav_order: 2
---

# PromptArena CLI Reference

Complete command-line interface reference for PromptArena, the LLM testing framework.

## Overview

PromptArena (`promptarena`) is a CLI tool for running multi-turn conversation simulations across multiple LLM providers, validating conversation flows, and generating comprehensive test reports.

```bash
promptarena [command] [flags]
```

## Commands

| Command | Description |
|---------|-------------|
| `run` | Run conversation simulations (main command) |
| `config-inspect` | Inspect and validate configuration |
| `debug` | Debug configuration and prompt loading |
| `prompt-debug` | Debug and test prompt generation |
| `render` | Generate HTML report from existing results |
| `completion` | Generate shell autocompletion script |
| `help` | Help about any command |

## Global Flags

```bash
-h, --help         help for promptarena
```

---

## `promptarena run`

Run multi-turn conversation simulations across multiple LLM providers.

### Usage

```bash
promptarena run [flags]
```

### Flags

#### Configuration

| Flag | Type | Default | Description |
|------|------|---------|-------------|
| `-c, --config` | string | `arena.yaml` | Configuration file path |

#### Execution Control

| Flag | Type | Default | Description |
|------|------|---------|-------------|
| `-j, --concurrency` | int | `6` | Number of concurrent workers |
| `-s, --seed` | int | `42` | Random seed for reproducibility |
| `--ci` | bool | `false` | CI mode (headless, minimal output) |

#### Filtering

| Flag | Type | Default | Description |
|------|------|---------|-------------|
| `--provider` | []string | all | Providers to use (comma-separated) |
| `--scenario` | []string | all | Scenarios to run (comma-separated) |
| `--region` | []string | all | Regions to run (comma-separated) |
| `--roles` | []string | all | Self-play role configurations to use |

#### Parameter Overrides

| Flag | Type | Default | Description |
|------|------|---------|-------------|
| `--temperature` | float32 | `0.6` | Override temperature for all scenarios |
| `--max-tokens` | int | - | Override max tokens for all scenarios |

#### Self-Play Mode

| Flag | Type | Default | Description |
|------|------|---------|-------------|
| `--selfplay` | bool | `false` | Enable self-play mode |

#### Mock Testing

| Flag | Type | Default | Description |
|------|------|---------|-------------|
| `--mock-provider` | bool | `false` | Replace all providers with MockProvider |
| `--mock-config` | string | - | Path to mock provider configuration (YAML) |

#### Output Configuration

| Flag | Type | Default | Description |
|------|------|---------|-------------|
| `-o, --out` | string | `out` | Output directory |
| `--format` | []string | from config | Output formats: json, junit, html, markdown |
| `--formats` | []string | from config | Alias for --format |

#### Legacy Output Flags (Deprecated)

| Flag | Type | Default | Description |
|------|------|---------|-------------|
| `--html` | bool | `false` | Generate HTML report (use --format html instead) |
| `--html-file` | string | `out/report-[timestamp].html` | HTML report output file |
| `--junit-file` | string | `out/junit.xml` | JUnit XML output file |
| `--markdown-file` | string | `out/results.md` | Markdown report output file |

#### Debugging

| Flag | Type | Default | Description |
|------|------|---------|-------------|
| `-v, --verbose` | bool | `false` | Enable verbose debug logging for API calls |

### Examples

#### Basic Run

```bash
# Run all tests with default configuration
promptarena run

# Specify configuration file
promptarena run --config my-arena.yaml
```

#### Filter Execution

```bash
# Run specific providers only
promptarena run --provider openai,anthropic

# Run specific scenarios
promptarena run --scenario basic-qa,edge-cases

# Combine filters
promptarena run --provider openai --scenario customer-support
```

#### Control Parallelism

```bash
# Run with 3 concurrent workers
promptarena run --concurrency 3

# Sequential execution (no parallelism)
promptarena run --concurrency 1
```

#### Override Parameters

```bash
# Override temperature for all tests
promptarena run --temperature 0.8

# Override max tokens
promptarena run --max-tokens 500

# Combined overrides
promptarena run --temperature 0.9 --max-tokens 1000
```

#### Output Formats

```bash
# Generate JSON and HTML reports
promptarena run --format json,html

# Generate all available formats
promptarena run --format json,junit,html,markdown

# Custom output directory
promptarena run --out test-results-2024-01-15

# Specify custom HTML filename (legacy)
promptarena run --html --html-file custom-report.html
```

#### Mock Testing

```bash
# Use mock provider instead of real APIs (fast, no cost)
promptarena run --mock-provider

# Use custom mock configuration
promptarena run --mock-config mock-responses.yaml
```

#### Self-Play Mode

```bash
# Enable self-play testing
promptarena run --selfplay

# Self-play with specific roles
promptarena run --selfplay --roles frustrated-customer,tech-support
```

#### CI/CD Mode

```bash
# Headless mode for CI pipelines
promptarena run --ci --format junit,json

# With specific quality gates
promptarena run --ci --concurrency 3 --format junit
```

#### Debugging

```bash
# Verbose output for troubleshooting
promptarena run --verbose

# Verbose with specific scenario
promptarena run --verbose --scenario failing-test
```

#### Reproducible Tests

```bash
# Use specific seed for reproducibility
promptarena run --seed 12345

# Same seed across runs produces same results
promptarena run --seed 12345 --provider openai
```

---

## `promptarena config-inspect`

Inspect and validate arena configuration, showing all loaded resources and validating cross-references.

### Usage

```bash
promptarena config-inspect [flags]
```

### Flags

| Flag | Type | Default | Description |
|------|------|---------|-------------|
| `-c, --config` | string | `arena.yaml` | Configuration file path |
| `--format` | string | `text` | Output format: text, json |
| `--verbose` | bool | `false` | Show detailed information |
| `--stats` | bool | `false` | Show cache statistics |

### Examples

```bash
# Inspect default configuration
promptarena config-inspect

# Inspect specific config file
promptarena config-inspect --config staging-arena.yaml

# Verbose output with details
promptarena config-inspect --verbose

# JSON output for programmatic use
promptarena config-inspect --format json

# Show cache statistics
promptarena config-inspect --stats
```

### Output

The command displays:
- Loaded prompt configurations
- Configured providers
- Available scenarios
- Tool definitions
- MCP server configurations
- Cross-reference validation results

**Example Output**:

```
Configuration: arena.yaml

Prompt Configs:
  ✓ support (prompts/support-bot.yaml)
  ✓ creative (prompts/content-gen.yaml)

Providers:
  ✓ openai-gpt4o-mini (providers/openai.yaml)
  ✓ claude-3-5-sonnet (providers/claude.yaml)

Scenarios:
  ✓ basic-qa (scenarios/qa.yaml) [task_type: support]
  ✓ tool-calling (scenarios/tools.yaml) [task_type: support]

Tools:
  ✓ get_weather (tools/weather.yaml) [mode: live]
  ✓ search_db (tools/database.yaml) [mode: mock]

Validation:
  ✓ All scenario task_types match prompt configs
  ✓ All provider references valid
  ✓ All tool references valid
```

---

## `promptarena debug`

Debug command shows loaded configuration, prompt packs, scenarios, and providers to help troubleshoot configuration issues.

### Usage

```bash
promptarena debug [flags]
```

### Flags

| Flag | Type | Default | Description |
|------|------|---------|-------------|
| `-c, --config` | string | `arena.yaml` | Configuration file path |

### Examples

```bash
# Debug default configuration
promptarena debug

# Debug specific config
promptarena debug --config test-arena.yaml
```

### Use Cases

- Troubleshoot configuration loading issues
- Verify all files are found and parsed correctly
- Check prompt pack assembly
- Validate provider initialization

---

## `promptarena prompt-debug`

Test prompt generation with specific regions, task types, and contexts. Useful for validating prompt assembly before running full tests.

### Usage

```bash
promptarena prompt-debug [flags]
```

### Flags

| Flag | Type | Default | Description |
|------|------|---------|-------------|
| `-c, --config` | string | `arena.yaml` | Configuration file path |
| `-t, --task-type` | string | - | Task type for prompt generation |
| `-r, --region` | string | - | Region for prompt generation |
| `--persona` | string | - | Persona ID to test |
| `--scenario` | string | - | Scenario file path to load task_type and context |
| `--context` | string | - | Context slot content |
| `--user` | string | - | User context (e.g., "iOS developer") |
| `--domain` | string | - | Domain hint (e.g., "mobile development") |
| `-l, --list` | bool | `false` | List available regions and task types |
| `-j, --json` | bool | `false` | Output as JSON |
| `-p, --show-prompt` | bool | `true` | Show the full assembled prompt |
| `-m, --show-meta` | bool | `true` | Show metadata and configuration info |
| `-s, --show-stats` | bool | `true` | Show statistics (length, tokens, etc.) |
| `-v, --verbose` | bool | `false` | Verbose output with debug info |

### Examples

```bash
# List available configurations
promptarena prompt-debug --list

# Test prompt generation for task type
promptarena prompt-debug --task-type support

# Test with region
promptarena prompt-debug --task-type support --region us

# Test with persona
promptarena prompt-debug --persona us-hustler-v1

# Test with scenario file
promptarena prompt-debug --scenario scenarios/customer-support.yaml

# Test with custom context
promptarena prompt-debug --task-type support --context "urgent billing issue"

# JSON output for parsing
promptarena prompt-debug --task-type support --json

# Minimal output (just the prompt)
promptarena prompt-debug --task-type support --show-meta=false --show-stats=false
```

### Output

The command shows:
- Assembled system prompt
- Metadata (task type, region, persona)
- Statistics (character count, estimated tokens)
- Configuration used

**Example Output**:

```
=== Prompt Debug ===

Task Type: support
Region: us
Persona: default

--- System Prompt ---
You are a helpful customer support agent for TechCo.

Your role:
- Answer product questions
- Help track orders
- Process returns and refunds
...

--- Statistics ---
Characters: 1,234
Estimated Tokens: 308
Lines: 42

--- Metadata ---
Prompt Config: support
Version: v1.0.0
Validators: 3
```

---

## `promptarena render`

Generate an HTML report from existing test results.

### Usage

```bash
promptarena render [index.json path] [flags]
```

### Flags

| Flag | Type | Default | Description |
|------|------|---------|-------------|
| `-o, --output` | string | `report-[timestamp].html` | Output HTML file path |

### Examples

```bash
# Render from default location
promptarena render out/index.json

# Custom output path
promptarena render out/index.json --output custom-report.html

# Render from archived results
promptarena render archive/2024-01-15/index.json --output reports/jan-15-report.html
```

### Use Cases

- Regenerate reports after test runs
- Create reports with different formatting
- Archive and view historical results
- Share results without re-running tests

---

## `promptarena completion`

Generate shell autocompletion script for bash, zsh, fish, or PowerShell.

### Usage

```bash
promptarena completion [bash|zsh|fish|powershell]
```

### Examples

```bash
# Bash
promptarena completion bash > /etc/bash_completion.d/promptarena

# Zsh
promptarena completion zsh > "${fpath[1]}/_promptarena"

# Fish
promptarena completion fish > ~/.config/fish/completions/promptarena.fish

# PowerShell
promptarena completion powershell > promptarena.ps1
```

---

## Environment Variables

PromptArena respects the following environment variables:

| Variable | Description |
|----------|-------------|
| `OPENAI_API_KEY` | OpenAI API authentication |
| `ANTHROPIC_API_KEY` | Anthropic API authentication |
| `GOOGLE_API_KEY` | Google AI API authentication |
| `PROMPTARENA_CONFIG` | Default configuration file (overrides `arena.yaml`) |
| `PROMPTARENA_OUTPUT` | Default output directory (overrides `out`) |

### Example

```bash
export OPENAI_API_KEY="sk-..."
export ANTHROPIC_API_KEY="sk-ant-..."
export PROMPTARENA_CONFIG="staging-arena.yaml"
export PROMPTARENA_OUTPUT="test-results"

promptarena run
```

---

## Exit Codes

| Code | Meaning |
|------|---------|
| `0` | Success - all tests passed |
| `1` | Failure - one or more tests failed or error occurred |

Check exit code in scripts:

```bash
if promptarena run --ci; then
  echo "✅ Tests passed"
else
  echo "❌ Tests failed"
  exit 1
fi
```

---

## Common Workflows

### Local Development

```bash
# Quick test with mock providers
promptarena run --mock-provider

# Test specific feature
promptarena run --scenario new-feature --verbose

# Inspect configuration
promptarena config-inspect --verbose
```

### CI/CD Pipeline

```bash
# Run in headless CI mode
promptarena run --ci --format junit,json

# Check specific providers
promptarena run --ci --provider openai,anthropic --format junit
```

### Debugging

```bash
# Validate configuration
promptarena config-inspect

# Debug prompt assembly
promptarena prompt-debug --task-type support --verbose

# Run with verbose logging
promptarena run --verbose --scenario failing-test

# Check configuration loading
promptarena debug
```

### Report Generation

```bash
# Run tests
promptarena run --format json

# Later, generate HTML from results
promptarena render out/index.json --output reports/latest.html
```

### Multi-Provider Comparison

```bash
# Test all providers
promptarena run --format html,json

# Test specific providers
promptarena run --provider openai,anthropic,gemini --format html
```

---

## Configuration File

PromptArena uses a YAML configuration file (default: `arena.yaml`). See the [Configuration Reference](../promptarena/config-reference.md) for complete documentation.

### Basic Structure

```yaml
apiVersion: promptkit.altairalabs.ai/v1alpha1
kind: Arena
metadata:
  name: my-arena
spec:
  prompt_configs:
    - id: assistant
      file: prompts/assistant.yaml

  providers:
    - file: providers/openai.yaml

  scenarios:
    - file: scenarios/test.yaml

  defaults:
    output:
      dir: out
      formats: ["json", "html"]
```

---

## Tips & Best Practices

### Performance

```bash
# Increase concurrency for faster execution
promptarena run --concurrency 10

# Reduce concurrency for stability
promptarena run --concurrency 1
```

### Cost Control

```bash
# Use mock provider during development
promptarena run --mock-provider

# Test with cheaper models first
promptarena run --provider gpt-3.5-turbo
```

### Reproducibility

```bash
# Always use same seed for consistent results
promptarena run --seed 42

# Document seed in test reports
promptarena run --seed 42 --format json,html
```

### Debugging

```bash
# Always start with config validation
promptarena config-inspect --verbose

# Use verbose mode to see API calls
promptarena run --verbose --scenario problematic-test

# Test prompt generation separately
promptarena prompt-debug --scenario scenarios/test.yaml
```

---

## Next Steps

- **[PromptArena Getting Started](../promptarena/getting-started.md)** - First project walkthrough
- **[Configuration Reference](../promptarena/config-reference.md)** - Complete config documentation
- **[CI/CD Integration](../promptarena/ci-cd-integration.md)** - Running in pipelines

---

**Need Help?**

```bash
# General help
promptarena --help

# Command-specific help
promptarena run --help
promptarena config-inspect --help
```
