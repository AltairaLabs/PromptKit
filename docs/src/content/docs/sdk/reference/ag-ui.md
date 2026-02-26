---
title: AG-UI Integration Reference
description: API reference for the AG-UI protocol bridge in sdk/agui
sidebar:
  order: 8
---

API reference for the `sdk/agui` package, which provides converters and an event adapter for bridging PromptKit conversations to the [AG-UI protocol](https://github.com/ag-ui-protocol/ag-ui).

---

## Package

```go
import "github.com/AltairaLabs/PromptKit/sdk/agui"
```

**Dependency:** This package requires the AG-UI Go community SDK:

```
github.com/ag-ui-protocol/ag-ui/sdks/community/go
```

---

## Message Converters

Functions for converting between PromptKit `types.Message` and AG-UI `types.Message`.

### MessageToAGUI

```go
func MessageToAGUI(msg *types.Message) aguitypes.Message
```

Converts a PromptKit message to an AG-UI message. Maps roles, content (text or multimodal), tool calls, and tool results.

### MessageFromAGUI

```go
func MessageFromAGUI(msg *aguitypes.Message) types.Message
```

Converts an AG-UI message to a PromptKit message. The inverse of `MessageToAGUI`.

### MessagesToAGUI

```go
func MessagesToAGUI(msgs []types.Message) []aguitypes.Message
```

Batch converts a slice of PromptKit messages to AG-UI messages.

### MessagesFromAGUI

```go
func MessagesFromAGUI(msgs []aguitypes.Message) []types.Message
```

Batch converts a slice of AG-UI messages to PromptKit messages.

---

## Tool Converters

### ToolsToAGUI

```go
func ToolsToAGUI(descs []tools.ToolDescriptor) []aguitypes.Tool
```

Converts PromptKit tool descriptors to AG-UI tool definitions. Use this to expose PromptKit tools to AG-UI frontends.

### ToolsFromAGUI

```go
func ToolsFromAGUI(aguiTools []aguitypes.Tool) []*tools.ToolDescriptor
```

Converts AG-UI tool definitions to PromptKit tool descriptors. Use this to pass frontend-defined tools into a PromptKit conversation.

---

## EventAdapter

The `EventAdapter` observes a PromptKit conversation and emits AG-UI events on a channel. It handles the translation from PromptKit's internal event model to the AG-UI SSE event stream.

### NewEventAdapter

```go
func NewEventAdapter(conv interface {
    Sender
    EventBusProvider
}, opts ...AdapterOption) *EventAdapter
```

Creates a new event adapter bound to a conversation. In practice, `*sdk.Conversation` satisfies both `Sender` and `EventBusProvider`. The adapter does not start emitting events until `RunSend` is called.

**Parameters:**

| Parameter | Description |
|-----------|-------------|
| `conv` | The PromptKit conversation (must implement `Sender` and `EventBusProvider`) |
| `opts` | Functional options (see below) |

### Events

```go
func (a *EventAdapter) Events() <-chan aguievents.Event
```

Returns a read-only channel of AG-UI events. The channel is closed when the run completes (either successfully or with an error). Consumers should range over this channel to receive all events.

### RunSend

```go
func (a *EventAdapter) RunSend(ctx context.Context, msg *types.Message) error
```

Executes a conversation turn with the given message and emits AG-UI events as the conversation progresses. This method blocks until the turn completes. Typically called in a goroutine so the caller can simultaneously read from `Events()`.

The event sequence for a successful run:

1. `RunStartedEvent`
2. `TextMessageStartEvent` (per assistant message)
3. `TextMessageContentEvent` (per text chunk)
4. `TextMessageEndEvent`
5. `RunFinishedEvent`

If the agent invokes tools, tool call events are interleaved:

1. `ToolCallStartEvent`
2. `ToolCallArgsEvent` (streamed argument chunks)
3. `ToolCallEndEvent`
4. `ToolCallResultEvent`

If an error occurs at any point, a `RunErrorEvent` is emitted and the channel is closed.

---

## EventAdapter Options

| Option | Signature | Description |
|--------|-----------|-------------|
| `WithThreadID` | `WithThreadID(id string) AdapterOption` | Sets the thread ID included in lifecycle events. |
| `WithRunID` | `WithRunID(id string) AdapterOption` | Sets the run ID included in lifecycle events. |
| `WithStateProvider` | `WithStateProvider(sp StateProvider) AdapterOption` | Attaches a state provider for emitting `STATE_SNAPSHOT` events at run start. |
| `WithWorkflowSteps` | `WithWorkflowSteps(enabled bool) AdapterOption` | Enables emission of `STEP_STARTED`/`STEP_FINISHED` events for workflow state transitions observed on the event bus. |

---

## StateProvider

```go
type StateProvider interface {
    Snapshot(sender Sender) (any, error)
}
```

Interface for providing state snapshots to the frontend. Called at the start of each run to emit a `STATE_SNAPSHOT` event.

| Method | Description |
|--------|-------------|
| `Snapshot(sender)` | Returns the current state snapshot. The `Sender` is provided so the state provider can query the conversation if needed. |

---

## Event Mapping Reference

Complete mapping from PromptKit conversation activity to AG-UI events:

| PromptKit Activity | AG-UI Event | Fields |
|--------------------|-------------|--------|
| Send starts | `RunStartedEvent` | `threadId`, `runId` |
| Text response begins | `TextMessageStartEvent` | `messageId`, `role` |
| Text token streamed | `TextMessageContentEvent` | `messageId`, `delta` |
| Text response ends | `TextMessageEndEvent` | `messageId` |
| Tool call initiated | `ToolCallStartEvent` | `toolCallId`, `toolCallName` |
| Tool arguments streamed | `ToolCallArgsEvent` | `toolCallId`, `delta` |
| Tool call complete | `ToolCallEndEvent` | `toolCallId` |
| Tool result returned | `ToolCallResultEvent` | `toolCallId`, `result` |
| Workflow step begins | `StepStartedEvent` | `stepName` |
| Workflow step ends | `StepFinishedEvent` | `stepName` |
| Send completes | `RunFinishedEvent` | `threadId`, `runId` |
| Error occurs | `RunErrorEvent` | `message` |

---

## See Also

- [AG-UI Concept](/concepts/ag-ui/) — protocol overview and design rationale
- [Tutorial: AG-UI Integration](/sdk/tutorials/11-ag-ui-integration/) — build an AG-UI endpoint step by step
- [AG-UI Protocol Repository](https://github.com/ag-ui-protocol/ag-ui) — protocol specification
- [AG-UI Go SDK](https://github.com/ag-ui-protocol/ag-ui/tree/main/sdks/community/go) — community Go SDK
