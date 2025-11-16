---
layout: docs
title: "Tutorial 3: Tool Integration"
nav_order: 3
parent: SDK Tutorials
grand_parent: SDK
---

# Tutorial 3: Tool Integration

Learn how to add function calling capabilities to your LLM with tools.

## What You'll Learn

- Register tools with the SDK
- Handle tool calls from the LLM
- Build a weather assistant with tools
- Implement tool error handling

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

Complete [Tutorial 1](01-first-conversation.md) and understand basic SDK usage.

## Step 1: Create a Simple Tool

Let's start with a calculator tool:

```go
package main

import (
    "context"
    "fmt"
    "log"
    "os"

    "github.com/AltairaLabs/PromptKit/sdk"
    "github.com/AltairaLabs/PromptKit/runtime/providers"
    "github.com/AltairaLabs/PromptKit/runtime/tools"
)

// Calculator tool function
func calculate(ctx context.Context, args map[string]interface{}) (interface{}, error) {
    expression := args["expression"].(string)
    
    // Simple evaluation (in production, use a safe eval library)
    switch expression {
    case "2+2":
        return 4, nil
    case "10*5":
        return 50, nil
    default:
        return fmt.Sprintf("Calculated: %s", expression), nil
    }
}

func main() {
    ctx := context.Background()

    // 1. Create tool registry
    registry := tools.NewRegistry()

    // 2. Register calculator tool
    registry.Register("calculator", &tools.Tool{
        Name:        "calculator",
        Description: "Perform mathematical calculations",
        Parameters: map[string]interface{}{
            "type": "object",
            "properties": map[string]interface{}{
                "expression": map[string]interface{}{
                    "type":        "string",
                    "description": "Math expression to evaluate (e.g., '2+2')",
                },
            },
            "required": []string{"expression"},
        },
        Function: calculate,
    })

    // 3. Create manager with tools
    apiKey := os.Getenv("OPENAI_API_KEY")
    provider := providers.NewOpenAIProvider(apiKey, "gpt-4o", false)
    
    manager, err := sdk.NewConversationManager(
        sdk.WithProvider(provider),
        sdk.WithToolRegistry(registry),
    )
    if err != nil {
        log.Fatal(err)
    }

    // 4. Update pack to include tools
    pack, err := manager.LoadPack("./assistant.pack.json")
    if err != nil {
        log.Fatal(err)
    }

    conv, err := manager.NewConversation(ctx, pack, sdk.ConversationConfig{
        UserID:     "user123",
        PromptName: "assistant",
    })
    if err != nil {
        log.Fatal(err)
    }

    // 5. Ask LLM to use the tool
    response, err := conv.Send(ctx, "What is 2+2?")
    if err != nil {
        log.Fatal(err)
    }

    fmt.Println("Assistant:", response.Content)  // "2+2 equals 4"
}
```

Update `assistant.pack.json` to enable tools:

```json
{
  "version": "1.0",
  "prompts": {
    "assistant": {
      "name": "assistant",
      "description": "A helpful AI assistant with tools",
      "system_prompt": "You are a helpful assistant. Use available tools when needed.",
      "available_tools": ["calculator"],
      "model_config": {
        "temperature": 0.7,
        "max_tokens": 1000
      }
    }
  }
}
```

Run it:

```bash
go run main.go
```

The LLM automatically calls your calculator tool!

## Step 2: Build a Weather Assistant

Now let's build something more useful - a weather assistant:

