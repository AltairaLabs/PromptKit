---
layout: default
title: tools
parent: SDK Examples
grand_parent: Guides
---

# Tools Example

This example demonstrates how to use tools with the PromptKit SDK to enable LLMs to interact with external functions and APIs.

## What are Tools?

Tools (also known as function calling) allow LLMs to:
- Call external functions to get real-time data
- Perform calculations or complex operations
- Interact with APIs and services
- Access databases or knowledge bases

When the LLM determines it needs a tool, it:
1. Decides which tool to call
2. Generates the appropriate arguments
3. The tool executes and returns results
4. The LLM incorporates the results into its response

## How It Works

### 1. Create a Tool Registry

```go
import "github.com/AltairaLabs/PromptKit/runtime/tools"

toolRegistry := tools.NewRegistry()
```

### 2. Register Tools

Tools are defined using `ToolDescriptor` with JSON Schema for validation:

```go
weatherTool := &tools.ToolDescriptor{
    Name:        "get_weather",
    Description: "Get current weather for a location",
    InputSchema: json.RawMessage(`{
        "type": "object",
        "properties": {
            "location": {
                "type": "string",
                "description": "City name"
            }
        },
        "required": ["location"]
    }`),
    OutputSchema: json.RawMessage(`{
        "type": "object",
        "properties": {
            "temperature": {"type": "number"},
            "conditions": {"type": "string"}
        }
    }`),
    Mode: "mock",  // or "live" for HTTP calls
    MockResult: json.RawMessage(`{
        "temperature": 72,
        "conditions": "Sunny"
    }`),
    TimeoutMs: 2000,
}

toolRegistry.Register(weatherTool)
```

### 3. Create Manager with Tool Registry

```go
manager, err := sdk.NewConversationManager(
    sdk.WithProvider(provider),
    sdk.WithToolRegistry(toolRegistry),  // Pass the registry
)
```

### 4. Configure Tool Policy

In your Pack's Prompt configuration:

```go
Prompts: map[string]*sdk.Prompt{
    "assistant": {
        ID:   "assistant",
        Name: "Assistant",
        SystemTemplate: "You are a helpful assistant with access to tools.",
        ToolPolicy: &sdk.ToolPolicy{
            ToolChoice:          "auto",  // "auto", "required", or "none"
            MaxToolCallsPerTurn: 5,       // Limit calls per turn
        },
        // ... other config
    },
}
```

## Tool Modes

### Mock Tools (`mode: "mock"`)

Best for:
- Development and testing
- Prototyping without external dependencies
- Demonstrations and examples
- CI/CD environments

Mock tools return predefined responses:

```go
Mode: "mock",
MockResult: json.RawMessage(`{"result": "static data"}`),
```

### Live Tools (`mode: "live"`)

Best for:
- Production environments
- Real-time data requirements
- External API integration

Live tools make HTTP requests:

```go
Mode: "live",
HTTPConfig: &tools.HTTPConfig{
    URL:       "https://api.example.com/weather",
    Method:    "GET",
    TimeoutMs: 3000,
    Headers: map[string]string{
        "Authorization": "Bearer token",
    },
},
```

### MCP Tools (`mode: "mcp"`)

Best for:
- Model Context Protocol servers
- Standardized tool interfaces
- Multi-model compatibility

MCP tools integrate with MCP servers for standardized tool execution.

## Tool Policy Options

### ToolChoice

- `"auto"`: LLM decides when to use tools (default)
- `"required"`: LLM must use at least one tool
- `"none"`: Disable tool usage for this conversation

### MaxToolCallsPerTurn

Limits the number of tool calls the LLM can make in a single turn. Prevents:
- Infinite loops
- Excessive API calls
- Cost overruns

### Blocklist

Prevent specific tools from being used:

```go
ToolPolicy: &sdk.ToolPolicy{
    ToolChoice: "auto",
    Blocklist: []string{"dangerous_tool", "expensive_api"},
}
```

## Example Tools

The example demonstrates three tools:

### 1. Get Current Time
- **Use case**: Provide current time information
- **Arguments**: None
- **Returns**: Time and timezone

### 2. Get Weather
- **Use case**: Weather information for locations
- **Arguments**: `location` (string)
- **Returns**: Temperature, conditions, humidity, wind

### 3. Calculator
- **Use case**: Mathematical operations
- **Arguments**: `operation`, `a`, `b`
- **Returns**: Calculation result

## Running the Example

```bash
# Set your OpenAI API key
export OPENAI_API_KEY="your-api-key-here"

# Run the example
go run main.go
```

## Expected Output

The example runs three scenarios:

1. **Time Query**: LLM calls `get_current_time` tool
2. **Weather Query**: LLM calls `get_weather` with location argument
3. **Calculator**: LLM calls `calculator` with operation and operands

You'll see:
- User messages
- Assistant responses (incorporating tool results)
- Tool calls made (with arguments)
- Cost and latency metrics
- Conversation history

## Best Practices

### 1. Clear Tool Descriptions
Write detailed descriptions so the LLM knows when to use each tool:

```go
Description: "Get current weather for a specific location. Use this when users ask about weather, temperature, or conditions."
```

### 2. Proper Input Schemas
Use JSON Schema to validate tool arguments:

```go
InputSchema: json.RawMessage(`{
    "type": "object",
    "properties": {
        "location": {
            "type": "string",
            "description": "City name, e.g., 'San Francisco'"
        }
    },
    "required": ["location"]
}`)
```

### 3. Set Appropriate Timeouts
Balance responsiveness with reliability:

```go
TimeoutMs: 5000,  // 5 seconds for API calls
```

### 4. Handle Tool Failures
Tools can fail - handle gracefully:

```go
if resp.ToolCalls[0].Error != "" {
    log.Printf("Tool failed: %s", resp.ToolCalls[0].Error)
}
```

### 5. Monitor Tool Usage
Track tool calls for cost and performance:

```go
if len(resp.ToolCalls) > 0 {
    log.Printf("Made %d tool calls", len(resp.ToolCalls))
    for _, tc := range resp.ToolCalls {
        log.Printf("Tool: %s, Args: %s", tc.Name, tc.Args)
    }
}
```

## Common Patterns

### Conditional Tool Availability

Different tools for different scenarios:

```go
// Support agent - limited tools
supportTools := []string{"get_order_status", "check_inventory"}

// Admin - full access
adminTools := []string{"get_order_status", "check_inventory", "cancel_order", "refund"}
```

### Tool Chaining

LLMs can call multiple tools in sequence:

```go
ToolPolicy: &sdk.ToolPolicy{
    ToolChoice:          "auto",
    MaxToolCallsPerTurn: 3,  // Allow chaining up to 3 tools
}
```

### Fallback Handling

Handle cases when tools are unavailable:

```go
SystemTemplate: `You have access to tools for real-time data.
If a tool fails, apologize and use your general knowledge instead.`
```

## Troubleshooting

### Tool Not Being Called

- Check tool description is clear and relevant
- Verify `ToolChoice` is not set to `"none"`
- Ensure tool is registered in the registry
- Review system prompt includes tool usage instructions

### Invalid Tool Arguments

- Validate InputSchema is correct JSON Schema
- Check if tool description mentions required parameters
- Review example usage in description

### Tool Execution Failures

- Verify timeout is sufficient
- Check HTTPConfig for live tools
- Review mock data format for mock tools
- Check tool executor is registered

## Next Steps

- Add your own custom tools
- Integrate with real APIs (live mode)
- Implement MCP tool servers
- Add retry logic for failed tools
- Monitor tool usage metrics
- Create domain-specific tool sets
