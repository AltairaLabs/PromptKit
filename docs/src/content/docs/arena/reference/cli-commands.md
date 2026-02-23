---
title: CLI Commands
---
Complete command-line interface reference for PromptArena, the LLM testing framework.

## Overview

PromptArena (`promptarena`) is a CLI tool for running multi-turn conversation simulations across multiple LLM providers, validating conversation flows, and generating comprehensive test reports.

```bash
promptarena [command] [flags]
```

## Commands

| Command | Description |
|---------|-------------|
| `init` | Initialize a new Arena test project from template (built-in or remote) |
| `run` | Run conversation simulations (main command) |
| `generate` | Generate scenario files from session data or external sources |
| `mocks` | Generate mock provider responses from Arena JSON results |
| `config-inspect` | Inspect and validate configuration |
| `debug` | Debug configuration and prompt loading |
| `prompt-debug` | Debug and test prompt generation |
| `render` | Generate HTML report from existing results |
| `validate` | Validate configuration files |
| `view` | View test results |
| `deploy` | Deploy prompt packs to cloud providers via adapter plugins |
| `skill` | Manage shared AgentSkills.io skills (install, list, remove) |
| `completion` | Generate shell autocompletion script |
| `help` | Help about any command |

## Global Flags

```bash
-h, --help         help for promptarena
```

---

## `promptarena init`

Initialize a new PromptArena test project from a built-in template.

### Usage

```bash
promprarena init [directory] [flags]
```

### Flags

| Flag | Type | Default | Description |
|------|------|---------|-------------|
| `--quick` | bool | `false` | Skip interactive prompts, use defaults |
| `--provider` | string | - | Provider to configure (mock, openai, claude, gemini) |
| `--template` | string | `quick-start` | Template to use for initialization |
| `--template-index` | string | `community` | Template repo name or index URL/path for remote templates |
| `--repo-config` | string | user config | Template repo config file |
| `--template-cache` | string | temp dir | Cache directory for remote templates |

### Built-In Templates

PromptArena includes 6 built-in templates:

| Template | Files Generated | Description |
|----------|-----------------|-------------|
| `basic-chatbot` | 6 files | Simple conversational testing setup |
| `customer-support` | 10 files | Support agent with KB search and order status tools |
| `code-assistant` | 9 files | Code generation and review with separate prompts |
| `content-generation` | 9 files | Creative content for blogs, products, social media |
| `multimodal` | 7 files | Image analysis and vision testing |
| `mcp-integration` | 7 files | MCP filesystem server integration |

### Examples

#### List Available Templates

```bash
# List remote templates (from the default community repo)
promptarena templates list

# List remote templates from a named repo
promptarena templates repo add --name internal --url https://example.com/index.yaml
promptarena templates list --index internal

# List using repo/template shorthand
promptarena templates list --index community
```

#### Quick Start

```bash
# Create project with defaults (basic-chatbot template)
promprarena init my-test --quick

# With specific provider
promprarena init my-test --quick --provider openai

# With specific template
promprarena init my-test --quick --template customer-support --provider openai

# Render a remote template explicitly
promptarena templates fetch --template community/basic-chatbot --version 1.0.0
promptarena templates render --template community/basic-chatbot --version 1.0.0 --out ./out
```

#### Interactive Mode

```bash
# Interactive prompts guide you through setup
promprarena init my-project
```

### What Gets Created

Depending on the template, `init` creates:

- `config.arena.yaml` - Main Arena configuration
- `prompts/` - Prompt configurations
- `providers/` - Provider configurations
- `scenarios/` - Test scenarios
- `tools/` - Tool definitions (customer-support template)
- `.env` - Environment variables with API key placeholders
- `.gitignore` - Ignores .env and output files
- `README.md` - Project documentation and usage instructions

### Template Comparison

**basic-chatbot** (6 files):

- Best for: Beginners, simple testing
- Includes: 1 prompt, 1 provider, 1 basic scenario

**customer-support** (10 files):

- Best for: Support agent testing, tool calling
- Includes: 1 prompt, 3 scenarios, 2 tools (KB search, order status)

**code-assistant** (9 files):

- Best for: Code generation workflows
- Includes: 2 prompts (generator, reviewer), 3 scenarios
- Temperature: 0.3 (deterministic)

**content-generation** (9 files):