```go
package main

import (
    "context"
    "fmt"
    "log"
    "os"

    "github.com/AltairaLabs/PromptKit/sdk"
    "github.com/AltairaLabs/PromptKit/runtime/providers"
    "github.com/AltairaLabs/PromptKit/runtime/tools"
)

// Mock weather data (in production, call a real API)
var weatherData = map[string]map[string]interface{}{
    "paris": {
        "temperature": 18,
        "conditions":  "Partly cloudy",
        "humidity":    65,
    },
    "london": {
        "temperature": 15,
        "conditions":  "Rainy",
        "humidity":    80,
    },
    "tokyo": {
        "temperature": 22,
        "conditions":  "Sunny",
        "humidity":    55,
    },
}

// Get weather for a location
func getWeather(ctx context.Context, args map[string]interface{}) (interface{}, error) {
    location := args["location"].(string)
    
    // Normalize location
    location = strings.ToLower(location)
    
    // Look up weather
    weather, ok := weatherData[location]
    if !ok {
        return nil, fmt.Errorf("weather data not available for %s", location)
    }
    
    return weather, nil
}

func main() {
    ctx := context.Background()

    // Register weather tool
    registry := tools.NewRegistry()
    registry.Register("get_weather", &tools.Tool{
        Name:        "get_weather",
        Description: "Get current weather for a location",
        Parameters: map[string]interface{}{
            "type": "object",
            "properties": map[string]interface{}{
                "location": map[string]interface{}{
                    "type":        "string",
                    "description": "City name (e.g., Paris, London, Tokyo)",
                },
            },
            "required": []string{"location"},
        },
        Function: getWeather,
    })

    // Setup
    apiKey := os.Getenv("OPENAI_API_KEY")
    provider := providers.NewOpenAIProvider(apiKey, "gpt-4o", false)
    
    manager, err := sdk.NewConversationManager(
        sdk.WithProvider(provider),
        sdk.WithToolRegistry(registry),
    )
    if err != nil {
        log.Fatal(err)
    }

    pack, err := manager.LoadPack("./weather.pack.json")
    if err != nil {
        log.Fatal(err)
    }

    conv, err := manager.NewConversation(ctx, pack, sdk.ConversationConfig{
        UserID:     "user123",
        PromptName: "weather_assistant",
    })
    if err != nil {
        log.Fatal(err)
    }

    // Try weather queries
    queries := []string{
        "What's the weather in Paris?",
        "Is it raining in London?",
        "Compare weather in Tokyo and Paris",
    }

    for _, query := range queries {
        fmt.Printf("\nUser: %s\n", query)
        
        response, err := conv.Send(ctx, query)
        if err != nil {
            log.Printf("Error: %v", err)
            continue
        }
        
        fmt.Printf("Assistant: %s\n", response.Content)
    }
}
```

Create `weather.pack.json`:

```json
{
  "version": "1.0",
  "prompts": {
    "weather_assistant": {
      "name": "weather_assistant",
      "description": "Weather information assistant",
      "system_prompt": "You are a helpful weather assistant. Use the get_weather tool to provide accurate weather information. Format responses in a friendly, conversational way.",
      "available_tools": ["get_weather"],
      "model_config": {
        "temperature": 0.7,
        "max_tokens": 500
      }
    }
  }
}
```

Run it:

```bash
go run main.go
```

Example output:

```
User: What's the weather in Paris?
Assistant: It's currently 18°C in Paris with partly cloudy skies and 65% humidity.

User: Is it raining in London?
Assistant: Yes, it's currently raining in London with a temperature of 15°C.

User: Compare weather in Tokyo and Paris
Assistant: Tokyo is warmer and sunnier at 22°C with clear skies, while Paris is cooler at 18°C with partly cloudy conditions.
```

## Step 3: Multiple Tools

Register multiple tools for more capabilities:

```go
// Search tool
func searchWeb(ctx context.Context, args map[string]interface{}) (interface{}, error) {
    query := args["query"].(string)
    
    // Mock search results
    return []map[string]string{
        {"title": "Result 1", "url": "https://example.com/1"},
        {"title": "Result 2", "url": "https://example.com/2"},
    }, nil
}

// Time tool
func getCurrentTime(ctx context.Context, args map[string]interface{}) (interface{}, error) {
    timezone := args["timezone"].(string)
    
    // Mock time data
    times := map[string]string{
        "UTC":    "14:30",
        "PST":    "06:30",
        "JST":    "23:30",
    }
    
    return times[timezone], nil
}

func main() {
    registry := tools.NewRegistry()
    
    // Register multiple tools
    registry.Register("get_weather", weatherTool)
    registry.Register("search", searchTool)
    registry.Register("get_time", timeTool)
    
    // LLM can now use all three tools
}
```

## Step 4: Tool Error Handling

Handle tool errors gracefully:

```go
func getWeather(ctx context.Context, args map[string]interface{}) (interface{}, error) {
    location := args["location"].(string)
    
    // Validate input
    if location == "" {
        return nil, fmt.Errorf("location cannot be empty")
    }
    
    // Call weather API (with error handling)
    weather, err := weatherAPI.Get(location)
    if err != nil {
        // Specific error messages help the LLM respond appropriately
        if err == ErrNotFound {
            return nil, fmt.Errorf("weather data not available for %s", location)
        }
        if err == ErrRateLimit {
            return nil, fmt.Errorf("weather service is temporarily unavailable")
        }
        return nil, fmt.Errorf("failed to get weather: %w", err)
    }
    
    return weather, nil
}
```

The SDK automatically sends errors back to the LLM:

```
User: What's the weather in Atlantis?
[Tool returns error: "weather data not available for Atlantis"]
Assistant: I don't have weather data for Atlantis. Could you specify a different location?
```

## Step 5: Tool with Complex Parameters

Tools can accept various parameter types:

```go
registry.Register("book_flight", &tools.Tool{
    Name:        "book_flight",
    Description: "Book a flight",
    Parameters: map[string]interface{}{
        "type": "object",
        "properties": map[string]interface{}{
            "from": map[string]interface{}{
                "type":        "string",
                "description": "Departure city",
            },
            "to": map[string]interface{}{
                "type":        "string",
                "description": "Destination city",
            },
            "date": map[string]interface{}{
                "type":        "string",
                "description": "Travel date (YYYY-MM-DD)",
            },
            "passengers": map[string]interface{}{
                "type":        "integer",
                "description": "Number of passengers",
                "minimum":     1,
                "maximum":     9,
            },
            "class": map[string]interface{}{
                "type":        "string",
                "description": "Travel class",
                "enum":        []string{"economy", "business", "first"},
            },
        },
        "required": []string{"from", "to", "date"},
    },
    Function: func(ctx context.Context, args map[string]interface{}) (interface{}, error) {
        from := args["from"].(string)
        to := args["to"].(string)
        date := args["date"].(string)
        
        passengers := 1
        if p, ok := args["passengers"].(float64); ok {
            passengers = int(p)
        }
        
        class := "economy"
        if c, ok := args["class"].(string); ok {
            class = c
        }
        
        // Book flight
        booking := bookFlight(from, to, date, passengers, class)
        return booking, nil
    },
})
```

## Step 6: Async Tool Execution

For long-running operations:

```go
func processDocument(ctx context.Context, args map[string]interface{}) (interface{}, error) {
    url := args["url"].(string)
    
    // Start async processing
    taskID := generateTaskID()
    
    go func() {
        // Long operation
        result := analyzeDocument(url)
        saveResult(taskID, result)
    }()
    
    return map[string]interface{}{
        "task_id": taskID,
        "status":  "processing",
        "message": "Document analysis started",
    }, nil
}
```

## Complete Example: Multi-Tool Assistant

Here's a production-ready multi-tool assistant:

