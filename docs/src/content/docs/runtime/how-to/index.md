---
title: Runtime how-to guides
description: Task-focused guides for building and operating applications on the PromptKit runtime
sidebar:
  order: 0
---

Each guide covers one task, with the code to carry it out. For the APIs behind them, see the [runtime reference](/runtime/reference/). To learn by building, start with the [runtime tutorials](/runtime/tutorials/). For the reasoning behind the design, see the [runtime explanation](/runtime/explanation/).

## Pipeline

| Task | Guide |
|---|---|
| Build and configure a pipeline | [Configure pipeline](/runtime/how-to/pipeline/configure-pipeline/) |
| Handle provider and pipeline errors | [Handle errors](/runtime/how-to/pipeline/handle-errors/) |
| Read a reply as it is generated | [Stream responses](/runtime/how-to/pipeline/streaming-responses/) |

## Providers

| Task | Guide |
|---|---|
| Connect to an LLM provider | [Set up providers](/runtime/how-to/providers/setup-providers/) |

## State

| Task | Guide |
|---|---|
| Persist conversations | [Manage state](/runtime/how-to/state/manage-state/) |

## Tools

| Task | Guide |
|---|---|
| Connect MCP servers and use their tools | [Integrate MCP](/runtime/how-to/tools/integrate-mcp/) |
| Map HTTP endpoints to tools | [Configure HTTP tool mapping](/runtime/how-to/tools/http-tool-mapping/) |

## A2A

| Task | Guide |
|---|---|
| Register a remote agent's skills as tools | [Use the A2A tool bridge](/runtime/how-to/a2a/use-a2a-tool-bridge/) |
| Test A2A calls against canned responses | [Use the A2A mock server](/runtime/how-to/a2a/use-a2a-mock-server/) |

## Observability

| Task | Guide |
|---|---|
| Track token usage and cost | [Monitor costs](/runtime/how-to/observability/monitor-costs/) |
| Expose Prometheus metrics | [Expose Prometheus metrics](/runtime/how-to/observability/prometheus-metrics/) |
| Send traces to an OpenTelemetry backend | [Export traces with OTLP](/runtime/how-to/observability/export-traces-otlp/) |
| Keep API keys out of debug logs | [Redact secrets from logs](/runtime/how-to/observability/redact-log-secrets/) |
