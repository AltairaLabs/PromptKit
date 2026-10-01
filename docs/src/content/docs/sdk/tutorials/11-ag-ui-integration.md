---
title: 'Tutorial 11: AG-UI Integration'
sidebar:
  order: 11
---

Serve a PromptKit agent to frontend applications using the AG-UI protocol.

**Time**: 20 minutes
**Level**: Intermediate

## What You'll Build

An HTTP endpoint that accepts AG-UI `RunAgentInput` requests and streams AG-UI events via Server-Sent Events (SSE), powered by a PromptKit SDK conversation.

## What You'll Learn

- Keep one PromptKit conversation per AG-UI thread
- Use the `EventAdapter` to run a turn and emit its AG-UI events
- Let the model call frontend tools, and continue once the application answers them
- Continue a turn held for tool approval
- Write SSE events using the AG-UI SDK's encoder

## Prerequisites

- Go 1.22+
- A compiled pack file (`.pack.json`)
- Completed [First Conversation tutorial](/sdk/tutorials/01-first-conversation/) (recommended)
- Familiarity with the [AG-UI concept](/concepts/ag-ui/)

---

## Step 1: Add the AG-UI Dependency

The `sdk/agui` package targets AG-UI 1.0 through the AG-UI Go community SDK. Add it to your module:

```bash
go get github.com/ag-ui-protocol/ag-ui/sdks/community/go
```

---

## Step 2: Keep a Conversation per Thread

An AG-UI thread spans many runs: a run that calls a frontend tool ends with the call unanswered, and the next run carries the answer. A PromptKit conversation holds the turn that is waiting for it, so every run on a thread must reach the same conversation:

```go
var (
	sessions   = make(map[string]*sdk.Conversation)
	sessionsMu sync.Mutex
)

func conversationFor(threadID string) (*sdk.Conversation, error) {
	sessionsMu.Lock()
	defer sessionsMu.Unlock()

	if conv, ok := sessions[threadID]; ok {
		return conv, nil
	}
	conv, err := sdk.Open("./support.pack.json", "chat")
	if err != nil {
		return nil, err
	}
	sessions[threadID] = conv
	return conv, nil
}
```

