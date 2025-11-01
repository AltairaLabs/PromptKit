# PromptKit

<!-- Build & Quality Badges -->
[![CI](https://github.com/AltairaLabs/PromptKit/workflows/CI/badge.svg)](https://github.com/AltairaLabs/PromptKit/actions/workflows/ci.yml)
[![codecov](https://codecov.io/gh/AltairaLabs/PromptKit/branch/main/graph/badge.svg)](https://codecov.io/gh/AltairaLabs/PromptKit)
[![Quality Gate Status](https://sonarcloud.io/api/project_badges/measure?project=AltairaLabs_promptkit-public&metric=alert_status)](https://sonarcloud.io/summary/new_code?id=AltairaLabs_promptkit-public)
[![Go Report Card](https://goreportcard.com/badge/github.com/AltairaLabs/PromptKit)](https://goreportcard.com/report/github.com/AltairaLabs/PromptKit)

<!-- Security & Compliance Badges -->
[![OpenSSF Scorecard](https://api.securityscorecards.dev/projects/github.com/AltairaLabs/PromptKit/badge)](https://api.securityscorecards.dev/projects/github.com/AltairaLabs/PromptKit)

<!-- Version & Distribution Badges -->
[![Release](https://img.shields.io/github/release/AltairaLabs/PromptKit.svg)](https://github.com/AltairaLabs/PromptKit/releases)
[![Go Version](https://img.shields.io/github/go-mod/go-version/AltairaLabs/PromptKit)](https://golang.org/)
[![Go Reference](https://pkg.go.dev/badge/github.com/AltairaLabs/PromptKit.svg)](https://pkg.go.dev/github.com/AltairaLabs/PromptKit)

<!-- License Badges -->
[![License: Apache 2.0](https://img.shields.io/badge/License-Apache%202.0-blue.svg)](https://opensource.org/licenses/Apache-2.0)
> **Professional LLM Testing & Production Deployment Framework**

PromptKit is an open-source framework for testing, optimizing, and deploying LLM-based applications with confidence.

## 🎯 What is PromptKit?

PromptKit provides two main components:

- **PromptKit Arena** - A comprehensive testing framework for LLM conversations, prompts, and tool usage
- **PromptKit SDK** - A production-ready library for deploying LLM applications

## 🚀 Quick Start

### Installation

```bash
# Install Arena CLI
go install github.com/AltairaLabs/PromptKit/tools/arena/cmd/promptarena@latest

# Or use as a library
go get github.com/AltairaLabs/PromptKit/pkg/...
```

### Arena - Test Your LLM Applications

```bash
# Run tests across multiple providers
promptarena run scenarios/customer-support.yaml --out results/

# Generate HTML report
promptarena run scenarios/customer-support.yaml --html report.html
```

### SDK - Deploy to Production

```go
import (
    "github.com/AltairaLabs/PromptKit/sdk"
    "github.com/AltairaLabs/PromptKit/runtime/providers"
)

// Create a conversation engine
engine := sdk.NewEngine(sdk.Config{
    Provider: providers.NewOpenAIProvider("gpt-4", ...),
    Prompts:  sdk.LoadPrompts("./prompts"),
})

// Execute conversations
result, err := engine.Chat(ctx, userMessage)
```

## 📦 Repository Structure

This is a monorepo containing multiple tools and libraries:

```
promptkit/
├── tools/
│   ├── arena/          # PromptKit Arena - Testing framework
│   └── packc/          # Pack Compiler - Prompt packaging tool
├── sdk/                # PromptKit SDK - Production library
├── runtime/            # Runtime components and shared libraries
├── pkg/                # Shared packages
├── examples/           # Example scenarios and configs
└── docs/               # Documentation
```

## ✨ Features

### Multi-Provider Support

- **OpenAI** (GPT-4, GPT-3.5)
- **Anthropic** (Claude 3 Opus, Sonnet, Haiku)
- **Google** (Gemini Pro, Ultra)
- Easy to add custom providers

### MCP Integration

- **Native Model Context Protocol support** - Connect to any MCP-compliant tool server
- **Real tool execution** - Test with actual tools, not mocks
- **Multi-server** - Use memory, filesystem, databases, and custom tools simultaneously
- **Auto-discovery** - Tools are automatically discovered from connected servers

### Testing Capabilities

- Multi-turn conversation testing
- Provider comparison matrices
- Tool/function calling validation with real MCP tools
- Self-play testing with AI personas
- Cost and latency tracking

### Production Ready

- Type-safe configuration
- Comprehensive error handling
- Context propagation
- Structured logging
- Tool execution framework

## 🤝 Contributing

We welcome contributions! Please see [CONTRIBUTING.md](./CONTRIBUTING.md) for details.

## 📄 License

Apache License 2.0 - See [LICENSE](./LICENSE) for details.

## 🏢 About AltairaLabs

PromptKit is built and maintained by [AltairaLabs.ai](https://altairalabs.ai), a company focused on making LLM development more reliable and production-ready.

---

Built with ❤️ by the AltairaLabs team