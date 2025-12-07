# Tools Example

Function calling with SDK v2.

## What You'll Learn

- Defining tools in pack files
- Registering tool handlers with `OnTool()`
- Automatic JSON serialization
- Multiple tool handlers with `OnTools()`

## Prerequisites

- Go 1.21+
- OpenAI API key

## Running the Example

```bash
export OPENAI_API_KEY=your-key
go run .
```

## Code Overview

```go
conv, err := sdk.Open("./tools.pack.json", "assistant")
if err != nil {
    log.Fatal(err)
}
defer conv.Close()

// Register tool handlers
conv.OnTool("get_time", func(args map[string]any) (any, error) {
    timezone := args["timezone"].(string)
    return map[string]string{
        "time":     "14:30:00",
        "timezone": timezone,
    }, nil
})

conv.OnTool("get_weather", func(args map[string]any) (any, error) {
    city := args["city"].(string)
    return map[string]any{
        "city":        city,
        "temperature": 22.5,
        "conditions":  "Sunny",
    }, nil
})

// LLM will automatically call tools when needed
resp, _ := conv.Send(ctx, "What's the weather in London?")
fmt.Println(resp.Text())
```

## Pack File Structure

Tools must be:
1. Defined in the `tools` section
2. Listed in the prompt's `tools` array

```json
{
  "prompts": {
    "assistant": {
      "system_template": "You are a helpful assistant...",
      "tools": ["get_weather", "get_time"]
    }
  },
  "tools": {
    "get_weather": {
      "name": "get_weather",
      "description": "Get current weather for a city",
      "parameters": {
        "type": "object",
        "properties": {
          "city": { "type": "string" },
          "country": { "type": "string" }
        },
        "required": ["city", "country"]
      }
    }
  }
}
```

## Handler Signature

```go
func(args map[string]any) (any, error)
```

- **args**: Parameters from the LLM (matches tool schema)
- **return**: Any JSON-serializable value, or error

## Multiple Tools

Register all handlers at once:

```go
conv.OnTools(map[string]sdk.ToolHandler{
    "get_time":    timeHandler,
    "get_weather": weatherHandler,
    "search":      searchHandler,
})
```

## Return Structs

Return Go structs - they're automatically serialized:

```go
type Weather struct {
    City        string  `json:"city"`
    Temperature float64 `json:"temperature"`
}

conv.OnTool("get_weather", func(args map[string]any) (any, error) {
    return Weather{City: "London", Temperature: 18.5}, nil
})
```

## Key Concepts

1. **Declarative Tools** - Define in pack, implement in code
2. **Auto Serialization** - Return any Go value
3. **Error Handling** - Return errors to inform the LLM
4. **Tool Calls** - LLM decides when to use tools

## Next Steps

- [Hello Example](../hello/) - Basic conversation
- [Streaming Example](../streaming/) - Real-time responses
- [HITL Example](../hitl/) - Human-in-the-loop approval
