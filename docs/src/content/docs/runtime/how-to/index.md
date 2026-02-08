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
1. [Configure Pipeline](configure-pipeline) - Set up basic execution
2. [Setup Providers](setup-providers) - Connect to LLM providers
3. [Handle Errors](handle-errors) - Robust error handling

## Guide Categories

### Pipeline Configuration
- [Configure Pipeline](configure-pipeline) - Pipeline setup and configuration
- [Handle Errors](handle-errors) - Error handling strategies
- [Manage State](manage-state) - Conversation persistence

### Providers
- [Setup Providers](setup-providers) - LLM provider configuration
- [Switch Providers](switch-providers) - Multi-provider strategies
- [Monitor Costs](monitor-costs) - Cost tracking and optimization

### Tools & MCP
- [Implement Tools](implement-tools) - Create custom tools
- [Integrate MCP](integrate-mcp) - Connect MCP servers
- [Validate Tools](validate-tools) - Tool argument validation

### Observability
- [Prometheus Metrics](prometheus-metrics) - Monitor with Prometheus and Grafana
- [Monitor Costs](monitor-costs) - Cost tracking and optimization

### A2A (Agent-to-Agent)
- [Use Tool Bridge](use-a2a-tool-bridge) - Register A2A agent skills as local tools
- [Use Mock Server](use-a2a-mock-server) - Deterministic testing with mock A2A servers

### Advanced Topics
- [Streaming Responses](streaming-responses) - Real-time output
- [Custom Middleware](custom-middleware) - Extend pipeline behavior
- [Performance Tuning](performance-tuning) - Optimize throughput

## Quick Reference

| Task | Guide | Time |
|------|-------|------|
| Set up basic pipeline | [Configure Pipeline](configure-pipeline) | 5 min |
| Connect to OpenAI/Claude | [Setup Providers](setup-providers) | 5 min |
| Add MCP tools | [Integrate MCP](integrate-mcp) | 10 min |
| Track costs | [Monitor Costs](monitor-costs) | 5 min |
| Monitor with Prometheus | [Prometheus Metrics](prometheus-metrics) | 10 min |
| Handle errors | [Handle Errors](handle-errors) | 10 min |
| Stream responses | [Streaming Responses](streaming-responses) | 10 min |
| Persist conversations | [Manage State](manage-state) | 15 min |

## Code Examples

All guides include:
- ✅ Complete working code
- ✅ Step-by-step instructions
- ✅ Common pitfalls and solutions
- ✅ Best practices
- ✅ Related reference documentation

## See Also

- [Reference](../reference/) - Complete API documentation
- [Tutorials](../tutorials/) - Learn by building
- [Explanation](../explanation/) - Architecture and concepts
