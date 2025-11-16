---
layout: default
title: Install PackC
parent: How-To Guides
grand_parent: PackC
nav_order: 1
---

# How to Install PackC

Install the packc compiler on your system.

## Goal

Get packc installed and ready to compile prompt packs.

## Prerequisites

- Go 1.22 or higher
- Git (for source installation)
- Network access to download dependencies

## Installation Methods

### Method 1: Install from Source (Recommended)

This installs the latest version from the repository:

```bash
# Install latest version
go install github.com/AltairaLabs/PromptKit/tools/packc@latest

# Verify installation
packc version
```

**Expected output:**

```
packc v0.1.0
```

### Method 2: Install Specific Version

Pin to a specific version for reproducible builds:

```bash
# Install specific version
go install github.com/AltairaLabs/PromptKit/tools/packc@v0.1.0

# Verify installation
packc version
```

### Method 3: Build from Source

For development or custom builds:

```bash
# Clone repository
git clone https://github.com/AltairaLabs/PromptKit.git
cd PromptKit

# Build packc
make build-packc

# Binary is at ./bin/packc
./bin/packc version
```

### Method 4: Docker

Run packc in a container:

```bash
# Pull image (when available)
docker pull ghcr.io/AltairaLabs/packc:latest

# Or build locally
docker build -t packc -f Dockerfile.packc .

# Run packc
docker run --rm -v $(pwd):/workspace packc version
```

## Verify Installation

Check that packc is properly installed:

```bash
# Check version
packc version

# Check help
packc help

# Check location
which packc
```

Expected outputs:

```
packc v0.1.0
packc - PromptKit Pack Compiler
Usage: packc <command> [options]
...
/Users/yourname/go/bin/packc
```

## Add to PATH

If `packc` is not found, add Go's bin directory to your PATH:

### macOS/Linux

Add to `~/.zshrc` or `~/.bashrc`:

```bash
export PATH="$PATH:$(go env GOPATH)/bin"
```

Apply changes:

```bash
source ~/.zshrc  # or ~/.bashrc
```

### Windows

Add to PATH in System Environment Variables:

1. Open System Properties > Environment Variables
2. Edit `Path` variable
3. Add: `%USERPROFILE%\go\bin`
4. Restart terminal

## Update PackC

### Update to Latest Version

```bash
# Reinstall latest
go install github.com/AltairaLabs/PromptKit/tools/packc@latest

# Verify new version
packc version
```

### Update from Source

```bash
cd PromptKit
git pull origin main
make build-packc
```

## Uninstall

### Remove Binary

```bash
# Find packc location
which packc

# Remove binary
rm $(which packc)
```

### Clean Go Cache

```bash
# Clean module cache
go clean -modcache
```

## Configuration

### Set Default Config Path

Create an alias for convenience:

```bash
# Add to ~/.zshrc or ~/.bashrc
alias packc-compile='packc compile --config ./config/arena.yaml'
```

### Environment Variables

Configure default behavior:

```bash
# Set default output directory
export PACKC_OUTPUT_DIR="./packs"

# Use in scripts
packc compile --config arena.yaml --output "$PACKC_OUTPUT_DIR/app.pack.json" --id app
```

## Platform-Specific Notes

### macOS

```bash
# Install with Homebrew (when available)
brew install AltairaLabs/tap/packc

# Or use go install
go install github.com/AltairaLabs/PromptKit/tools/packc@latest
```

### Linux

```bash
# Install with go
go install github.com/AltairaLabs/PromptKit/tools/packc@latest

# Or download binary (when available)
curl -L https://github.com/AltairaLabs/PromptKit/releases/download/v0.1.0/packc-linux-amd64 -o /usr/local/bin/packc
chmod +x /usr/local/bin/packc
```

### Windows

```powershell
# Install with go
go install github.com/AltairaLabs/PromptKit/tools/packc@latest

# Or download binary (when available)
# Download from https://github.com/AltairaLabs/PromptKit/releases
# Extract to C:\Program Files\packc\
# Add to PATH
```

## CI/CD Installation

### GitHub Actions

```yaml
- name: Install packc
  run: go install github.com/AltairaLabs/PromptKit/tools/packc@latest

- name: Verify installation
  run: packc version
```

### GitLab CI

```yaml
install_packc:
  script:
    - go install github.com/AltairaLabs/PromptKit/tools/packc@latest
    - packc version
```

### Jenkins

```groovy
stage('Install packc') {
  steps {
    sh 'go install github.com/AltairaLabs/PromptKit/tools/packc@latest'
    sh 'packc version'
  }
}
```

### Docker

```dockerfile
FROM golang:1.22

RUN go install github.com/AltairaLabs/PromptKit/tools/packc@latest

RUN packc version
```

## Troubleshooting

### packc: command not found

**Problem**: packc not in PATH

**Solution**: Add Go's bin directory to PATH:

```bash
export PATH="$PATH:$(go env GOPATH)/bin"
```

### Permission denied

**Problem**: Binary not executable

**Solution**: Make binary executable:

```bash
chmod +x $(which packc)
```

### Go version too old

**Problem**: Go version < 1.22

**Solution**: Update Go:

```bash
# macOS
brew upgrade go

# Linux
# Download from https://go.dev/dl/

# Verify
go version
```

### Installation fails

**Problem**: Network or dependency issues

**Solution**: Check network and clean cache:

```bash
# Clean cache
go clean -modcache

# Try again
go install github.com/AltairaLabs/PromptKit/tools/packc@latest
```

## Next Steps

- [Compile Your First Pack](compile-packs.md)
- [Validate Packs](validate-packs.md)
- [First Pack Tutorial](../tutorials/01-first-pack.md)

## See Also

- [version command](../reference/version.md) - Check packc version
- [System Requirements](../../getting-started/installation.md) - Complete system setup
