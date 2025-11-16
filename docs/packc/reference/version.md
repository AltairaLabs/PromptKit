---
layout: default
title: version
parent: Reference
grand_parent: PackC
nav_order: 5
---

# packc version

Display the version of the packc compiler.

## Synopsis

```bash
packc version
```

## Description

The `version` command displays the current version of the packc compiler. This is useful for:

- Verifying installation
- Checking compatibility with pack format versions
- Reporting issues or bugs
- Ensuring consistent builds across environments
- Documenting build environments

The version follows semantic versioning (MAJOR.MINOR.PATCH) and is embedded in compiled packs to track which compiler version was used.

## Usage

```bash
packc version
```

**Output:**

```
packc v0.1.0
```

## Version Format

PackC uses semantic versioning with the format `vMAJOR.MINOR.PATCH`:

- **MAJOR** - Incompatible API changes or pack format changes
- **MINOR** - New functionality in a backwards-compatible manner
- **PATCH** - Backwards-compatible bug fixes

Example versions:
- `v0.1.0` - Initial development release
- `v1.0.0` - First stable release
- `v1.1.0` - Added new features
- `v1.1.1` - Bug fixes only

## Compiler Version in Packs

When packc compiles a pack, it embeds the compiler version in the pack metadata:

```json
{
  "id": "customer-support",
  "version": "1.0.0",
  "compiler_version": "packc-v0.1.0",
  "prompts": { ... },
  "metadata": {
    "compiled_at": "2025-01-16T10:30:00Z",
    "compiler_version": "packc-v0.1.0"
  }
}
```

This allows you to:
- Track which compiler built each pack
- Ensure compatibility with SDK versions
- Reproduce builds with the same compiler
- Audit pack provenance

## Examples

### Check Version

```bash
$ packc version
packc v0.1.0
```

### Verify Installation

```bash
# Check if packc is installed
if command -v packc &> /dev/null; then
  echo "packc is installed: $(packc version)"
else
  echo "packc is not installed"
fi
```

Output:

```
packc is installed: packc v0.1.0
```

### Version in Scripts

```bash
#!/bin/bash
# check-packc-version.sh

required_version="v0.1.0"
installed_version=$(packc version | cut -d' ' -f2)

if [ "$installed_version" = "$required_version" ]; then
  echo "✓ Correct packc version: $installed_version"
else
  echo "✗ Version mismatch: required $required_version, found $installed_version"
  exit 1
fi
```

### Document Build Environment

```bash
# Create build info file
cat > build-info.txt <<EOF
Build Information
=================
Compiler: $(packc version)
Date: $(date -u +"%Y-%m-%d %H:%M:%S UTC")
Host: $(hostname)
User: $(whoami)
Git Commit: $(git rev-parse --short HEAD)
EOF
```

## Integration

### CI/CD Version Check

```yaml
# .github/workflows/verify-compiler.yml
name: Verify Compiler Version

on: [push, pull_request]

jobs:
  verify:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v3
      
      - name: Install packc
        run: go install github.com/AltairaLabs/PromptKit/tools/packc@latest
      
      - name: Check version
        run: |
          version=$(packc version)
          echo "Installed: $version"
          echo "PACKC_VERSION=$version" >> $GITHUB_ENV
      
      - name: Verify minimum version
        run: |
          required="v0.1.0"
          if [[ "$PACKC_VERSION" < "packc $required" ]]; then
            echo "Error: packc version must be at least $required"
            exit 1
          fi
```

### Makefile Version Check

```makefile
.PHONY: check-version
check-version:
	@echo "Checking packc version..."
	@version=$$(packc version 2>/dev/null); \
	if [ $$? -eq 0 ]; then \
		echo "✓ Found: $$version"; \
	else \
		echo "✗ packc not installed"; \
		exit 1; \
	fi

.PHONY: build-with-version
build-with-version: check-version
	@version=$$(packc version | cut -d' ' -f2); \
	echo "Building with packc $$version"; \
	packc compile --config arena.yaml --output "packs/app-$$version.pack.json" --id app
```

### Docker Version Check

