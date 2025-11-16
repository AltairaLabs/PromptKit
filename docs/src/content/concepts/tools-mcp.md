---
title: Tools & MCP
docType: guide
order: 6
---
# Tools & MCP

Understanding function calling and the Model Context Protocol.

## What are Tools?

**Tools** (also called "function calling") allow LLMs to execute code, query databases, call APIs, and interact with external systems.

## Why Tools?

**Extend capabilities**: LLMs can do more than generate text  
**Real-time data**: Access current information  
**Take actions**: Update databases, send emails, etc.  
**Accuracy**: Use code for math, not LLM reasoning  

## How Tools Work

1. **Define tools**: Describe available functions
2. **LLM decides**: When to use which tool
3. **Execute**: Run the tool and get results
4. **LLM responds**: Incorporate tool results in response

## Example Flow

```
User: "What's the weather in Paris?"
  ↓
LLM: "I should use the weather tool"
  ↓
Tool Call: get_weather(city="Paris")
  ↓
Tool Result: {"temp": 18, "condition": "cloudy"}
  ↓
LLM: "It's 18°C and cloudy in Paris"
```

## Tools in PromptKit

### Define Tools

```go
import "github.com/AltairaLabs/PromptKit/runtime/tools"

// Define tool
weatherTool := &tools.ToolDef{
    Name:        "get_weather",
    Description: "Get current weather for a city",
    Parameters: json.RawMessage(`{
        "type": "object",
        "properties": {
            "city": {
                "type": "string",
                "description": "City name"
            }
        },
        "required": ["city"]
    }`),
}
```

### Implement Executor

```go
type WeatherExecutor struct{}

func (e *WeatherExecutor) Execute(ctx context.Context, call *types.ToolCall) (string, error) {
    var params struct {
        City string `json:"city"`
    }
    json.Unmarshal(call.Arguments, &params)
    
    // Call weather API
    weather := getWeather(params.City)
    
    return fmt.Sprintf(`{"temp": %d, "condition": "%s"}`, weather.Temp, weather.Condition), nil
}

func (e *WeatherExecutor) Name() string {
    return "get_weather"
}
```

### Use in Pipeline

```go
// Create registry
registry := tools.NewToolRegistry()
registry.RegisterTool(weatherTool, &WeatherExecutor{})

// Add to pipeline
pipe := pipeline.NewPipeline(
    middleware.ToolMiddleware(registry),
    middleware.ProviderMiddleware(provider, nil, nil, nil),
)

// Execute
result, _ := pipe.Execute(ctx, "user", "What's the weather in Paris?")
// LLM will use the tool automatically
```

## Model Context Protocol (MCP)

**MCP** is a standard for connecting LLMs to external data sources and tools.

### What is MCP?

MCP provides:
- **Standard interface**: Connect any tool to any LLM
- **Tool discovery**: LLMs learn available tools
- **Secure execution**: Sandboxed tool execution
- **Composability**: Combine multiple tool servers

### MCP Architecture

```
LLM Application
      ↓
  MCP Client
      ↓
  MCP Server(s)
      ↓
External Systems (filesystem, databases, APIs)
```

## MCP in PromptKit

### Start MCP Server

```bash
# Filesystem server
npx @modelcontextprotocol/server-filesystem ~/documents

# Memory server
npx @modelcontextprotocol/server-memory
```

### Connect in Code

```go
import "github.com/AltairaLabs/PromptKit/runtime/mcp"

// Connect to MCP server
mcpClient, err := mcp.NewStdioClient("npx", []string{
    "@modelcontextprotocol/server-filesystem",
    "/path/to/files",
})
if err != nil {
    log.Fatal(err)
}
defer mcpClient.Close()

// Create MCP executor
executor := mcp.NewMCPExecutor(mcpClient)

// Register tools
registry := tools.NewToolRegistry()
mcpTools, _ := mcpClient.ListTools()
for _, tool := range mcpTools {
    registry.RegisterTool(tool, executor)
}

// Use in pipeline
pipe := pipeline.NewPipeline(
    middleware.ToolMiddleware(registry),
    middleware.ProviderMiddleware(provider, nil, nil, nil),
)
```

## Common Tools

### File Operations

```go
fileTools := []tools.ToolDef{
    {
        Name:        "read_file",
        Description: "Read contents of a file",
    },
    {
        Name:        "write_file",
        Description: "Write contents to a file",
    },
    {
        Name:        "list_directory",
        Description: "List files in a directory",
    },
}
```

### Database Queries

```go
dbTool := &tools.ToolDef{
    Name:        "query_database",
    Description: "Execute SQL query",
    Parameters: json.RawMessage(`{
        "type": "object",
        "properties": {
            "query": {"type": "string"}
        }
    }`),
}
```

### API Calls

```go
apiTool := &tools.ToolDef{
    Name:        "fetch_url",
    Description: "Fetch data from URL",
    Parameters: json.RawMessage(`{
        "type": "object",
        "properties": {
            "url": {"type": "string"}
        }
    }`),
}
```

