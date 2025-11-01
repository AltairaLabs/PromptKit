# PromptKit Arena - CLI User Guide

PromptKit Arena is a comprehensive testing framework for LLM applications. It enables you to run multi-turn conversation simulations across multiple providers, validate tool usage, and generate detailed performance reports.

## Quick Start

### Installation

```bash
# Install from source
go install github.com/AltairaLabs/PromptKit/tools/arena/cmd/promptarena@latest

# Or build locally
make build-arena
./bin/promptarena --help
```

### Basic Usage

```bash
# Run tests from a scenario file
promptarena run examples/customer-support/arena.yaml

# Generate HTML report
promptarena run examples/customer-support/arena.yaml --html report.html

# Run with specific providers
promptarena run scenarios/test.yaml --providers openai,anthropic

# Verbose output for debugging
promptarena run scenarios/test.yaml --verbose
```

## Command Reference

### `promptarena run`

Run test scenarios defined in a configuration file.

**Syntax:**

```bash
promptarena run <config-file> [flags]
```

**Flags:**

- `--providers` - Comma-separated list of providers to test (default: all configured)
- `--scenarios` - Comma-separated list of scenarios to run (default: all)
- `--output, -o` - Output directory for results (default: `./results`)
- `--html` - Generate HTML report at specified path
- `--json` - Generate JSON report at specified path
- `--verbose, -v` - Enable verbose logging
- `--parallel` - Number of parallel executions (default: 1)
- `--timeout` - Timeout for individual tests (default: 30s)

**Examples:**

```bash
# Basic test run
promptarena run arena.yaml

# Generate both JSON and HTML reports
promptarena run arena.yaml --json results.json --html report.html

# Run specific scenarios with verbose output
promptarena run arena.yaml --scenarios "customer-inquiry,technical-support" --verbose

# Parallel execution with custom timeout
promptarena run arena.yaml --parallel 3 --timeout 60s
```

### `promptarena validate`

Validate configuration files without running tests.

**Syntax:**

```bash
promptarena validate <config-file>
```

**Examples:**

```bash
# Validate configuration
promptarena validate arena.yaml

# Validate multiple files
promptarena validate scenarios/*.yaml
```

### `promptarena init`

Initialize a new Arena project with example configurations.

**Syntax:**

```bash
promptarena init [project-name]
```

**Examples:**

```bash
# Create new project in current directory
promptarena init

# Create new project in specified directory
promptarena init my-llm-tests
```

## Configuration File Format

Arena uses YAML configuration files to define test scenarios, providers, and validation rules.

### Basic Structure

```yaml
# arena.yaml
version: "1.0"
name: "Customer Support Tests"
description: "Test scenarios for customer support chatbot"

providers:
  openai:
    model: "gpt-4"
    api_key_env: "OPENAI_API_KEY"
  
  anthropic:
    model: "claude-3-opus-20240229"
    api_key_env: "ANTHROPIC_API_KEY"

personas:
  support_agent:
    name: "Customer Support Agent"
    system_prompt: |
      You are a helpful customer support agent. Be polite, 
      professional, and try to resolve customer issues quickly.

scenarios:
  - name: "basic-inquiry"
    description: "Handle a basic product inquiry"
    persona: "support_agent"
    turns:
      - user: "Hi, I'm interested in your pricing plans."
        assertions:
          - type: "contains"
            value: "pricing"
          - type: "tone"
            value: "helpful"
      
      - user: "What's included in the basic plan?"
        assertions:
          - type: "contains"
            value: "basic plan"
          - type: "response_time"
            max_ms: 5000
```

### Advanced Configuration

#### MCP Integration

```yaml
mcp:
  servers:
    - name: memory
      command: npx
      args: ["-y", "@modelcontextprotocol/server-memory"]
    
    - name: filesystem
      command: npx
      args: ["-y", "@modelcontextprotocol/server-filesystem", "./data"]

scenarios:
  - name: "tool-usage-test"
    description: "Test tool calling with MCP servers"
    persona: "assistant"
    turns:
      - user: "Remember that my favorite color is blue"
        assertions:
          - type: "tool_called"
            tool: "memory_store"
      
      - user: "What files are in the data directory?"
        assertions:
          - type: "tool_called"
            tool: "list_files"
```