```dockerfile
# Dockerfile
FROM golang:1.22 AS builder

RUN go install github.com/AltairaLabs/PromptKit/tools/packc@latest

# Verify installation
RUN packc version

FROM alpine:latest
COPY --from=builder /go/bin/packc /usr/local/bin/packc
RUN packc version
```

### Version Matrix Testing

```yaml
# .github/workflows/test-versions.yml
name: Test Multiple Versions

on: [push]

jobs:
  test:
    strategy:
      matrix:
        packc-version: ['v0.1.0', 'v0.2.0', 'latest']
    
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v3
      
      - name: Install specific packc version
        run: |
          if [ "${{ matrix.packc-version }}" = "latest" ]; then
            go install github.com/AltairaLabs/PromptKit/tools/packc@latest
          else
            go install github.com/AltairaLabs/PromptKit/tools/packc@${{ matrix.packc-version }}
          fi
      
      - name: Show version
        run: packc version
      
      - name: Compile packs
        run: |
          packc compile --config arena.yaml --output packs/test.pack.json --id test
          packc validate packs/test.pack.json
```

## Troubleshooting

### Command Not Found

```bash
$ packc version
bash: packc: command not found
```

**Solution**: Install packc or add to PATH:

```bash
# Install from source
go install github.com/AltairaLabs/PromptKit/tools/packc@latest

# Or add to PATH
export PATH="$PATH:/path/to/packc"
```

### Wrong Version Installed

```bash
$ packc version
packc v0.0.9
```

**Solution**: Update to latest version:

```bash
go install github.com/AltairaLabs/PromptKit/tools/packc@latest
```

### Version Mismatch in CI

```
Error: Pack compiled with packc v0.2.0 but SDK expects v0.1.0
```

**Solution**: Pin packc version in CI:

```yaml
- name: Install packc
  run: go install github.com/AltairaLabs/PromptKit/tools/packc@v0.1.0
```

## Version Compatibility

### Pack Format Versions

| PackC Version | Pack Format | SDK Compatibility |
|--------------|-------------|-------------------|
| v0.1.0       | 1.0         | SDK v0.1.x        |
| v0.2.0       | 1.1         | SDK v0.2.x        |
| v1.0.0       | 2.0         | SDK v1.x          |

### Checking Compatibility

```bash
# Check pack format version
packc inspect packs/app.pack.json | grep "Pack Format"

# Check SDK compatibility
go list -m github.com/AltairaLabs/PromptKit/sdk
```

### Upgrading

When upgrading packc:

1. Check changelog for breaking changes
2. Update in development environment first
3. Test compilation of existing packs
4. Validate with SDK version
5. Update CI/CD environment
6. Document version in project

```bash
# Save current version
current_version=$(packc version)
echo "$current_version" > .packc-version

# Upgrade
go install github.com/AltairaLabs/PromptKit/tools/packc@latest

# Test
packc compile --config arena.yaml --output /tmp/test.pack.json --id test
packc validate /tmp/test.pack.json
```

## Best Practices

### 1. Document Required Version

Create a `.tool-versions` file:

```
packc v0.1.0
```

Or in README:

```markdown
## Requirements

- Go 1.22+
- packc v0.1.0+
```

### 2. Pin Versions in CI/CD

```yaml
# Always use specific version in CI
- name: Install packc
  run: go install github.com/AltairaLabs/PromptKit/tools/packc@v0.1.0
```

### 3. Check Version Before Building

```bash
#!/bin/bash
required="v0.1.0"
installed=$(packc version | cut -d' ' -f2)

if [ "$installed" != "$required" ]; then
  echo "Warning: Expected packc $required, found $installed"
  exit 1
fi
```

### 4. Include in Build Output

```bash
echo "Compiled with $(packc version)" >> packs/build-info.txt
```

## See Also

- [compile command](compile.md) - Main compilation command
- [inspect command](inspect.md) - View pack metadata including compiler version
- [Installation Guide](../how-to/install.md) - Install packc
- [SDK Compatibility](../../sdk/explanation/architecture.md) - SDK version requirements
