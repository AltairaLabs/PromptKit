---
title: 'Tutorial 3: Tool integration'
sidebar:
  order: 3
verified:
  commit: 004b9cbbd4d40f770a2e3c76053b600a55189355
  sources:
    - runtime/pipeline/stage/stages_provider.go
    - runtime/prompt/schema/promptpack.schema.json
    - sdk/conversation.go
    - sdk/conversation_tools.go
    - sdk/examples/tools/main_interactive.go
    - sdk/tool_executors.go
---
## What we will build

In this tutorial we build a weather and time assistant. The pack declares two tools, and Go handlers answer the model's calls. Without tools the model cannot read live data; with tools it calls our handler and uses the result.

## Prerequisites

Complete [Tutorial 1: Your first conversation](/sdk/tutorials/01-first-conversation/). We reuse its project and the `OPENAI_API_KEY` it sets.

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

The prompt's `tools` list names the tools the model may call, and the top-level `tools` object describes each one's parameters. The pack loads when we open it in Step 2.

## Step 2: Register tool handlers

`OnTool()` registers a handler by tool name. Replace `main.go` with:

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

    // Register the time tool
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

    // Register the weather tool, with simulated data for London only
    conv.OnTool("get_weather", func(args map[string]any) (any, error) {
        city, _ := args["city"].(string)
        if city != "London" {
            return nil, fmt.Errorf("no weather data for %q", city)
        }
        return map[string]any{
            "city":        city,
            "temperature": 22.5,
            "conditions":  "Partly cloudy",
            "humidity":    65,
        }, nil
    })

    // Ask about the weather: the model calls get_weather
    ctx := context.Background()
    resp, err := conv.Send(ctx, "What's the weather in London?")
    if err != nil {
        log.Fatal(err)
    }

    fmt.Println(resp.Text())
}
```

A handler receives the arguments the model chose as a `map[string]any` and returns any JSON-serializable value or an error. The SDK serializes the value to JSON and sends it to the model as the tool result.

## Step 3: Run it

Run the program:

```bash
go run main.go
```

The model typically calls `get_weather` and answers with the handler's data, for example:

```text
It's currently 22.5°C and partly cloudy in London, with 65% humidity.
```

## Step 4: See a tool failure

The weather handler returns an error for any city but London. In `main.go`, change the question to:

```go
resp, err := conv.Send(ctx, "What's the weather in Atlantis?")
```

Run it with `go run main.go`. The model receives `Tool execution failed: no weather data for "Atlantis"` as the tool result, and the reply typically says it could not get the weather for Atlantis.

## Next steps

- [Tutorial 4: Variables and templates](/sdk/tutorials/04-state-management/) covers template variables.
- [Tutorial 5: Human-in-the-loop approval](/sdk/tutorials/05-custom-pipelines/) adds approval workflows.
- [Register tools](/sdk/how-to/tools/register-tools/) covers context-aware, HTTP and typed handlers.
- The complete example is in `sdk/examples/tools/`.
