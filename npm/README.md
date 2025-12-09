# PromptKit npm Packages

This directory contains npm package wrappers for PromptKit's Go CLI tools, enabling easy installation and usage through the npm ecosystem.

## 📦 Available Packages

### [@altairalabs/promptarena](./promptarena)

PromptKit Arena - Multi-turn conversation simulation and testing framework for LLM applications.

```bash
npm install -g @altairalabs/promptarena
# or
npx @altairalabs/promptarena run -c ./config
```

**Use for:**
- Testing LLM conversations across multiple providers
- Running multi-turn simulations
- Validating outputs with assertions
- Generating test reports
- Testing guardrails and tool usage

### [@altairalabs/packc](./packc)

PromptKit Pack Compiler - Compile and validate prompt configurations.

```bash
npm install -g @altairalabs/packc
# or
npx @altairalabs/packc compile -c config.arena.yaml
```

**Use for:**
- Compiling prompts into optimized packs
- Validating YAML configurations
- Inspecting prompt structures
- CI/CD integration

## 🚀 Quick Start

### Using npx (No Installation)

The fastest way to try PromptKit tools:

```bash
# Run arena tests
npx @altairalabs/promptarena run -c examples/customer-support

# Compile prompts
npx @altairalabs/packc compile -c config.arena.yaml
```

### Global Installation

Install tools globally for command-line use:

```bash
npm install -g @altairalabs/promptarena @altairalabs/packc

# Use directly
promptarena --version
packc version
```

### Project Dev Dependencies

Add to your JavaScript/TypeScript project:

```bash
npm install --save-dev @altairalabs/promptarena @altairalabs/packc
```

Then add to `package.json`:

```json
{
  "scripts": {
    "test:prompts": "promptarena run -c ./tests/arena-config",
    "build:prompts": "packc compile -c ./prompts/config.arena.yaml",
    "validate:prompts": "packc validate -c ./prompts/config.arena.yaml"
  }
}
```

## 🔧 How It Works

These npm packages provide a JavaScript-friendly way to use PromptKit's Go CLI tools:

