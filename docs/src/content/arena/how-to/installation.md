---
title: Install PromptArena
docType: how-to
order: 1
---
# Install PromptArena

Learn how to install and set up PromptArena for testing LLM applications.

## Installation Methods

### Option 1: Homebrew (macOS/Linux - Recommended)

```bash
# Install PromptKit (includes PromptArena)
brew install promptkit

# Verify installation
promptarena --version
```

### Option 2: Go Install

```bash
# Install directly with Go
go install github.com/altairalabs/promptkit/tools/arena@latest

# The binary will be in your $GOPATH/bin
promptarena --version
```

### Option 3: Download Pre-built Binary

Visit the [PromptKit Releases](https://github.com/altairalabs/promptkit/releases) page and download the appropriate binary for your platform.

```bash
# Example for macOS (adjust version and platform as needed)
curl -LO https://github.com/AltairaLabs/PromptKit/releases/latest/download/promptarena-darwin-amd64
chmod +x promptarena-darwin-amd64
sudo mv promptarena-darwin-amd64 /usr/local/bin/promptarena
```

### For Developers: Build from Source

```bash
# Clone the repository
git clone https://github.com/AltairaLabs/PromptKit.git
cd PromptKit

# Build and install
make install-arena
```

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
apiVersion: promptkit.altairalabs.ai/v1alpha1
kind: Arena
metadata:
  name: my-llm-tests

spec:
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

### Command Not Found (Go Install)

If `promptarena` is not found after `go install`:

```bash
# Ensure Go bin is in your PATH
export PATH=$PATH:$(go env GOPATH)/bin

# Add to your shell profile (~/.zshrc or ~/.bashrc)
echo 'export PATH=$PATH:$(go env GOPATH)/bin' >> ~/.zshrc
source ~/.zshrc
```

### Permission Denied (Binary Download)

```bash
# Make the binary executable
chmod +x promptarena-*
```

### Homebrew Not Found

Install Homebrew first:

```bash
# macOS/Linux
/bin/bash -c "$(curl -fsSL https://raw.githubusercontent.com/Homebrew/install/HEAD/install.sh)"
```
