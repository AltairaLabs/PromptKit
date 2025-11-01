# Contributing to PromptKit

Thank you for your interest in contributing to PromptKit! This document provides guidelines and instructions for contributing.

## Code of Conduct

Be respectful, inclusive, and constructive in all interactions.

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
4. **Request review** - Tag maintainers
5. **Address feedback** - Respond to review comments
6. **Squash commits** - Clean commit history before merge

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