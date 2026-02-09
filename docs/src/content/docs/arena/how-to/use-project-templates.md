---
title: Use Project Templates
sidebar:
  order: 1
---

Learn how to quickly scaffold new PromptArena test projects using templates.

## Overview

The `promptarena init` command uses templates to generate complete, ready-to-use test projects. This eliminates manual file creation and ensures your projects follow best practices from the start.

## Quick Mode

The fastest way to create a new project:

```bash
# Create with mock provider (no API calls needed)
promptarena init my-test --quick --provider mock

# Or with a real LLM provider
promptarena init my-test --quick --provider openai
```

Quick mode uses sensible defaults and creates:

- **config.arena.yaml** - Main configuration with your project name
- **prompts/assistant.yaml** - Basic prompt configuration
- **providers/{provider}.yaml** - Provider configuration for your chosen LLM
- **scenarios/basic-test.yaml** - Sample test scenario with assertions
- **.env** - Environment variables (with placeholders for API keys)
- **.gitignore** - Ignores .env and temporary files
- **README.md** - Project documentation and next steps

## Available Built-In Templates

PromptArena includes 6 built-in templates for common testing scenarios:

| Template | Description | Use Case |
|----------|-------------|----------|
| `basic-chatbot` | Simple conversational testing | General purpose, beginners |
| `customer-support` | Support agent with KB and order tools | Customer service testing |
| `code-assistant` | Separate generator and reviewer prompts | Code generation workflows |
| `content-generation` | Creative content for blogs, products, social | Marketing and content testing |
| `multimodal` | Vision analysis with image inputs | Image/audio/video AI |
| `mcp-integration` | MCP filesystem server configuration | Tool calling and MCP testing |

### List Available Templates

View all available templates:

```bash
promptarena templates list
```

## Community Templates (Remote)

You can fetch templates from a community index (default points to the promptkit-templates repo).

```bash
# List remote templates (uses default index)
promptarena templates list

# Fetch a remote template into cache
promptarena templates fetch --template basic-chatbot --version 1.0.0

# Render a template to a temp/out directory
promptarena templates render --template basic-chatbot --version 1.0.0 --values values.yaml --out ./out

# Update all cached templates
promptarena templates update

# Init a project using a remote template
promptarena init my-project --template basic-chatbot --template-index https://raw.githubusercontent.com/AltairaLabs/promptkit-templates/main/index.yaml
```

Flags:
- `--index` / `--template-index`: override index URL/path
- `--cache-dir` / `--template-cache`: override cache location
- `--values`/`--set`: provide variables for render

## Provider Options

```bash
# Mock provider (no API required, great for testing)
promptarena init my-test --quick --provider mock

# OpenAI (GPT models)
promptarena init my-test --quick --provider openai

# Anthropic (Claude models)
promptarena init my-test --quick --provider anthropic

# Google (Gemini models)
promptarena init my-test --quick --provider google
```

## Interactive Mode

For more control over your project configuration:

```bash
promptarena init my-project

# You'll be prompted for:
# - Project name and description
# - Provider selection
# - System prompt customization
# - Model parameters (temperature, max_tokens)
# - Test scenario details
```

Example interactive session:

```text
? Project Name: customer-support-tests
? Description: Testing customer support conversation flows
? Select LLM Provider: openai
? System Prompt: You are a helpful customer support agent...
? Temperature (0.0-2.0): 0.7
? Max Tokens: 2000
? Create sample test scenario? Yes
? Scenario Name: basic-greeting
```

## What Gets Generated

### 1. Arena Configuration

```yaml
# config.arena.yaml
apiVersion: promptkit.altairalabs.ai/v1alpha1
kind: Arena
metadata:
  name: my-test

spec:
  prompts:
    - path: ./prompts
  
  providers:
    - path: ./providers
  
  scenarios:
    - path: ./scenarios
```

### 2. Prompt Configuration

```yaml
# prompts/assistant.yaml
apiVersion: promptkit.altairalabs.ai/v1alpha1
kind: PromptConfig
metadata:
  name: assistant

spec:
  task_type: assistant
  system_prompt: |
    You are a helpful AI assistant.
  
  defaults:
    temperature: 0.7
    max_tokens: 2000
```

