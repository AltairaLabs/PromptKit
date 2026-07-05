---
title: Runtime How-To
sidebar:
  order: 0
---
Task-focused guides for common Runtime operations.

## Overview

These guides show you how to accomplish specific tasks with the PromptKit Runtime. Each guide focuses on a single goal with practical, working code.

## Getting Started

**New to Runtime?** Start with:
1. [Configure Pipeline](/runtime/how-to/pipeline/configure-pipeline/) - Set up basic execution
2. [Setup Providers](/runtime/how-to/providers/setup-providers/) - Connect to LLM providers
3. [Handle Errors](/runtime/how-to/pipeline/handle-errors/) - Robust error handling

## Guide Categories

### Pipeline Configuration
- [Configure Pipeline](/runtime/how-to/pipeline/configure-pipeline/) - Pipeline setup and configuration
- [Handle Errors](/runtime/how-to/pipeline/handle-errors/) - Error handling strategies
- [Manage State](/runtime/how-to/state/manage-state/) - Conversation persistence

### Providers
- [Setup Providers](/runtime/how-to/providers/setup-providers/) - LLM provider configuration
- [Switch Providers](/runtime/how-to/providers/setup-providers/) - Multi-provider strategies
- [Monitor Costs](/runtime/how-to/observability/monitor-costs/) - Cost tracking and optimization

### Tools & MCP
- [Integrate MCP](/runtime/how-to/tools/integrate-mcp/) - Connect MCP servers and create tools

### Observability
- [Prometheus Metrics](/runtime/how-to/observability/prometheus-metrics/) - Monitor with Prometheus and Grafana
- [Export Traces with OTLP](/runtime/how-to/observability/export-traces-otlp/) - Send distributed traces to OpenTelemetry backends
- [Monitor Costs](/runtime/how-to/observability/monitor-costs/) - Cost tracking and optimization

### A2A (Agent-to-Agent)
- [Use Tool Bridge](/runtime/how-to/a2a/use-a2a-tool-bridge/) - Register A2A agent skills as local tools
- [Use Mock Server](/runtime/how-to/a2a/use-a2a-mock-server/) - Deterministic testing with mock A2A servers

### Advanced Topics
- [Streaming Responses](/runtime/how-to/pipeline/streaming-responses/) - Real-time output

## Quick Reference

| Task | Guide | Time |
|------|-------|------|
| Set up basic pipeline | [Configure Pipeline](/runtime/how-to/pipeline/configure-pipeline/) | 5 min |
| Connect to OpenAI/Claude | [Setup Providers](/runtime/how-to/providers/setup-providers/) | 5 min |
| Add MCP tools | [Integrate MCP](/runtime/how-to/tools/integrate-mcp/) | 10 min |
| Track costs | [Monitor Costs](/runtime/how-to/observability/monitor-costs/) | 5 min |
| Monitor with Prometheus | [Prometheus Metrics](/runtime/how-to/observability/prometheus-metrics/) | 10 min |
| Export OTLP traces | [Export Traces with OTLP](/runtime/how-to/observability/export-traces-otlp/) | 10 min |
| Handle errors | [Handle Errors](/runtime/how-to/pipeline/handle-errors/) | 10 min |
| Stream responses | [Streaming Responses](/runtime/how-to/pipeline/streaming-responses/) | 10 min |
| Persist conversations | [Manage State](/runtime/how-to/state/manage-state/) | 15 min |

## Code Examples

All guides include:
- ✅ Complete working code
- ✅ Step-by-step instructions
- ✅ Common pitfalls and solutions
- ✅ Best practices
- ✅ Related reference documentation

## See Also

- [Reference](/runtime/reference/) - Complete API documentation
- [Tutorials](/runtime/tutorials/) - Learn by building
- [Explanation](/runtime/explanation/) - Architecture and concepts
