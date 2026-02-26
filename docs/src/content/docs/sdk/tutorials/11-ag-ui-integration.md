---
title: 'Tutorial: AG-UI Integration'
sidebar:
  order: 11
---

Serve a PromptKit agent to frontend applications using the AG-UI protocol.

**Time**: 20 minutes
**Level**: Intermediate

## What You'll Build

An HTTP endpoint that accepts AG-UI `RunAgentInput` requests and streams AG-UI events via Server-Sent Events (SSE), powered by a PromptKit SDK conversation.

## What You'll Learn

- Convert between AG-UI and PromptKit message formats
- Use the `EventAdapter` to emit AG-UI events from a conversation
- Write SSE events using the AG-UI SDK's encoder
- Manage conversation sessions across requests

## Prerequisites

- Go 1.22+
- A compiled pack file (`.pack.json`)
- Completed [First Conversation tutorial](/sdk/tutorials/01-first-conversation/) (recommended)
- Familiarity with the [AG-UI concept](/concepts/ag-ui/)

---

## Step 1: Add the AG-UI Dependency

The `sdk/agui` package depends on the AG-UI Go community SDK. Add it to your module:

```bash
go get github.com/ag-ui-protocol/ag-ui/sdks/community/go
```

---

## Step 2: Set Up the HTTP Server

Start with a basic HTTP server that exposes a single endpoint:

```go
package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"

	aguiTypes "github.com/ag-ui-protocol/ag-ui/sdks/community/go/pkg/core/types"
	"github.com/ag-ui-protocol/ag-ui/sdks/community/go/pkg/encoding/sse"

	"github.com/AltairaLabs/PromptKit/sdk"
	"github.com/AltairaLabs/PromptKit/sdk/agui"
)

func main() {
	http.HandleFunc("/ag-ui", handleAGUI)

	fmt.Println("AG-UI server listening on http://localhost:8080")
	log.Fatal(http.ListenAndServe(":8080", nil))
}
```

The `/ag-ui` endpoint will accept POST requests with a `RunAgentInput` body and respond with an SSE stream.

---

## Step 3: Decode the Request

Parse the incoming `RunAgentInput` and convert the last message to PromptKit format:

```go
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

	// Convert the latest AG-UI message to PromptKit format.
	lastMsg := input.Messages[len(input.Messages)-1]
	msg := agui.MessageFromAGUI(&lastMsg)
```

The `MessageFromAGUI` converter handles role mapping and content format translation.

---

## Step 4: Open a Conversation

Create a PromptKit conversation from your pack file:

```go
	conv, err := sdk.Open("./support.pack.json", "chat")
	if err != nil {
		http.Error(w, "failed to open conversation", http.StatusInternalServerError)
		return
	}
	defer conv.Close()
```

In a production application, you would maintain a map of conversations keyed by `input.ThreadID` so that multiple requests in the same thread share conversation history. See [Session Management](#session-management) below for that pattern.

---

## Step 5: Create the EventAdapter

The `EventAdapter` bridges the PromptKit conversation to AG-UI events:

```go
	adapter := agui.NewEventAdapter(conv,
		agui.WithThreadID(input.ThreadID),
		agui.WithRunID(input.RunID),
	)
```

The adapter options set the thread and run IDs that appear in lifecycle events (`RUN_STARTED`, `RUN_FINISHED`).

---

## Step 6: Stream SSE Events

Set the SSE headers, start the conversation turn in a goroutine, and write events as they arrive:

```go
	// Set SSE headers.
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	encoder := sse.NewSSEWriter()

	// Run the conversation turn in the background.
	go adapter.RunSend(r.Context(), &msg)

	// Stream events to the client.
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
```

The `Events()` channel closes automatically when the run completes or encounters an error, so the `range` loop exits cleanly.

---

## Complete Example

Here is the full server in one file:

```go
package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"

	aguiTypes "github.com/ag-ui-protocol/ag-ui/sdks/community/go/pkg/core/types"
	"github.com/ag-ui-protocol/ag-ui/sdks/community/go/pkg/encoding/sse"

	"github.com/AltairaLabs/PromptKit/sdk"
	"github.com/AltairaLabs/PromptKit/sdk/agui"
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

	// Convert the latest AG-UI message to PromptKit format.
	lastMsg := input.Messages[len(input.Messages)-1]
	msg := agui.MessageFromAGUI(&lastMsg)

	// Open a PromptKit conversation.
	conv, err := sdk.Open("./support.pack.json", "chat")
	if err != nil {
		http.Error(w, "failed to open conversation", http.StatusInternalServerError)
		return
	}
	defer conv.Close()

	// Create the AG-UI event adapter.
	adapter := agui.NewEventAdapter(conv,
		agui.WithThreadID(input.ThreadID),
		agui.WithRunID(input.RunID),
	)

	// Set SSE headers.
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	encoder := sse.NewSSEWriter()

	// Run the conversation turn in the background.
	go adapter.RunSend(r.Context(), &msg)

	// Stream events to the client.
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
    "messages": [{"id": "msg-1", "role": "human", "content": "Hello!"}],
    "tools": [],
    "context": []
  }'
```

---

## Session Management

The basic example creates a new conversation per request. For multi-turn conversations, maintain a session map:

```go
import "sync"

var (
	sessions   = make(map[string]*sdk.Conversation)
	sessionsMu sync.RWMutex
)

func getOrCreateConversation(threadID string) (*sdk.Conversation, error) {
	sessionsMu.RLock()
	conv, ok := sessions[threadID]
	sessionsMu.RUnlock()
	if ok {
		return conv, nil
	}

	sessionsMu.Lock()
	defer sessionsMu.Unlock()

	// Double-check after acquiring write lock.
	if conv, ok = sessions[threadID]; ok {
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

Then in the handler, replace `sdk.Open(...)` with:

```go
conv, err := getOrCreateConversation(input.ThreadID)
if err != nil {
    http.Error(w, "failed to open conversation", http.StatusInternalServerError)
    return
}
// Note: don't defer conv.Close() here — the session owns the lifetime.
```

Add cleanup logic (timeouts, explicit close endpoint) appropriate to your application.

---

## Adding Workflow Steps

If your pack uses workflows, pass the step names to the adapter to emit `STEP_STARTED` and `STEP_FINISHED` events:

```go
adapter := agui.NewEventAdapter(conv,
    agui.WithThreadID(input.ThreadID),
    agui.WithRunID(input.RunID),
    agui.WithWorkflowSteps(true),
)
```

The adapter subscribes to the PromptKit event bus and emits step events automatically as the workflow engine transitions between steps.

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
  messages: [{ id: "msg-1", role: "human", content: "Hello!" }],
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

- How to decode AG-UI requests and convert messages with `MessageFromAGUI`
- How to use `EventAdapter` to bridge PromptKit conversations to AG-UI events
- How to stream SSE events using the AG-UI SDK's writer
- How to manage conversation sessions across requests
- How to enable workflow steps and state synchronization

## Next Steps

- [AG-UI Concept](/concepts/ag-ui/) — understand the protocol design
- [AG-UI Reference](/sdk/reference/ag-ui/) — complete API documentation
- [A2A Server Tutorial](/sdk/tutorials/10-a2a-server/) — expose your agent via A2A instead

## See Also

- [AG-UI Protocol Repository](https://github.com/ag-ui-protocol/ag-ui) — protocol specification and SDKs
- [AG-UI Go SDK](https://github.com/ag-ui-protocol/ag-ui/tree/main/sdks/community/go) — community Go SDK
