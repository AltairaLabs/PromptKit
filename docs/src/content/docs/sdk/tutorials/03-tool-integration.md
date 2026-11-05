---
title: 'Tutorial 3: Tool Integration'
sidebar:
  order: 3
verified:
  commit: 885cafa787a84bfba66f158fe51cda15a077ec23
  sources:
    - runtime/pipeline/stage/stages_provider.go
    - runtime/prompt/schema/promptpack.schema.json
    - sdk/conversation.go
    - sdk/conversation_tools.go
    - sdk/examples/tools/main_interactive.go
    - sdk/tool_executors.go
    - sdk/tools/http.go
---
Give the model tools: declare them in a pack and register Go handlers with `OnTool()`.

## What you will build

We build a weather and time assistant. The pack declares two tools, and Go handlers answer the model's calls. Without tools the model cannot read live data; with tools it calls your handler and uses the result.

## Prerequisites

Complete [Tutorial 1](/sdk/tutorials/01-first-conversation/).

## Step 1: Create a pack with tools

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

## Step 2: Register tool handlers

`OnTool()` registers a handler by tool name. We create `main.go`:

```go
package main

import (
    "context"
    "fmt"
    "log"
    "time"

    "github.com/AltairaLabs/PromptKit/sdk/v2"
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

    // Ask about weather: the model calls the tool
    ctx := context.Background()
    resp, err := conv.Send(ctx, "What's the weather in London?")
    if err != nil {
        log.Fatal(err)
    }

    fmt.Println(resp.Text())
}
```

## Step 3: Run it

We set the API key as in Tutorial 1 and run the program:

```bash
go run main.go
```

The model calls `get_weather` and answers with the handler's data, for example:

```text
It's currently 22.5°C and partly cloudy in London, with 65% humidity.
```

## Understanding OnTool

A handler receives the arguments the model chose, as a `map[string]any`, and returns any JSON-serializable value, or an error:

```go
conv.OnTool("tool_name", func(args map[string]any) (any, error) {
    return result, nil
})
```

The SDK serializes the return value to JSON and sends it to the model as the tool result. See [Register Tools](/sdk/how-to/tools/register-tools/) for context-aware handlers, HTTP tools and typed handlers.

## Error handling

Return an error when the tool fails:

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

The model receives the error text as the tool result and can tell the user.

## Next steps

- [Tutorial 4: Variables](/sdk/tutorials/04-state-management/): template variables
- [Tutorial 5: Human-in-the-Loop](/sdk/tutorials/05-custom-pipelines/): approval workflows
- [Register Tools](/sdk/how-to/tools/register-tools/): context-aware, HTTP and typed handlers

The complete example is in `sdk/examples/tools/`.
