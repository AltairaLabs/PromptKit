---
title: Register Tools
sidebar:
  order: 3
---
Learn how to add function calling with `OnTool()`.

## Basic Tool Registration

```go
conv.OnTool("get_time", func(args map[string]any) (any, error) {
    return time.Now().Format(time.RFC3339), nil
})
```

## Handler Signature

```go
func(args map[string]any) (any, error)
```

- **args**: Parameters from the LLM (matches tool schema)
- **return**: Any JSON-serializable value, or error

## Accessing Arguments

```go
conv.OnTool("search", func(args map[string]any) (any, error) {
    // String argument
    query, _ := args["query"].(string)
    
    // Number argument (JSON numbers are float64)
    limit, _ := args["limit"].(float64)
    
    // Boolean argument
    exact, _ := args["exact"].(bool)
    
    return searchResults(query, int(limit), exact), nil
})
```

## Return Values

### Return Map

```go
conv.OnTool("get_user", func(args map[string]any) (any, error) {
    return map[string]any{
        "id":    "123",
        "name":  "Alice",
        "email": "alice@example.com",
    }, nil
})
```

### Return Struct

```go
type User struct {
    ID    string `json:"id"`
    Name  string `json:"name"`
    Email string `json:"email"`
}

conv.OnTool("get_user", func(args map[string]any) (any, error) {
    return User{
        ID:    "123",
        Name:  "Alice",
        Email: "alice@example.com",
    }, nil
})
```

### Return Slice

```go
conv.OnTool("list_items", func(args map[string]any) (any, error) {
    return []string{"item1", "item2", "item3"}, nil
})
```

## With Context

Use `OnToolCtx` when you need the context:

```go
conv.OnToolCtx("fetch_data", func(ctx context.Context, args map[string]any) (any, error) {
    // Use context for HTTP calls, database queries, etc.
    url := args["url"].(string)
    
    req, _ := http.NewRequestWithContext(ctx, "GET", url, nil)
    resp, err := http.DefaultClient.Do(req)
    if err != nil {
        return nil, err
    }
    defer resp.Body.Close()
    
    // Process response...
    return result, nil
})
```

## Multiple Tools

Register multiple tools at once:

```go
conv.OnTools(map[string]sdk.ToolHandler{
    "get_time": func(args map[string]any) (any, error) {
        return time.Now().Format(time.RFC3339), nil
    },
    "get_weather": func(args map[string]any) (any, error) {
        city := args["city"].(string)
        return map[string]any{"city": city, "temp": 22}, nil
    },
    "search": func(args map[string]any) (any, error) {
        query := args["query"].(string)
        return []string{"result1", "result2"}, nil
    },
})
```

## Typed Handlers

For type-safe tool arguments, use `tools.OnTyped`:

```go
import sdktools "github.com/AltairaLabs/PromptKit/sdk/tools"

type SearchArgs struct {
    Query      string `map:"query"`
    MaxResults int    `map:"max_results"`
}

sdktools.OnTyped(conv, "search", func(args SearchArgs) (any, error) {
    return searchAPI(args.Query, args.MaxResults), nil
})
```

## HTTP Tools

Register tools that call external APIs:

```go
import sdktools "github.com/AltairaLabs/PromptKit/sdk/tools"

conv.OnToolHTTP("create_ticket", sdktools.NewHTTPToolConfig(
    "https://api.example.com/tickets",
    sdktools.WithMethod("POST"),
    sdktools.WithHeader("Authorization", "Bearer "+apiKey),
))
```

See [HTTP Tools](/sdk/how-to/http-tools/) for the full builder API.

## Custom Executors

Register a runtime `tools.Executor` implementation directly:

```go
conv.OnToolExecutor("custom_tool", &MyCustomExecutor{})
```

The executor must implement `runtime/tools.Executor`.

## Async Tools (HITL)

Register tools that require human approval before execution:

```go
conv.OnToolAsync("process_refund",
    func(args map[string]any) sdktools.PendingResult {
        amount := args["amount"].(float64)
        if amount > 1000 {
            return sdktools.PendingResult{
                Reason:  "high_value_refund",
                Message: fmt.Sprintf("Refund of $%.2f requires approval", amount),
            }
        }
        return sdktools.PendingResult{} // proceed immediately
    },
    func(args map[string]any) (any, error) {
        return refundAPI.Process(args)
    },
)

resp, _ := conv.Send(ctx, "Process refund for order #123")
for _, pending := range resp.PendingTools() {
    conv.ResolveTool(pending.ID)  // or conv.RejectTool(pending.ID, "reason")
}
resp, _ = conv.Continue(ctx)
```

## Error Handling

Return errors to inform the LLM:

```go
conv.OnTool("get_user", func(args map[string]any) (any, error) {
    userID := args["user_id"].(string)
    
    user, err := db.GetUser(userID)
    if err != nil {
        return nil, fmt.Errorf("user not found: %s", userID)
    }
    
    return user, nil
})
```

The LLM receives the error and can inform the user appropriately.

## Define Tools in Pack

Tools must be defined in your pack file:

```json
{
  "tools": {
    "get_time": {
      "name": "get_time",
      "description": "Get the current time",
      "parameters": {
        "type": "object",
        "properties": {
          "timezone": {
            "type": "string",
            "description": "Timezone (e.g., UTC, EST)"
          }
        }
      }
    }
  },
  "prompts": {
    "assistant": {
      "tools": ["get_time"]
    }
  }
}
```

## Complete Example

```go
package main

import (
    "context"
    "fmt"
    "log"
    "time"

    "github.com/AltairaLabs/PromptKit/sdk"
)

func main() {
    conv, _ := sdk.Open("./tools.pack.json", "assistant")
    defer conv.Close()

    // Register tools
    conv.OnTool("get_time", func(args map[string]any) (any, error) {
        tz := "UTC"
        if t, ok := args["timezone"].(string); ok {
            tz = t
        }
        return map[string]any{
            "time":     time.Now().Format("15:04:05"),
            "timezone": tz,
        }, nil
    })

    conv.OnTool("get_weather", func(args map[string]any) (any, error) {
        city, _ := args["city"].(string)
        return map[string]any{
            "city":       city,
            "temp":       22.5,
            "conditions": "Sunny",
        }, nil
    })

    // Use tools
    ctx := context.Background()
    resp, _ := conv.Send(ctx, "What's the weather in London?")
    fmt.Println(resp.Text())
}
```

## See Also

- [HTTP Tools](/sdk/how-to/http-tools/) — builder API for HTTP tool handlers
- [Exec Tools](/sdk/how-to/exec-tools/) — bind tools to external subprocesses (Python, Node.js, etc.)
- [Client-Side Tools](/sdk/how-to/client-tools/) — tools that run on the caller's device
- [Tutorial 3: Tools](/sdk/tutorials/03-tool-integration/) — step-by-step walkthrough
