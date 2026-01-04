---
title: Hello World Example
description: Example demonstrating hello
sidebar:
  order: 100
---


The simplest SDK example - open a conversation and send a message.

## What You'll Learn

- Opening a conversation from a pack file with `sdk.Open()`
- Setting template variables with `SetVar()`
- Sending messages with `Send()`
- Multi-turn conversation context

## Prerequisites

- Go 1.21+
- OpenAI API key

## Running the Example

```bash
export OPENAI_API_KEY=your-key
go run .
```

## Code Overview

```go
// Open a conversation from a pack file
conv, err := sdk.Open("./hello.pack.json", "chat")
if err != nil {
    log.Fatal(err)
}
defer conv.Close()

// Set template variables (substituted in system prompt)
conv.SetVar("user_name", "World")

// Send a message and get a response
ctx := context.Background()
resp, err := conv.Send(ctx, "Say hello!")
fmt.Println(resp.Text())

// Context is maintained across turns
resp, _ = conv.Send(ctx, "What's my name?")
fmt.Println(resp.Text())  // Remembers "World"
```

## Pack File Structure

The `hello.pack.json` defines:

- **Provider**: OpenAI with `gpt-4o-mini`
- **Prompt**: A friendly assistant with `{{user_name}}` template variable

```json
{
  "prompts": {
    "chat": {
      "system_template": "You are a friendly assistant. The user's name is {{user_name}}."
    }
  }
}
```

## Key Concepts

1. **Pack-First Design** - All configuration lives in the pack file
2. **Template Variables** - Use `{{variable}}` in prompts, set with `SetVar()`
3. **Conversation Context** - Messages are remembered across `Send()` calls
4. **Resource Cleanup** - Always `defer conv.Close()`

## Next Steps

- [Streaming Example](../streaming/) - Real-time response streaming
- [Tools Example](../tools/) - Function calling
- [HITL Example](../hitl/) - Human-in-the-loop approval