### 3. Provider Configuration

```yaml
# providers/openai.yaml (example)
apiVersion: promptkit.altairalabs.ai/v1alpha1
kind: Provider
metadata:
  name: openai-gpt4o-mini

spec:
  type: openai
  model: gpt-4o-mini
  
  defaults:
    temperature: 0.7
    max_tokens: 2000
```

### 4. Test Scenario

```yaml
# scenarios/basic-test.yaml
apiVersion: promptkit.altairalabs.ai/v1alpha1
kind: Scenario
metadata:
  name: basic-test

spec:
  task_type: assistant
  description: Basic conversation test
  
  turns:
    - role: user
      content: "Hello! Can you help me?"
      assertions:
        - type: content_matches
          params:
            pattern: ".+"
            message: "Should respond to greeting"
```

### 5. Environment Setup

```bash
# .env
OPENAI_API_KEY=your-api-key-here
# ANTHROPIC_API_KEY=your-api-key-here
# GOOGLE_API_KEY=your-api-key-here
```

## Using Generated Projects

After generation, your project is ready to use:

```bash
cd my-test

# Add your API key to .env
echo "OPENAI_API_KEY=sk-..." > .env

# Run tests
promptarena run

# View results
cat output/results.json
```

## Customizing After Generation

All generated files are standard PromptKit YAML and can be edited freely:

```bash
# Edit the prompt
vim prompts/assistant.yaml

# Add more scenarios
cp scenarios/basic-test.yaml scenarios/advanced-test.yaml
vim scenarios/advanced-test.yaml

# Configure additional providers
vim providers/claude.yaml
```

## Best Practices

### 1. Start with Quick Mode

Begin with quick mode to understand the structure:

```bash
promptarena init learning --quick --provider mock
cd learning
cat config.arena.yaml prompts/assistant.yaml scenarios/basic-test.yaml
```

### 2. Use Mock Provider for Development

Mock provider responses are instant and free:

```bash
promptarena init dev-test --quick --provider mock
```

Switch to real providers when ready:

```bash
# Edit providers/mock.yaml -> change type to openai
# Or create a new provider file
```

### 3. Version Control from Day One

Generated projects include `.gitignore`:

```bash
promptarena init my-test --quick --provider openai
cd my-test
git init
git add .
git commit -m "Initial project setup"
```

### 4. Organize Multiple Projects

```bash
# Create separate projects for different use cases
promptarena init customer-support --quick --provider openai
promptarena init content-generation --quick --provider anthropic
promptarena init qa-testing --quick --provider mock
```

## Advanced Usage

### Inspect Generated Files

Review what was created:

```bash
promptarena init my-test --quick --provider openai
cd my-test
tree .

# Output:
# .
# ├── .env
# ├── .gitignore
# ├── README.md
# ├── config.arena.yaml
# ├── prompts/
# │   └── assistant.yaml
# ├── providers/
# │   └── openai.yaml
# └── scenarios/
#     └── basic-test.yaml
```

## Troubleshooting

### "Directory already exists"

```bash
# Choose a different name
promptarena init my-test-2 --quick --provider openai

# Or remove the existing directory
rm -rf my-test
promptarena init my-test --quick --provider openai
```

### "Provider not recognized"

Valid providers are: `mock`, `openai`, `anthropic`, `google`

```bash
# Use a valid provider
promptarena init my-test --quick --provider openai
```

### Missing API Key

Generated projects create `.env` with placeholders:

```bash
cd my-test
echo "OPENAI_API_KEY=sk-your-actual-key" > .env
```

## Next Steps

- **[Write Test Scenarios](write-scenarios)** - Customize your test scenarios
- **[Configure Providers](configure-providers)** - Set up additional providers
- **[Tutorial: First Test](../tutorials/01-first-test)** - Complete walkthrough

## Related Documentation

- **[CLI Reference](../reference/cli-commands)** - All `promptarena` commands
- **[Configuration Schema](../reference/config-schema)** - Full schema documentation
- **[Examples](../../examples/)** - Real-world project examples
