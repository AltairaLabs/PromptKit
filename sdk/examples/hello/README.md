# Hello World Example

The simplest SDK example - open a conversation and send a message.

## What You'll Learn

- Opening a conversation from a pack file with `sdk.Open()`
- Setting template variables with `SetVar()`
- Sending messages with `Send()`
- Multi-turn conversation context

## Prerequisites

- Go 1.26+
- An API key for one of: OpenAI, Anthropic, or Google

## Running the Example

The pack does not name a provider — the SDK detects one from the environment,
so any of these works:

```bash
export OPENAI_API_KEY=your-key       # uses gpt-4o
# or
export ANTHROPIC_API_KEY=your-key
# or
export GOOGLE_API_KEY=your-key

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

- **Prompt**: A friendly assistant with `{{user_name}}` template variable

It deliberately does not pin a provider, so the example runs against whichever
of OpenAI / Anthropic / Google you have a key for.

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
