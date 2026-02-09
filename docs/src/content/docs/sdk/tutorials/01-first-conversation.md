---
title: 'Tutorial 1: Your First Conversation'
sidebar:
  order: 1
---
Build a chatbot in 5 lines of code using the PromptKit SDK.

## What You'll Learn

- Open a conversation from a pack file
- Send messages and receive responses
- Use template variables
- Multi-turn conversations

## Prerequisites

- Go 1.21+ installed
- OpenAI API key (get one at [platform.openai.com](https://platform.openai.com))

## Step 1: Set Up Your Project

Create a new Go module:

```bash
mkdir my-chatbot
cd my-chatbot
go mod init my-chatbot
```

Install the SDK:

```bash
go get github.com/AltairaLabs/PromptKit/sdk
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

This pack defines your assistant's behavior and configuration.

## Step 3: Write Your First Chatbot

Create `main.go`:

```go
package main

import (
    "context"
    "fmt"
    "log"

    "github.com/AltairaLabs/PromptKit/sdk"
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

That's it! **5 lines of functional code** (excluding error handling).

## Step 4: Run Your Chatbot

Set your API key:

```bash
export OPENAI_API_KEY="your-api-key-here"
```

Run the program:

```bash
go run main.go
```

You should see:

```
Hello World! How can I help you today?
```

🎉 **Congratulations!** You've built your first chatbot.

## Understanding the Code

### sdk.Open()

```go
conv, err := sdk.Open("./hello.pack.json", "chat")
```

- First argument: path to your pack file
- Second argument: prompt name from the pack
- Returns a `Conversation` ready to use

### conv.SetVar()

```go
conv.SetVar("user_name", "World")
```

Sets template variables that are substituted into the system prompt.

### conv.Send()

```go
resp, err := conv.Send(ctx, "Hello!")
```

Sends a message and returns the response. The conversation context is maintained automatically.

### resp.Text()

```go
fmt.Println(resp.Text())
```

Gets the text content from the response.

## Multi-Turn Conversations

The SDK automatically maintains conversation history:

```go
// Turn 1
resp1, _ := conv.Send(ctx, "My name is Alice")
fmt.Println(resp1.Text())  // "Nice to meet you, Alice!"

// Turn 2 - context is remembered
resp2, _ := conv.Send(ctx, "What's my name?")
fmt.Println(resp2.Text())  // "Your name is Alice."
```

## Configuration Options

### Open with Options

```go
conv, err := sdk.Open("./hello.pack.json", "chat",
    sdk.WithModel("gpt-4o"),
)
```

### Different Providers

The pack file can specify different providers:

```json
{
  "id": "my-chatbot",
  "name": "My Chatbot",
  "version": "1.0.0",
  "template_engine": {
    "version": "v1",
    "syntax": "{{variable}}"
  },
  "provider": {
    "name": "anthropic",
    "model": "claude-3-5-sonnet-20241022"
  },
  "prompts": {
    "chat": {
      "id": "chat",
      "name": "Chat",
      "version": "1.0.0",
      "system_template": "You are a helpful assistant."
    }
  }
}
```

## Interactive Chat Loop

Make it interactive:

```go
package main

import (
    "bufio"
    "context"
    "fmt"
    "log"
    "os"
    "strings"

    "github.com/AltairaLabs/PromptKit/sdk"
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

## What You've Learned

✅ Open conversations with `sdk.Open()`  
✅ Set template variables with `SetVar()`  
✅ Send messages with `Send()`  
✅ Multi-turn conversation context  
✅ Configuration options  

## Next Steps

- **[Tutorial 2: Streaming](02-streaming-responses)** - Real-time responses
- **[Tutorial 3: Tools](03-tool-integration)** - Add function calling
- **[How-To: Send Messages](../how-to/send-messages)** - Advanced messaging

## Complete Example

See the full example at `sdk/examples/hello/`.
