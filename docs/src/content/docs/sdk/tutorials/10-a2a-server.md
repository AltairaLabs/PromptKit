---
title: 'Tutorial: A2A Server'
sidebar:
  order: 10
---

Expose a PromptKit conversation as an A2A-compliant agent server.

**Time**: 15 minutes
**Level**: Intermediate

## What You'll Build

An A2A server that exposes a PromptKit SDK conversation as a remotely-callable agent, discoverable via its agent card.

## What You'll Learn

- Create an A2A server with `sdk.NewA2AServer`
- Use `sdk.A2AOpener` to bridge SDK conversations to the server
- Configure agent cards
- Start and gracefully shut down the server

## Prerequisites

- Go 1.22+
- A compiled pack file (`.pack.json`)
- Completed [First Conversation tutorial](/sdk/tutorials/01-first-conversation/) (recommended)

---

## Step 1: Define the Agent Card

The agent card describes your agent's identity and capabilities:

```go
package main

import (
    "context"
    "fmt"
    "log"
    "os"
    "os/signal"
    "syscall"

    "github.com/AltairaLabs/PromptKit/runtime/a2a"
    "github.com/AltairaLabs/PromptKit/sdk"
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

---

## Step 2: Create the Conversation Opener

Use `sdk.A2AOpener` to create conversations from a pack file:

```go
    opener := sdk.A2AOpener("./assistant.pack.json", "chat")
```

Each A2A request creates a new SDK conversation via this opener. The `contextID` from the A2A protocol groups related requests.

---

## Step 3: Create and Start the Server

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
    fmt.Println("Agent card: http://localhost:9999/.well-known/agent.json")
    if err := server.ListenAndServe(); err != nil {
        log.Fatal(err)
    }
}
```

Run the server:

```bash
go run main.go
```

Verify the agent card:

```bash
curl http://localhost:9999/.well-known/agent.json | jq .
```

---

## Step 4: Test with a Client

In a separate terminal, use the A2A client to send a message:

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

See the [A2A Client Tutorial](/runtime/tutorials/07-a2a-client/) for a complete walkthrough.

---

## Step 5: Custom Task Store

By default, the server uses an in-memory task store. For persistence, implement the `sdk.A2ATaskStore` interface:

```go
server := sdk.NewA2AServer(opener,
    sdk.WithA2ACard(&card),
    sdk.WithA2APort(9999),
    sdk.WithA2ATaskStore(myCustomStore),
)
```

See the [SDK A2A Reference](/sdk/reference/a2a-server/) for the `A2ATaskStore` interface.

---

## Step 6: Embed in an Existing Server

If you already have an HTTP server, use `Handler()` to get the `http.Handler`:

```go
server := sdk.NewA2AServer(opener, sdk.WithA2ACard(&card))

mux := http.NewServeMux()
mux.Handle("/", server.Handler())

http.ListenAndServe(":8080", mux)
```

---

## Server Options

| Option | Description |
|--------|-------------|
| `sdk.WithA2ACard(card)` | Sets the agent card served at `/.well-known/agent.json` |
| `sdk.WithA2APort(port)` | Sets the TCP port for `ListenAndServe` |
| `sdk.WithA2ATaskStore(store)` | Sets a custom task store (default: in-memory) |

---

## What You've Learned

- How to configure an agent card
- How to use `sdk.A2AOpener` to bridge SDK conversations
- How to create, start, and gracefully shut down an A2A server
- How to embed the server in an existing HTTP server

## Next Steps

- [A2A Client Tutorial](/runtime/tutorials/07-a2a-client/) — connect to this server from a client
- [Use Tool Bridge](/runtime/how-to/use-a2a-tool-bridge/) — register this agent as a tool in another agent
- [A2A Concept](/concepts/a2a/) — understand the protocol in depth

## See Also

- [SDK A2A Reference](/sdk/reference/a2a-server/) — complete server API documentation
- [Examples](https://github.com/AltairaLabs/PromptKit/tree/main/examples/a2a-demo) — full working examples
