---
title: 'Tutorial 10: A2A server'
sidebar:
  order: 10
verified:
  commit: 8489bf3d95c7498f9de0f024769db9399c24e884
  sources:
    - runtime/a2a/client.go
    - runtime/credentials/resolver.go
    - sdk/a2a.go
    - sdk/a2a_exports.go
    - sdk/sdk.go
    - server/a2a/server.go
---

## What we will build

In this tutorial we build an A2A server that exposes a PromptKit conversation as a remotely callable agent, plus a small client that discovers it and sends it a message. The server publishes an agent card, opens one SDK conversation per A2A context with `sdk.A2AOpener`, and shuts down cleanly on Ctrl+C.

## Prerequisites

- Go 1.26 or later
- An OpenAI API key, exported as `OPENAI_API_KEY`
- [Tutorial 1](/sdk/tutorials/01-first-conversation/) completed (recommended)

## Step 1: Set up the project

We create a module and add the SDK and runtime:

```bash
mkdir a2a-agent
cd a2a-agent
go mod init a2a-agent
go get github.com/AltairaLabs/PromptKit/sdk/v2 github.com/AltairaLabs/PromptKit/runtime/v2
```

Both modules now appear in `go.mod`.

## Step 2: Create the pack

We create `assistant.pack.json`. Its system prompt has no template variables, because the A2A opener opens each conversation without setting any:

```json
{
  "id": "a2a-assistant",
  "name": "A2A Assistant",
  "version": "1.0.0",
  "template_engine": {
    "version": "v1",
    "syntax": "{{variable}}"
  },
  "prompts": {
    "chat": {
      "id": "chat",
      "name": "Chat Assistant",
      "version": "1.0.0",
      "system_template": "You are a helpful assistant. Answer questions clearly and concisely.",
      "parameters": {
        "max_tokens": 1024
      }
    }
  }
}
```

The project now holds `go.mod` and `assistant.pack.json`.

## Step 3: Write and start the server

We create `main.go`. It defines the agent card, builds an opener from the pack, and serves both on port 9999:

```go
package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"

	"github.com/AltairaLabs/PromptKit/runtime/v2/a2a"
	"github.com/AltairaLabs/PromptKit/sdk/v2"
)

func main() {
	card := a2a.AgentCard{
		Name:               "Assistant Agent",
		Description:        "A helpful assistant powered by PromptKit",
		DefaultInputModes:  []string{"text/plain"},
		DefaultOutputModes: []string{"text/plain"},
		Capabilities:       a2a.AgentCapabilities{Streaming: true},
		Skills: []a2a.AgentSkill{
			{
				ID:          "chat",
				Name:        "Chat",
				Description: "General-purpose chat assistant",
			},
		},
	}

	opener := sdk.A2AOpener("./assistant.pack.json", "chat")

	server := sdk.NewA2AServer(opener,
		sdk.WithA2ACard(&card),
		sdk.WithA2APort(9999),
	)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	go func() {
		if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Fatal(err)
		}
	}()

	fmt.Println("A2A server listening on http://localhost:9999")
	fmt.Println("Agent card: http://localhost:9999/.well-known/agent-card.json")

	<-ctx.Done()
	fmt.Println("Shutting down...")
	if err := server.Shutdown(context.Background()); err != nil {
		log.Fatal(err)
	}
}
```

The server calls the opener the first time it sees a `contextID`, then reuses that conversation for later requests with the same `contextID`.

We start it:

```bash
go run .
```

The terminal prints the `A2A server listening` and `Agent card` lines and keeps running.

## Step 4: Fetch the agent card

In a second terminal, we request the card:

```bash
curl -s http://localhost:9999/.well-known/agent-card.json
```

The output is the JSON agent card, with the name `Assistant Agent` and the `chat` skill.

## Step 5: Call the agent from a client

We create `client/main.go` in the same module. It discovers the agent, then sends one message and waits for the finished task:

```go
package main

import (
	"context"
	"fmt"
	"log"

	"github.com/AltairaLabs/PromptKit/runtime/v2/a2a"
)

func main() {
	ctx := context.Background()
	client := a2a.NewClient("http://localhost:9999")

	card, err := client.Discover(ctx)
	if err != nil {
		log.Fatalf("discover: %v", err)
	}
	fmt.Printf("Agent: %s\n", card.Name)

	text := "Hello!"
	task, err := client.SendMessage(ctx, &a2a.SendMessageRequest{
		Message: a2a.Message{
			Role:  a2a.RoleUser,
			Parts: []a2a.Part{{Text: &text}},
		},
		Configuration: &a2a.SendMessageConfiguration{Blocking: true},
	})
	if err != nil {
		log.Fatalf("send: %v", err)
	}

	for _, artifact := range task.Artifacts {
		for _, part := range artifact.Parts {
			if part.Text != nil {
				fmt.Println(*part.Text)
			}
		}
	}
}
```

We run it from the project root:

```bash
go run ./client
```

The program prints `Agent: Assistant Agent`, then the agent's reply, which is typically a short greeting. Back in the first terminal, Ctrl+C prints `Shutting down...` and the server exits.

## Next steps

- [Choose an A2A server mode](/sdk/how-to/interop/choose-a2a-server-mode/): persist tasks in your own store, mount the server on an existing HTTP server, or run it without conversations
- [Tutorial 7: A2A client](/runtime/tutorials/07-a2a-client/): the client side in depth
- [Use the A2A tool bridge](/runtime/how-to/a2a/use-a2a-tool-bridge/): register this agent as a tool in another agent
- [A2A](/concepts/a2a/): the protocol and how PromptKit implements it
- [SDK A2A server reference](/sdk/reference/a2a-server/): the complete server API
- [a2a-demo example](https://github.com/AltairaLabs/PromptKit/tree/main/server/a2a/examples/a2a-demo): a full working server and client
