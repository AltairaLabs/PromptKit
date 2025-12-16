---
title: 'Tutorial 3: Tool Integration'
docType: tutorial
order: 3
---
# Tutorial 3: Tool Integration

Learn how to add function calling capabilities to your LLM with tools.

## What You'll Learn

- Register tool handlers with `OnTool()`
- Handle tool arguments and return values
- Build a weather assistant
- Use HTTP tools for external APIs

## Why Tools?

Tools let LLMs perform actions and access real-time data:

**Without Tools:**
```
User: What's the weather in Paris?
LLM: I don't have access to real-time weather data.
```

**With Tools:**
```
User: What's the weather in Paris?
LLM: [calls weather tool]
LLM: It's currently 18°C and partly cloudy in Paris.
```

## Prerequisites

Complete [Tutorial 1](01-first-conversation) and understand basic SDK usage.

## Step 1: Create a Pack with Tools

Create `tools.pack.json`:

```json
{
  "id": "tools-demo",
  "name": "Tools Demo",
  "version": "1.0.0",
  "template_engine": {
    "version": "v1",
    "syntax": "{{variable}}"
  },
  "prompts": {
    "assistant": {
      "id": "assistant",
      "name": "Tool Assistant",
      "version": "1.0.0",
      "system_template": "You are a helpful assistant. Use tools when needed.",
      "tools": ["get_time", "get_weather"]
    }
  },
  "tools": {
    "get_time": {
      "name": "get_time",
      "description": "Get the current time in a timezone",
      "parameters": {
        "type": "object",
        "properties": {
          "timezone": {
            "type": "string",
            "description": "Timezone (e.g., UTC, EST, PST)"
          }
        }
      }
    },
    "get_weather": {
      "name": "get_weather",
      "description": "Get weather for a location",
      "parameters": {
        "type": "object",
        "properties": {
          "city": {
            "type": "string",
            "description": "City name"
          },
          "country": {
            "type": "string",
            "description": "Country code"
          }
        },
        "required": ["city"]
      }
    }
  }
}
```

## Step 2: Register Tool Handlers

Use `OnTool()` to register handlers:

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
    conv, err := sdk.Open("./tools.pack.json", "assistant")
    if err != nil {
        log.Fatal(err)
    }
    defer conv.Close()

    // Register time tool
    conv.OnTool("get_time", func(args map[string]any) (any, error) {
        timezone := "UTC"
        if tz, ok := args["timezone"].(string); ok && tz != "" {
            timezone = tz
        }
        return map[string]string{
            "time":     time.Now().Format("15:04:05"),
            "timezone": timezone,
            "date":     time.Now().Format("2006-01-02"),
        }, nil
    })

    // Register weather tool
    conv.OnTool("get_weather", func(args map[string]any) (any, error) {
        city, _ := args["city"].(string)
        country, _ := args["country"].(string)
        
        // Simulated weather data
        return map[string]any{
            "city":        city,
            "country":     country,
            "temperature": 22.5,
            "conditions":  "Partly cloudy",
            "humidity":    65,
        }, nil
    })

    // Ask about weather - LLM will call the tool
    ctx := context.Background()
    resp, err := conv.Send(ctx, "What's the weather in London?")
    if err != nil {
        log.Fatal(err)
    }

    fmt.Println(resp.Text())
}
```

## Understanding OnTool

### Handler Signature

```go
conv.OnTool("tool_name", func(args map[string]any) (any, error) {
    // args contains the parameters from the LLM
    // Return any JSON-serializable value
    return result, nil
})
```

### Automatic JSON Serialization

Return structs directly - they're automatically serialized:

```go
type Weather struct {
    City        string  `json:"city"`
    Temperature float64 `json:"temperature"`
    Conditions  string  `json:"conditions"`
}

conv.OnTool("get_weather", func(args map[string]any) (any, error) {
    return Weather{
        City:        args["city"].(string),
        Temperature: 22.5,
        Conditions:  "Sunny",
    }, nil
})
```

## Tool with Context

Use `OnToolCtx` when you need the context:

```go
conv.OnToolCtx("search", func(ctx context.Context, args map[string]any) (any, error) {
    query := args["query"].(string)
    
    // Use context for timeouts, cancellation
    results, err := searchAPI(ctx, query)
    if err != nil {
        return nil, err
    }
    
    return results, nil
})
```

## HTTP Tools

For external API calls, use HTTP tools:

```go
import "github.com/AltairaLabs/PromptKit/sdk/tools"

conv.OnToolHTTP("stock_price", &tools.HTTPToolConfig{
    BaseURL: "https://api.stocks.example.com",
    Method:  "GET",
    Path:    "/v1/price",
    Headers: map[string]string{
        "Authorization": "Bearer " + apiKey,
    },
    QueryParams: map[string]string{
        "symbol": "{{symbol}}",  // From tool args
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

The LLM will receive the error and can inform the user.

## Multiple Tools Example

Register all tools at once:

```go
conv.OnTools(map[string]sdk.ToolHandler{
    "get_time": func(args map[string]any) (any, error) {
        return time.Now().Format(time.RFC3339), nil
    },
    "get_weather": func(args map[string]any) (any, error) {
        return map[string]any{"temp": 22}, nil
    },
    "search": func(args map[string]any) (any, error) {
        return []string{"result1", "result2"}, nil
    },
})
```

## What You've Learned

✅ Define tools in pack files  
✅ Register handlers with `OnTool()`  
✅ Handle arguments and return values  
✅ Use `OnToolCtx` for context access  
✅ HTTP tools for external APIs  

## Next Steps

- **[Tutorial 4: Variables](04-state-management)** - Template variables
- **[Tutorial 5: HITL](05-custom-pipelines)** - Approval workflows
- **[How-To: Register Tools](../how-to/register-tools)** - Advanced patterns

## Complete Example

See the full example at `sdk/examples/tools/`.
