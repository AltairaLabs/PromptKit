# Contributing to PromptKit

Thank you for your interest in contributing to PromptKit! This document provides comprehensive guidelines and instructions for contributing to our open source project.

## Code of Conduct

This project adheres to the [Contributor Covenant Code of Conduct](./CODE_OF_CONDUCT.md). By participating, you are expected to uphold this code. Please report unacceptable behavior to [conduct@altairalabs.ai](mailto:conduct@altairalabs.ai).

## Developer Certificate of Origin (DCO)

This project uses the Developer Certificate of Origin (DCO) to ensure that contributors have the right to submit their contributions. By making a contribution to this project, you certify that:

1. The contribution was created in whole or in part by you and you have the right to submit it under the open source license indicated in the file; or
2. The contribution is based upon previous work that, to the best of your knowledge, is covered under an appropriate open source license and you have the right under that license to submit that work with modifications, whether created in whole or in part by you, under the same open source license (unless you are permitted to submit under a different license), as indicated in the file; or
3. The contribution was provided directly to you by some other person who certified (1), (2) or (3) and you have not modified it.

### Signing Your Commits

To sign off on your commits, add the `-s` flag to your git commit command:

```bash
git commit -s -m "Your commit message"
```

This adds a "Signed-off-by" line to your commit message:

```
Signed-off-by: Your Name <your.email@example.com>
```

## How to Contribute

### Reporting Bugs

- Check existing issues first
- Provide clear reproduction steps
- Include version information
- Share relevant configuration/code samples

### Suggesting Features

- Open an issue describing the feature
- Explain the use case and benefits
- Discuss implementation approach

### Submitting Changes

1. **Fork the repository**
2. **Create a feature branch**: `git checkout -b feature/your-feature-name`
3. **Make your changes**
4. **Write/update tests**
5. **Run tests**: `make test`
6. **Run linter**: `make lint`
7. **Commit your changes**: Use clear, descriptive commit messages
8. **Push to your fork**: `git push origin feature/your-feature-name`
9. **Open a Pull Request**

## Development Setup

### Prerequisites

- Go 1.21 or later
- Make (for build automation)

### Setup

```bash
# Clone your fork
git clone https://github.com/YOUR_USERNAME/PromptKit.git
cd PromptKit

# Install dependencies
make install

# Run tests
make test

# Build all components
make build
```

### Project Structure

```
PromptKit/
├── runtime/          # Core runtime components
├── pkg/              # Shared packages
├── tools/arena/      # PromptKit Arena CLI
├── tools/packc/      # Pack Compiler tool
├── sdk/              # PromptKit SDK
├── examples/         # Example configurations
└── docs/             # Documentation
```

## Component-Specific Contribution Guidelines

### Arena CLI (`tools/arena/`)

**Focus**: Testing framework for LLM applications

**Key Areas for Contribution:**
- New test scenario types and assertions
- Provider integrations (OpenAI, Anthropic, Google, etc.)
- MCP (Model Context Protocol) tool integrations
- Report generation and visualization improvements
- Performance optimizations for large test suites

**Testing Arena Changes:**
```bash
# Build Arena
make build-arena

# Run Arena tests
cd tools/arena && go test ./...

# Test with example scenarios
./bin/promptarena run examples/customer-support/arena.yaml
```

### PackC CLI (`tools/packc/`)

**Focus**: Prompt packaging and compilation tool

**Key Areas for Contribution:**
- Pack format enhancements and validation
- New output formats and deployment targets
- Template processing improvements
- Schema validation and error reporting

**Testing PackC Changes:**
```bash
# Build PackC
make build-packc

# Run PackC tests
cd tools/packc && go test ./...

# Test pack compilation
./bin/packc build examples/customer-support/packs/
```

### SDK (`sdk/`)

**Focus**: Production-ready library for deploying LLM applications

**Key Areas for Contribution:**
- High-level API improvements
- New conversation patterns and middleware
- Integration helpers and utilities
- Performance optimizations
- Example applications and tutorials

**Testing SDK Changes:**
```bash
# Build and test SDK
cd sdk && go test ./...

# Run integration tests
cd sdk && go test -tags=integration ./...

# Test with examples
cd examples/customer-support && go run main.go
```

### Runtime (`runtime/`)

**Focus**: Core engine and shared components

**Key Areas for Contribution:**
- Provider implementations and optimizations
- Tool execution framework
- State management and persistence
- Pipeline processing improvements
- Security and error handling

**Testing Runtime Changes:**
```bash
# Build and test runtime
cd runtime && go test ./...

# Run comprehensive tests
make test

# Check coverage
make coverage
```

## Coding Guidelines

### Go Code Style

- Follow standard Go conventions
- Use `gofmt` for formatting (included in `make fmt`)
- Write clear, descriptive variable/function names
- Add package-level documentation comments
- Keep functions focused and testable

### Testing

- Write unit tests for new functionality
- Maintain test coverage above 50%
- Use table-driven tests where appropriate
- Mock external dependencies

### Documentation

- Update README.md if adding features
- Add inline comments for complex logic
- Update relevant example configurations
- Add package documentation for new packages

## Pull Request Process

1. **Ensure CI passes** - All tests and linter checks must pass
2. **Update documentation** - README, examples, inline docs
3. **Add changelog entry** - Describe your changes
4. **Request review** - Tag maintainers (see `.github/CODEOWNERS`)
5. **Address feedback** - Respond to review comments
6. **Resolve all conversations** - All review comments must be marked as resolved
7. **Sign commits** - Use `git commit -s` for DCO compliance
8. **Keep branch updated** - Rebase or merge with latest `main`
9. **Squash merge** - Maintains clean commit history (preferred)

**Note**: The `main` branch is protected. See [Branch Protection Guide](docs/devops/branch-protection.md) and [Quick Reference](docs/devops/branch-protection-quickref.md) for details.

## Release Process

Maintainers handle releases:

1. Update version numbers
2. Update CHANGELOG.md
3. Create git tag
4. Build and test release artifacts
5. Publish to GitHub releases

## Questions?

- Open a GitHub issue for questions
- Check existing documentation
- Review closed issues and PRs

## License

By contributing, you agree that your contributions will be licensed under the Apache License 2.0.