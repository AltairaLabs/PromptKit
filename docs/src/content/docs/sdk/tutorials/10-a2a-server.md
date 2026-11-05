---
title: 'Tutorial 10: A2A Server'
sidebar:
  order: 10
verified:
  commit: 885cafa787a84bfba66f158fe51cda15a077ec23
  sources:
    - runtime/a2a/client.go
    - sdk/a2a.go
    - sdk/a2a_exports.go
    - server/a2a/handler.go
    - server/a2a/server.go
---

In this tutorial we expose a PromptKit conversation as an A2A-compliant agent server.

**Time**: 15 minutes
**Level**: Intermediate

## What we build

An A2A server that exposes a PromptKit SDK conversation as a remotely callable agent, discoverable through its agent card. We create the server with `sdk.NewA2AServer`, bridge SDK conversations to it with `sdk.A2AOpener`, and start and gracefully shut it down.

## Prerequisites

- Go 1.26 or later
- A compiled pack file (`.pack.json`)
- Completed [First Conversation tutorial](/sdk/tutorials/01-first-conversation/) (recommended)

---

## Step 1: Define the agent card

The agent card describes the agent's identity and capabilities:

```go
package main

import (
    "context"
    "fmt"
    "log"
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
```

The card is only a value so far; Step 3 serves it.

---

## Step 2: Create the conversation opener

We use `sdk.A2AOpener` to create conversations from a pack file:

```go
    opener := sdk.A2AOpener("./assistant.pack.json", "chat")
```

The server calls the opener the first time it sees a `contextID`, then reuses that conversation for later requests with the same `contextID`.

:::note[Two ways to run the server]
This tutorial uses the conversation-owning mode: we give the server an opener,
and it keeps the conversations it opens. It suits A2A and the runtime in one
process.

If the runtime lives in another service and sessions are already tracked, use
[`a2aserver.NewStatelessServer`](/sdk/reference/a2a-server/). It hands each
message to a handler you write and keeps nothing between calls. See [Choosing a
server mode](/sdk/how-to/interop/choose-a2a-server-mode/).
:::

---

## Step 3: Create and start the server

```go
    server := sdk.NewA2AServer(opener,
        sdk.WithA2ACard(&card),
        sdk.WithA2APort(9999),
    )

    // Handle graceful shutdown.
    sigCh := make(chan os.Signal, 1)
    signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
    go func() {
        <-sigCh
        fmt.Println("\nShutting down...")
        server.Shutdown(context.Background())
        os.Exit(0)
    }()

    fmt.Println("A2A server listening on http://localhost:9999")
    fmt.Println("Agent card: http://localhost:9999/.well-known/agent-card.json")
    if err := server.ListenAndServe(); err != nil {
        log.Fatal(err)
    }
}
```

We run the server:

```bash
go run main.go
```

The terminal prints the two `listening` and `Agent card` lines. We verify the agent card:

```bash
curl http://localhost:9999/.well-known/agent-card.json | jq .
```

The output is the JSON agent card, with the name `Assistant Agent` and the `chat` skill.

---

## Step 4: Test with a client

In a separate terminal, we use the A2A client to send a message:

```go
client := a2a.NewClient("http://localhost:9999")

card, _ := client.Discover(ctx)
fmt.Printf("Agent: %s\n", card.Name)

text := "Hello!"
task, _ := client.SendMessage(ctx, &a2a.SendMessageRequest{
    Message: a2a.Message{
        Role:  a2a.RoleUser,
        Parts: []a2a.Part{{Text: &text}},
    },
    Configuration: &a2a.SendMessageConfiguration{Blocking: true},
})

for _, artifact := range task.Artifacts {
    for _, part := range artifact.Parts {
        if part.Text != nil {
            fmt.Println(*part.Text)
        }
    }
}
```

The program prints `Agent: Assistant Agent` and then the agent's reply text. See the [A2A Client Tutorial](/runtime/tutorials/07-a2a-client/) for a complete walkthrough.

---

## Step 5: Use a custom task store

By default, the server uses an in-memory task store. For persistence, implement the `sdk.A2ATaskStore` interface:

```go
server := sdk.NewA2AServer(opener,
    sdk.WithA2ACard(&card),
    sdk.WithA2APort(9999),
    sdk.WithA2ATaskStore(myCustomStore),
)
```

Tasks now persist in `myCustomStore` instead of memory. See the [SDK A2A Reference](/sdk/reference/a2a-server/) for the `A2ATaskStore` interface.

---

## Step 6: Embed in an existing server

If there is already an HTTP server, we use `Handler()` to get the `http.Handler`:

```go
server := sdk.NewA2AServer(opener, sdk.WithA2ACard(&card))

mux := http.NewServeMux()
mux.Handle("/", server.Handler())

http.ListenAndServe(":8080", mux)
```

The agent card is now served at `http://localhost:8080/.well-known/agent-card.json`.

---

## Server options

| Option | Description |
|--------|-------------|
| `sdk.WithA2ACard(card)` | Sets the agent card served at `/.well-known/agent-card.json` |
| `sdk.WithA2APort(port)` | Sets the TCP port for `ListenAndServe` |
| `sdk.WithA2ATaskStore(store)` | Sets a custom task store (default: in-memory) |

---

## Next steps

- [A2A Client Tutorial](/runtime/tutorials/07-a2a-client/): connect to this server from a client
- [Use Tool Bridge](/runtime/how-to/a2a/use-a2a-tool-bridge/): register this agent as a tool in another agent
- [A2A Concept](/concepts/a2a/): the protocol in depth

## See Also

- [SDK A2A Reference](/sdk/reference/a2a-server/): complete server API documentation
- [Examples](https://github.com/AltairaLabs/PromptKit/tree/main/server/a2a/examples/a2a-demo): a full working example