#### Multi-Provider Testing

```yaml
provider_matrix:
  - providers: ["openai", "anthropic"]
    scenarios: ["basic-inquiry", "complex-support"]
  
  - providers: ["openai"]
    scenarios: ["tool-usage-test"]  # Only test tools with OpenAI

reporting:
  compare_providers: true
  include_costs: true
  include_latency: true
```

### Assertion Types

Arena supports various assertion types for validating responses:

#### Content Assertions

```yaml
assertions:
  - type: "contains"
    value: "expected text"
  
  - type: "not_contains"
    value: "forbidden text"
  
  - type: "regex"
    pattern: "\\d{3}-\\d{3}-\\d{4}"  # Phone number pattern
  
  - type: "length"
    min: 10
    max: 500
```

#### Behavior Assertions

```yaml
assertions:
  - type: "tone"
    value: "professional"  # Uses built-in tone analysis
  
  - type: "response_time"
    max_ms: 3000
  
  - type: "token_count"
    max: 1000
  
  - type: "cost"
    max_usd: 0.05
```

#### Tool Assertions

```yaml
assertions:
  - type: "tool_called"
    tool: "search_database"
  
  - type: "tool_result"
    tool: "get_user_info"
    contains: "user found"
  
  - type: "no_tools_called"  # Ensure no tools were used
```

## Best Practices

### Organizing Test Scenarios

1. **Group by Feature**: Create separate scenario files for different features
2. **Use Descriptive Names**: Make scenario names clear and specific
3. **Progressive Complexity**: Start with simple scenarios, add complexity gradually

```text
tests/
├── arena.yaml              # Main configuration
├── scenarios/
│   ├── basic-chat.yaml    # Simple conversation tests  
│   ├── tool-usage.yaml    # Tool calling tests
│   └── edge-cases.yaml    # Error handling tests
└── personas/
    ├── support-agent.yaml
    └── technical-expert.yaml
```

### Provider Testing Strategy

1. **Start with One Provider**: Validate logic with a single, reliable provider
2. **Add Provider Comparison**: Test differences between providers
3. **Test Provider-Specific Features**: Some tools/features may work differently

### Assertion Design

1. **Multiple Small Assertions**: Better than one complex assertion
2. **Test Both Positive and Negative Cases**: Ensure the system behaves correctly
3. **Include Performance Metrics**: Response time and cost tracking

### Debugging Failed Tests

1. **Use Verbose Mode**: `--verbose` flag shows detailed execution
2. **Check Individual Assertions**: Review which specific assertion failed
3. **Examine Tool Calls**: Verify MCP tool integration is working correctly
4. **Review Provider Responses**: Look at raw provider outputs in results

## Examples

### Customer Support Testing

```yaml
scenarios:
  - name: "escalation-path"
    description: "Test proper escalation to human agent"
    persona: "support_agent"
    turns:
      - user: "Your product completely broke my business!"
        assertions:
          - type: "tone"
            value: "empathetic"
          - type: "contains"
            value: "sorry"
      
      - user: "I demand to speak to a manager right now!"
        assertions:
          - type: "contains"
            value: "escalate"
          - type: "not_contains"
            value: "cannot help"
```

### Technical Documentation Testing

```yaml
scenarios:
  - name: "code-explanation"
    description: "Test code explanation capabilities"
    persona: "technical_assistant"
    turns:
      - user: "Explain this Python function: def fibonacci(n): return n if n <= 1 else fibonacci(n-1) + fibonacci(n-2)"
        assertions:
          - type: "contains"
            value: "recursive"
          - type: "contains"
            value: "fibonacci"
          - type: "length"
            min: 100
```

## Troubleshooting

### Common Issues

#### Configuration Validation Errors

```bash
# Always validate before running
promptarena validate arena.yaml
```

#### Provider Authentication

```bash
# Ensure environment variables are set
export OPENAI_API_KEY="your-key-here"
export ANTHROPIC_API_KEY="your-key-here"
```

#### MCP Server Issues

```bash
# Test MCP servers independently
npx -y @modelcontextprotocol/server-memory
```

#### Performance Issues

```bash
# Reduce parallelism for stability
promptarena run arena.yaml --parallel 1 --timeout 60s
```

For more examples, see the `examples/` directory in the PromptKit repository.
