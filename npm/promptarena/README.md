# @altairalabs/promptarena

> PromptKit Arena - Multi-turn conversation simulation and testing tool for LLM applications

## Installation

### npx (No Installation Required)

```bash
npx @altairalabs/promptarena run -c ./examples/customer-support
```

### Global Installation

```bash
npm install -g @altairalabs/promptarena

# Use directly
promptarena --version
promptarena run -c ./config
```

### Project Dev Dependency

```bash
npm install --save-dev @altairalabs/promptarena

# Use via npm scripts
# Add to package.json:
{
  "scripts": {
    "test:prompts": "promptarena run -c ./tests/arena-config"
  }
}
```

## What is PromptKit Arena?

PromptKit Arena is a comprehensive testing framework for LLM-based applications. It allows you to:

- 🎯 **Test conversations** across multiple LLM providers (OpenAI, Anthropic, Google, Azure)
- 🔄 **Run multi-turn simulations** with automated agent interactions
- ✅ **Validate outputs** using assertions and quality metrics
- 📊 **Generate reports** with detailed analysis and comparisons
- 🛡️ **Test guardrails** and safety measures
- 🔧 **Validate tool usage** and function calling

## Quick Start

1. Create a test configuration:

```yaml
# arena.yaml
name: Customer Support Test
prompts:
  - name: support-agent
    system_prompt: |
      You are a helpful customer support agent.
      Be professional and empathetic.

conversations:
  - name: refund-request
    turns:
      - role: user
        content: "I'd like a refund for order #12345"
      - role: assistant
        expected_topics: ["refund", "order"]

providers:
  - type: openai
    model: gpt-4
    api_key: ${OPENAI_API_KEY}
```

2. Run the test:

```bash
promptarena run -c arena.yaml
```

3. View the HTML report:

```bash
open out/report.html
```

## Features

### Multi-Provider Testing

Test the same prompts across different LLM providers:

```yaml
providers:
  - type: openai
    model: gpt-4
  - type: anthropic
    model: claude-3-5-sonnet-20241022
  - type: google
    model: gemini-1.5-pro
```

### Automated Assertions

Validate LLM responses automatically:

```yaml
turns:
  - role: assistant
    assertions:
      - type: contains
        value: "refund"
      - type: tone
        expected: professional
      - type: length
        min: 50
        max: 500
```

### Self-Play Mode

Let AI agents interact with each other:

```yaml
self_play:
  enabled: true
  rounds: 5
  agents:
    - role: customer
      prompt: "Act as a frustrated customer"
    - role: support
      prompt: "Act as a patient support agent"
```

## How It Works

This npm package downloads pre-built Go binaries from [GitHub Releases](https://github.com/AltairaLabs/PromptKit/releases) during installation. The binaries are:

1. Downloaded for your specific OS and architecture
2. Extracted from the release archive
3. Made executable (Unix-like systems)
4. Invoked through a thin Node.js wrapper

No Go toolchain is required on your machine.

## Supported Platforms

- macOS (Intel and Apple Silicon)
- Linux (x86_64 and arm64)
- Windows (x86_64 and arm64)

## Documentation

- [Full Documentation](https://github.com/AltairaLabs/PromptKit#readme)
- [Examples](https://github.com/AltairaLabs/PromptKit/tree/main/examples)
- [Configuration Reference](https://github.com/AltairaLabs/PromptKit/tree/main/docs)

## Troubleshooting

### Binary Download Fails

If the postinstall script fails:

1. Check your internet connection
2. Verify the version exists in [GitHub Releases](https://github.com/AltairaLabs/PromptKit/releases)
3. Check npm proxy/registry settings
4. Try manual installation:

```bash
# Download binary directly
curl -L https://github.com/AltairaLabs/PromptKit/releases/download/v0.0.1/PromptKit_v0.0.1_Darwin_arm64.tar.gz -o promptarena.tar.gz
tar -xzf promptarena.tar.gz promptarena
chmod +x promptarena
```

### Permission Denied

On Unix-like systems:

```bash
chmod +x node_modules/@altairalabs/promptarena/promptarena
```

## Alternative Installation Methods

- **Homebrew**: `brew install altairalabs/tap/promptkit`
- **Go Install**: `go install github.com/AltairaLabs/PromptKit/tools/arena/cmd/promptarena@latest`
- **Direct Download**: [GitHub Releases](https://github.com/AltairaLabs/PromptKit/releases)
- **Build from Source**: Clone repo and run `make install-tools`

## License

Apache-2.0 - see [LICENSE](https://github.com/AltairaLabs/PromptKit/blob/main/LICENSE)

## Contributing

Contributions welcome! See [CONTRIBUTING.md](https://github.com/AltairaLabs/PromptKit/blob/main/CONTRIBUTING.md)

## Support

- [GitHub Issues](https://github.com/AltairaLabs/PromptKit/issues)
- [Discussions](https://github.com/AltairaLabs/PromptKit/discussions)
