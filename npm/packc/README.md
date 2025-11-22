# @altairalabs/packc

> PromptKit Pack Compiler - Compile and validate prompt packs for LLM applications

## Installation

### npx (No Installation Required)

```bash
npx @altairalabs/packc compile -c arena.yaml
npx @altairalabs/packc validate -c arena.yaml
```

### Global Installation

```bash
npm install -g @altairalabs/packc

# Use directly
packc version
packc compile -c arena.yaml
```

### Project Dev Dependency

```bash
npm install --save-dev @altairalabs/packc

# Use via npm scripts
# Add to package.json:
{
  "scripts": {
    "build:prompts": "packc compile -c arena.yaml",
    "validate:prompts": "packc validate -c arena.yaml"
  }
}
```

## What is PackC?

PackC is the PromptKit Pack Compiler - a tool for compiling and validating prompt configurations. It helps you:

- 📦 **Compile prompts** from YAML configurations into optimized packs
- ✅ **Validate configurations** before deployment
- 🔍 **Inspect prompts** to understand structure and metadata
- 🚀 **Optimize** prompt loading for production use

## Quick Start

1. Create a configuration file:

```yaml
# arena.yaml
name: My Application
version: 1.0.0

prompts:
  - name: assistant
    system_prompt: |
      You are a helpful AI assistant.
    
  - name: code-helper
    system_prompt: |
      You are an expert programmer.
    context:
      - type: file
        path: ./docs/guidelines.md
```

2. Compile the prompts:

```bash
packc compile -c arena.yaml -o prompts.pack
```

3. Validate the configuration:

```bash
packc validate -c arena.yaml
```

4. Inspect a specific prompt:

```bash
packc inspect -c arena.yaml -p assistant
```

## Commands

### compile

Compile all prompts from a configuration file into a single pack:

```bash
packc compile -c arena.yaml -o output.pack
```

Options:
- `-c, --config`: Path to arena.yaml configuration file (required)
- `-o, --output`: Output pack file path (default: prompts.pack)

### compile-prompt

Compile a single prompt:

```bash
packc compile-prompt -c arena.yaml -p assistant -o assistant.json
```

Options:
- `-c, --config`: Path to configuration file (required)
- `-p, --prompt`: Name of the prompt to compile (required)
- `-o, --output`: Output file path (default: stdout)

### validate

Validate configuration file without compiling:

```bash
packc validate -c arena.yaml
```

Checks for:
- YAML syntax errors
- Missing required fields
- Invalid prompt references
- Malformed context definitions
- File path validation

### inspect

Inspect a specific prompt's configuration:

```bash
packc inspect -c arena.yaml -p assistant
```

Shows:
- Prompt name and metadata
- System prompt content
- Context sources
- Variable definitions
- Formatting details

### version

Display version information:

```bash
packc version
```

## Use Cases

### CI/CD Validation

Add prompt validation to your CI pipeline:

```yaml
# .github/workflows/validate.yml
- name: Validate prompts
  run: npx @altairalabs/packc validate -c config/arena.yaml
```

### Build Scripts

Compile prompts as part of your build process:

```json
{
  "scripts": {
    "prebuild": "packc compile -c arena.yaml",
    "build": "your-build-command"
  }
}
```

### Local Development

Quickly validate changes during development:

```bash
# Watch for changes and validate
watch -n 2 'packc validate -c arena.yaml'
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
- [Configuration Reference](https://github.com/AltairaLabs/PromptKit/tree/main/docs)
- [Examples](https://github.com/AltairaLabs/PromptKit/tree/main/examples)

## Troubleshooting

### Binary Download Fails

If the postinstall script fails:

1. Check your internet connection
2. Verify the version exists in [GitHub Releases](https://github.com/AltairaLabs/PromptKit/releases)
3. Check npm proxy/registry settings
4. Try manual installation:

```bash
# Download binary directly
curl -L https://github.com/AltairaLabs/PromptKit/releases/download/v0.0.1/PromptKit_v0.0.1_Darwin_arm64.tar.gz -o packc.tar.gz
tar -xzf packc.tar.gz packc
chmod +x packc
```

### Permission Denied

On Unix-like systems:

```bash
chmod +x node_modules/@altairalabs/packc/packc
```

## Alternative Installation Methods

- **Homebrew**: `brew install altairalabs/tap/promptkit`
- **Go Install**: `go install github.com/AltairaLabs/PromptKit/tools/packc@latest`
- **Direct Download**: [GitHub Releases](https://github.com/AltairaLabs/PromptKit/releases)
- **Build from Source**: Clone repo and run `make install-tools`

## Related Tools

- **[@altairalabs/promptarena](https://www.npmjs.com/package/@altairalabs/promptarena)** - Test and evaluate your prompts
- **PromptKit SDK** - Production deployment library

## License

Apache-2.0 - see [LICENSE](https://github.com/AltairaLabs/PromptKit/blob/main/LICENSE)

## Contributing

Contributions welcome! See [CONTRIBUTING.md](https://github.com/AltairaLabs/PromptKit/blob/main/CONTRIBUTING.md)

## Support

- [GitHub Issues](https://github.com/AltairaLabs/PromptKit/issues)
- [Discussions](https://github.com/AltairaLabs/PromptKit/discussions)
