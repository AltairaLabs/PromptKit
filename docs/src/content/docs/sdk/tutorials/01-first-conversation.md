---
title: 'Tutorial 1: Your First Conversation'
sidebar:
  order: 1
verified:
  commit: 885cafa787a84bfba66f158fe51cda15a077ec23
  sources:
    - runtime/credentials/resolver.go
    - runtime/prompt/schema/promptpack.schema.json
    - sdk/conversation.go
    - sdk/examples/hello/main_interactive.go
    - sdk/options.go
    - sdk/response.go
    - sdk/sdk.go
---
Build a chatbot with the PromptKit SDK: open a conversation from a pack, send messages, and keep context across turns.

## What you will build

We open a conversation from a pack file, send messages, fill a template variable, and hold a multi-turn chat.

## Prerequisites

- Go 1.26 or later
- An OpenAI API key (get one at [platform.openai.com](https://platform.openai.com))

## Step 1: Set up your project

Create a new Go module:

```bash
mkdir my-chatbot
cd my-chatbot
go mod init my-chatbot
```

Install the SDK:

```bash
go get github.com/AltairaLabs/PromptKit/sdk/v2
```

## Step 2: Create a PromptPack

Create `hello.pack.json`:

```json
{
  "id": "hello-chatbot",
  "name": "Hello Chatbot",
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
      "system_template": "You are a helpful AI assistant. Be concise and friendly. The user's name is {{user_name}}.",
      "parameters": {
        "temperature": 0.7,
        "max_tokens": 1000
      }
    }
  }
}
```

The pack defines the assistant's system prompt and generation parameters.

## Step 3: Write your first chatbot

Create `main.go`:

```go
package main

import (
    "context"
    "fmt"
    "log"

    "github.com/AltairaLabs/PromptKit/sdk/v2"
)

func main() {
    // Open a conversation from a pack file
    conv, err := sdk.Open("./hello.pack.json", "chat")
    if err != nil {
        log.Fatal(err)
    }
    defer conv.Close()

    // Set template variables (optional)
    conv.SetVar("user_name", "World")

    // Send a message and get a response
    ctx := context.Background()
    resp, err := conv.Send(ctx, "Hello!")
    if err != nil {
        log.Fatal(err)
    }

    // Print the response
    fmt.Println(resp.Text())
}
```

## Step 4: Run your chatbot

Set your API key:

```bash
export OPENAI_API_KEY="your-api-key-here"
```

Run the program:

```bash
go run main.go
```

We see a greeting from the model, for example:

```text
Hello World! How can I help you today?
```

We have built our first chatbot.

## Understanding the code

### `sdk.Open()`

```go
conv, err := sdk.Open("./hello.pack.json", "chat")
```

`sdk.Open` takes the path to the pack file and the prompt name, and returns a `Conversation`.

### `conv.SetVar()`

```go
conv.SetVar("user_name", "World")
```

`SetVar` sets a template variable that the runtime substitutes into the system prompt.

### `conv.Send()`

```go
resp, err := conv.Send(ctx, "Hello!")
```

`Send` sends a message and returns the response. The conversation keeps its history between calls.

### `resp.Text()`

```go
fmt.Println(resp.Text())
```

`Text` returns the text content of the response.

## Multi-turn conversations

The conversation keeps its history, so a second `Send` sees the first. We add two turns to `main.go` after the first send:

```go
// Turn 1
resp1, _ := conv.Send(ctx, "My name is Alice")
fmt.Println(resp1.Text()) // for example: "Nice to meet you, Alice!"

// Turn 2: the model sees turn 1
resp2, _ := conv.Send(ctx, "What's my name?")
fmt.Println(resp2.Text()) // for example: "Your name is Alice."
```

## Interactive chat loop

We replace `main.go` with a loop that reads from the terminal:

```go
package main

import (
    "bufio"
    "context"
    "fmt"
    "log"
    "os"
    "strings"

    "github.com/AltairaLabs/PromptKit/sdk/v2"
)

func main() {
    conv, err := sdk.Open("./hello.pack.json", "chat")
    if err != nil {
        log.Fatal(err)
    }
    defer conv.Close()

    ctx := context.Background()
    scanner := bufio.NewScanner(os.Stdin)

    fmt.Println("Chat ready! Type 'quit' to exit.")
    for {
        fmt.Print("You: ")
        if !scanner.Scan() {
            break
        }

        msg := strings.TrimSpace(scanner.Text())
        if msg == "quit" {
            break
        }

        resp, err := conv.Send(ctx, msg)
        if err != nil {
            log.Printf("Error: %v", err)
            continue
        }
        
        fmt.Printf("Assistant: %s\n\n", resp.Text())
    }
}
```

Run it with `go run main.go`, type a message and the assistant answers; type `quit` to exit.

## Next steps

- [Tutorial 2: Streaming](/sdk/tutorials/02-streaming-responses/): real-time responses
- [Tutorial 3: Tools](/sdk/tutorials/03-tool-integration/): add function calling
- [Send Messages](/sdk/how-to/conversations/send-messages/): the full set of send options
- [Initialize a Conversation](/sdk/how-to/conversations/initialize/): choose a model or provider

The complete example is in `sdk/examples/hello/`.