1. **During Installation** (`npm install`):
   - Detects your OS and CPU architecture
   - Downloads the appropriate pre-built binary from [GitHub Releases](https://github.com/AltairaLabs/PromptKit/releases)
   - Extracts the binary from the release archive
   - Makes it executable (Unix-like systems)

2. **During Execution** (`npx` or direct command):
   - Node.js wrapper script spawns the Go binary
   - All arguments are forwarded to the binary
   - stdin/stdout/stderr are inherited

**Benefits:**
- ✅ No Go toolchain required
- ✅ No compilation on your machine
- ✅ Fast installation (downloads pre-built binaries)
- ✅ Works with npm, yarn, pnpm
- ✅ Integrates with existing npm workflows
- ✅ Same binaries as direct GitHub releases

## 🌍 Supported Platforms

All packages support:
- **macOS**: Intel (x86_64) and Apple Silicon (arm64)
- **Linux**: x86_64 and arm64
- **Windows**: x86_64 and arm64

## 📚 Documentation

- [PromptKit Main Documentation](https://github.com/AltairaLabs/PromptKit#readme)
- [Examples](https://github.com/AltairaLabs/PromptKit/tree/main/examples)
- [Configuration Reference](https://github.com/AltairaLabs/PromptKit/tree/main/docs)
- [Contributing Guide](https://github.com/AltairaLabs/PromptKit/blob/main/CONTRIBUTING.md)

## 🔍 Use Cases

### CI/CD Integration

```yaml
# .github/workflows/test.yml
- name: Install PromptKit tools
  run: npm install -g @altairalabs/promptarena @altairalabs/packc

- name: Validate prompts
  run: packc validate -c config/config.arena.yaml

- name: Run tests
  run: promptarena run -c config/config.arena.yaml
  env:
    OPENAI_API_KEY: ${{ secrets.OPENAI_API_KEY }}
```

### Local Development

```bash
# Quick validation during development
npx @altairalabs/packc validate -c prompts.yaml

# Run specific test scenarios
npx @altairalabs/promptarena run -c tests/edge-cases.yaml
```

### Monorepo Integration

```json
{
  "devDependencies": {
    "@altairalabs/promptarena": "^0.0.1",
    "@altairalabs/packc": "^0.0.1"
  },
  "scripts": {
    "test": "npm run test:prompts && npm run test:unit",
    "test:prompts": "promptarena run -c ./config/config.arena.yaml",
    "build": "packc compile -c ./config/config.arena.yaml && next build"
  }
}
```

## 🛠️ Troubleshooting

### Binary Download Fails

**Symptoms:**
- Installation hangs or fails
- `postinstall` script errors

**Solutions:**

1. **Check internet connection and GitHub access**
   ```bash
   curl -I https://github.com
   ```

2. **Verify release exists**
   - Visit [GitHub Releases](https://github.com/AltairaLabs/PromptKit/releases)
   - Confirm the version exists

3. **Check npm proxy settings**
   ```bash
   npm config get proxy
   npm config get https-proxy
   ```

4. **Try with verbose logging**
   ```bash
   npm install @altairalabs/promptarena --loglevel verbose
   ```

5. **Manual installation**
   ```bash
   # Download and extract manually
   curl -L https://github.com/AltairaLabs/PromptKit/releases/download/v0.0.1/PromptKit_v0.0.1_Darwin_arm64.tar.gz -o promptkit.tar.gz
   tar -xzf promptkit.tar.gz
   chmod +x promptarena packc
   ```

### Permission Denied Errors

**On macOS/Linux:**
```bash
chmod +x node_modules/@altairalabs/promptarena/promptarena
chmod +x node_modules/@altairalabs/packc/packc
```

**On Windows:**
- Run terminal as Administrator
- Check antivirus settings

### Version Mismatches

Ensure all packages are at the same version:

```bash
npm list @altairalabs/promptarena @altairalabs/packc
```

Update to latest:

```bash
npm update @altairalabs/promptarena @altairalabs/packc
```

### Platform Not Supported

If you get "Unsupported platform" error:

1. Check your platform:
   ```bash
   node -e "console.log(process.platform, process.arch)"
   ```

2. Verify it's in the supported list above
3. Report issue if your platform should be supported

## 🆚 Alternative Installation Methods

npm is just one way to install PromptKit tools:

| Method | Pros | Cons | Best For |
|--------|------|------|----------|
| **npm** | Easy for JS developers, no Go needed | Requires Node.js | JS/TS projects, CI/CD |
| **Homebrew** | Simple on macOS, auto-updates | macOS only | Mac users |
| **Go Install** | Always latest, native Go | Requires Go toolchain | Go developers |
| **Direct Download** | No dependencies | Manual updates | Quick testing |
| **Build from Source** | Full control, latest commits | Requires Go, slow | Contributors |

### Homebrew (macOS)

```bash
brew install altairalabs/tap/promptkit
```

### Go Install

```bash
go install github.com/AltairaLabs/PromptKit/tools/arena/cmd/promptarena@latest
go install github.com/AltairaLabs/PromptKit/tools/packc@latest
```

### Direct Binary Download

Visit [GitHub Releases](https://github.com/AltairaLabs/PromptKit/releases) and download for your platform.

## 🤝 Contributing

Contributions to the npm packages are welcome!

**To contribute:**

1. Fork the repository
2. Create a feature branch
3. Make your changes to files in `npm/`
4. Test locally with `npm pack` and `npm install <tarball>`
5. Submit a pull request

**Testing changes locally:**

```bash
# In the package directory
cd npm/promptarena
npm pack

# In another directory
npm install /path/to/altairalabs-promptarena-0.0.1.tgz
npx promptarena --version
```

## 📄 License

Apache-2.0 - see [LICENSE](https://github.com/AltairaLabs/PromptKit/blob/main/LICENSE)

## 🔗 Links

- [PromptKit Repository](https://github.com/AltairaLabs/PromptKit)
- [Issue Tracker](https://github.com/AltairaLabs/PromptKit/issues)
- [Discussions](https://github.com/AltairaLabs/PromptKit/discussions)
- [npm Organization](https://www.npmjs.com/org/altairalabs)

## 📧 Support

- **Issues**: [GitHub Issues](https://github.com/AltairaLabs/PromptKit/issues)
- **Discussions**: [GitHub Discussions](https://github.com/AltairaLabs/PromptKit/discussions)
- **Security**: See [SECURITY.md](https://github.com/AltairaLabs/PromptKit/blob/main/SECURITY.md)
