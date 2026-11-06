---
title: Manage Variables
description: Set, read and load template variables on a conversation, and see what wins when a name is set more than once.
sidebar:
  order: 4
verified:
  commit: 8489bf3d95c7498f9de0f024769db9399c24e884
  sources:
    - sdk/conversation.go
    - sdk/internal/pipeline/builder.go
    - sdk/json_input.go
    - sdk/options.go
    - sdk/session_vars.go
---
Fill the `{{placeholders}}` in your pack's system prompt with `SetVar`, `SetVars` and `GetVar`.

## Prerequisites

- An open conversation (`conv`). See [Open a Conversation](/sdk/how-to/conversations/initialize/).
- A prompt whose `system_template` uses placeholders:

  ```json
  {
    "prompts": {
      "support": {
        "system_template": "You are helping {{user_name}}. They are a {{role}} user."
      }
    }
  }
  ```

Variables substitute into the system prompt only. Message text goes to the model as written, so a `{{name}}` in a message stays a placeholder. See [How Variables Resolve](/sdk/explanation/variables/).

## Steps

1. Set one variable. Values are strings:

   ```go
   conv.SetVar("user_name", "Alice")
   ```

2. Set several at once. `SetVars` formats each value with `%v`:

   ```go
   conv.SetVars(map[string]any{
       "user_name": "Alice",
       "role":      "admin",
       "language":  "en",
   })
   ```

3. Read a variable back. The second result is `false` when the variable is not set:

   ```go
   name, ok := conv.GetVar("user_name")
   if !ok {
       conv.SetVar("user_name", "guest")
   }
   _ = name
   ```

4. Load variables from the environment. `SetVarsFromEnv` takes every variable with the prefix, strips it and lowercases the rest:

   ```go
   conv.SetVarsFromEnv("MYAPP_")
   // MYAPP_USER_NAME=Alice sets user_name to "Alice"
   ```

5. Set starting values when you open the conversation:

   ```go
   conv, err := sdk.Open("./support.pack.json", "support",
       sdk.WithVariables(map[string]string{"role": "standard"}),
   )
   ```

6. Fork to try different values on the same history. A fork has its own variables:

   ```go
   branch, err := conv.Fork()
   if err != nil {
       log.Fatal(err)
   }
   branch.SetVar("user_name", "Bob")
   ```

`SetVar`, `SetVars`, `SetVarsFromEnv` and `GetVar` are safe to call from several goroutines.

## Precedence

When the same name is set more than one way, the later row wins:

1. `WithVariables` at `Open()`
2. A `variables.Provider`, computed per turn
3. `SetVar` and `SetVars`, which stay set until you change them
4. `WithJSONInput` bindings, which apply to a single `Send`

A unary conversation (`Open`) resolves variables on every turn, so a variable changed between sends changes the next prompt. A duplex conversation (`OpenDuplex`) renders the system prompt once, when the first input starts the session. Set its variables before sending any input: a later `SetVar` is stored but never reaches the provider, and the first one is logged as a warning.

## Verify

Subscribe to `EventTemplateRendered` to print the system prompt the model receives:

```go
package main

import (
    "context"
    "fmt"
    "log"

    "github.com/AltairaLabs/PromptKit/runtime/v2/events"
    "github.com/AltairaLabs/PromptKit/sdk/v2"
)

func main() {
    bus := events.NewEventBus()
    bus.Subscribe(events.EventTemplateRendered, func(e *events.Event) {
        if d, ok := e.Data.(*events.TemplateRenderedData); ok {
            fmt.Println(d.SystemPrompt)
        }
    })

    conv, err := sdk.Open("./support.pack.json", "support", sdk.WithEventBus(bus))
    if err != nil {
        log.Fatal(err)
    }
    defer conv.Close()

    conv.SetVars(map[string]any{"user_name": "Alice", "role": "premium"})

    if _, err := conv.Send(context.Background(), "I need help with my order"); err != nil {
        log.Fatal(err)
    }
}
```

The program prints `You are helping Alice. They are a premium user.`

## Troubleshoot

A placeholder with no value fails the `Send` and nothing reaches the model. The error lists the missing names. Set the variable before the `Send`.

## Related

- [How Variables Resolve](/sdk/explanation/variables/): precedence, when each source is read, and what an unresolved placeholder does
- [Open a Conversation](/sdk/how-to/conversations/initialize/)
- [Tutorial 4: Variables](/sdk/tutorials/04-state-management/)