- Best for: Marketing, creative writing
- Includes: 2 prompts (blog, marketing), 3 scenarios
- Temperature: 0.8 (creative)

**multimodal** (7 files):

- Best for: Vision AI, image analysis
- Includes: 1 vision prompt, 2 scenarios with sample images

**mcp-integration** (7 files):

- Best for: MCP server testing, tool integration
- Includes: 1 prompt, 2 scenarios, MCP filesystem server config

### After Initialization

```bash
# Navigate to project
cd my-test

# Add your API key to .env
echo "OPENAI_API_KEY=sk-..." >> .env

# Run tests
promprarena run

# View results
open out/report.html
```

---

## `promptarena mocks generate`

Generate mock provider YAML from recorded Arena JSON results so you can replay conversations without calling real LLMs.

### Usage

```bash
promptarena mocks generate [flags]
```

### Flags

| Flag | Type | Default | Description |
|------|------|---------|-------------|
| `--input, -i` | string | `out` | Arena JSON result file or directory containing `*.json` runs |
| `--output, -o` | string | `providers/mock-generated.yaml` | Output file path or directory (when `--per-scenario` is set) |
| `--per-scenario` | bool | `false` | Write one YAML file per scenario (in `--output` directory) |
| `--merge` | bool | `false` | Merge with existing mock file(s) instead of overwriting |
| `--scenario` | []string | - | Only include specified scenario IDs |
| `--provider` | []string | - | Only include specified provider IDs |
| `--dry-run` | bool | `false` | Print generated YAML instead of writing files |
| `--default-response` | string | - | Set `defaultResponse` when not present |

### Examples

Generate a consolidated mock file from the latest runs:

```bash
promptarena mocks generate \
  --input out \
  --scenario hardware-faults \
  --provider openai-gpt4o \
  --output providers/mock-generated.yaml \
  --merge
```

Write one file per scenario:

```bash
promptarena mocks generate \
  --input out \
  --per-scenario \
  --output providers/responses \
  --merge
```

Preview without writing:

```bash
promptarena mocks generate --input out --dry-run
```

---

## `promptarena generate`

Generate Arena scenario YAML files from recorded sessions or external session sources. Uses pluggable `SessionSourceAdapter` implementations to load session data and convert it into reproducible test scenarios.

This is particularly useful for turning production conversations with failing assertions into reproducible test scenarios.

### Usage

```bash
promptarena generate [flags]
```

### Flags

| Flag | Type | Default | Description |
|------|------|---------|-------------|
| `--from-recordings` | string | - | Glob path to local recording files |
| `--source` | string | - | Named session source adapter (e.g., `omnia`) |
| `--filter-passed` | bool | - | Filter by pass/fail status (tri-state: omit for all, `true` for passed, `false` for failed) |
| `--filter-eval-type` | string | - | Filter sessions by assertion failure type (e.g., `content_matches`) |
| `--pack` | string | - | Pack file path; when set, generates workflow scenarios with steps instead of conversation turns |
| `--output` | string | `.` | Output directory for generated scenario files |
| `--dedup` | bool | `true` | Deduplicate sessions by failure pattern fingerprint |

:::note
You must specify either `--from-recordings` or `--source`. The `--from-recordings` flag uses the built-in recordings adapter, while `--source` looks up a named adapter from the plugin registry.
:::

### Examples

Generate scenarios from local recording files:

```bash
promptarena generate \
  --from-recordings "recordings/*.recording.json" \
  --output scenarios/generated
```

Generate only from sessions with failing assertions:

```bash
promptarena generate \
  --from-recordings "recordings/*.recording.json" \
  --filter-passed=false
```

Filter by a specific assertion failure type:

```bash
promptarena generate \
  --from-recordings "recordings/*.recording.json" \
  --filter-eval-type content_matches \
  --output scenarios/content-failures
```

Generate workflow scenarios using a pack file:

```bash
promptarena generate \
  --from-recordings "recordings/*.recording.json" \
  --pack prompts/support.pack.json \
  --output scenarios/workflow
```

Generate from a named external source adapter:

```bash
promptarena generate \
  --source omnia \
  --filter-passed=false \
  --output scenarios/production-failures
```

### Output

Each session produces a K8s-style scenario YAML file:

