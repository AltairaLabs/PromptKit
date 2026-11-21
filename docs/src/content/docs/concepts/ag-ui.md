---
title: AG-UI (Agent-User Interaction)
description: How the AG-UI protocol connects agents to frontend applications in PromptKit
sidebar:
  order: 8
verified:
  commit: ebf8d92f226c18ca12b790f73b09a762bd8d2512
  sources:
    - sdk/agui/adapter.go
    - sdk/agui/adapter_stream.go
    - sdk/agui/adapter_reasoning.go
    - sdk/agui/adapter_state.go
    - sdk/agui/adapter_usage.go
    - sdk/agui/adapter_messages.go
    - sdk/agui/convert.go
    - sdk/streaming.go
    - sdk/conversation_tools.go
    - sdk/workflow.go
---

How the AG-UI protocol bridges AI agents and frontend applications, enabling real-time streaming of agent activity to user interfaces.

---

## The Protocol Triangle

Modern AI systems communicate across three boundaries, each served by a dedicated protocol:

| Protocol | Connection | Purpose |
|----------|------------|---------|
| MCP | Agent ↔ Tools | Structured tool discovery and execution |
| A2A | Agent ↔ Agents | Inter-agent task delegation and collaboration |
| AG-UI | Agent ↔ Frontends | Real-time agent activity streaming to user interfaces |

MCP gives agents access to tools. A2A lets agents collaborate with each other. AG-UI connects agents to the people who use them, giving frontends a standard way to observe and interact with agent execution in real time.

---

## What AG-UI Solves

Without AG-UI, every frontend that displays agent activity needs custom integration code: polling endpoints, proprietary WebSocket protocols, or framework-specific bindings. AG-UI standardizes this into a single request/response pattern:

1. The frontend sends a `RunAgentInput` request describing the conversation state.
2. The server responds with a Server-Sent Events (SSE) stream of typed events.
3. The frontend renders events as they arrive: text, tool calls, state, workflow steps.

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

data: {"type":"TEXT_MESSAGE_CONTENT","messageId":"msg-2","delta":"Order #1234 is "}

data: {"type":"TEXT_MESSAGE_CONTENT","messageId":"msg-2","delta":"currently in transit."}

data: {"type":"TEXT_MESSAGE_END","messageId":"msg-2"}

