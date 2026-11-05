---
title: 'Tutorial 4: Variables and Templates'
sidebar:
  order: 4
verified:
  commit: 885cafa787a84bfba66f158fe51cda15a077ec23
  sources:
    - runtime/prompt/pack.go
    - runtime/template/renderer.go
    - sdk/conversation.go
    - sdk/examples/hello/main_interactive.go
    - sdk/sdk.go
    - sdk/session/unary_session.go
    - sdk/session_vars.go
---
In this tutorial we set template variables on a conversation and watch them change the system prompt. We set variables one at a time and in bulk, read them back, load them from the environment, and keep them separate in a forked conversation.

A variable is a `{{name}}` placeholder in a prompt's `system_template`. The conversation fills it in before each call to the model.

## Prerequisites

Complete [Tutorial 1](/sdk/tutorials/01-first-conversation/).

## Step 1: Create a template pack

Create `dynamic.pack.json`:

```json
{
  "id": "dynamic-support",
  "name": "Dynamic Support",
  "version": "1.0.0",
  "template_engine": {
    "version": "v1",
    "syntax": "{{variable}}"
  },
  "prompts": {
    "support": {
      "id": "support",
      "name": "Support Agent",
      "version": "1.0.0",
      "system_template": "You are a {{role}} at {{company}}. Help customers in {{language}}. The customer's name is {{customer_name}}.",
      "parameters": {
        "temperature": 0.7
      }
    }
  }
}
```

## Step 2: Set variables

We use `SetVar()` to set individual variables:

```go
package main

import (
    "context"
    "fmt"
    "log"

    "github.com/AltairaLabs/PromptKit/sdk/v2"
)

func main() {
    conv, err := sdk.Open("./dynamic.pack.json", "support")
    if err != nil {
        log.Fatal(err)
    }
    defer conv.Close()

    // Set individual variables
    conv.SetVar("role", "customer support specialist")
    conv.SetVar("company", "TechCorp")
    conv.SetVar("language", "English")
    conv.SetVar("customer_name", "Alice")

    ctx := context.Background()
    resp, err := conv.Send(ctx, "I need help with my order")
    if err != nil {
        log.Fatal(err)
    }

    fmt.Println(resp.Text())
}
```

Run it. The reply is addressed to Alice as a TechCorp support specialist, because all four variables were filled into the system prompt.

## Step 3: Set several variables at once

We use `SetVars()` to set several variables in one call:

```go
conv.SetVars(map[string]any{
    "role":          "sales representative",
    "company":       "GlobalTech",
    "language":      "Spanish",
    "customer_name": "Carlos",
})
```

Send a message after this call. The reply is now in Spanish, from a sales representative at GlobalTech, speaking to Carlos.

## Step 4: Read variables back

We retrieve variables with `GetVar()`:

```go
// Set a variable
conv.SetVar("user_id", "12345")

// Get it back
userID, ok := conv.GetVar("user_id")
if ok {
    fmt.Printf("User ID: %s\n", userID)  // "12345"
}

// Non-existent variables return false
_, ok = conv.GetVar("nonexistent")
fmt.Printf("Found: %v\n", ok)  // false
```

The program prints `User ID: 12345` and then `Found: false`.

## Step 5: Load variables from the environment

`SetVarsFromEnv()` takes a prefix. Every environment variable that starts with it becomes a template variable, with the prefix removed and the name lowercased:

```go
// PROMPTKIT_API_KEY=abc123 becomes the variable "api_key"
conv.SetVarsFromEnv("PROMPTKIT_")

apiKey, _ := conv.GetVar("api_key")
fmt.Println(apiKey) // "abc123"
```

With `PROMPTKIT_API_KEY=abc123` set in the shell, the program prints `abc123`.

## Step 6: Change a variable between sends

A conversation renders the system prompt on every `Send()`, so a variable you change between sends takes effect on the next one:

```go
ctx := context.Background()
conv.SetVar("customer_name", "Unknown")

// First interaction
resp1, _ := conv.Send(ctx, "Hi, my name is Bob")

// Update the variable from what the customer said
conv.SetVar("customer_name", "Bob")

// The system prompt now says "The customer's name is Bob"
resp2, _ := conv.Send(ctx, "What products do you have?")
```

The second reply can address Bob by name; the first could not.

## Step 7: Pass non-string values

Variables are strings. `SetVars()` accepts a `map[string]any` and converts each value to a string:

```go
conv.SetVars(map[string]any{
    "name":       "Alice",
    "user_id":    12345,
    "is_premium": true,
})

userID, _ := conv.GetVar("user_id")
fmt.Println(userID) // "12345"
```

The integer `12345` comes back as the string `12345`.

## Step 8: Set variables from several goroutines

Several goroutines can set variables on one conversation:

```go
var wg sync.WaitGroup

for i := 0; i < 10; i++ {
    wg.Add(1)
    go func(n int) {
        defer wg.Done()
        conv.SetVar(fmt.Sprintf("var_%d", n), fmt.Sprintf("%d", n))
    }(i)
}

wg.Wait()
```

All ten variables (`var_0` to `var_9`) are set, and the program finishes without a data race.

## Step 9: Fork a conversation

`Fork()` copies a conversation, including its variables. Changing a variable in the fork leaves the original untouched:

```go
conv1, _ := sdk.Open("./pack.json", "chat")
conv1.SetVar("user", "Alice")

conv2, err := conv1.Fork()
if err != nil {
    log.Fatal(err)
}
conv2.SetVar("user", "Bob")

// conv1 still has "Alice"
user1, _ := conv1.GetVar("user")
user2, _ := conv2.GetVar("user")
fmt.Println(user1)  // "Alice"
fmt.Println(user2)  // "Bob"
```

The program prints `Alice` and then `Bob`.

## Next steps

- **[Tutorial 5: HITL](/sdk/tutorials/05-custom-pipelines/)** - Approval workflows
- **[How-To: Manage State](/sdk/how-to/conversations/manage-state/)** - Advanced state
- The variable handling in `sdk/examples/hello/` shows a complete program.
