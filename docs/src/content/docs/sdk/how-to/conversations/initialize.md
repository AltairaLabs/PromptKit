---
title: Open a Conversation
description: Open a conversation from a pack with sdk.Open, set options, handle errors and close it.
sidebar:
  order: 1
verified:
  commit: 004b9cbbd4d40f770a2e3c76053b600a55189355
  sources:
    - runtime/evals/handlers/length.go
    - runtime/evals/normalize.go
    - runtime/hooks/guardrails/spec.go
    - sdk/conversation.go
    - sdk/errors.go
    - sdk/internal/provider/detect.go
    - sdk/options.go
    - sdk/response.go
    - sdk/sdk.go
---
Open a conversation from a pack file with `sdk.Open()`, send a message, and close it.

## Prerequisites

- A pack file with at least one prompt. See [Tutorial 1: First Conversation](/sdk/tutorials/01-first-conversation/).
- An API key for one provider in the environment: `OPENAI_API_KEY`, `ANTHROPIC_API_KEY`, `GOOGLE_API_KEY` or `GEMINI_API_KEY`. `Open` detects the provider from the key it finds.

## Open the conversation

1. Pass the pack path (relative or absolute) and the name of a prompt under `prompts` in the pack:

   ```go
   conv, err := sdk.Open("./app.pack.json", "assistant")
   if err != nil {
       log.Fatal(err)
   }
   defer conv.Close()
   ```

2. Add options after the prompt name. `WithModel` overrides the model the pack sets:

   ```go
   conv, err := sdk.Open("./app.pack.json", "assistant",
       sdk.WithModel("gpt-4o"),
   )
   ```

3. Register guardrails and hooks that intercept LLM calls, tool execution and the session lifecycle. `myToolAuditHook` and `mySessionLogger` stand for your own `hooks.ToolHook` and `hooks.SessionHook` implementations:

   ```go
   conv, err := sdk.Open("./app.pack.json", "assistant",
       sdk.WithGuardrail(
           guardrails.Input("banned_words", map[string]any{"patterns": []string{"hack", "exploit"}}),
           guardrails.Output("length", map[string]any{"max_characters": 2000}),
       ),
       sdk.WithToolHook(myToolAuditHook),
       sdk.WithSessionHook(mySessionLogger),
   )
   ```

   `guardrails` is `github.com/AltairaLabs/PromptKit/runtime/v2/hooks/guardrails`. An input guardrail that matches blocks the LLM call and returns a canned assistant reply. See [Hooks & Guardrails Reference](/runtime/reference/hooks/) for the hook API.

4. Check the error from `Open`. Its errors are plain messages (see [Troubleshoot](#troubleshoot)), so log them and stop:

   ```go
   conv, err := sdk.Open("./app.pack.json", "assistant")
   if err != nil {
       log.Fatalf("failed to open: %v", err)
   }
   defer conv.Close()
   ```

5. Close the conversation when you finish. After `Close`, `Send` and `Stream` return `sdk.ErrConversationClosed`.

## Verify

Set a variable, send a message and print the reply:

```go
package main

import (
    "context"
    "fmt"
    "log"

    "github.com/AltairaLabs/PromptKit/sdk/v2"
)

func main() {
    conv, err := sdk.Open("./app.pack.json", "assistant",
        sdk.WithModel("gpt-4o"),
    )
    if err != nil {
        log.Fatal(err)
    }
    defer conv.Close()

    conv.SetVar("user_name", "Alice")

    resp, err := conv.Send(context.Background(), "Hello!")
    if err != nil {
        log.Fatal(err)
    }
    fmt.Println(resp.Text())
}
```

The program prints the model's reply.

## Troubleshoot

| Error text | Cause |
|---|---|
| `failed to detect provider: no provider detected: set OPENAI_API_KEY, ANTHROPIC_API_KEY, or GOOGLE_API_KEY` | None of the provider keys is set, and no key or provider option was passed. |
| `prompt "x" not found in pack (available: [...])` | The second argument is not a key under `prompts` in the pack. |
| `failed to resolve pack path: pack file not found: <path>` | The pack path does not exist. |
| `failed to load pack: ...` | The file cannot be read, is not valid JSON, or fails schema validation. |

## Related

- [Send Messages](/sdk/how-to/conversations/send-messages/)
- [Manage Variables](/sdk/how-to/conversations/manage-state/)
- [Register Tools](/sdk/how-to/tools/register-tools/)
