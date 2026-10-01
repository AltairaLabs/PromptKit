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
3. The frontend renders events as they arrive — text, tool calls, state, workflow steps

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
| `tools` | Frontend tools: the application's own tools, which the agent may call and the application executes |
| `context` | Additional context values for the agent |
| `state` | The state the run starts from |
| `forwardedProps` | An application-specific value passed through to the agent |

### Response: SSE Event Stream

The server responds with `Content-Type: text/event-stream` and emits a sequence of typed events:

```
data: {"type":"RUN_STARTED","threadId":"thread-abc123","runId":"run-xyz789"}

data: {"type":"TEXT_MESSAGE_START","messageId":"msg-2","role":"assistant"}

data: {"type":"TEXT_MESSAGE_CONTENT","messageId":"msg-2","delta":"Order #1234 is currently in transit."}

data: {"type":"TEXT_MESSAGE_END","messageId":"msg-2"}

data: {"type":"RUN_FINISHED","threadId":"thread-abc123","runId":"run-xyz789"}
```

---

## Event Types

AG-UI defines events for the whole lifecycle of a run. The ones below are those PromptKit's adapter emits; the protocol also defines state deltas, message snapshots, reasoning, activity and subagent events, which it does not.

### Lifecycle Events

| Event | Description |
|-------|-------------|
| `RUN_STARTED` | Agent run has begun |
| `RUN_FINISHED` | Agent run ended without failing. An `interrupt` outcome means it stopped to ask for outside input |
| `RUN_ERROR` | Agent run failed |

### Text Message Events

| Event | Description |
|-------|-------------|
| `TEXT_MESSAGE_START` | New assistant message beginning |
| `TEXT_MESSAGE_CONTENT` | Text of the message |
| `TEXT_MESSAGE_END` | Assistant message complete |

### Tool Call Events

| Event | Description |
|-------|-------------|
| `TOOL_CALL_START` | Agent is calling a tool |
| `TOOL_CALL_ARGS` | The call's arguments |
| `TOOL_CALL_END` | The call's arguments are complete |
| `TOOL_CALL_RESULT` | The result of a tool the agent executed |

### State Events

| Event | Description |
|-------|-------------|
| `STATE_SNAPSHOT` | Full state snapshot |

### Step Events

| Event | Description |
|-------|-------------|
| `STEP_STARTED` | A step of the run has begun |
| `STEP_FINISHED` | The step has finished |

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

### Protocol Version

The `sdk/agui` package targets AG-UI 1.0 through the AG-UI community Go SDK. Not every 1.0 feature is produced yet; the gaps are listed under [What the Adapter Does Not Do](#what-the-adapter-does-not-do).

### Event Mapping

The `EventAdapter` runs one conversation turn as one AG-UI run. When the turn has run, it emits the messages the turn produced, in order:

| PromptKit Activity | AG-UI Event(s) |
|--------------------|----------------|
| Turn starts | `RUN_STARTED` |
| State provider configured | `STATE_SNAPSHOT` |
| Workflow conversation | `STEP_STARTED` naming the workflow state the run starts in |
| Each assistant message in the turn | `TEXT_MESSAGE_START` → `TEXT_MESSAGE_CONTENT` (its whole text) → `TEXT_MESSAGE_END` |
| Each tool call the model made | `TOOL_CALL_START` → `TOOL_CALL_ARGS` → `TOOL_CALL_END` |
| Each result of a tool the agent ran | `TOOL_CALL_RESULT` carrying what the tool returned |
| Workflow transition committed | `STEP_FINISHED` for the old state, `STEP_STARTED` for the new one |
| Turn completes | `STEP_FINISHED` for the open step, then `RUN_FINISHED` |
| Turn held for tool approval | `RUN_FINISHED` with an `interrupt` outcome naming the held calls |
| Error occurs | `RUN_ERROR` |

A model that calls a tool and then answers produces two assistant messages in one turn; the run carries both, with the call and its result between them.

### Frontend Tools

The tools in `RunAgentInput.tools` belong to the application: the agent proposes a call, the application executes it. AG-UI has no channel for the application to answer while a run is in progress, so a run that calls a frontend tool ends with the call unanswered — no `TOOL_CALL_RESULT` — and the application answers it in the next run's input, as a tool message. `EventAdapter.RunResume` takes those answers and continues the turn.

### Approval Holds

A tool registered with `OnToolAsync` can hold a call for approval. The run then ends with `RUN_FINISHED` carrying an `interrupt` outcome whose interrupts name each held call. Once your application approves or rejects the calls (`ResolveTool` / `RejectTool`), `EventAdapter.RunContinue` continues the turn and reports the approved tool's result.

### Delivery

The adapter never drops an event. If the consumer stops reading, the adapter waits; canceling the run's context releases it.

Read `Events()` while the run is in progress, from another goroutine. Once its buffer of 64 events is full, the run waits for the reader, so code that lets `RunSend` finish before it starts reading blocks there. Before this version the adapter dropped the events that did not fit, including `RUN_FINISHED`.

### What the Adapter Does Not Do

- **Stream tokens.** Each message's text arrives as one `TEXT_MESSAGE_CONTENT` once the turn has run.
- **Emit** `STATE_DELTA`, `MESSAGES_SNAPSHOT`, or reasoning events.
- **Name pending calls on** `RUN_FINISHED`. A run that ends with a frontend tool call unanswered finishes without an outcome, which means success; a consumer finds the pending calls as the calls that received no `TOOL_CALL_RESULT`.
- **Keep message ids stable.** The converters mint a new id each time they convert a message.
- **Read the whole history from the input.** A PromptKit conversation keeps its own history, so the integration in the [tutorial](/sdk/tutorials/11-ag-ui-integration/) passes only the new input to it. History a client edits or replaces is not reflected.

---

## Frontend Connectivity

AG-UI frontends connect using standard HTTP. The official AG-UI client SDK provides `HttpAgent` for JavaScript/TypeScript applications:

```typescript
import { HttpAgent } from "@ag-ui/client";

const agent = new HttpAgent({ url: "http://localhost:8080/ag-ui" });

agent.runAgent({
  threadId: "thread-1",
  runId: "run-1",
  messages: [{ id: "msg-1", role: "user", content: "Hello" }],
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
