---
title: 'Tutorial 7: A2A client'
description: Discover an A2A agent from its agent card and send it blocking and streaming messages with the runtime client.
sidebar:
  order: 7
verified:
  commit: 8489bf3d95c7498f9de0f024769db9399c24e884
  sources:
    - runtime/a2a/client.go
    - runtime/a2a/types.go
    - server/a2a/server_stream.go
---
## What we will build

In this tutorial we build a client that discovers a remote A2A agent, sends it a blocking message, looks the task up again by ID, and streams a second reply.

## Prerequisites

- Go 1.26 or later
- The A2A server from the [A2A server tutorial](/sdk/tutorials/10-a2a-server/), running on `http://localhost:9999`

## Step 1: Discover the agent

In a new directory, create a module and install the runtime:

```bash
mkdir a2a-client
cd a2a-client
go mod init a2a-client
go get github.com/AltairaLabs/PromptKit/runtime/v2@latest
```

Create `main.go`. `a2a.NewClient` takes the agent's base URL, and `Discover` fetches its agent card:

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
		log.Fatalf("Discover: %v", err)
	}
	fmt.Printf("Agent: %s\n", card.Name)
	fmt.Printf("Description: %s\n", card.Description)
	for _, skill := range card.Skills {
		fmt.Printf("  Skill: %s: %s\n", skill.ID, skill.Description)
	}

	// Step 2 goes here.
}
```

Run it:

```bash
go run .
```

The program prints the server's agent card:

```text
Agent: Assistant Agent
Description: A helpful assistant powered by PromptKit
  Skill: chat: General-purpose chat assistant
```

The client caches the card and uses it to choose where later calls go. [Where the client sends calls](/concepts/a2a/#where-the-client-sends-calls) describes the rules.

## Step 2: Send a message

Replace the `// Step 2 goes here.` comment with a blocking `SendMessage` call that prints the task state and the reply text:

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
				fmt.Printf("Reply: %s\n", *part.Text)
			}
		}
	}

	// Step 3 goes here.
```

Run the program again. After the card, it prints the task state and the model's reply, whose text varies:

```text
Task state: completed
Reply: Hello! How can I help you today?
```

## Step 3: Look up the task

The server keeps each task under its ID. Replace `// Step 3 goes here.` with a `GetTask` call for the task from Step 2:

```go
	fetched, err := client.GetTask(ctx, task.ID)
	if err != nil {
		log.Fatalf("GetTask: %v", err)
	}
	fmt.Printf("Fetched task state: %s\n", fetched.Status.State)

	// Step 4 goes here.
```

Run the program. The new line shows the stored task in the same state:

```text
Fetched task state: completed
```

## Step 4: Stream a reply

Replace `// Step 4 goes here.` with a `SendMessageStream` call. It returns a channel of events: status updates and artifact updates that carry the reply text. A stream that fails ends with an event whose `Error` is set: an `*a2a.RPCError` from the agent, or `a2a.ErrSSEIdleTimeout` when nothing arrives within the idle timeout (five minutes by default).

```go
	streamText := "Tell me a short story"
	events, err := client.SendMessageStream(ctx, &a2a.SendMessageRequest{
		Message: a2a.Message{
			Role:  a2a.RoleUser,
			Parts: []a2a.Part{{Text: &streamText}},
		},
	})
	if err != nil {
		log.Fatalf("SendMessageStream: %v", err)
	}

	for event := range events {
		if event.Error != nil {
			log.Fatalf("stream: %v", event.Error)
		}
		if event.StatusUpdate != nil {
			fmt.Printf("\n[status] %s\n", event.StatusUpdate.Status.State)
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
```

Run the program. After the earlier lines it prints the story as it arrives, between status lines, ending in `[status] completed`. A server whose conversation cannot stream sends the whole reply as artifact events, followed by the completed status.

## Next steps

- [Use the A2A tool bridge](/runtime/how-to/a2a/use-a2a-tool-bridge/) to register this agent as a tool.
- [Use the A2A mock server](/runtime/how-to/a2a/use-a2a-mock-server/) to test without a real server.
- [A2A concept](/concepts/a2a/) explains the protocol in depth.
- The [runtime A2A reference](/runtime/reference/a2a/) lists the complete client API, including `CancelTask`, `ListTasks`, `WithAuth` and `WithHTTPClient`.
- The [a2a-demo example](https://github.com/AltairaLabs/PromptKit/tree/main/server/a2a/examples/a2a-demo) is a full working program.
