---
title: 'Tutorial: A2A Client'
sidebar:
  order: 7
---

Discover an A2A agent and send messages using the runtime client.

**Time**: 10 minutes
**Level**: Intermediate

## What You'll Build

A client that discovers a remote A2A agent and sends messages — both synchronous and streaming.

## What You'll Learn

- Create an A2A client
- Discover an agent's capabilities via its agent card
- Send synchronous messages
- Receive streaming events

## Prerequisites

- Go 1.22+
- A running A2A server (see [A2A Server Tutorial](/sdk/tutorials/10-a2a-server/))

---

## Step 1: Create the Client

```go
package main

import (
    "context"
    "fmt"
    "log"

    "github.com/AltairaLabs/PromptKit/runtime/a2a"
)

func main() {
    ctx := context.Background()
    client := a2a.NewClient("http://localhost:9999")
```

You can configure the client with options:

```go
client := a2a.NewClient("http://localhost:9999",
    a2a.WithAuth("Bearer", "my-token"),
    a2a.WithHTTPClient(customHTTPClient),
)
```

---

## Step 2: Discover the Agent

Fetch the agent card to learn what the agent can do:

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

The card is cached after the first call, so subsequent `Discover` calls are free.

---

## Step 3: Send a Message

Send a synchronous message with `Blocking: true`:

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

Expected output:

```
Agent: Echo Agent
Description: Echoes back whatever you send
  Skill: echo — Echoes the input message back
Task state: completed
Agent: Echo: Hello from the client!
```

---

## Step 4: Try Streaming

Use `SendMessageStream` to receive incremental updates:

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

Streaming requires the server to support it. If the server doesn't support streaming, you'll see a single artifact event followed by completion.

---

## Step 5: Manage Tasks

Retrieve or cancel tasks by ID:

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

## What You've Learned

- How to create an A2A client and configure authentication
- How to discover agents via their agent card
- How to send synchronous and streaming messages
- How to manage tasks (get, cancel, list)

## Next Steps

- [A2A Server Tutorial](/sdk/tutorials/10-a2a-server/) — build the server side
- [Use Tool Bridge](/runtime/how-to/use-a2a-tool-bridge/) — register this agent as a tool
- [Use Mock Server](/runtime/how-to/use-a2a-mock-server/) — test without a real server
- [A2A Concept](/concepts/a2a/) — understand the protocol in depth

## See Also

- [Runtime A2A Reference](/runtime/reference/a2a/) — complete client API documentation
- [Examples](https://github.com/AltairaLabs/PromptKit/tree/main/examples/a2a-demo) — full working examples
