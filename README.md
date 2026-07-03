# PromptKit

[![CI](https://github.com/AltairaLabs/PromptKit/workflows/CI/badge.svg)](https://github.com/AltairaLabs/PromptKit/actions/workflows/ci.yml)
[![Docs](https://github.com/AltairaLabs/PromptKit/actions/workflows/docs.yml/badge.svg)](https://github.com/AltairaLabs/PromptKit/actions/workflows/docs.yml)
[![Quality Gate Status](https://sonarcloud.io/api/project_badges/measure?project=AltairaLabs_PromptKit&metric=alert_status)](https://sonarcloud.io/summary/new_code?id=AltairaLabs_PromptKit)
[![Coverage](https://sonarcloud.io/api/project_badges/measure?project=AltairaLabs_PromptKit&metric=coverage)](https://sonarcloud.io/summary/new_code?id=AltairaLabs_PromptKit)
[![Go Reference](https://pkg.go.dev/badge/github.com/AltairaLabs/PromptKit.svg)](https://pkg.go.dev/github.com/AltairaLabs/PromptKit)
[![License: Apache 2.0](https://img.shields.io/badge/License-Apache%202.0-blue.svg)](https://opensource.org/licenses/Apache-2.0)

> **The high-performance Go runtime and SDK for production LLM applications.**

PromptKit is a Go library for building LLM apps that hold up under real load:
multi-provider streaming with a bounded back-pressure stack, native tool calling
(MCP), agent-to-agent orchestration, workflow state machines, voice, and
multimodal — all behind one pipeline with first-class metrics. It stays flat on
memory and CPU where other frameworks balloon or fall over (see
[Performance](#performance)).

Config is portable via the [**PromptPack**](https://github.com/AltairaLabs/promptpack-spec)
spec, so your prompts, providers, and tools aren't locked to a vendor.

## How it fits together

```
PromptPack  ── open spec for portable prompts (JSON, vendor-neutral)
    │
    ├── PromptKit  ── this repo: Go runtime + SDK (embed in your application)
    │
    └── PromptArena  ── github.com/AltairaLabs/promptarena
         ├── promptarena  ── test, red-team, evaluate (CLI)
         └── packc        ── compile config → portable pack
```

PromptKit is the runtime your application links against. The
[**PromptArena**](https://github.com/AltairaLabs/promptarena) CLI (testing,
red-team, evaluation) and the **PackC** compiler build on it and live in their
own repo.

## Performance

The [efficiency benchmark](./benchmarks) ([#919](https://github.com/AltairaLabs/PromptKit/pull/919))
runs a realistic tool-calling profile — the framework receives tool calls,
executes them against a mock tool endpoint, feeds results back, and streams the
final response — against a shared mock upstream. Resident memory and CPU are
sampled at 100 / 500 / 1000 / 2000 concurrent streams; cost is computed against
a `c6g.xlarge` reference ($0.136/hr). The load-test harness lives in
[`benchmarks/`](./benchmarks); reproduce the numbers below with
`make -C benchmarks round1-tools` (Docker spins up the mock upstream plus each
framework). Full write-up:
[*Bulletproofing Streaming LLM Calls*](https://altairalabs.ai/blog/streaming-llm-back-pressure).

**Resident memory** (MB, lower is better)

| Concurrent | PromptKit | LangChain | Vercel AI | Strands |
|---|---|---|---|---|
| 100  | **74**  | 220   | 370   | 458   |
| 500  | **210** | 688   | 903   | 1,229 |
| 1000 | **348** | 1,331 | 1,024 | 2,355 |
| 2000 | **607** | OOM   | 1,024 | 4,608 |

**CPU utilization** (%, lower is better)

| Concurrent | PromptKit | LangChain | Vercel AI | Strands |
|---|---|---|---|---|
| 100  | **29** | 67 | 54  | 54 |
| 500  | **29** | 98 | 140 | 96 |
| 1000 | **29** | 99 | 131 | 99 |
| 2000 | **29** | —  | 115 | 99 |

**Cost per million requests** (USD, lower is better)

| Concurrent | PromptKit | LangChain | Vercel AI | Strands |
|---|---|---|---|---|
| 100  | **$0.03** | $0.14 | $0.06 | $0.09 |
| 500  | **$0.03** | $0.11 | $0.05 | $0.08 |
| 1000 | **$0.03** | $0.12 | $0.03 | $0.10 |
| 2000 | **$0.03** | —     | $0.04 | $0.33 |

PromptKit holds **~29% CPU and $0.03 per million requests from 100 concurrent
all the way to 2000**, with memory growing linearly instead of exploding.
LangChain OOMs at 2000; Strands uses **6.6× more memory** at 1000 concurrent.
That flatness comes from bounded concurrency, a cross-call retry budget, and
acquire-before-work back-pressure — not from cutting corners. The benchmark
harness is in-tree and CI runs a perf-regression gate on every PR, so a
throughput or memory regression fails the build.

## Capabilities

| | |
|---|---|
| **Providers** | One interface across capability types — **inference**, **speech-to-text**, **text-to-speech**, **embeddings**, and **image** — for OpenAI (Chat + Responses), Anthropic Claude, Google Gemini, Ollama, vLLM, Imagen, and VoyageAI, with structured error types |
| **Platforms** | Run any provider on its direct vendor API or a hyperscaler **platform** with keyless auth — Azure, AWS Bedrock, or Google Vertex |
| **Resilient streaming** | Three-layer back-pressure: bounded pre-first-chunk retry, per-provider retry budget (thundering-herd control), and a total in-flight semaphore |
| **Tools & MCP** | Native tool calling with a real [Model Context Protocol](https://modelcontextprotocol.io) client for live tool execution |
| **Agent-to-Agent (A2A)** | Multi-agent orchestration, discovery, and an A2A protocol server (`server/a2a`) |
| **Workflow composition** | Event-driven state machines with orchestration modes and context carry-forward |
| **Evals & guardrails** | Pluggable eval primitives (RAG, safety, quality) and inline guardrails that enforce in production |
| **Voice / duplex** | Streaming STT + TTS stages and full-duplex realtime sessions (Gemini Live, OpenAI Realtime) with the same retry semantics as text |
| **Multimodal** | Audio + vision + video inputs, plus image/media generation |
| **Skills** | Native [AgentSkills.io](https://agentskills.io) support with progressive, demand-driven knowledge loading |
| **Memory & state** | Conversation memory, a pluggable state store, and session recording / replay |
| **Observability** | Direct-update Prometheus metrics (in-flight gauges, retry budgets, first-chunk latency) that don't drop under load |

## Install

```bash
go get github.com/AltairaLabs/PromptKit/sdk
```

Requires Go 1.26+.

## Quick Start

Embed a conversation in your Go application. Prompts, providers, and tools are
loaded from a portable [PromptPack](https://github.com/AltairaLabs/promptpack-spec)
(hand-written, or compiled from YAML with [`packc`](https://github.com/AltairaLabs/promptarena)):

```go
package main

import (
	"context"
	"fmt"
	"log"

	"github.com/AltairaLabs/PromptKit/sdk"
)

func main() {
	conv, err := sdk.Open("./app.pack.json", "chat", sdk.WithModel("gpt-4o"))
	if err != nil {
		log.Fatal(err)
	}
	defer conv.Close()

	resp, err := conv.Send(context.Background(), "Summarize the Q3 report.")
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println(resp.Text())
}
```

For streaming, tools, workflows, A2A, and duplex voice, see the
[SDK documentation](https://promptkit.altairalabs.ai/sdk/).

## Repository Structure

```
promptkit/
├── sdk/               # Production SDK (Open/OpenDuplex/OpenWorkflow, capabilities, options)
├── runtime/           # Core runtime (providers, pipeline, streaming, tools, mcp, a2a, voice, deploy)
├── pkg/               # Shared config and schema-validation packages
├── server/a2a/        # A2A protocol server module
├── tools/schema-gen/  # JSON Schema generator for config types
├── benchmarks/        # Efficiency/throughput harness vs LangChain, Vercel AI, Strands
├── examples/          # SDK examples (A2A, logging config)
└── docs/              # Documentation
```

## Documentation

Full docs at [promptkit.altairalabs.ai](https://promptkit.altairalabs.ai).

## Contributing

See [CONTRIBUTING.md](./CONTRIBUTING.md).

## AI Development

For AI coding assistants working on this repository, see [AGENTS.md](./AGENTS.md) for critical development rules and pre-commit requirements.

## License

Apache 2.0 - See [LICENSE](./LICENSE).

---

Built by [AltairaLabs.ai](https://altairalabs.ai)
