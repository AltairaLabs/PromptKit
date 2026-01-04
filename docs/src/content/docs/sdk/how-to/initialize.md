---
title: Open a Conversation
sidebar:
  order: 1
---
Learn how to start using the SDK v2 with `sdk.Open()`.

## Basic Usage

```go
import "github.com/AltairaLabs/PromptKit/sdk"

conv, err := sdk.Open("./app.pack.json", "assistant")
if err != nil {
    log.Fatal(err)
}
defer conv.Close()
```

## Parameters

### Pack Path

First argument is the path to your pack file:

```go
// Relative path
conv, _ := sdk.Open("./prompts/app.pack.json", "chat")

// Absolute path
conv, _ := sdk.Open("/etc/myapp/prompts.pack.json", "chat")
```

### Prompt Name

Second argument is the prompt name from your pack:

```go
// Pack contains: prompts.assistant, prompts.support, prompts.sales
conv, _ := sdk.Open("./app.pack.json", "assistant")
conv, _ := sdk.Open("./app.pack.json", "support")
conv, _ := sdk.Open("./app.pack.json", "sales")
```

## Options

### Override Model

```go
conv, _ := sdk.Open("./app.pack.json", "chat",
    sdk.WithModel("gpt-4o"),
)
```

### Override Temperature

```go
conv, _ := sdk.Open("./app.pack.json", "chat",
    sdk.WithTemperature(0.9),
)
```

### Multiple Options

```go
conv, _ := sdk.Open("./app.pack.json", "chat",
    sdk.WithModel("gpt-4o"),
    sdk.WithTemperature(0.8),
    sdk.WithMaxTokens(2000),
)
```

## Environment Variables

The SDK uses these environment variables:

```bash
# Provider API keys (one required)
export OPENAI_API_KEY="sk-..."
export ANTHROPIC_API_KEY="sk-ant-..."
export GOOGLE_API_KEY="..."

# Optional: Default model
export PROMPTKIT_MODEL="gpt-4o-mini"
```

## Error Handling

```go
conv, err := sdk.Open("./app.pack.json", "chat")
if err != nil {
    switch {
    case errors.Is(err, sdk.ErrPackNotFound):
        log.Fatal("Pack file not found")
    case errors.Is(err, sdk.ErrPromptNotFound):
        log.Fatal("Prompt not in pack")
    case errors.Is(err, sdk.ErrInvalidPack):
        log.Fatal("Pack file is invalid")
    default:
        log.Fatalf("Failed to open: %v", err)
    }
}
defer conv.Close()
```

## Always Close

Always close conversations when done:

```go
conv, err := sdk.Open("./app.pack.json", "chat")
if err != nil {
    log.Fatal(err)
}
defer conv.Close()  // Important!

// Use conversation...
```

## Complete Example

```go
package main

import (
    "context"
    "fmt"
    "log"

    "github.com/AltairaLabs/PromptKit/sdk"
)

func main() {
    // Open conversation
    conv, err := sdk.Open("./app.pack.json", "assistant",
        sdk.WithModel("gpt-4o"),
    )
    if err != nil {
        log.Fatal(err)
    }
    defer conv.Close()

    // Set variables
    conv.SetVar("user_name", "Alice")

    // Send message
    ctx := context.Background()
    resp, err := conv.Send(ctx, "Hello!")
    if err != nil {
        log.Fatal(err)
    }

    fmt.Println(resp.Text())
}
```

## See Also

- [Send Messages](send-messages)
- [Register Tools](register-tools)
- [Tutorial 1: First Conversation](../tutorials/01-first-conversation)
