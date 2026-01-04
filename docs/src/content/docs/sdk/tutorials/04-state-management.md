---
title: 'Tutorial 4: Variables and Templates'
sidebar:
  order: 4
---
Learn how to use template variables for dynamic prompts and conversation context.

## What You'll Learn

- Set and get template variables
- Use variables in system prompts
- Bulk variable operations
- Environment variable integration

## Why Variables?

Variables make prompts dynamic and reusable:

**Static Prompt:**
```
You are a helpful assistant.
```

**Dynamic Prompt:**
```
You are a {{role}} assistant for {{company}}. 
Speak in {{language}}.
```

## Prerequisites

Complete [Tutorial 1](01-first-conversation) and understand basic SDK usage.

## Step 1: Create a Template Pack

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

## Step 2: Set Variables

Use `SetVar()` to set individual variables:

```go
package main

import (
    "context"
    "fmt"
    "log"

    "github.com/AltairaLabs/PromptKit/sdk"
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

## Bulk Variable Setting

Use `SetVars()` for multiple variables at once:

```go
conv.SetVars(map[string]any{
    "role":          "sales representative",
    "company":       "GlobalTech",
    "language":      "Spanish",
    "customer_name": "Carlos",
})
```

## Get Variables

Retrieve variables with `GetVar()`:

```go
// Set a variable
conv.SetVar("user_id", "12345")

// Get it back
userID := conv.GetVar("user_id")
fmt.Printf("User ID: %v\n", userID)  // "12345"

// Non-existent variables return nil
missing := conv.GetVar("nonexistent")
fmt.Printf("Missing: %v\n", missing)  // nil
```

## Environment Variables

Load variables from environment:

```go
// Set from environment
// Uses PROMPTKIT_ prefix by default
// e.g., PROMPTKIT_API_KEY becomes "api_key"
conv.SetVarsFromEnv("PROMPTKIT_")

// Or set individual env vars
import "os"
conv.SetVar("api_key", os.Getenv("MY_API_KEY"))
```

## Dynamic Context Updates

Update variables during conversation:

```go
conv, _ := sdk.Open("./support.pack.json", "support")

conv.SetVar("customer_name", "Unknown")

// First interaction
resp1, _ := conv.Send(ctx, "Hi, my name is Bob")

// Update context based on conversation
conv.SetVar("customer_name", "Bob")

// Now system prompt includes "The customer's name is Bob"
resp2, _ := conv.Send(ctx, "What products do you have?")
```

## Type-Safe Variables

Variables support any JSON-serializable type:

```go
// Strings
conv.SetVar("name", "Alice")

// Numbers
conv.SetVar("user_id", 12345)
conv.SetVar("score", 98.5)

// Booleans
conv.SetVar("is_premium", true)

// Slices
conv.SetVar("tags", []string{"vip", "enterprise"})

// Maps
conv.SetVar("metadata", map[string]any{
    "region": "us-west",
    "tier":   "gold",
})
```

## Concurrent Safety

Variables are thread-safe for concurrent access:

```go
var wg sync.WaitGroup

// Safe to set from multiple goroutines
for i := 0; i < 10; i++ {
    wg.Add(1)
    go func(n int) {
        defer wg.Done()
        conv.SetVar(fmt.Sprintf("var_%d", n), n)
    }(i)
}

wg.Wait()
```

## Forked Conversations

Forked conversations get their own variable scope:

```go
conv1, _ := sdk.Open("./pack.json", "chat")
conv1.SetVar("user", "Alice")

// Fork creates isolated copy
conv2 := conv1.Fork()
conv2.SetVar("user", "Bob")

// conv1 still has "Alice"
fmt.Println(conv1.GetVar("user"))  // "Alice"
fmt.Println(conv2.GetVar("user"))  // "Bob"
```

## What You've Learned

✅ Set variables with `SetVar()` and `SetVars()`  
✅ Get variables with `GetVar()`  
✅ Use variables in system prompts  
✅ Load from environment  
✅ Thread-safe concurrent access  
✅ Variable isolation in forks  

## Next Steps

- **[Tutorial 5: HITL](05-custom-pipelines)** - Approval workflows
- **[How-To: Manage State](../how-to/manage-state)** - Advanced state

## Complete Example

See the variable handling in `sdk/examples/hello/`.
