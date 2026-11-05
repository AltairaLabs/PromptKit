---
title: 'Tutorial 4: Variables and templates'
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
## What we will build

In this tutorial we build a support assistant whose system prompt changes with template variables. We set variables one at a time and in bulk, read them back, load them from the environment, change them between sends, and keep them separate in a forked conversation.

A variable fills a `{{name}}` placeholder in a prompt's `system_template`. The conversation fills it in before each call to the model.

## Prerequisites

Complete [Tutorial 1: Your first conversation](/sdk/tutorials/01-first-conversation/). We reuse its project and the `OPENAI_API_KEY` it sets.

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

The `support` prompt has four placeholders: `role`, `company`, `language` and `customer_name`. We fill all four before every send.

## Step 2: Set variables

`SetVar()` sets one variable. Replace `main.go` with:

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

    // Fill the four placeholders in the system prompt
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

Run it with `go run main.go`. The reply typically comes from a TechCorp support specialist and addresses Alice, because all four variables were filled into the system prompt.

## Step 3: Set several variables at once

`SetVars()` sets several variables in one call. Replace the four `SetVar` calls with:

```go
conv.SetVars(map[string]any{
    "role":          "sales representative",
    "company":       "GlobalTech",
    "language":      "Spanish",
    "customer_name": "Carlos",
})
```

Run it again. The reply is typically in Spanish, from a sales representative at GlobalTech, speaking to Carlos.

## Step 4: Read variables back

`GetVar()` returns a variable's value and whether it is set. Add these lines after the `SetVars` call:

```go
name, ok := conv.GetVar("customer_name")
fmt.Println("customer_name:", name, ok)
_, ok = conv.GetVar("nonexistent")
fmt.Println("nonexistent:", ok)
```

Run it again. Before the reply, the program prints:

```text
customer_name: Carlos true
nonexistent: false
```

## Step 5: Load variables from the environment

`SetVarsFromEnv()` takes a prefix. Every environment variable that starts with it becomes a template variable, with the prefix removed and the name lowercased. Add these lines after the `GetVar` lines:

```go
conv.SetVarsFromEnv("PROMPTKIT_")
company, _ := conv.GetVar("company")
fmt.Println("company:", company)
```

Set a variable in the shell and run the program:

```bash
export PROMPTKIT_COMPANY=Initech
go run main.go
```

The program prints `company: Initech`, and the reply typically comes from Initech instead of GlobalTech.

## Step 6: Pass non-string values

Variables are strings. `SetVars()` accepts a `map[string]any` and converts each value with `fmt.Sprintf("%v", value)`. Add these lines next:

```go
conv.SetVars(map[string]any{
    "order_count": 3,
    "is_premium":  true,
})
count, _ := conv.GetVar("order_count")
fmt.Println("order_count:", count)
```

Run it again. The program prints `order_count: 3`: the integer comes back as the string `3`.

## Step 7: Change a variable between sends

A conversation renders the system prompt on every `Send()`, so a variable changed between sends takes effect on the next one. Replace the `ctx := context.Background()` line and everything after it in `main` with:

```go
ctx := context.Background()
conv.SetVar("customer_name", "Unknown")
resp1, err := conv.Send(ctx, "Hi, my name is Bob")
if err != nil {
    log.Fatal(err)
}
fmt.Println("Reply 1:", resp1.Text())

conv.SetVar("customer_name", "Bob")
resp2, err := conv.Send(ctx, "What products do you have?")
if err != nil {
    log.Fatal(err)
}
fmt.Println("Reply 2:", resp2.Text())
```

Run it again. For the first send the system prompt names the customer `Unknown`; for the second it names Bob, and the second reply typically addresses Bob by name.

## Step 8: Set variables from several goroutines

Several goroutines can set variables on one conversation. Add `"sync"` to the imports, and add these lines at the end of `main`:

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
last, _ := conv.GetVar("var_9")
fmt.Println("var_9:", last)
```

Run it with the race detector:

```bash
go run -race main.go
```

The program prints `var_9: 9`, and the race detector reports nothing.

## Step 9: Fork a conversation

`Fork()` copies a conversation, including its variables. Here we fork `conv`, the conversation opened from `./dynamic.pack.json` with the `support` prompt. Add these lines at the end of `main`:

```go
fork, err := conv.Fork()
if err != nil {
    log.Fatal(err)
}
defer fork.Close()
fork.SetVar("customer_name", "Dana")

original, _ := conv.GetVar("customer_name")
forked, _ := fork.GetVar("customer_name")
fmt.Println("original:", original, "fork:", forked)
```

Run it again. The program prints `original: Bob fork: Dana`: changing the variable in the fork leaves the original untouched.

The finished `main.go`:

```go
package main

import (
    "context"
    "fmt"
    "log"
    "sync"

    "github.com/AltairaLabs/PromptKit/sdk/v2"
)

func main() {
    conv, err := sdk.Open("./dynamic.pack.json", "support")
    if err != nil {
        log.Fatal(err)
    }
    defer conv.Close()

    // Step 3: set several variables at once
    conv.SetVars(map[string]any{
        "role":          "sales representative",
        "company":       "GlobalTech",
        "language":      "Spanish",
        "customer_name": "Carlos",
    })

    // Step 4: read variables back
    name, ok := conv.GetVar("customer_name")
    fmt.Println("customer_name:", name, ok)
    _, ok = conv.GetVar("nonexistent")
    fmt.Println("nonexistent:", ok)

    // Step 5: load variables from the environment
    conv.SetVarsFromEnv("PROMPTKIT_")
    company, _ := conv.GetVar("company")
    fmt.Println("company:", company)

    // Step 6: pass non-string values
    conv.SetVars(map[string]any{
        "order_count": 3,
        "is_premium":  true,
    })
    count, _ := conv.GetVar("order_count")
    fmt.Println("order_count:", count)

    // Step 7: change a variable between sends
    ctx := context.Background()
    conv.SetVar("customer_name", "Unknown")
    resp1, err := conv.Send(ctx, "Hi, my name is Bob")
    if err != nil {
        log.Fatal(err)
    }
    fmt.Println("Reply 1:", resp1.Text())

    conv.SetVar("customer_name", "Bob")
    resp2, err := conv.Send(ctx, "What products do you have?")
    if err != nil {
        log.Fatal(err)
    }
    fmt.Println("Reply 2:", resp2.Text())

    // Step 8: set variables from several goroutines
    var wg sync.WaitGroup
    for i := 0; i < 10; i++ {
        wg.Add(1)
        go func(n int) {
            defer wg.Done()
            conv.SetVar(fmt.Sprintf("var_%d", n), fmt.Sprintf("%d", n))
        }(i)
    }
    wg.Wait()
    last, _ := conv.GetVar("var_9")
    fmt.Println("var_9:", last)

    // Step 9: fork the conversation
    fork, err := conv.Fork()
    if err != nil {
        log.Fatal(err)
    }
    defer fork.Close()
    fork.SetVar("customer_name", "Dana")

    original, _ := conv.GetVar("customer_name")
    forked, _ := fork.GetVar("customer_name")
    fmt.Println("original:", original, "fork:", forked)
}
```

## Next steps

- [Tutorial 5: Human-in-the-loop approval](/sdk/tutorials/05-custom-pipelines/) adds approval workflows.
- [Manage state](/sdk/how-to/conversations/manage-state/) covers conversation state beyond variables.
- The variable handling in `sdk/examples/hello/` shows a complete program.
