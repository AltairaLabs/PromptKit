---
title: Install PromptArena
docType: how-to
order: 1
---
# Install PromptArena

Learn how to install and set up PromptArena for testing LLM applications.

## Installation Methods

### Option 1: npm/npx (Recommended for JavaScript/TypeScript Projects)

The easiest way to use PromptArena, especially if you're working in a JavaScript/TypeScript environment:

```bash
# Use without installation (recommended for trying it out)
npx @altairalabs/promptarena run -c arena.yaml

# Install globally
npm install -g @altairalabs/promptarena

# Or add to your project as a dev dependency
npm install --save-dev @altairalabs/promptarena
```

**Benefits:**
- No Go toolchain required
- Works with npm, yarn, and pnpm
- Easy integration with package.json scripts
- Automatic platform detection

**Add to your package.json:**
```json
{
  "scripts": {
    "test:prompts": "promptarena run -c ./tests/arena.yaml",
    "test:watch": "promptarena run -c ./tests/arena.yaml --watch"
  },
  "devDependencies": {
    "@altairalabs/promptarena": "^0.0.1"
  }
}
```

### Option 2: Homebrew (macOS/Linux)

```bash
# Install PromptKit (includes PromptArena)
brew install promptkit

# Verify installation
promptarena --version
```

### Option 3: Go Install

```bash
# Install directly with Go
go install github.com/altairalabs/promptkit/tools/arena@latest

# The binary will be in your $GOPATH/bin
promptarena --version
```

### Option 4: Download Pre-built Binary

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

## Create Your First Project

After installation, use the project generator to get started instantly:

```bash
# Create a new test project with guided setup
promptarena init my-llm-tests

# Or use quick mode with defaults
promptarena init my-llm-tests --quick --provider mock

# Navigate to your project
cd my-llm-tests

# Run your first test
promptarena run
```

The `init` command creates everything you need:

- Arena configuration (`arena.yaml`)
- Provider setup (`providers/`)
- Sample test scenario (`scenarios/`)
- Prompt configuration (`prompts/`)
- Environment variables (`.env`)
- README with next steps

### Manual Configuration (Advanced)

If you prefer to create files manually:

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
