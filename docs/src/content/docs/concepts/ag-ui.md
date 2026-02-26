---
title: AG-UI (Agent-User Interaction)
description: How the AG-UI protocol connects agents to frontend applications in PromptKit
sidebar:
  order: 8
---

How the AG-UI protocol bridges AI agents and frontend applications, enabling real-time streaming of agent activity to user interfaces.

---

## The Protocol Triangle

Modern AI systems communicate across three boundaries, each served by a dedicated protocol:

| Protocol | Connection | Purpose |
|----------|------------|---------|
| **MCP** | Agent ↔ Tools | Structured tool discovery and execution |
| **A2A** | Agent ↔ Agents | Inter-agent task delegation and collaboration |
| **AG-UI** | Agent ↔ Frontends | Real-time agent activity streaming to user interfaces |

MCP gives agents access to tools. A2A lets agents collaborate with each other. AG-UI closes the loop by connecting agents to the humans who use them, providing a standard way for frontends to observe and interact with agent execution in real time.

---

## What AG-UI Solves

Without AG-UI, every frontend that displays agent activity needs custom integration code: polling endpoints, proprietary WebSocket protocols, or framework-specific bindings. AG-UI standardizes this into a single request/response pattern:

1. The frontend sends a **`RunAgentInput`** request describing the conversation state
2. The server responds with a **Server-Sent Events (SSE)** stream of typed events
3. The frontend renders events as they arrive — text tokens, tool calls, state changes, workflow steps

This decouples agent logic from UI rendering. Any AG-UI-compatible frontend can connect to any AG-UI-compatible backend.

---

## Protocol Overview

### Request: RunAgentInput

The frontend sends a JSON payload describing the current conversation:

```json
{
  "threadId": "thread-abc123",
  "runId": "run-xyz789",
  "messages": [
    {
      "id": "msg-1",
      "role": "user",
      "content": "What is the status of order #1234?"
    }
  ],
  "tools": [],
  "context": []
}
```

Key fields:

| Field | Description |
|-------|-------------|
| `threadId` | Identifies the conversation thread |
| `runId` | Unique identifier for this execution run |
| `messages` | Conversation history in AG-UI message format |
| `tools` | Frontend-defined tools the agent can call |
| `context` | Additional context values for the agent |

### Response: SSE Event Stream

The server responds with `Content-Type: text/event-stream` and emits a sequence of typed events:

```
data: {"type":"RUN_STARTED","threadId":"thread-abc123","runId":"run-xyz789"}

data: {"type":"TEXT_MESSAGE_START","messageId":"msg-2","role":"assistant"}

data: {"type":"TEXT_MESSAGE_CONTENT","messageId":"msg-2","delta":"Order #1234 is "}

data: {"type":"TEXT_MESSAGE_CONTENT","messageId":"msg-2","delta":"currently in transit."}

data: {"type":"TEXT_MESSAGE_END","messageId":"msg-2"}

data: {"type":"RUN_FINISHED","threadId":"thread-abc123","runId":"run-xyz789"}
```

---

## Event Types

AG-UI defines a rich set of event types covering the full lifecycle of an agent run:

### Lifecycle Events

| Event | Description |
|-------|-------------|
| `RUN_STARTED` | Agent run has begun |
| `RUN_FINISHED` | Agent run completed successfully |
| `RUN_ERROR` | Agent run encountered an error |

### Text Streaming Events

| Event | Description |
|-------|-------------|
| `TEXT_MESSAGE_START` | New assistant message beginning |
| `TEXT_MESSAGE_CONTENT` | Incremental text token/chunk |
| `TEXT_MESSAGE_END` | Assistant message complete |

### Tool Call Events

| Event | Description |
|-------|-------------|
| `TOOL_CALL_START` | Agent is invoking a tool |
| `TOOL_CALL_ARGS` | Incremental tool call arguments (streamed) |
| `TOOL_CALL_END` | Tool invocation complete |
| `TOOL_CALL_RESULT` | Result returned from tool execution |

### State Synchronization Events

| Event | Description |
|-------|-------------|
| `STATE_SNAPSHOT` | Full state snapshot |
| `STATE_DELTA` | Incremental state update (JSON Patch) |

### Workflow Step Events

| Event | Description |
|-------|-------------|
| `STEP_STARTED` | Workflow step has begun |
| `STEP_FINISHED` | Workflow step completed |

---

## How PromptKit Integrates

PromptKit provides the `sdk/agui` package as a bridge between SDK conversations and the AG-UI protocol. The integration follows a clear separation of concerns:

**PromptKit provides:**
- **Converters** — bidirectional mapping between PromptKit messages/tools and AG-UI types
- **EventAdapter** — observes a PromptKit conversation and emits AG-UI events

**Your application provides:**
- **HTTP endpoint** — accepts `RunAgentInput` requests and writes SSE responses
- **Session management** — maps thread IDs to PromptKit conversations

This design means PromptKit does not impose any HTTP framework or server architecture. The `EventAdapter` produces a channel of events; how you serve them is up to you.

### Event Mapping

When the `EventAdapter` observes a PromptKit conversation, it translates internal events to AG-UI events:

| PromptKit Activity | AG-UI Event(s) |
|--------------------|----------------|
| Send starts | `RUN_STARTED` |
| Text response begins | `TEXT_MESSAGE_START` |
| Text token streamed | `TEXT_MESSAGE_CONTENT` |
| Text response ends | `TEXT_MESSAGE_END` |
| Tool call initiated | `TOOL_CALL_START` → `TOOL_CALL_ARGS` → `TOOL_CALL_END` |
| Tool result returned | `TOOL_CALL_RESULT` |
| Workflow step transition | `STEP_STARTED` / `STEP_FINISHED` |
| Send completes | `RUN_FINISHED` |
| Error occurs | `RUN_ERROR` |

---

## Frontend Connectivity

AG-UI frontends connect using standard HTTP. The official AG-UI client SDK provides `HttpAgent` for JavaScript/TypeScript applications:

```typescript
import { HttpAgent } from "@ag-ui/client";

const agent = new HttpAgent({ url: "http://localhost:8080/ag-ui" });

agent.runAgent({
  threadId: "thread-1",
  runId: "run-1",
  messages: [{ id: "msg-1", role: "human", content: "Hello" }],
  tools: [],
  context: [],
});
```

Any HTTP client that can consume SSE streams works with AG-UI — the protocol is not tied to any specific frontend framework.

---

## Next Steps

- [AG-UI Integration Reference](/sdk/reference/ag-ui/) — complete API documentation for `sdk/agui`
- [Tutorial: AG-UI Integration](/sdk/tutorials/11-ag-ui-integration/) — build an AG-UI endpoint step by step
- [A2A Concept](/concepts/a2a/) — the complementary agent-to-agent protocol
- [AG-UI Protocol Repository](https://github.com/ag-ui-protocol/ag-ui) — protocol specification and SDKs
- [AG-UI Go SDK](https://github.com/ag-ui-protocol/ag-ui/tree/main/sdks/community/go) — Go community SDK