data: {"type":"RUN_FINISHED","threadId":"thread-abc123","runId":"run-xyz789"}
```

---

## Event Types

AG-UI defines events for the whole lifecycle of a run. The ones below are those PromptKit's adapter emits; the protocol also defines activity and subagent events, which it does not.

### Lifecycle Events

| Event | Description |
|-------|-------------|
| `RUN_STARTED` | Agent run has begun |
| `RUN_FINISHED` | Agent run ended without failing, with the run's token `usage`. An `interrupt` outcome means it stopped to ask for outside input; a `success` outcome with `pendingToolCallIds` names the frontend tool calls it left unanswered |
| `RUN_ERROR` | Agent run failed, with the `usage` of the model calls it made before failing |

### Text Message Events

| Event | Description |
|-------|-------------|
| `TEXT_MESSAGE_START` | New assistant message beginning |
| `TEXT_MESSAGE_CONTENT` | A chunk of the message's text; the chunks of a message make up its text |
| `TEXT_MESSAGE_END` | Assistant message complete |

### Reasoning Events

| Event | Description |
|-------|-------------|
| `REASONING_START` | A span of the model's reasoning begins |
| `REASONING_MESSAGE_START` | A reasoning message begins, with role `reasoning` |
| `REASONING_MESSAGE_CONTENT` | A chunk of the reasoning's text |
| `REASONING_MESSAGE_END` | The reasoning message is complete |
| `REASONING_END` | The span ends |

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
| `STATE_DELTA` | A change to the state, as an RFC 6902 JSON Patch against the state the client holds |
| `MESSAGES_SNAPSHOT` | The conversation's history as the agent holds it. Emitted only with `WithMessagesSnapshot(true)` |

### Step Events

| Event | Description |
|-------|-------------|
| `STEP_STARTED` | A step of the run has begun |
| `STEP_FINISHED` | The step has finished |

### Raw Events

| Event | Description |
|-------|-------------|
| `RAW` | A PromptKit runtime event, with source `promptkit`. Emitted only with `WithRuntimeEvents` |

---

## How PromptKit Integrates

The `sdk/agui` package bridges SDK conversations and the AG-UI protocol. Responsibilities split as follows.

PromptKit provides:

- Converters: bidirectional mapping between PromptKit messages and tools and AG-UI types.
- `EventAdapter`: runs a PromptKit conversation turn and emits AG-UI events.

Your application provides:

- An HTTP endpoint that accepts `RunAgentInput` requests and writes SSE responses.
- Session management that maps thread IDs to PromptKit conversations.

PromptKit imposes no HTTP framework or server architecture. The `EventAdapter` produces a channel of events, and you choose how to serve them.

### Protocol Version

The `sdk/agui` package targets AG-UI 1.0 through the AG-UI community Go SDK. The 1.0 features the adapter does not produce are listed under [What the Adapter Does Not Do](#what-the-adapter-does-not-do).

### Event Mapping

The `EventAdapter` runs one conversation turn as one AG-UI run. For an `*sdk.Conversation` it drives the turn through `Conversation.Stream` and emits each round of the turn as it happens: the reply's text as the model streams it, the round's tool calls once the reply has streamed and before they run, then their results. The events come in this order:

| PromptKit Activity | AG-UI Event(s) |
|--------------------|----------------|
| Turn starts | `RUN_STARTED` |
| State provider configured | `STATE_SNAPSHOT` of the state, omitted if the provider returns an error or no state |
| Workflow conversation | `STEP_STARTED` naming the workflow state the run starts in |
| The model reasons, with `WithReasoning(true)`, on a conversation opened with `sdk.WithStreamReasoning` | `REASONING_START` → `REASONING_MESSAGE_START` → a `REASONING_MESSAGE_CONTENT` for each chunk of reasoning the model streams → `REASONING_MESSAGE_END` → `REASONING_END`, closed when the round's text or tool calls start |
| Each assistant message in the turn | `TEXT_MESSAGE_START` → a `TEXT_MESSAGE_CONTENT` for each chunk of text the model streams → `TEXT_MESSAGE_END` |
| Each tool call the model made | `TOOL_CALL_START` → `TOOL_CALL_ARGS` (when the call has arguments) → `TOOL_CALL_END`, once the round's reply has streamed |
| Each result of a tool the agent ran | `TOOL_CALL_RESULT` carrying what the tool returned, then `STATE_DELTA` when the state changed and `WithStateAfterEachTool(true)` is set |
| Workflow transition committed | `STEP_FINISHED` for the old state, `STEP_STARTED` for the new one |
| `WithRuntimeEvents` set | `RAW` for each runtime event the conversation's event bus delivers while the run is open |
| Reasoning that did not stream, with `WithReasoning(true)` | The same reasoning events, carrying the round's reasoning whole: after the turn's text when the turn streamed, before the round's text when it did not |
| Turn completes | `STATE_DELTA` when the state changed, `STEP_FINISHED` for the open step, `MESSAGES_SNAPSHOT` when `WithMessagesSnapshot(true)` is set, then `RUN_FINISHED` with the run's `usage` |
| Turn left frontend tool calls unanswered | `RUN_FINISHED` with a `success` outcome whose `pendingToolCallIds` name them |
| Turn held for tool approval | `RUN_FINISHED` with an `interrupt` outcome naming the held calls |
| Error occurs | `TEXT_MESSAGE_END` for a message still streaming, then `RUN_ERROR` with the `usage` of the model calls made before the error |

A model that calls a tool and then answers produces two assistant messages in one turn; the run carries both, with the call and its result between them:

```text
RUN_STARTED
TEXT_MESSAGE_START                                              message 1
TEXT_MESSAGE_CONTENT                                            "Let me "
TEXT_MESSAGE_CONTENT                                            "look that up."
TEXT_MESSAGE_END                                                message 1
TOOL_CALL_START / TOOL_CALL_ARGS / TOOL_CALL_END                lookup_order, parent message 1
TOOL_CALL_RESULT                                                what lookup_order returned
TEXT_MESSAGE_START                                              message 2
TEXT_MESSAGE_CONTENT                                            "Your order "
TEXT_MESSAGE_CONTENT                                            "shipped yesterday."
TEXT_MESSAGE_END                                                message 2
RUN_FINISHED
```

How the text is split depends on the provider: each `TEXT_MESSAGE_CONTENT` carries one chunk it streamed. A provider that does not stream sends each message's text in one `TEXT_MESSAGE_CONTENT` once its reply is complete. A round that only calls tools sends no text message, and its calls still name a parent message id.

When an output guardrail cuts a reply that has started to stream, the client has already shown part of it, and AG-UI cannot take text back. The streamed message ends, and the guardrail's message follows as a new text message. The same happens whenever a reply's final text is not the text that streamed.

For a pack with a workflow, open it with `sdk.OpenWorkflow` and create the adapter with `NewWorkflowEventAdapter`. A workflow conversation does not stream: each turn runs to completion, then its messages are emitted, each message's text as one `TEXT_MESSAGE_CONTENT`. Each run then opens a step named after the workflow state it starts in and closes it before `RUN_FINISHED`; a turn that moves the workflow to another state finishes that step and starts one for the new state. `WithWorkflowSteps(false)` leaves the steps out.

A `TOOL_CALL_RESULT` is never empty, because the AG-UI Go SDK rejects an event with empty content. A result with no text carries its error, a description of the media it holds (such as `[image/png image]`), or the JSON encoding of its value (`""` for an empty string).

### Reasoning

A provider configured to return its reasoning (a Claude `thinking_budget`, Gemini's `include_thoughts`, OpenAI's `reasoning_summary`) gives each round of the turn a reasoning trace. The adapter sends it only with `WithReasoning(true)`: reasoning routinely holds what the model chose not to say, so the client gets it only when the application asks. It goes out as one reasoning span holding one reasoning message per round. On a conversation opened with `sdk.WithStreamReasoning()` it streams as the model produces it, ahead of the round's text. Without that option each round's reasoning is sent whole: after the turn's text when the turn streamed, so it never lands inside an open text message, or before the round's text when the conversation runs through `Send`.

A reply an output guardrail blocks keeps no reasoning: the runtime drops it from the reply, its history entry and its `Response`, so settled reasoning never shows the thinking behind a blocked reply. Streamed reasoning is different: it goes out as the model produces it, before the output guardrails judge the reply it leads to, and the client keeps what it has received. Enable streamed reasoning only for a client that may see what a guardrail would block.

Only the reasoning's text is sent. Opaque reasoning, the signatures and encrypted or redacted blocks a provider needs back on the next call, stays in the conversation and is not sent as `REASONING_ENCRYPTED_VALUE`. Reasoning follows `WithReasoning` in the `RAW` events of `WithRuntimeEvents` too: with it off, `reasoning.delta` and `reasoning.completed` are not forwarded and a `message.created` carries no reasoning; with it on, they carry the reasoning's text and never its opaque tokens.

### State

A `StateProvider` sends the state at the start of each run as `STATE_SNAPSHOT`. The adapter reads it again once the turn has run, before `RUN_FINISHED`, and sends what changed as a `STATE_DELTA`: an RFC 6902 JSON Patch against the state the client holds, with one `add`, `remove` or `replace` per changed value. A change a patch cannot carry is sent as a new `STATE_SNAPSHOT`: one that replaces the whole state, or sets a value to `null`, which the AG-UI Go SDK cannot encode in an operation. A state that does not change sends nothing. By default `Snapshot` is never called while the turn runs. `WithStateAfterEachTool(true)` also reads the state after each `TOOL_CALL_RESULT`, so the client sees a tool's change as it lands; the turn's next tools can be running then, so `Snapshot` must be safe to call concurrently with the tools that change the state.

### Usage

`RUN_FINISHED` carries the token usage of every model call the run made, one entry per provider and model, labelled with the provider's id and the model it served the call with. A call whose cost names no provider is labelled `unknown`. The counts follow AG-UI's accounting: `inputTokens` includes cache reads and writes, `outputTokens` includes reasoning, and `cachedInputTokens`, `cacheWriteInputTokens` and `reasoningTokens` are parts of them, set only when the provider reported them. A run that resumes a turn reports only the calls it made. A run that fails reports the calls made before the failure on `RUN_ERROR`. Usage carries no cost; read `Response.Cost()` for that.

### Messages Snapshot

`WithMessagesSnapshot(true)` ends each run with a `MESSAGES_SNAPSHOT` restating the conversation's history as the agent holds it, for a client that reconnects, or one whose copy of the history drifted from the agent's (a guardrail that replaced a streamed reply, say). AG-UI reconciles a snapshot by message id and drops the client's messages that it does not name. PromptKit's messages carry no id, so each is keyed by the thread id and its position in the history (`thread-1-0`, `thread-1-1`, ...), and a later run's snapshot replaces the earlier one's messages in place. That holds only when the thread id is the same in every run: set it with `WithThreadID` from the input's `threadId`, or each run's adapter generates its own. The messages streamed during the run had ids of their own, and the snapshot replaces them too. The history keeps no reasoning, so with `WithReasoning(true)` the snapshot restates the reasoning of the run's own turns, each as a reasoning message before the assistant message it led to, matched by its text and tool calls rather than its position, so a store that trims or summarizes the history does not move it. AG-UI reads that as the complete set of reasoning messages, so the client drops the reasoning of earlier runs. A history that reads back empty, or shorter than the run's own turn, as when the store fails, sends no snapshot rather than one that would clear the client. The snapshot is available for an `*sdk.Conversation`, which exposes its history.

`WithRuntimeEvents` forwards the conversation's [runtime events](/architecture/runtime-events/) as `RAW` events, with the `events.Event` as the payload. Name event types to forward only those, such as `events.EventToolCallStarted`; with none, every event is forwarded. By default none are. The bus delivers asynchronously, so a `RAW` event is not ordered against the run's other events, and an event the bus delivers after `RUN_FINISHED` or `RUN_ERROR` is not sent. A bus shared by several conversations carries all of their events; filter on the payload's `SessionID` or `ConversationID`.

### Frontend Tools

The tools in `RunAgentInput.tools` belong to the application: the agent proposes a call, the application executes it. AG-UI has no channel for the application to answer while a run is in progress, so a run that calls a frontend tool ends with the call unanswered (no `TOOL_CALL_RESULT`), and the application answers it in the next run's input, as a tool message per call appended to `messages`:

```json
{"id": "msg-4", "role": "tool", "toolCallId": "call-1", "content": "{\"city\":\"Paris\"}"}
```

A tool that failed is still answered, with `error` set. `EventAdapter.RunResume` takes those answers and continues the turn; the run that follows carries the model's reply, without echoing the answers back.

### Approval Holds

A tool registered with `OnToolAsync` can hold a call for approval. The run then ends with `RUN_FINISHED` carrying an `interrupt` outcome, with one interrupt per held call:

```json
{"type": "RUN_FINISHED", "threadId": "thread-1", "runId": "run-2",
 "outcome": {"type": "interrupt", "interrupts": [
   {"id": "call-2", "reason": "requires_approval", "message": "Approve the refund?", "toolCallId": "call-2"}]}}
