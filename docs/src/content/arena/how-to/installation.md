---
title: Install PromptArena
docType: how-to
order: 1
---
# Install PromptArena

Learn how to install and set up PromptArena for testing LLM applications.

## Prerequisites

- Go 1.23 or later
- Git

## Installation Methods

### Option 1: Install from Source (Recommended)

```bash
# Clone the repository
git clone https://github.com/altairalabs/promptkit.git
cd promptkit

# Build and install Arena
make install-arena
```

This installs the `promptarena` binary to your system.

### Option 2: Build Locally

```bash
# From the repository root
make build-arena

# The binary is available at:
./bin/promptarena
```

### Option 3: Download Pre-built Binary

Visit the [PromptKit Releases](https://github.com/altairalabs/promptkit/releases) page and download the appropriate binary for your platform.

## Verify Installation

```bash
# Check that Arena is installed
promptarena --help

# Should display command usage and available commands
```

## Initial Configuration

Create a basic configuration file:

```bash
# Create a test directory
mkdir my-llm-tests
cd my-llm-tests

# Create a minimal config file
cat > arena.yaml << 'EOF'
version: "1.0"

prompts:
  - path: ./prompts

providers:
  - path: ./providers

scenarios:
  - path: ./scenarios
EOF
```

## Next Steps

- **[Write Your First Test Scenario](write-scenarios)** - Create test scenarios
- **[Configure Providers](configure-providers)** - Set up LLM providers
- **[Tutorial: First Test](../tutorials/01-first-test)** - Complete hands-on guide

## Troubleshooting

### Command Not Found

If `promptarena` is not found after installation:

```bash
# Ensure Go bin is in your PATH
export PATH=$PATH:$(go env GOPATH)/bin

# Add to your shell profile (~/.zshrc or ~/.bashrc)
echo 'export PATH=$PATH:$(go env GOPATH)/bin' >> ~/.zshrc
```

### Permission Denied

```bash
# Make the binary executable
chmod +x ./bin/promptarena
```

### Build Failures

Ensure you have Go 1.23 or later:

```bash
go version
# Should show: go version go1.23.x or higher
```
