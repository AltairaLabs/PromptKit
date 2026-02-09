---
title: Manage Variables
sidebar:
  order: 4
---
Learn how to use template variables with `SetVar()` and `GetVar()`.

## Set a Variable

```go
conv.SetVar("user_name", "Alice")
```

## Get a Variable

```go
name, ok := conv.GetVar("user_name")
if ok {
    fmt.Println(name)  // "Alice"
}
```

## Set Multiple Variables

```go
conv.SetVars(map[string]any{
    "user_name": "Alice",
    "role":      "admin",
    "language":  "en",
})
```

## Variable Types

Variables are strings:

```go
conv.SetVar("name", "Alice")
conv.SetVar("count", "42")
conv.SetVar("is_premium", "true")
conv.SetVar("region", "us-west")
```

## Use in Templates

Variables are substituted in system prompts:

**Pack file:**
```json
{
  "prompts": {
    "support": {
      "system_template": "You are helping {{user_name}}. They are a {{role}} user."
    }
  }
}
```

**Code:**
```go
conv.SetVar("user_name", "Alice")
conv.SetVar("role", "premium")

// System prompt becomes:
// "You are helping Alice. They are a premium user."
```

## Environment Variables

Load from environment:

```go
import "os"

// Single variable
conv.SetVar("api_key", os.Getenv("MY_API_KEY"))

// From environment with prefix
conv.SetVarsFromEnv("MYAPP_")
// MYAPP_USER_NAME -> "user_name"
// MYAPP_API_KEY -> "api_key"
```

## Check if Set

```go
value, ok := conv.GetVar("optional_var")
if !ok {
    // Variable not set
    conv.SetVar("optional_var", "default")
}
```

## Thread Safety

Variables are thread-safe:

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

## Fork Isolation

Forked conversations have isolated variables:

```go
conv1, _ := sdk.Open("./pack.json", "chat")
conv1.SetVar("user", "Alice")

// Fork creates a copy
conv2 := conv1.Fork()
conv2.SetVar("user", "Bob")

// Variables are isolated
user1, _ := conv1.GetVar("user")
user2, _ := conv2.GetVar("user")
fmt.Println(user1)  // "Alice"
fmt.Println(user2)  // "Bob"
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
    conv, _ := sdk.Open("./support.pack.json", "support")
    defer conv.Close()

    // Set customer context
    conv.SetVars(map[string]any{
        "customer_name": "Alice",
        "customer_tier": "gold",
        "language":      "English",
    })

    // Variables are used in system prompt
    ctx := context.Background()
    resp, _ := conv.Send(ctx, "I need help with my order")
    
    fmt.Println(resp.Text())
}
```

## See Also

- [Open a Conversation](initialize)
- [Tutorial 4: Variables](../tutorials/04-state-management)