### Calculations

```go
calcTool := &tools.ToolDef{
    Name:        "calculate",
    Description: "Perform mathematical calculation",
    Parameters: json.RawMessage(`{
        "type": "object",
        "properties": {
            "expression": {"type": "string"}
        }
    }`),
}
```

## Tool Design Best Practices

### Clear Descriptions

**Bad**:
```go
Description: "Gets stuff"
```

**Good**:
```go
Description: "Get current weather for a specified city. Returns temperature in Celsius and current conditions."
```

### Detailed Parameters

```go
Parameters: json.RawMessage(`{
    "type": "object",
    "properties": {
        "city": {
            "type": "string",
            "description": "City name (e.g., 'Paris', 'New York')"
        },
        "units": {
            "type": "string",
            "enum": ["celsius", "fahrenheit"],
            "description": "Temperature units",
            "default": "celsius"
        }
    },
    "required": ["city"]
}`)
```

### Error Handling

```go
func (e *WeatherExecutor) Execute(ctx context.Context, call *types.ToolCall) (string, error) {
    // Validate parameters
    if params.City == "" {
        return "", errors.New("city is required")
    }
    
    // Handle API errors
    weather, err := api.GetWeather(params.City)
    if err != nil {
        return "", fmt.Errorf("failed to get weather: %w", err)
    }
    
    // Return structured result
    return json.Marshal(weather)
}
```

### Security

✅ **Validate inputs**:
```go
if !isValidCity(params.City) {
    return "", errors.New("invalid city name")
}
```

✅ **Limit access**:
```go
// Only allow reading specific directories
allowedPaths := []string{"/data", "/docs"}
```

✅ **Timeout operations**:
```go
ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
defer cancel()
```

❌ **Don't expose sensitive data**:
```go
// Bad: Returns API keys
return fmt.Sprintf("API_KEY=%s", apiKey)

// Good: Returns only needed data
return fmt.Sprintf("weather=%s", weather)
```

## Testing Tools

### Unit Tests

```go
func TestWeatherTool(t *testing.T) {
    executor := &WeatherExecutor{}
    
    call := &types.ToolCall{
        Name: "get_weather",
        Arguments: json.RawMessage(`{"city": "Paris"}`),
    }
    
    result, err := executor.Execute(context.Background(), call)
    assert.NoError(t, err)
    assert.Contains(t, result, "temp")
}
```

### Integration Tests

```yaml
# arena.yaml
tests:
  - name: Weather Tool Test
    prompt: "What's the weather in Paris?"
    assertions:
      - type: tool_call
        tool_name: get_weather
      - type: contains
        value: "Paris"
```

## Monitoring Tools

### Track Usage

```go
type ToolMetrics struct {
    CallCount     map[string]int
    ErrorCount    map[string]int
    AvgLatency    map[string]time.Duration
}

func RecordToolCall(toolName string, duration time.Duration, err error) {
    metrics.CallCount[toolName]++
    if err != nil {
        metrics.ErrorCount[toolName]++
    }
    metrics.AvgLatency[toolName] = updateAverage(duration)
}
```

### Log Tool Calls

```go
logger.Info("tool executed",
    zap.String("tool", call.Name),
    zap.Duration("duration", duration),
    zap.Bool("success", err == nil),
)
```

## Common Patterns

### Tool Chaining

LLM uses multiple tools:

```
User: "Analyze the sales data"
  ↓
Tool 1: read_file("sales.csv")
  ↓
Tool 2: calculate("sum(column)")
  ↓
Tool 3: create_chart(data)
  ↓
Response: "Here's your sales analysis [chart]"
```

### Conditional Tools

```go
if userTier == "premium" {
    registry.RegisterTool(advancedAnalyticsTool, executor)
}
```

### Cached Tools

```go
type CachedExecutor struct {
    inner Executor
    cache map[string]string
}

func (e *CachedExecutor) Execute(ctx context.Context, call *types.ToolCall) (string, error) {
    key := getCacheKey(call)
    if cached, ok := e.cache[key]; ok {
        return cached, nil
    }
    
    result, err := e.inner.Execute(ctx, call)
    if err == nil {
        e.cache[key] = result
    }
    return result, err
}
```

## Summary

Tools & MCP provide:

✅ **Extended capabilities** - LLMs can do more  
✅ **Real-time data** - Access current information  
✅ **Actions** - Interact with external systems  
✅ **Standardization** - MCP provides common interface  
✅ **Composability** - Combine multiple tools  

## Related Documentation

- [Integrate Tools](../runtime/how-to/integrate-tools) - Implementation guide
- [MCP Integration Tutorial](../runtime/tutorials/03-mcp-integration) - Step-by-step guide
- [Tools Reference](../runtime/reference/tools) - API documentation
- [MCP Architecture](../runtime/explanation/runtime-tools-mcp) - Technical details
