---
title: Register Tools
docType: how-to
order: 3
---
# How to Register Tools

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

- [HTTP Tools](http-tools)
- [HITL Workflows](hitl-workflows)
- [Tutorial 3: Tools](../tutorials/03-tool-integration)
