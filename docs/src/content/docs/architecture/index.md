---
title: Architecture
description: How the PromptKit modules, pipeline, providers, tools and event system fit together
sidebar:
  order: 0
---
These pages explain how PromptKit works internally: the components, the data flow between them, and the reasons for the main design choices.

- [System overview](/architecture/system-overview/): the modules and how a message moves through them.
- [Runtime pipeline](/architecture/runtime-pipeline/): the stage-based streaming execution engine.
- [Runtime events](/architecture/runtime-events/): the event bus and the events the runtime emits.
- [Runtime providers](/architecture/runtime-providers/): how the runtime talks to LLM providers.
- [Runtime tools and MCP](/architecture/runtime-tools-mcp/): tool execution and the Model Context Protocol client.