```go
package main

import (
    "bufio"
    "context"
    "fmt"
    "log"
    "os"
    "strings"
    "time"

    "github.com/AltairaLabs/PromptKit/sdk"
    "github.com/AltairaLabs/PromptKit/runtime/providers"
    "github.com/AltairaLabs/PromptKit/runtime/tools"
)

func main() {
    ctx := context.Background()

    // Create registry with multiple tools
    registry := tools.NewRegistry()
    
    // Weather tool
    registry.Register("get_weather", &tools.Tool{
        Name:        "get_weather",
        Description: "Get current weather for a location",
        Parameters: map[string]interface{}{
            "type": "object",
            "properties": map[string]interface{}{
                "location": map[string]interface{}{
                    "type":        "string",
                    "description": "City name",
                },
            },
            "required": []string{"location"},
        },
        Function: getWeather,
    })
    
    // Time tool
    registry.Register("get_time", &tools.Tool{
        Name:        "get_time",
        Description: "Get current time in a timezone",
        Parameters: map[string]interface{}{
            "type": "object",
            "properties": map[string]interface{}{
                "timezone": map[string]interface{}{
                    "type":        "string",
                    "description": "Timezone code (e.g., UTC, PST, JST)",
                },
            },
            "required": []string{"timezone"},
        },
        Function: getTime,
    })
    
    // Calculator tool
    registry.Register("calculator", &tools.Tool{
        Name:        "calculator",
        Description: "Perform calculations",
        Parameters: map[string]interface{}{
            "type": "object",
            "properties": map[string]interface{}{
                "expression": map[string]interface{}{
                    "type":        "string",
                    "description": "Math expression",
                },
            },
            "required": []string{"expression"},
        },
        Function: calculate,
    })

    // Setup manager
    apiKey := os.Getenv("OPENAI_API_KEY")
    provider := providers.NewOpenAIProvider(apiKey, "gpt-4o", false)
    
    manager, err := sdk.NewConversationManager(
        sdk.WithProvider(provider),
        sdk.WithToolRegistry(registry),
    )
    if err != nil {
        log.Fatal(err)
    }

    pack, err := manager.LoadPack("./multi-tool.pack.json")
    if err != nil {
        log.Fatal(err)
    }

    conv, err := manager.NewConversation(ctx, pack, sdk.ConversationConfig{
        UserID:     "user123",
        PromptName: "assistant",
    })
    if err != nil {
        log.Fatal(err)
    }

    // Interactive loop
    scanner := bufio.NewScanner(os.Stdin)
    fmt.Println("Multi-Tool Assistant ready!")
    fmt.Println("Try: 'weather in Paris', 'time in UTC', 'calculate 15*7'")
    fmt.Println()

    for {
        fmt.Print("You: ")
        if !scanner.Scan() {
            break
        }

        message := strings.TrimSpace(scanner.Text())
        if message == "" {
            continue
        }
        if message == "quit" {
            break
        }

        response, err := conv.Send(ctx, message)
        if err != nil {
            fmt.Printf("Error: %v\n\n", err)
            continue
        }

        fmt.Printf("Assistant: %s\n\n", response.Content)
    }
}

// Tool implementations
func getWeather(ctx context.Context, args map[string]interface{}) (interface{}, error) {
    location := strings.ToLower(args["location"].(string))
    
    weather := map[string]map[string]interface{}{
        "paris":  {"temp": 18, "conditions": "Partly cloudy"},
        "london": {"temp": 15, "conditions": "Rainy"},
        "tokyo":  {"temp": 22, "conditions": "Sunny"},
    }
    
    if w, ok := weather[location]; ok {
        return w, nil
    }
    
    return nil, fmt.Errorf("weather data not available for %s", location)
}

func getTime(ctx context.Context, args map[string]interface{}) (interface{}, error) {
    timezone := strings.ToUpper(args["timezone"].(string))
    
    // In production, use time.LoadLocation()
    times := map[string]string{
        "UTC": time.Now().UTC().Format("15:04 MST"),
        "PST": time.Now().Add(-8 * time.Hour).Format("15:04 MST"),
        "JST": time.Now().Add(9 * time.Hour).Format("15:04 MST"),
    }
    
    if t, ok := times[timezone]; ok {
        return t, nil
    }
    
    return nil, fmt.Errorf("unknown timezone: %s", timezone)
}

func calculate(ctx context.Context, args map[string]interface{}) (interface{}, error) {
    expression := args["expression"].(string)
    
    // Use a proper math eval library in production
    // This is simplified for the tutorial
    
    return fmt.Sprintf("Result: %s", expression), nil
}
```

Create `multi-tool.pack.json`:

```json
{
  "version": "1.0",
  "prompts": {
    "assistant": {
      "name": "assistant",
      "description": "Multi-tool assistant",
      "system_prompt": "You are a helpful assistant with access to weather, time, and calculator tools. Use tools when appropriate to provide accurate information.",
      "available_tools": ["get_weather", "get_time", "calculator"],
      "model_config": {
        "temperature": 0.7,
        "max_tokens": 1000
      }
    }
  }
}
```

## What You've Learned

✅ Register tools with the SDK  
✅ Define tool parameters and descriptions  
✅ Handle tool calls from LLMs  
✅ Build multi-tool assistants  
✅ Implement error handling  
✅ Work with complex parameters  

## Next Steps

Continue to [Tutorial 4: State Management](04-state-management.md) to learn how to persist conversations across sessions.

## Further Reading

- [How to Register Tools](../how-to/register-tools.md)
- [Tool Examples](../../../examples/tools/)
- [Tool Registry Reference](../reference/tool-registry.md)