```

The next run's input answers each interrupt in `resume`:

```json
"resume": [{"interruptId": "call-2", "status": "resolved"}]
```

Once your application approves or rejects the calls (`ResolveTool` / `RejectTool`), `EventAdapter.RunContinue` continues the turn: it reports the approved tool's result as a `TOOL_CALL_RESULT` and carries the model's reply.

### What the Input's Fields Do

The integration in the [tutorial](/sdk/tutorials/11-ag-ui-integration/) uses each `RunAgentInput` field as follows:

| Field | What the integration does with it |
|-------|------------------------------------|
| `threadId`, `runId` | Select the conversation, and label `RUN_STARTED` / `RUN_FINISHED` |
| `messages` | Only the new input is read: the user's new message, or the tool messages answering pending calls. The conversation keeps the rest of the history itself, so history a client edits or replaces is not reflected |
| `tools` | Marked as client tools when the pack's prompt declares a tool of the same name |
| `resume` | Approves or rejects the held calls before `RunContinue` |
| `context` | Not passed to the model. If the prompt template declares a variable for it, set it with `conv.SetVar` before the run |
| `state` | Not read. A `StateProvider` sends the server's state at the start of each run, and its changes as `STATE_DELTA` |
| `forwardedProps` | Left to the application: read it in the handler |

### Delivery

The adapter never drops an event. If the consumer stops reading, the adapter waits; canceling the run's context releases it.

Read `Events()` while the run is in progress, from another goroutine. Once its buffer of 64 events is full, the run waits for the reader, so code that lets `RunSend` finish before it starts reading blocks there.

### What the Adapter Does Not Do

- Stream every run. `RunSend` and `RunResume` stream an `*sdk.Conversation`'s turn. A workflow conversation's turn, a `RunContinue` run and the turn a `ToolResultProvider` resumes inside a run are emitted once they have run, each message's text as one `TEXT_MESSAGE_CONTENT`.
- Order runtime events against the run's messages, tool calls and steps. Those come from the turn itself, which keeps them ordered.
- Send `protocolVersion` on `RUN_STARTED`: the AG-UI Go SDK has no field for it.
- Send `REASONING_ENCRYPTED_VALUE`, activity or subagent events.
- Report a run's cost: usage carries token counts only.
- Keep message ids stable across runs, except in a `MESSAGES_SNAPSHOT`. The converters mint a new id each time they convert a message.
- Read the whole history from the input. A PromptKit conversation keeps its own history, so the integration in the [tutorial](/sdk/tutorials/11-ag-ui-integration/) passes only the new input to it. History a client edits or replaces is not reflected.

---

## Frontend Connectivity

AG-UI frontends connect using standard HTTP. The AG-UI client SDK, `@ag-ui/client`, provides `HttpAgent` for JavaScript and TypeScript applications. The agent holds the thread ID and the message history; `runAgent` takes the per-run fields and a subscriber, and returns a promise that settles when the run ends:

```javascript
import { HttpAgent } from "@ag-ui/client";

const agent = new HttpAgent({ url: "http://localhost:8080/ag-ui", threadId: "thread-1" });
agent.addMessage({ id: "msg-1", role: "user", content: "Hello" });

await agent.runAgent(
  { tools: [], context: [] },
  { onTextMessageEndEvent: ({ textMessageBuffer }) => console.log(textMessageBuffer) },
);
```

Any HTTP client that can consume SSE streams works with AG-UI. The protocol is not tied to a frontend framework.

---

## Next Steps

- [AG-UI Integration Reference](/sdk/reference/ag-ui/) — complete API documentation for `sdk/agui`
- [Tutorial: AG-UI Integration](/sdk/tutorials/11-ag-ui-integration/) — build an AG-UI endpoint step by step
- [A2A Concept](/concepts/a2a/) — the complementary agent-to-agent protocol
- [AG-UI Protocol Repository](https://github.com/ag-ui-protocol/ag-ui) — protocol specification and SDKs
- [AG-UI Go SDK](https://github.com/ag-ui-protocol/ag-ui/tree/main/sdks/community/go) — Go community SDK
