---
title: 'Tutorial 1: Your first conversation'
sidebar:
  order: 1
verified:
  commit: 885cafa787a84bfba66f158fe51cda15a077ec23
  sources:
    - runtime/credentials/resolver.go
    - runtime/logger/config.go
    - runtime/prompt/schema/promptpack.schema.json
    - sdk/conversation.go
    - sdk/examples/hello/main_interactive.go
    - sdk/response.go
    - sdk/sdk.go
---
## What we will build

In this tutorial we build a command-line chatbot with the PromptKit SDK. It opens a conversation from a pack file, fills a template variable, holds a multi-turn exchange, and ends as an interactive chat loop.

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

`go.mod` now has a `require` line for `github.com/AltairaLabs/PromptKit/sdk/v2`.

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

The pack defines the assistant's system prompt and generation parameters. The `{{user_name}}` placeholder is a template variable that our program fills in.

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
    // Open a conversation from the pack file, using its "chat" prompt
    conv, err := sdk.Open("./hello.pack.json", "chat")
    if err != nil {
        log.Fatal(err)
    }
    defer conv.Close()

    // Fill the {{user_name}} placeholder in the system prompt
    conv.SetVar("user_name", "World")

    // Send a message and wait for the reply
    ctx := context.Background()
    resp, err := conv.Send(ctx, "Hello!")
    if err != nil {
        log.Fatal(err)
    }

    fmt.Println(resp.Text())
}
```

`sdk.Open` takes the pack path and the prompt name and returns a `Conversation`. `SetVar` fills a placeholder in the system prompt, `Send` sends a message and returns the response, and `Text` returns the response's text.

## Step 4: Run your chatbot

Set your API key:

```bash
export OPENAI_API_KEY="your-api-key-here"
```

Run the program:

```bash
go run main.go
```

We see a greeting from the model. The wording varies; it typically reads like this:

```text
Hello World! How can I help you today?
```

The SDK also writes `INFO` log lines, such as `conversation opened`, to standard error.

## Step 5: Hold a multi-turn conversation

The conversation keeps its history, so a second `Send` sees the first. Replace `main.go` with:

```go
package main

import (
    "context"
    "fmt"
    "log"

    "github.com/AltairaLabs/PromptKit/sdk/v2"
)

func main() {
    conv, err := sdk.Open("./hello.pack.json", "chat")
    if err != nil {
        log.Fatal(err)
    }
    defer conv.Close()

    conv.SetVar("user_name", "World")
    ctx := context.Background()

    // Turn 1
    resp1, err := conv.Send(ctx, "My favorite color is green.")
    if err != nil {
        log.Fatal(err)
    }
    fmt.Println("Assistant:", resp1.Text())

    // Turn 2: the model sees turn 1
    resp2, err := conv.Send(ctx, "What is my favorite color?")
    if err != nil {
        log.Fatal(err)
    }
    fmt.Println("Assistant:", resp2.Text())
}
```

Run it with `go run main.go`. The second reply typically names green, because the model received turn 1 along with turn 2:

```text
Assistant: Nice! Green is a great color.
Assistant: Your favorite color is green.
```

## Step 6: Build an interactive chat loop

Replace `main.go` with a loop that reads from the terminal:

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

    conv.SetVar("user_name", "World")

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

Run it with `go run main.go`. The program prints `Chat ready! Type 'quit' to exit.` and a `You: ` prompt. Type a message and the reply follows on an `Assistant:` line; type `quit` to exit.

## Next steps

- [Tutorial 2: Streaming responses](/sdk/tutorials/02-streaming-responses/) shows the reply as it is generated.
- [Tutorial 3: Tool integration](/sdk/tutorials/03-tool-integration/) adds function calling.
- [Send messages](/sdk/how-to/conversations/send-messages/) covers the full set of send options.
- [Initialize a conversation](/sdk/how-to/conversations/initialize/) shows how to choose a model or provider.
- The complete example is in `sdk/examples/hello/`.
