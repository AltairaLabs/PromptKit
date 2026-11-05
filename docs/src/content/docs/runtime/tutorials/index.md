---
title: Runtime Tutorials
description: A learning path for the PromptKit runtime, from a first pipeline to production patterns and the A2A client.
sidebar:
  order: 0
verified:
  commit: 885cafa787a84bfba66f158fe51cda15a077ec23
  sources:
    - runtime/a2a/client.go
    - runtime/hooks/guardrails/factory.go
    - runtime/pipeline/stage/stage.go
    - sdk/examples/mcp-tools/main_interactive.go
---
The runtime tutorials build on each other.

## Learning path

| # | Tutorial | You build | Time |
|---|---|---|---|
| 1 | [First pipeline](/runtime/tutorials/01-first-pipeline/) | A program that sends a prompt through a stage pipeline and prints the response, tokens and cost | 15 min |
| 2 | [Multi-turn conversations](/runtime/tutorials/02-multi-turn/) | A chatbot that keeps its history in a state store, in memory and in Redis | 20 min |
| 3 | [MCP integration](/runtime/tutorials/03-mcp-integration/) | A chatbot with file tools from an MCP server | 20 min |
| 4 | [Validation and guardrails](/runtime/tutorials/04-validation-guardrails/) | A chatbot whose guardrails block, replace or reject content | 20 min |
| 5 | [Production deployment](/runtime/tutorials/05-production-deployment/) | An API server with error handling, monitoring and cost tracking | 40 min |
| 6 | [Advanced patterns](/runtime/tutorials/06-advanced-patterns/) | Response caching, a custom stage with a spend budget, buffered streaming and parallel batching | 30 min |
| 7 | [A2A client](/runtime/tutorials/07-a2a-client/) | A client that discovers an A2A agent and sends it messages | 10 min |

## Prerequisites

- Go 1.26 or later
- An OpenAI API key in `OPENAI_API_KEY`
- A terminal

Tutorial 5 uses keys for more than one provider. Tutorial 7 needs a running A2A server.

## Time estimates

| Path | Tutorials | Time |
|---|---|---|
| Quick start | 1 | 15 min |
| Core skills | 1-3 | 55 min |
| Production ready | 1-5 | 115 min |
| Complete | 1-7 | 155 min |

Start with [First pipeline](/runtime/tutorials/01-first-pipeline/).

## See also

- [Runtime Reference](/runtime/reference/index/) for the API
- [Runtime How-To](/runtime/how-to/index/) for task-focused guides
- [Runtime Explanation](/runtime/explanation/index/) for architecture