The conversation keeps the thread's history itself. Add cleanup (timeouts, an explicit close endpoint) that suits your application, and see [What the Input's Fields Do](#what-the-inputs-fields-do) for what that means for `messages`.

---

## Step 3: Offer the Frontend Tools

`RunAgentInput.tools` lists the application's own tools. The application executes them, not the server, so a call to one must suspend the turn rather than run here. `ToolsFromAGUI` returns descriptors marked as client tools; copy that onto the conversation's tools of the same name:

```go
func bindFrontendTools(conv *sdk.Conversation, tools []aguiTypes.Tool) {
	for _, tool := range agui.ToolsFromAGUI(tools) {
		if desc, err := conv.ToolRegistry().GetTool(tool.Name); err == nil {
			desc.Mode = tool.Mode
		}
	}
}
```

The model is offered only the tools your pack's prompt declares, so declare each frontend tool there too (its name, description and parameters). A frontend tool the pack does not declare is never offered to the model.

---

## Step 4: Choose the Run

Each request starts one of three kinds of run:

- **The input carries `resume` entries.** The previous run was held for tool approval (see [Approval Holds](#approval-holds)). Approve or reject each held call, then `RunContinue` continues the turn. Check this first: the input can also end with tool messages, the results of server tools that ran before the hold.
- **The input ends with tool messages.** They answer the frontend tool calls the previous run left pending. `ToolResultsFromAGUI` extracts them and `RunResume` hands them to the conversation and continues the turn. The input repeats the results of any server tools that ran in the same round; the conversation already holds those and ignores them.
- **Otherwise** the last message is the user's new message, and `RunSend` sends it.

```go
func applyResume(ctx context.Context, conv *sdk.Conversation, entries []aguiTypes.ResumeEntry) error {
	for _, entry := range entries {
		var err error
		if entry.Status == aguiTypes.ResumeStatusResolved {
			_, err = conv.ResolveTool(ctx, entry.InterruptID)
		} else {
			_, err = conv.RejectTool(ctx, entry.InterruptID, "declined by the user")
		}
		if err != nil {
			return err
		}
	}
	return nil
}

func runFor(adapter *agui.EventAdapter, input *aguiTypes.RunAgentInput) func(context.Context) error {
	if len(input.Resume) > 0 {
		return adapter.RunContinue
	}

	if results := agui.ToolResultsFromAGUI(input.Messages); len(results) > 0 {
		return func(ctx context.Context) error {
			return adapter.RunResume(ctx, results)
		}
	}

	msg := agui.MessageFromAGUI(&input.Messages[len(input.Messages)-1])
	return func(ctx context.Context) error {
		return adapter.RunSend(ctx, &msg)
	}
}
```

What a resolved entry means is your application's decision; here a `resolved` entry approves the call and a `cancelled` one rejects it. `applyResume` runs before the response starts, so a failure can still be reported as an HTTP error.

---

## Step 5: Stream SSE Events

Create the adapter with the input's thread and run IDs, start the run in a goroutine, and write events as they arrive:

```go
	adapter := agui.NewEventAdapter(conv,
		agui.WithThreadID(input.ThreadID),
		agui.WithRunID(input.RunID),
	)
	run := runFor(adapter, &input)

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	encoder := sse.NewSSEWriter()

	go func() {
		if err := run(r.Context()); err != nil {
			log.Printf("run failed: %v", err)
		}
	}()

	for event := range adapter.Events() {
		if err := encoder.WriteEvent(r.Context(), w, event); err != nil {
			log.Printf("SSE write error: %v", err)
			return
		}
		if f, ok := w.(http.Flusher); ok {
			f.Flush()
		}
	}
```

The run must be in its own goroutine, as here: the adapter's channel holds 64 events, and once it is full the run waits for the loop to read. Code that runs `RunSend` to completion before reading `Events()` blocks there; earlier versions dropped the events that did not fit instead.

The `Events()` channel closes when the run ends, so the `range` loop exits cleanly. The adapter never drops an event: while the client is slow to read, the run waits. If the client disconnects, the request context is canceled, which releases the run.

---

## Complete Example

Here is the full server in one file:

```go
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"sync"

	aguiTypes "github.com/ag-ui-protocol/ag-ui/sdks/community/go/pkg/core/types"
	"github.com/ag-ui-protocol/ag-ui/sdks/community/go/pkg/encoding/sse"

	"github.com/AltairaLabs/PromptKit/sdk/v2"
	"github.com/AltairaLabs/PromptKit/sdk/v2/agui"
)

var (
	sessions   = make(map[string]*sdk.Conversation)
	sessionsMu sync.Mutex
)

func main() {
	http.HandleFunc("/ag-ui", handleAGUI)

	fmt.Println("AG-UI server listening on http://localhost:8080")
	log.Fatal(http.ListenAndServe(":8080", nil))
}

func handleAGUI(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var input aguiTypes.RunAgentInput
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}
	if len(input.Messages) == 0 {
		http.Error(w, "no messages provided", http.StatusBadRequest)
		return
	}

	conv, err := conversationFor(input.ThreadID)
	if err != nil {
		http.Error(w, "failed to open conversation", http.StatusInternalServerError)
		return
	}
	bindFrontendTools(conv, input.Tools)
	if err := applyResume(r.Context(), conv, input.Resume); err != nil {
		http.Error(w, "cannot resume: "+err.Error(), http.StatusConflict)
		return
	}

	adapter := agui.NewEventAdapter(conv,
		agui.WithThreadID(input.ThreadID),
		agui.WithRunID(input.RunID),
	)
	run := runFor(adapter, &input)

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	encoder := sse.NewSSEWriter()

	go func() {
		if err := run(r.Context()); err != nil {
			log.Printf("run failed: %v", err)
		}
	}()

	for event := range adapter.Events() {
		if err := encoder.WriteEvent(r.Context(), w, event); err != nil {
			log.Printf("SSE write error: %v", err)
			return
		}
		if f, ok := w.(http.Flusher); ok {
			f.Flush()
		}
	}
}

func conversationFor(threadID string) (*sdk.Conversation, error) {
	sessionsMu.Lock()
	defer sessionsMu.Unlock()

	if conv, ok := sessions[threadID]; ok {
		return conv, nil
	}
	conv, err := sdk.Open("./support.pack.json", "chat")
	if err != nil {
		return nil, err
	}
	sessions[threadID] = conv
	return conv, nil
}

func bindFrontendTools(conv *sdk.Conversation, tools []aguiTypes.Tool) {
	for _, tool := range agui.ToolsFromAGUI(tools) {
		if desc, err := conv.ToolRegistry().GetTool(tool.Name); err == nil {
			desc.Mode = tool.Mode
		}
	}
}

func applyResume(ctx context.Context, conv *sdk.Conversation, entries []aguiTypes.ResumeEntry) error {
	for _, entry := range entries {
		var err error
		if entry.Status == aguiTypes.ResumeStatusResolved {
			_, err = conv.ResolveTool(ctx, entry.InterruptID)
		} else {
			_, err = conv.RejectTool(ctx, entry.InterruptID, "declined by the user")
		}
		if err != nil {
			return err
		}
	}
	return nil
}

func runFor(adapter *agui.EventAdapter, input *aguiTypes.RunAgentInput) func(context.Context) error {
	if len(input.Resume) > 0 {
		return adapter.RunContinue
	}

	if results := agui.ToolResultsFromAGUI(input.Messages); len(results) > 0 {
		return func(ctx context.Context) error {
			return adapter.RunResume(ctx, results)
		}
	}

	msg := agui.MessageFromAGUI(&input.Messages[len(input.Messages)-1])
	return func(ctx context.Context) error {
		return adapter.RunSend(ctx, &msg)
	}
}
```

Run it:

```bash
go run main.go
```

Test with curl:

```bash
curl -X POST http://localhost:8080/ag-ui \
  -H "Content-Type: application/json" \
  -d '{
    "threadId": "thread-1",
    "runId": "run-1",
    "messages": [{"id": "msg-1", "role": "user", "content": "Hello!"}],
    "tools": [],
    "context": []
  }'
```

---

## What a Run Emits

A turn can take several model calls: a model that calls a tool and then answers produces two assistant messages. The run carries every message the turn produced, in order:

```
RUN_STARTED
TEXT_MESSAGE_START / TEXT_MESSAGE_CONTENT / TEXT_MESSAGE_END    "Let me look that up."
TOOL_CALL_START / TOOL_CALL_ARGS / TOOL_CALL_END                lookup_order
TOOL_CALL_RESULT                                                what lookup_order returned
TEXT_MESSAGE_START / TEXT_MESSAGE_CONTENT / TEXT_MESSAGE_END    "Your order shipped yesterday."
RUN_FINISHED
```

The events are emitted once the turn has run. Each message's text arrives in one `TEXT_MESSAGE_CONTENT`; it is not streamed token by token.

### Frontend Tool Calls

When the model calls a frontend tool, the run emits the call (`TOOL_CALL_START`, `TOOL_CALL_ARGS`, `TOOL_CALL_END`) and finishes with it unanswered: AG-UI forbids the server from answering a frontend tool. The application executes it, then starts the next run with a tool message per call appended to `messages`:

```json
{"id": "msg-4", "role": "tool", "toolCallId": "call-1", "content": "{\"city\":\"Paris\"}"}
```

A tool that failed is still answered, with `error` set. `RunResume` hands the answers to the conversation; the run that follows carries the model's reply, without echoing the answers back.

### Approval Holds

A tool registered with `conv.OnToolAsync` can hold a call until someone approves it. The run then ends with `RUN_FINISHED` carrying an `interrupt` outcome, with one interrupt per held call:

```json
{"type": "RUN_FINISHED", "threadId": "thread-1", "runId": "run-2",
 "outcome": {"type": "interrupt", "interrupts": [
   {"id": "call-2", "reason": "requires_approval", "message": "Approve the refund?", "toolCallId": "call-2"}]}}
```

The next run's input answers each interrupt in `resume`:

```json
"resume": [{"interruptId": "call-2", "status": "resolved"}]
```

`conv.ResolveTool` runs the approved tool (`conv.RejectTool` declines it), and `RunContinue` continues the turn: it reports the tool's result as a `TOOL_CALL_RESULT` and carries the model's reply.

---

## What the Input's Fields Do

| Field | What this integration does with it |
|-------|------------------------------------|
| `threadId`, `runId` | Select the conversation, and label `RUN_STARTED` / `RUN_FINISHED` |
| `messages` | Only the new input is read: the user's new message, or the tool messages answering pending calls. The conversation keeps the rest of the history itself, so history a client edits or replaces is not reflected |
| `tools` | Marked as client tools when the pack's prompt declares a tool of the same name |
| `resume` | Approves or rejects the held calls before `RunContinue` |
| `context` | Not passed to the model. If your prompt template declares a variable for it, set it with `conv.SetVar` before the run |
| `state` | Not read. A `StateProvider` (below) sends the server's state at the start of each run |
| `forwardedProps` | Yours: read it in the handler |

---

## Adding Workflow Steps

For a pack with a workflow, open it with `sdk.OpenWorkflow` and create the adapter with `NewWorkflowEventAdapter`:

```go
wc, err := sdk.OpenWorkflow("./support.pack.json")
if err != nil {
    return err
}

adapter := agui.NewWorkflowEventAdapter(wc,
    agui.WithThreadID(input.ThreadID),
    agui.WithRunID(input.RunID),
)
```

Each run opens a step named after the workflow state it starts in, and closes it before `RUN_FINISHED`. When the turn moves the workflow to another state, the run finishes that step and starts one for the new state. Pass `agui.WithWorkflowSteps(false)` to leave the steps out.

---

## Adding State Synchronization

To push application state to the frontend, implement the `StateProvider` interface and attach it to the adapter:

```go
adapter := agui.NewEventAdapter(conv,
    agui.WithThreadID(input.ThreadID),
    agui.WithRunID(input.RunID),
    agui.WithStateProvider(myStateProvider),
)
```

The adapter calls `Snapshot()` at the start of each run and emits a `STATE_SNAPSHOT` event. See the [AG-UI Reference](/sdk/reference/ag-ui/) for the `StateProvider` interface.

---

## Frontend Connection

On the frontend, use the AG-UI client SDK to connect:

```typescript
import { HttpAgent } from "@ag-ui/client";

const agent = new HttpAgent({
  url: "http://localhost:8080/ag-ui",
});

const run = agent.runAgent({
  threadId: "thread-1",
  runId: crypto.randomUUID(),
  messages: [{ id: "msg-1", role: "user", content: "Hello!" }],
  tools: [],
  context: [],
});

run.on("TEXT_MESSAGE_CONTENT", (event) => {
  process.stdout.write(event.delta);
});

run.on("RUN_FINISHED", () => {
  console.log("\nDone.");
});
```

The `@ag-ui/client` package handles SSE parsing, reconnection, and event typing. Any AG-UI-compatible frontend framework (CopilotKit, custom React apps, etc.) can connect to your endpoint.

---

## What You've Learned

- How to keep one conversation per AG-UI thread
- How to run a turn with `RunSend`, and continue it with `RunResume` or `RunContinue`
- How frontend tool calls and approval holds cross the run boundary
- How to stream SSE events using the AG-UI SDK's writer
- How to enable workflow steps and state synchronization

## Next Steps

- [AG-UI Concept](/concepts/ag-ui/) — understand the protocol design
- [AG-UI Reference](/sdk/reference/ag-ui/) — complete API documentation
- [A2A Server Tutorial](/sdk/tutorials/10-a2a-server/) — expose your agent via A2A instead

## See Also

- [AG-UI Protocol Repository](https://github.com/ag-ui-protocol/ag-ui) — protocol specification and SDKs
- [AG-UI Go SDK](https://github.com/ag-ui-protocol/ag-ui/tree/main/sdks/community/go) — community Go SDK
