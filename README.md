# PromptKit

[![CI](https://github.com/AltairaLabs/PromptKit/workflows/CI/badge.svg)](https://github.com/AltairaLabs/PromptKit/actions/workflows/ci.yml)
[![Provider Matrix](https://github.com/AltairaLabs/PromptKit/actions/workflows/capability-matrix.yml/badge.svg)](https://github.com/AltairaLabs/PromptKit/actions/workflows/capability-matrix.yml)
[![Docs](https://github.com/AltairaLabs/PromptKit/actions/workflows/docs.yml/badge.svg)](https://github.com/AltairaLabs/PromptKit/actions/workflows/docs.yml)
[![Quality Gate Status](https://sonarcloud.io/api/project_badges/measure?project=AltairaLabs_PromptKit&metric=alert_status)](https://sonarcloud.io/summary/new_code?id=AltairaLabs_PromptKit)
[![Coverage](https://sonarcloud.io/api/project_badges/measure?project=AltairaLabs_PromptKit&metric=coverage)](https://sonarcloud.io/summary/new_code?id=AltairaLabs_PromptKit)
[![Go Report Card](https://goreportcard.com/badge/github.com/AltairaLabs/PromptKit)](https://goreportcard.com/report/github.com/AltairaLabs/PromptKit)
[![Go Reference](https://pkg.go.dev/badge/github.com/AltairaLabs/PromptKit.svg)](https://pkg.go.dev/github.com/AltairaLabs/PromptKit)
[![License: Apache 2.0](https://img.shields.io/badge/License-Apache%202.0-blue.svg)](https://opensource.org/licenses/Apache-2.0)

> **Multi-provider prompt testing and deployment. Test prompts before they fail in production.**

PromptKit is the open-source toolkit for [**PromptPack**](https://github.com/AltairaLabs/promptpack-spec) — test prompts across Claude, OpenAI, Gemini, Azure, and local models, run red-team scenarios in CI, and compile portable packs for runtime. The spec lives upstream so your config isn't locked to a vendor.

## How it fits together

```
PromptPack  ── open spec for portable prompts (JSON, vendor-neutral)
    │
    └── PromptKit  ── this repo
         ├── promptarena  ── test, red-team, evaluate (CLI)
         ├── packc        ── compile config → portable pack
         └── SDK          ── embed in your Go application
```

## Why PromptKit

| | PromptKit | Promptfoo | LangSmith | Helicone |
|---|---|---|---|---|
| Multi-provider testing | ✅ | ✅ | LangChain-only | Observability-only |
| Built-in workflow orchestration | ✅ | ❌ | Partial | ❌ |
| Red-team / security scenarios | ✅ | Partial | ❌ | ❌ |
| Voice self-play (persona callers → realtime agent) | ✅ | ❌¹ | ❌ | ❌ |
| Speech-emotion / paralinguistic checks | ✅ | ❌ | ❌ | ❌ |
| Multimodal scenarios (audio + vision + video) | ✅ | Partial | ❌ | ❌ |
| MCP integration | ✅ | ❌ | ❌ | ❌ |
| Spec-driven (portable packs) | ✅ ([PromptPack](https://github.com/AltairaLabs/promptpack-spec)) | ❌ | ❌ | ❌ |
| License | Apache 2.0 | MIT | Closed | Closed |

<sub>¹ Promptfoo has a text-only [simulated user](https://www.promptfoo.dev/docs/providers/simulated-user/) and separate audio testing, but doesn't combine them — it can't drive a persona-driven caller through a realtime voice agent.</sub>

## Install

```bash
npm install -g @altairalabs/promptarena @altairalabs/packc
```

Building from source: see [CONTRIBUTING.md](./CONTRIBUTING.md).

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

## Voice-agent self-play

You can't unit-test a voice agent — so PromptKit has AI personas *call it*. Synthetic, personality-driven callers (hostile, evasive, anxious) are driven through TTS into your realtime agent (Gemini Live, OpenAI Realtime), and structured assertions score whether it holds policy under pressure — never issuing an unauthorized refund, escalating when it should. It even checks the caller *sounds* angry (speech-emotion recognition), not just says angry words.

Try it in one command — keyless, runs green out of the box:

```bash
promptarena init my-refund-demo --template voice-refund-demo
cd my-refund-demo
promptarena run --provider mock-duplex --ci   # no API keys needed
```

Swap in `--provider gemini-2-flash` or `openai-gpt4o-realtime` (plus TTS keys) to run it against a live voice agent — pass rates vary, and that variation is the test. Full breakdown: [voice-refund walkthrough](https://altairalabs.github.io/PromptKit/arena/examples/voice-refund-demo/).

## Features

| Feature | Description |
|---------|-------------|
| **Multi-Provider** | OpenAI, Anthropic, Google Gemini, Azure OpenAI, Ollama, vLLM |
| **Skills** | Native [AgentSkills.io](https://agentskills.io) support — demand-driven knowledge loading with progressive disclosure |
| **A2A Protocol** | Agent-to-Agent communication with multi-agent orchestration and discovery |
| **Workflows** | Event-driven state machines with orchestration modes and context carry-forward |
| **MCP Integration** | Native Model Context Protocol for real tool execution |
| **Deploy Adapters** | Plan, apply, and manage deployments via pluggable adapter SDK |
| **Self-Play Testing** | AI personas for adversarial and user simulation |
| **Voice Self-Play** | Adversarial TTS personas stress-test realtime voice agents (Gemini Live, OpenAI Realtime), scored on behavior + speech-emotion |
| **Red-Team** | Security testing with prompt injection detection |
| **Tool Validation** | Mock or live tool call verification with three-level scoping |
| **SDK Deployment** | Compile prompts to portable packs for production |

## GitHub Actions

Integrate PromptKit into your CI/CD pipelines with our GitHub Actions:

### PromptArena Action

Run prompt tests in your workflows:

```yaml
- name: Run prompt tests
  uses: AltairaLabs/PromptKit/.github/actions/promptarena-action@v1
  with:
    config-file: config.arena.yaml
  env:
    OPENAI_API_KEY: ${{ secrets.OPENAI_API_KEY }}
```

### PackC Action

Compile and publish prompt packs to OCI registries:

```yaml
- name: Build and publish pack
  uses: AltairaLabs/PromptKit/.github/actions/packc-action@v1
  with:
    config-file: config.arena.yaml
    registry: ghcr.io
    repository: ${{ github.repository }}/prompts
    username: ${{ github.actor }}
    password: ${{ secrets.GITHUB_TOKEN }}
```

See the [GitHub Actions documentation](https://altairalabs.github.io/PromptKit/devops/) for full usage details.

## Repository Structure

```
promptkit/
├── tools/arena/     # PromptKit Arena CLI (testing, red-team, deploy)
├── tools/packc/     # Pack Compiler CLI
├── sdk/             # Production SDK (conversations, workflows, A2A, skills)
├── runtime/         # Shared runtime (providers, pipeline, tools, skills, a2a, deploy)
├── examples/        # Example projects
└── docs/            # Documentation
```

## Contributing

See [CONTRIBUTING.md](./CONTRIBUTING.md).

## AI Development

For AI coding assistants working on this repository, see [AGENTS.md](./AGENTS.md) for critical development rules and pre-commit requirements.

## License

Apache 2.0 - See [LICENSE](./LICENSE).

---

Built by [AltairaLabs.ai](https://altairalabs.ai)
