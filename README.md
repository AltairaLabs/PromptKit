# PromptKit

[![CI](https://github.com/AltairaLabs/PromptKit/workflows/CI/badge.svg)](https://github.com/AltairaLabs/PromptKit/actions/workflows/ci.yml)
[![Quality Gate Status](https://sonarcloud.io/api/project_badges/measure?project=AltairaLabs_PromptKit&metric=alert_status)](https://sonarcloud.io/summary/new_code?id=AltairaLabs_PromptKit)
[![Coverage](https://sonarcloud.io/api/project_badges/measure?project=AltairaLabs_PromptKit&metric=coverage)](https://sonarcloud.io/summary/new_code?id=AltairaLabs_PromptKit)
[![Go Report Card](https://goreportcard.com/badge/github.com/AltairaLabs/PromptKit)](https://goreportcard.com/report/github.com/AltairaLabs/PromptKit)
[![Go Reference](https://pkg.go.dev/badge/github.com/AltairaLabs/PromptKit.svg)](https://pkg.go.dev/github.com/AltairaLabs/PromptKit)
[![License: Apache 2.0](https://img.shields.io/badge/License-Apache%202.0-blue.svg)](https://opensource.org/licenses/Apache-2.0)

**Test, red-team, and deploy LLM applications with confidence.**

## Install

```bash
git clone https://github.com/AltairaLabs/PromptKit.git && cd PromptKit
make install-tools-user
```

## Quick Start

### 1. Create a project

```bash
promptarena init my-project --template iot-maintenance-demo
cd my-project
```

![init project](recordings/gifs/02-init-project.gif)

### 2. Inspect configuration

```bash
promptarena config-inspect
```

![config overview](recordings/gifs/03-config-overview.gif)

### 3. Run a test scenario

```bash
promptarena run --scenario scenarios/hardware-faults.scenario.yaml
```

![run scenario](recordings/gifs/05-run-scenario.gif)

### 4. Red-team security testing

```bash
promptarena run --scenario scenarios/redteam-selfplay.scenario.yaml
```

![redteam test](recordings/gifs/06-redteam-test.gif)

### 5. Review results

```bash
promptarena view
```

![view conversation](recordings/gifs/07-view-conversation.gif)

### 6. Deploy with the SDK

Compile prompts and run in your Go application:

```bash
packc compile -c config.arena.yaml -o app.pack.json
```

![sdk demo](recordings/gifs/08-sdk-demo.gif)

## Features

| Feature | Description |
|---------|-------------|
| **Multi-Provider** | OpenAI, Anthropic, Google Gemini, Azure OpenAI |
| **MCP Integration** | Native Model Context Protocol for real tool execution |
| **Self-Play Testing** | AI personas for adversarial and user simulation |
| **Red-Team** | Security testing with prompt injection detection |
| **Tool Validation** | Mock or live tool call verification |
| **SDK Deployment** | Compile prompts to portable packs for production |

## Repository Structure

```
promptkit/
├── tools/arena/     # PromptKit Arena CLI
├── tools/packc/     # Pack Compiler CLI
├── sdk/             # Production SDK
├── runtime/         # Shared runtime
├── examples/        # Example projects
└── docs/            # Documentation
```

## Contributing

See [CONTRIBUTING.md](./CONTRIBUTING.md).

## License

Apache 2.0 - See [LICENSE](./LICENSE).

---

Built by [AltairaLabs.ai](https://altairalabs.ai)
