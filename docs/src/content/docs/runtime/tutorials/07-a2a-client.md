---
title: 'Tutorial: A2A Client'
description: Discover an A2A agent from its agent card and send it blocking and streaming messages with the runtime client.
sidebar:
  order: 7
verified:
  commit: 885cafa787a84bfba66f158fe51cda15a077ec23
  sources:
    - runtime/a2a/client.go
    - runtime/a2a/executor.go
    - runtime/a2a/types.go
    - server/a2a/server_stream.go
---

Discover an A2A agent and send messages using the runtime client.

**Time**: 10 minutes
**Level**: Intermediate

## What we build

In this tutorial we build a client that discovers a remote A2A agent, then sends it a blocking message and a streaming message.

## Prerequisites

- Go 1.26+
- A running A2A server (see [A2A Server Tutorial](/sdk/tutorials/10-a2a-server/))

---

## Step 1: Create the client

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
```

Configure the client with options:

```go
client := a2a.NewClient("http://localhost:9999",
    a2a.WithAuth("Bearer", "my-token"),
    a2a.WithHTTPClient(customHTTPClient),
)
```

---

## Step 2: Discover the agent

We fetch the agent card to learn what the agent can do:

```go
    card, err := client.Discover(ctx)
    if err != nil {
        log.Fatalf("Discover: %v", err)
    }

    fmt.Printf("Agent: %s\n", card.Name)
    fmt.Printf("Description: %s\n", card.Description)
    for _, skill := range card.Skills {
        fmt.Printf("  Skill: %s — %s\n", skill.ID, skill.Description)
    }
```

The client caches the card, so later `Discover` calls return it without a request.

After `Discover`, calls go to the card's first JSON-RPC interface that serves the version the client speaks (otherwise its first JSON-RPC interface), with that interface's `tenant`. The scheme, host and port you gave `NewClient` always win: from the card the client takes only an interface on that same host and port (default ports filled in): its path (an agent serving JSON-RPC at `/`, for example), version and tenant. An interface at the default `/a2a` keeps your URL as given. A card that names a different host or port is not followed, and its tenant and version are not used. Calls go to `{base}/a2a` and the client logs a warning once (an `http` interface on the `https` base host, as a TLS-terminating proxy reports, is not warned about); to call that host, configure the client with it. Without a card, calls go to `{base}/a2a`. A2A tool executors discover the card themselves on first use.

---

## Step 3: Send a message

We send a synchronous message with `Blocking: true`:

```go
    text := "Hello from the client!"
    task, err := client.SendMessage(ctx, &a2a.SendMessageRequest{
        Message: a2a.Message{
            Role:  a2a.RoleUser,
            Parts: []a2a.Part{{Text: &text}},
        },
        Configuration: &a2a.SendMessageConfiguration{Blocking: true},
    })
    if err != nil {
        log.Fatalf("SendMessage: %v", err)
    }

    fmt.Printf("Task state: %s\n", task.Status.State)
    for _, artifact := range task.Artifacts {
        for _, part := range artifact.Parts {
            if part.Text != nil {
                fmt.Printf("Agent: %s\n", *part.Text)
            }
        }
    }
```

Against the server from the [A2A Server Tutorial](/sdk/tutorials/10-a2a-server/), the output looks like this. The last line is the model's reply, so its text varies:

```
Agent: Assistant Agent
Description: A helpful assistant powered by PromptKit
  Skill: chat — General-purpose chat assistant
Task state: completed
Agent: Hello! How can I help you today?
```

---

## Step 4: Try streaming

We use `SendMessageStream` to receive incremental updates:

```go
    streamText := "Tell me a story"
    events, err := client.SendMessageStream(ctx, &a2a.SendMessageRequest{
        Message: a2a.Message{
            Role:  a2a.RoleUser,
            Parts: []a2a.Part{{Text: &streamText}},
        },
    })
    if err != nil {
        log.Fatalf("Stream: %v", err)
    }

    for event := range events {
        if event.StatusUpdate != nil {
            fmt.Printf("[status] %s\n", event.StatusUpdate.Status.State)
        }
        if event.ArtifactUpdate != nil {
            for _, part := range event.ArtifactUpdate.Artifact.Parts {
                if part.Text != nil {
                    fmt.Print(*part.Text)
                }
            }
        }
    }
    fmt.Println()
}
```

A server whose conversation cannot stream sends the whole reply as artifact events, followed by the completed status.

---

## Step 5: Manage tasks

We retrieve or cancel tasks by ID:

```go
// Get a task by ID.
task, err := client.GetTask(ctx, "task-123")

// Cancel a running task.
err = client.CancelTask(ctx, "task-123")

// List tasks by context.
tasks, err := client.ListTasks(ctx, &a2a.ListTasksRequest{
    ContextID: "ctx-456",
})
```

---

## Next steps

- [A2A Server Tutorial](/sdk/tutorials/10-a2a-server/) — build the server side
- [Use Tool Bridge](/runtime/how-to/a2a/use-a2a-tool-bridge/) — register this agent as a tool
- [Use Mock Server](/runtime/how-to/a2a/use-a2a-mock-server/) — test without a real server
- [A2A Concept](/concepts/a2a/) — understand the protocol in depth

## See also

- [Runtime A2A Reference](/runtime/reference/a2a/) — complete client API documentation
- [Examples](https://github.com/AltairaLabs/PromptKit/tree/main/server/a2a/examples/a2a-demo) — full working examples