```yaml
apiVersion: promptkit.altairalabs.ai/v1alpha1
kind: Scenario
metadata:
  name: session-abc123
spec:
  id: session-abc123
  description: "Generated from session abc123, contains assertion failures"
  task_type: conversation
  turns:
    - role: user
      content: "Hello, I need help with my order"
  conversation_assertions:
    - type: content_matches
      params:
        pattern: "order.*status"
      message: "Response should mention order status"
```

When `--dedup` is enabled (default), sessions with identical failure patterns and user messages are deduplicated using SHA-256 fingerprinting. The first session per unique fingerprint is kept.

### Pluggable Adapters

The `generate` command supports pluggable session source adapters via the `SessionSourceAdapter` interface:

```go
type SessionSourceAdapter interface {
    Name() string
    List(ctx context.Context, opts ListOptions) ([]SessionSummary, error)
    Get(ctx context.Context, sessionID string) (*SessionDetail, error)
}
```

External adapters register themselves with the global registry in `init()` functions and are selected via `--source <name>`. See the [generate package](https://github.com/AltairaLabs/PromptKit/tree/main/tools/arena/generate) for the full interface definition.

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
| `-c, --config` | string | `config.arena.yaml` | Configuration file path |

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
promptarena run --provider openai,claude

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

## `promptarena deploy`

Deploy prompt packs to cloud providers through adapter plugins. Supports planning, applying, monitoring, and destroying deployments across multiple environments.

### Usage

```bash
promptarena deploy [subcommand] [flags]
```

### Subcommands

| Subcommand | Description |
|------------|-------------|
| *(none)* | Run plan + apply sequentially (default) |
| `plan` | Preview changes without modifying resources |
| `apply` | Apply changes (called automatically by `deploy`) |
| `status` | Show deployment status and resource health |
| `destroy` | Tear down all managed resources |
| `refresh` | Refresh local state from the live environment |
| `import` | Import a pre-existing resource into state |
| `adapter install` | Install an adapter plugin |
| `adapter list` | List installed adapters |
| `adapter remove` | Remove an installed adapter |

### Global Deploy Flags

| Flag | Short | Default | Description |
|------|-------|---------|-------------|
| `--env` | `-e` | `"default"` | Target environment name |
| `--config` | | `arena.yaml` | Path to config file |
| `--pack` | | Auto-detected | Path to `.pack.json` file |

### Examples

```bash
# Install an adapter and deploy
promptarena deploy adapter install agentcore
promptarena deploy --env production

# Preview changes first
promptarena deploy plan --env staging

# Check status
promptarena deploy status

# Tear down
promptarena deploy destroy --env staging
```

For the complete deploy command reference, see [Deploy: CLI Commands](deploy/cli-commands/).

---

## `promptarena config-inspect`

Inspect and validate arena configuration, showing all loaded resources and validating cross-references. This command provides a rich, styled display of your configuration with validation results.

### Usage

```bash
promptarena config-inspect [flags]
```

### Flags

| Flag | Type | Default | Description |
|------|------|---------|-------------|
| `-c, --config` | string | `config.arena.yaml` | Configuration file path |
| `--format` | string | `text` | Output format: text, json |
| `-s, --short` | bool | `false` | Show only validation results (shortcut for `--section validation`) |
| `--section` | string | - | Focus on specific section: prompts, providers, scenarios, tools, selfplay, judges, defaults, validation |
| `--verbose` | bool | `false` | Show detailed information including file contents |
| `--stats` | bool | `false` | Show cache statistics |

### Examples

```bash
# Inspect default configuration
promptarena config-inspect

# Inspect specific config file
promptarena config-inspect --config staging-arena.yaml

# Verbose output with full details
promptarena config-inspect --verbose

# Quick validation check only
promptarena config-inspect --short
# or
promptarena config-inspect -s

# Focus on specific section
promptarena config-inspect --section providers
promptarena config-inspect --section selfplay
promptarena config-inspect --section validation

# JSON output for programmatic use
promptarena config-inspect --format json

# Show cache statistics
promptarena config-inspect --stats
```

### Sections

The `--section` flag allows focusing on specific parts of the configuration:

| Section | Description |
|---------|-------------|
| `prompts` | Prompt configurations with task types, variables, validators |
| `providers` | Provider details organized by group (default, judge, selfplay) |
| `scenarios` | Scenario details with turn counts and assertion summaries |
| `tools` | Tool definitions with modes, parameters, timeouts |
| `selfplay` | Self-play configuration including personas and roles |
| `judges` | Judge configurations for LLM-as-judge validators |
| `defaults` | Default settings (temperature, max tokens, concurrency) |
| `validation` | Validation results and connectivity checks |

### Output

The command displays styled boxes with:
- Loaded prompt configurations with task types, variables, and validators
- Configured providers organized by group (default, judge, selfplay)
- Available scenarios with turn counts and assertion summaries
- Tool definitions with modes and parameters
- Self-play roles with persona associations
- Judge configurations
- Default settings
- Cross-reference validation results with connectivity checks

**Example Output**:

```
✨ PromptArena Configuration Inspector ✨

╭──────────────────────────────────────────────────────────────────────────────╮
│ Configuration: arena.yaml                                                    │
╰──────────────────────────────────────────────────────────────────────────────╯

  📋 Prompt Configs (2)

╭──────────────────────────────────────────────────────────────────────────────╮
│ troubleshooter-v2                                                            │
│   Task Type: troubleshooting                                                 │
│   File: prompts/troubleshooter-v2.prompt.yaml                                │
╰──────────────────────────────────────────────────────────────────────────────╯

  🔌 Providers (3)

╭──────────────────────────────────────────────────────────────────────────────╮
│ [default]                                                                    │
│   openai-gpt4o: gpt-4o (temp: 0.70, max: 1000)                               │
│                                                                              │
│ [judge]                                                                      │
│   judge-provider: gpt-4o-mini (temp: 0.00, max: 500)                         │
│                                                                              │
│ [selfplay]                                                                   │
│   mock-selfplay: mock-model (temp: 0.80, max: 1000)                          │
╰──────────────────────────────────────────────────────────────────────────────╯

  🎭 Self-Play (2 personas, 2 roles)

Personas:
╭──────────────────────────────────────────────────────────────────────────────╮
│ red-team-attacker                                                            │
│ plant-operator                                                               │
╰──────────────────────────────────────────────────────────────────────────────╯
Roles:
╭──────────────────────────────────────────────────────────────────────────────╮
│ attacker (red-team-attacker) → openai-gpt4o                                  │
│ operator (plant-operator) → openai-gpt4o                                     │
╰──────────────────────────────────────────────────────────────────────────────╯

  ✅ Validation

╭──────────────────────────────────────────────────────────────────────────────╮
│ ✓ Configuration is valid                                                     │
│                                                                              │
│ Connectivity Checks:                                                         │
│   ☑ Tools are used by prompts                                                │
│   ☑ Unique task types per prompt                                             │
│   ☑ Scenario task types exist                                                │
│   ☑ Allowed tools are defined                                                │
│   ☑ Self-play roles have valid providers                                     │
╰──────────────────────────────────────────────────────────────────────────────╯
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
| `-c, --config` | string | `config.arena.yaml` | Configuration file path |

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
| `-c, --config` | string | `config.arena.yaml` | Configuration file path |
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
| `PROMPTARENA_CONFIG` | Default configuration file (overrides `config.arena.yaml`) |
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
promptarena run --ci --provider openai,claude --format junit
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
promptarena run --provider openai,claude,gemini --format html
```

---

## Configuration File

PromptArena uses a YAML configuration file (default: `config.arena.yaml`). See the [Configuration Reference](../promptarena/config-reference) for complete documentation.

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

## Multimodal Content & Media Rendering

PromptArena supports multimodal content (images, audio, video) in test scenarios with comprehensive media rendering in all output formats.

### Media Content in Scenarios

Test scenarios can include multimodal content using the `parts` array:

```yaml
apiVersion: promptkit.altairalabs.ai/v1alpha1
kind: Scenario
metadata:
  name: image-analysis
spec:
  turns:
    - role: user
      parts:
        - type: text
          patterns: ["What's in this image?"]
        - type: image
          media:
            file_path: test-data/sample.jpg
            detail: high
```

#### Supported Media Types

- **Images**: JPEG, PNG, GIF, WebP
- **Audio**: MP3, WAV, OGG, M4A
- **Video**: MP4, WebM, MOV

#### Media Sources

Media can be loaded from three sources:

**1. Local Files**
```yaml
- type: image
  media:
    file_path: images/diagram.png
    detail: high
```

**2. URLs** (fetched during test execution)
```yaml
- type: image
  media:
    url: https://example.com/photo.jpg
    detail: auto
```

**3. Inline Base64 Data**
```yaml
- type: image
  media:
    data: "iVBORw0KGgoAAAANSUhEUgAAAAUA..."
    mime_type: image/png
    detail: low
```

### Media Rendering in Reports

All output formats include media statistics and rendering:

#### HTML Reports

HTML reports include:

**Media Summary Dashboard**
- Visual statistics cards showing:
  - Total images, audio, and video files
  - Successfully loaded vs. failed media
  - Total media size in human-readable format
  - Media type icons (🖼️ 🎵 🎬)

**Media Badges**
```
🖼️ x3  🎵 x2  ✅ 5  ❌ 0  💾 1.2 MB
```

**Media Items Display**
- Individual media items with:
  - Type icon and format badge
  - Source (file path, URL, or "inline")
  - MIME type
  - File size
  - Load status (✅ loaded / ❌ error)

**Example HTML Output**:
```html
<div class="media-summary">
  <div class="stat-card">
    <div class="stat-value">5</div>
    <div class="stat-label">🖼️ Images</div>
  </div>
  <div class="stat-card">
    <div class="stat-value">3</div>
    <div class="stat-label">🎵 Audio</div>
  </div>
  <!-- ... -->
</div>
```

#### JUnit XML Reports

JUnit XML includes media metadata as test suite properties:

```xml
<testsuite name="image-analysis" tests="1">
  <properties>
    <property name="media.images.total" value="5"/>
    <property name="media.audio.total" value="3"/>
    <property name="media.video.total" value="0"/>
    <property name="media.loaded.success" value="8"/>
    <property name="media.loaded.errors" value="0"/>
    <property name="media.size.total_bytes" value="1245678"/>
  </properties>
  <testcase name="test-001" classname="image-analysis" time="2.34"/>
</testsuite>
```

**Property Naming Convention**:
- `media.{type}.total` - Count by media type (images, audio, video)
- `media.loaded.success` - Successfully loaded media items
- `media.loaded.errors` - Failed media loads
- `media.size.total_bytes` - Total size in bytes

These properties are useful for:
- CI/CD metrics and tracking
- Test result analysis
- Media resource monitoring

#### Markdown Reports

Markdown reports include a media statistics table in the overview section:

```markdown
## 📊 Overview

| Metric | Value |
|--------|-------|
| Tests Run | 6 |
| Passed | 5 ✅ |
| Failed | 1 ❌ |
| Success Rate | 83.3% |
| Total Cost | $0.0245 |
| Total Duration | 12.5s |

### 🎨 Media Content

| Type | Count |
|------|-------|
| 🖼️  Images | 5 |
| 🎵 Audio Files | 3 |
| 🎬 Videos | 0 |
| ✅ Loaded | 8 |
| ❌ Errors | 0 |
| 💾 Total Size | 1.2 MB |
```

### Media Loading Options

Control how media is loaded and processed:

#### HTTP Media Loader

For URL-based media, configure the HTTP loader:

```yaml
spec:
  defaults:
    media:
      http:
        timeout: 30s
        max_file_size: 50MB
```

#### Local File Paths

Relative paths are resolved from the configuration file directory:

```yaml
# If arena.yaml is in /project/tests/
# This resolves to /project/tests/images/sample.jpg
- type: image
  media:
    file_path: images/sample.jpg
```

### Media Validation

PromptArena validates media content:

**Path Security**
- Prevents path traversal attacks (`..` sequences)
- Validates file paths are within allowed directories
- Checks symlink targets

**File Validation**
- Verifies MIME types match content types
- Checks file existence
- Validates file sizes against limits
- Ensures files are regular files (not directories)

**Error Handling**
- Media load failures are captured in test results
- Errors reported in all output formats
- Tests can continue with partial media failures

### Examples

#### Testing Image Analysis

```yaml
apiVersion: promptkit.altairalabs.ai/v1alpha1
kind: Scenario
metadata:
  name: product-image-analysis
spec:
  task_type: vision
  turns:
    - role: user
      parts:
        - type: text
          patterns: ["Analyze this product image for defects"]
        - type: image
          media:
            file_path: test-data/product-123.jpg
            detail: high
  assertions:
    - type: content_includes
      patterns: ["quality", "inspection"]
```

#### Testing Audio Transcription

```yaml
apiVersion: promptkit.altairalabs.ai/v1alpha1
kind: Scenario
metadata:
  name: audio-transcription
spec:
  task_type: transcription
  turns:
    - role: user
      parts:
        - type: text
          patterns: ["Transcribe this audio"]
        - type: audio
          media:
            file_path: test-data/meeting-recording.mp3
  assertions:
    - type: content_includes
      patterns: ["meeting", "agenda"]
```

#### Mixed Multimodal Content

```yaml
apiVersion: promptkit.altairalabs.ai/v1alpha1
kind: Scenario
metadata:
  name: multimodal-analysis
spec:
  turns:
    - role: user
      parts:
        - type: text
          patterns: ["Compare these media files"]
        - type: image
          media:
            file_path: charts/q1-results.png
        - type: image
          media:
            file_path: charts/q2-results.png
        - type: audio
          media:
            file_path: presentations/summary.mp3
```

### Generate Media-Rich Reports

```bash
# Run multimodal tests with all formats
promptarena run --format html,junit,markdown

# HTML report includes interactive media dashboard
open out/report.html

# JUnit XML includes media metrics for CI
cat out/junit.xml | grep "media\."

# Markdown shows media statistics
cat out/results.md
```

### Media Statistics in CI/CD

Extract media metrics from JUnit XML:

```bash
# Count total images tested
xmllint --xpath "//property[@name='media.images.total']/@value" out/junit.xml

# Check for media load errors
xmllint --xpath "//property[@name='media.loaded.errors']/@value" out/junit.xml
```

### Best Practices

**File Organization**
```
project/
├── arena.yaml
├── test-data/
│   ├── images/
│   │   ├── valid/
│   │   └── invalid/
│   ├── audio/
│   └── video/
└── scenarios/
    └── multimodal-tests.yaml
```

**Size Limits**
- Keep test media files small (<10MB recommended)
- Use compressed formats (WebP for images, MP3 for audio)
- Consider using thumbnails for image tests

**URL Loading**
- Use reliable, stable URLs for CI/CD
- Consider local copies for critical tests
- Set appropriate timeouts for remote resources

**Assertions**
- Validate media is processed in responses
- Check for expected content types
- Verify quality/accuracy of analysis

---

## Media Assertions (Phase 1)

Arena provides six specialized media validators to test media content in LLM responses. These assertions validate format, dimensions, duration, and resolution of images, audio, and video outputs.

### Image Assertions

#### `image_format`

Validates that images in assistant responses match allowed formats.

**Parameters:**

- `formats` ([]string, required): List of allowed formats (e.g., `png`, `jpeg`, `jpg`, `webp`, `gif`)

**Example:**

```yaml
turns:
  - role: user
    parts:
      - type: text
        patterns: ["Generate a PNG image of a sunset"]
    
    assertions:
      - type: image_format
        params:
          formats:
            - png
```

**Use Cases:**

- Validate model outputs correct image format
- Test format conversion capabilities
- Ensure compatibility with downstream systems

#### `image_dimensions`

Validates image dimensions (width and height) in assistant responses.

**Parameters:**

- `width` (int, optional): Exact required width in pixels
- `height` (int, optional): Exact required height in pixels
- `min_width` (int, optional): Minimum width in pixels
- `max_width` (int, optional): Maximum width in pixels
- `min_height` (int, optional): Minimum height in pixels
- `max_height` (int, optional): Maximum height in pixels

**Example:**

```yaml
turns:
  - role: user
    parts:
      - type: text
        patterns: ["Create a 1920x1080 wallpaper"]
    
    assertions:
      # Exact dimensions
      - type: image_dimensions
        params:
          width: 1920
          height: 1080
```

```yaml
turns:
  - role: user
    parts:
      - type: text
        patterns: ["Generate a thumbnail"]
    
    assertions:
      # Size range
      - type: image_dimensions
        params:
          min_width: 100
          max_width: 400
          min_height: 100
          max_height: 400
```

**Use Cases:**

- Validate exact resolution requirements
- Test minimum/maximum size constraints
- Verify thumbnail generation
- Ensure HD/4K resolution compliance

### Audio Assertions

#### `audio_format`

Validates audio format in assistant responses.

**Parameters:**

- `formats` ([]string, required): List of allowed formats (e.g., `mp3`, `wav`, `ogg`, `m4a`, `flac`)

**Example:**

```yaml
turns:
  - role: user
    parts:
      - type: text
        patterns: ["Generate an audio clip"]
    
    assertions:
      - type: audio_format
        params:
          formats:
            - mp3
            - wav
```

**Use Cases:**

- Validate audio output format
- Test format compatibility
- Ensure codec requirements

#### `audio_duration`

Validates audio duration in assistant responses.

**Parameters:**

- `min_seconds` (float, optional): Minimum duration in seconds
- `max_seconds` (float, optional): Maximum duration in seconds

**Example:**

```yaml
turns:
  - role: user
    parts:
      - type: text
        patterns: ["Create a 30-second audio clip"]
    
    assertions:
      - type: audio_duration
        params:
          min_seconds: 29
          max_seconds: 31
```

```yaml
turns:
  - role: user
    parts:
      - type: text
        patterns: ["Generate a brief notification sound"]
    
    assertions:
      - type: audio_duration
        params:
          max_seconds: 5
```

**Use Cases:**

- Validate exact duration requirements
- Test length constraints
- Verify podcast/music length
- Ensure compliance with platform limits

### Video Assertions

#### `video_resolution`

Validates video resolution in assistant responses.

**Parameters:**

- `presets` ([]string, optional): List of resolution presets
- `min_width` (int, optional): Minimum width in pixels
- `max_width` (int, optional): Maximum width in pixels
- `min_height` (int, optional): Minimum height in pixels
- `max_height` (int, optional): Maximum height in pixels

**Supported Presets:**

- `480p`, `sd` - Standard Definition (480 height)
- `720p`, `hd` - HD (720 height)
- `1080p`, `fhd`, `full_hd` - Full HD (1080 height)
- `1440p`, `2k`, `qhd` - QHD (1440 height)
- `2160p`, `4k`, `uhd` - 4K Ultra HD (2160 height)
- `4320p`, `8k` - 8K (4320 height)

**Example with Presets:**

```yaml
turns:
  - role: user
    parts:
      - type: text
        patterns: ["Generate a 1080p video"]
    
    assertions:
      - type: video_resolution
        params:
          presets:
            - 1080p
            - fhd
```

**Example with Dimensions:**

```yaml
turns:
  - role: user
    parts:
      - type: text
        patterns: ["Create a high-resolution video"]
    
    assertions:
      - type: video_resolution
        params:
          min_width: 1920
          min_height: 1080
```

**Use Cases:**

- Validate exact resolution requirements
- Test HD/4K compliance
- Verify minimum quality standards
- Validate aspect ratios

#### `video_duration`

Validates video duration in assistant responses.

**Parameters:**

- `min_seconds` (float, optional): Minimum duration in seconds
- `max_seconds` (float, optional): Maximum duration in seconds

**Example:**

```yaml
turns:
  - role: user
    parts:
      - type: text
        patterns: ["Create a 1-minute video clip"]
    
    assertions:
      - type: video_duration
        params:
          min_seconds: 59
          max_seconds: 61
```

**Use Cases:**

- Validate exact duration requirements
- Test length constraints
- Verify platform compliance (e.g., TikTok 60s limit)
- Ensure streaming segment sizes

### Combining Media Assertions

You can combine multiple media assertions on a single turn:

```yaml
turns:
  - role: user
    parts:
      - type: text
        patterns: ["Create a 30-second 4K video in MP4 format"]
    
    assertions:
      # Validate format (if you add video_format validator)
      - type: content_includes
        params:
          patterns: ["video"]
      
      # Validate resolution
      - type: video_resolution
        params:
          presets:
            - 4k
            - uhd
      
      # Validate duration
      - type: video_duration
        params:
          min_seconds: 29
          max_seconds: 31
```

### Complete Example Scenario

```yaml
apiVersion: promptkit.altairalabs.ai/v1alpha1
kind: Scenario
metadata:
  name: media-validation-complete
  description: Comprehensive media validation testing
spec:
  provider: gpt-4-vision
  
  turns:
    # Image validation
    - role: user
      parts:
        - type: text
          patterns: ["Generate a 1920x1080 PNG wallpaper"]
      
      assertions:
        - type: image_format
          params:
            formats: [png]
        
        - type: image_dimensions
          params:
            width: 1920
            height: 1080
    
    # Audio validation
    - role: user
      parts:
        - type: text
          patterns: ["Create a 10-second MP3 audio clip"]
      
      assertions:
        - type: audio_format
          params:
            formats: [mp3]
        
        - type: audio_duration
          params:
            min_seconds: 9
            max_seconds: 11
    
    # Video validation
    - role: user
      parts:
        - type: text
          patterns: ["Generate a 30-second 4K video"]
      
      assertions:
        - type: video_resolution
          params:
            presets: [4k, uhd]
        
        - type: video_duration
          params:
            min_seconds: 29
            max_seconds: 31
```

### Media Assertion Best Practices

#### Format Validation

- Always specify multiple acceptable formats when possible
- Use lowercase format names for consistency
- Test format conversion capabilities

#### Dimension/Resolution Testing

- Use min/max ranges to allow for encoding variations
- Test common aspect ratios (16:9, 4:3, 9:16)
- Validate minimum quality standards

#### Duration Testing

- Allow small tolerance ranges (±1-2 seconds)
- Test edge cases (very short/long durations)
- Verify platform-specific limits

#### Performance

- Media assertions execute on assistant responses only
- No API calls are made for validation
- Assertions run in parallel with other validators

### Example Test Scenarios

See complete examples in `examples/arena-media-test/`:

- `image-validation.yaml` - Image format and dimension testing
- `audio-validation.yaml` - Audio format and duration testing
- `video-validation.yaml` - Video resolution and duration testing

---

## `promptarena skill`

Manage shared skills — install from Git repositories or local paths, list installed skills, and remove them.

### Usage

```bash
promptarena skill [subcommand] [flags]
```

### Subcommands

| Subcommand | Description |
|------------|-------------|
| `install` | Install a skill from a Git repository or local path |
| `list` | List installed skills |
| `remove` | Remove an installed skill |

---

### `promptarena skill install`

Install a skill by reference.

#### Usage

```bash
promptarena skill install <ref> [flags]
```

#### Skill Reference Format

| Format | Description |
|--------|-------------|
| `@org/name` | Install latest from GitHub (`https://github.com/org/name`) |
| `@org/name@version` | Install a specific Git tag or ref |
| `./path/to/skill` | Install from a local directory |

#### Flags

| Flag | Type | Default | Description |
|------|------|---------|-------------|
| `--project` | bool | `false` | Install to project-level (`.promptkit/skills/`) instead of user-level |
| `--into` | string | — | Install directly into a specific directory (mutually exclusive with `--project`) |

#### Installation Locations

| Scope | Directory |
|-------|-----------|
| **User-level** (default) | `~/.config/promptkit/skills/org/name/` |
| **Project-level** (`--project`) | `.promptkit/skills/org/name/` |
| **Custom** (`--into <dir>`) | `<dir>/name/` |

The user-level directory respects `XDG_CONFIG_HOME` if set.

#### Examples

```bash
# Install from GitHub (user-level)
promptarena skill install @anthropic/pdf-processing

# Pin to a specific version
promptarena skill install @anthropic/pdf-processing@v1.0.0

# Install from a local path
promptarena skill install ./path/to/skill

# Install to project-level
promptarena skill install @anthropic/pdf-processing --project

# Install into a workflow stage directory
promptarena skill install @anthropic/pci-compliance --into ./skills/billing
```

---

### `promptarena skill list`

List all installed skills, grouped by location.

#### Usage

```bash
promptarena skill list
```

#### Example Output

```
Project:
  @anthropic/pci-compliance  (.promptkit/skills/anthropic/pci-compliance)

User:
  @anthropic/pdf-processing  (~/.config/promptkit/skills/anthropic/pdf-processing)
```

---

### `promptarena skill remove`

Remove an installed skill.

#### Usage

```bash
promptarena skill remove <ref>
```

#### Examples

```bash
# Remove a user-level skill
promptarena skill remove @anthropic/pdf-processing

# Remove a project-level skill
promptarena skill remove @anthropic/pci-compliance --project
```

---

## Tips & Best Practices

### Execution Performance

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

### Debugging Tips

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

- **[PromptArena Getting Started](../promptarena/getting-started)** - First project walkthrough
- **[Configuration Reference](../promptarena/config-reference)** - Complete config documentation
- **[CI/CD Integration](../promptarena/ci-cd-integration)** - Running in pipelines

---

**Need Help?**

```bash
# General help
promptarena --help

# Command-specific help
promptarena run --help
promptarena config-inspect --help
```
