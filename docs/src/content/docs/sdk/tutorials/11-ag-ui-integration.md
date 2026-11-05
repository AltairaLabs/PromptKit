---
title: 'Tutorial 11: AG-UI integration'
description: Serve a PromptKit conversation to frontend applications over the AG-UI protocol with Server-Sent Events
sidebar:
  order: 11
verified:
  commit: 885cafa787a84bfba66f158fe51cda15a077ec23
  sources:
    - runtime/pipeline/stage/composition_executor.go
    - sdk/agui/adapter.go
    - sdk/agui/convert.go
    - sdk/conversation_tools.go
    - sdk/inference_integration_test.go
---

## What we will build

In this tutorial we build an HTTP endpoint that accepts AG-UI `RunAgentInput` requests and streams AG-UI events over Server-Sent Events (SSE), powered by a PromptKit SDK conversation. We keep one conversation per AG-UI thread, run each turn with the `EventAdapter`, mark the application's tools as frontend tools, continue turns held for tool approval, send the server's state with each run, and call the endpoint from a JavaScript client.

## Prerequisites

- Go 1.26 or later
- An OpenAI API key, exported as `OPENAI_API_KEY`
- Node.js 18 or later, for Step 8
- [Tutorial 1](/sdk/tutorials/01-first-conversation/) completed (recommended)

## Step 1: Set up the project

We create a module and add the SDK and the AG-UI Go community SDK, which `sdk/agui` uses to target AG-UI 1.0:

```bash
mkdir agui-server
cd agui-server
go mod init agui-server
go get github.com/AltairaLabs/PromptKit/sdk/v2 github.com/ag-ui-protocol/ag-ui/sdks/community/go
```

We create `support.pack.json`. Its system prompt has no template variables, because the handler never sets any:

```json
{
  "id": "support-agent",
  "name": "Support Agent",
  "version": "1.0.0",
  "template_engine": {
    "version": "v1",
    "syntax": "{{variable}}"
  },
  "prompts": {
    "chat": {
      "id": "chat",
      "name": "Support Assistant",
      "version": "1.0.0",
      "system_template": "You are a friendly support assistant. Keep answers short.",
      "parameters": {
        "max_tokens": 1024
      }
    }
  }
}
```

Both modules now appear in `go.mod`, next to the pack file.

## Step 2: Keep a conversation per thread

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

The same thread ID now returns the same conversation, which keeps the thread's history itself. Cleanup (timeouts, an explicit close endpoint) is up to the application. [What the input's fields do](/concepts/ag-ui/#what-the-inputs-fields-do) covers what this means for `messages`.

## Step 3: Offer the frontend tools

`RunAgentInput.tools` lists the application's own tools. The application executes them, not the server, so a call to one must suspend the turn rather than run here. `ToolsFromAGUI` returns descriptors marked as client tools; we copy that onto the conversation's tools of the same name:

```go
func bindFrontendTools(conv *sdk.Conversation, tools []aguiTypes.Tool) {
	for _, tool := range agui.ToolsFromAGUI(tools) {
		if desc, err := conv.ToolRegistry().GetTool(tool.Name); err == nil {
			desc.Mode = tool.Mode
		}
	}
}
```

The model is offered only the tools the pack's prompt declares, so a frontend tool must be declared there too (its name, description and parameters). This tutorial's pack declares none, so the function marks a tool only once you add one to the pack.

## Step 4: Choose the run

Each request starts one of three kinds of run:

- The input carries `resume` entries: the previous run was held for tool approval (see [Approval holds](/concepts/ag-ui/#approval-holds)). We approve or reject each held call, then `RunContinue` continues the turn. This is checked first, because the input can also end with tool messages, the results of server tools that ran before the hold.
- The input ends with tool messages: they answer the frontend tool calls the previous run left pending. `ToolResultsFromAGUI` extracts them and `RunResume` hands them to the conversation and continues the turn. The input repeats the results of any server tools that ran in the same round; the conversation already holds those and ignores them.
- Otherwise the last message is the user's new message, and `RunSend` sends it.

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

Here a `resolved` entry approves the call and a `cancelled` one rejects it. `applyResume` runs before the response starts, so a failure can still be reported as an HTTP error. Every request now maps to exactly one of the three runs.

## Step 5: Stream SSE events

We create the adapter with the input's thread and run IDs, start the run in a goroutine, and write events as they arrive:

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

The run must be in its own goroutine: the adapter's channel holds 64 events, and once it is full the run waits for the loop to read. The `Events()` channel closes when the run ends, so the `range` loop exits on its own.

## Step 6: Run the server

We put the pieces together in `main.go`:

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

We run it:

```bash
go run .
```

The terminal prints `AG-UI server listening on http://localhost:8080`. In a second terminal, we send a run with curl:

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

The response is an SSE stream that begins with `RUN_STARTED`, carries the assistant's message as `TEXT_MESSAGE_START`, one `TEXT_MESSAGE_CONTENT` with the whole reply, and `TEXT_MESSAGE_END`, and ends with `RUN_FINISHED`. The reply text is typically a short greeting.

## Step 7: Send the server's state

A `StateProvider` gives the frontend the server's state at the start of each run. We add one to `main.go` that reports the thread ID:

```go
type threadState struct{ threadID string }

func (s threadState) Snapshot(agui.Sender) (any, error) {
	return map[string]string{"threadId": s.threadID}, nil
}
```

and pass it to the adapter in `handleAGUI`:

```go
	adapter := agui.NewEventAdapter(conv,
		agui.WithThreadID(input.ThreadID),
		agui.WithRunID(input.RunID),
		agui.WithStateProvider(threadState{threadID: input.ThreadID}),
	)
```

We restart the server and repeat the curl request. A `STATE_SNAPSHOT` event now follows `RUN_STARTED`:

```text
data: {"type":"STATE_SNAPSHOT","timestamp":...,"snapshot":{"threadId":"thread-1"}}
```

## Step 8: Connect a frontend

In a new directory, we install the AG-UI client SDK:

```bash
mkdir agui-client
cd agui-client
npm install @ag-ui/client
```

We create `client.mjs`. The agent carries the thread ID and the message history; `runAgent` takes the per-run fields and a subscriber, and returns a promise that settles when the run ends:

```javascript
import { HttpAgent } from "@ag-ui/client";

const agent = new HttpAgent({
  url: "http://localhost:8080/ag-ui",
  threadId: "thread-1",
});

agent.addMessage({ id: "msg-1", role: "user", content: "Hello!" });

await agent.runAgent(
  { tools: [], context: [] },
  {
    onTextMessageEndEvent({ textMessageBuffer }) {
      console.log(textMessageBuffer);
    },
    onRunFinishedEvent() {
      console.log("Done.");
    },
  },
);
```

With the server still running, we run it:

```bash
node client.mjs
```

The script prints the assistant's reply, typically a short greeting, then `Done.`

## Next steps

- [AG-UI](/concepts/ag-ui/): what a run emits, frontend tool calls, approval holds, workflow steps, and what the input's fields do
- [AG-UI reference](/sdk/reference/ag-ui/): the complete `sdk/agui` API
- [Tutorial 10: A2A server](/sdk/tutorials/10-a2a-server/): expose the agent over A2A instead
- [AG-UI protocol repository](https://github.com/ag-ui-protocol/ag-ui): the protocol specification and SDKs
- [AG-UI Go SDK](https://github.com/ag-ui-protocol/ag-ui/tree/main/sdks/community/go): the community Go SDK
