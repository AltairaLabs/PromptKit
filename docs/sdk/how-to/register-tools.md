---
layout: docs
title: Register Tools
nav_order: 6
parent: SDK How-To Guides
grand_parent: SDK
---

# How to Register Tools

Enable LLM function calling by registering tools with the SDK.

## Basic Tool Registration

### Step 1: Create Tool Registry

```go
import "github.com/AltairaLabs/PromptKit/runtime/tools"

registry := tools.NewRegistry()
```

### Step 2: Define Tool Function

```go
func searchWeb(ctx context.Context, args map[string]interface{}) (interface{}, error) {
    query := args["query"].(string)
    
    // Perform search
    results := performSearch(query)
    
    return results, nil
}
```

### Step 3: Register Tool

```go
registry.Register("search", &tools.Tool{
    Name:        "search",
    Description: "Search the web for information",
    Parameters: map[string]interface{}{
        "type": "object",
        "properties": map[string]interface{}{
            "query": map[string]interface{}{
                "type":        "string",
                "description": "The search query",
            },
        },
        "required": []string{"query"},
    },
    Function: searchWeb,
})
```

### Step 4: Use with Manager

```go
manager, _ := sdk.NewConversationManager(
    sdk.WithProvider(provider),
    sdk.WithToolRegistry(registry),
)
```

## Complete Example

```go
package main

import (
    "context"
    "fmt"
    "log"

    "github.com/AltairaLabs/PromptKit/sdk"
    "github.com/AltairaLabs/PromptKit/runtime/providers"
    "github.com/AltairaLabs/PromptKit/runtime/tools"
)

func main() {
    // 1. Create registry
    registry := tools.NewRegistry()

    // 2. Register tools
    registry.Register("calculator", &tools.Tool{
        Name:        "calculator",
        Description: "Perform mathematical calculations",
        Parameters: map[string]interface{}{
            "type": "object",
            "properties": map[string]interface{}{
                "expression": map[string]interface{}{
                    "type":        "string",
                    "description": "Math expression to evaluate",
                },
            },
            "required": []string{"expression"},
        },
        Function: calculate,
    })

    // 3. Create manager with tools
    provider := providers.NewOpenAIProvider(apiKey, "gpt-4o", false)
    manager, _ := sdk.NewConversationManager(
        sdk.WithProvider(provider),
        sdk.WithToolRegistry(registry),
    )

    // 4. Use tools in conversation
    pack, _ := manager.LoadPack("./assistant.pack.json")
    conv, _ := manager.NewConversation(ctx, pack, sdk.ConversationConfig{
        UserID:     "user123",
        PromptName: "assistant",
    })

    // LLM will call calculator tool
    resp, _ := conv.Send(ctx, "What is 123 * 456?")
    fmt.Println(resp.Content)  // "56,088"
}

func calculate(ctx context.Context, args map[string]interface{}) (interface{}, error) {
    expression := args["expression"].(string)
    // Evaluate expression safely
    result := eval(expression)
    return result, nil
}
```

## Common Tool Patterns

### Search Tool

```go
registry.Register("search", &tools.Tool{
    Name:        "search",
    Description: "Search for information on the web",
    Parameters: map[string]interface{}{
        "type": "object",
        "properties": map[string]interface{}{
            "query": map[string]interface{}{
                "type":        "string",
                "description": "Search query",
            },
            "limit": map[string]interface{}{
                "type":        "integer",
                "description": "Number of results (default: 10)",
            },
        },
        "required": []string{"query"},
    },
    Function: func(ctx context.Context, args map[string]interface{}) (interface{}, error) {
        query := args["query"].(string)
        limit := 10
        if l, ok := args["limit"].(float64); ok {
            limit = int(l)
        }

        results := searchAPI(query, limit)
        return results, nil
    },
})
```

### Database Query Tool

```go
registry.Register("query_db", &tools.Tool{
    Name:        "query_db",
    Description: "Query the customer database",
    Parameters: map[string]interface{}{
        "type": "object",
        "properties": map[string]interface{}{
            "customer_id": map[string]interface{}{
                "type":        "string",
                "description": "Customer ID to look up",
            },
        },
        "required": []string{"customer_id"},
    },
    Function: func(ctx context.Context, args map[string]interface{}) (interface{}, error) {
        customerID := args["customer_id"].(string)

        // Query database
        customer, err := db.GetCustomer(ctx, customerID)
        if err != nil {
            return nil, fmt.Errorf("customer not found: %w", err)
        }

        return customer, nil
    },
})
```

### API Call Tool

```go
registry.Register("get_weather", &tools.Tool{
    Name:        "get_weather",
    Description: "Get current weather for a location",
    Parameters: map[string]interface{}{
        "type": "object",
        "properties": map[string]interface{}{
            "location": map[string]interface{}{
                "type":        "string",
                "description": "City name or coordinates",
            },
        },
        "required": []string{"location"},
    },
    Function: func(ctx context.Context, args map[string]interface{}) (interface{}, error) {
        location := args["location"].(string)

        // Call weather API
        weather, err := weatherAPI.Get(location)
        if err != nil {
            return nil, err
        }

        return map[string]interface{}{
            "temperature": weather.Temp,
            "conditions":  weather.Conditions,
            "humidity":    weather.Humidity,
        }, nil
    },
})
```

### File Operations Tool

```go
registry.Register("read_file", &tools.Tool{
    Name:        "read_file",
    Description: "Read contents of a file",
    Parameters: map[string]interface{}{
        "type": "object",
        "properties": map[string]interface{}{
            "path": map[string]interface{}{
                "type":        "string",
                "description": "File path to read",
            },
        },
        "required": []string{"path"},
    },
    Function: func(ctx context.Context, args map[string]interface{}) (interface{}, error) {
        path := args["path"].(string)

        // Security: validate path
        if !isAllowedPath(path) {
            return nil, fmt.Errorf("access denied: %s", path)
        }

        content, err := os.ReadFile(path)
        if err != nil {
            return nil, err
        }

        return string(content), nil
    },
})
```

## Tool Parameter Types

### String Parameter

```go
"name": map[string]interface{}{
    "type":        "string",
    "description": "User's name",
}
```

### Integer Parameter

```go
"count": map[string]interface{}{
    "type":        "integer",
    "description": "Number of items",
    "minimum":     1,
    "maximum":     100,
}
```

### Boolean Parameter

```go
"verbose": map[string]interface{}{
    "type":        "boolean",
    "description": "Enable verbose output",
    "default":     false,
}
```

### Enum Parameter

```go
"format": map[string]interface{}{
    "type":        "string",
    "description": "Output format",
    "enum":        []string{"json", "xml", "csv"},
}
```

### Array Parameter

```go
"tags": map[string]interface{}{
    "type":        "array",
    "description": "List of tags",
    "items": map[string]interface{}{
        "type": "string",
    },
}
```

### Object Parameter

```go
"filters": map[string]interface{}{
    "type":        "object",
    "description": "Search filters",
    "properties": map[string]interface{}{
        "minPrice": map[string]interface{}{"type": "number"},
        "maxPrice": map[string]interface{}{"type": "number"},
        "category": map[string]interface{}{"type": "string"},
    },
}
```

## Error Handling in Tools

### Return Errors

```go
Function: func(ctx context.Context, args map[string]interface{}) (interface{}, error) {
    query := args["query"].(string)

    results, err := searchAPI(query)
    if err != nil {
        // Error is sent back to LLM
        return nil, fmt.Errorf("search failed: %w", err)
    }

    return results, nil
}
```

### Validation

```go
Function: func(ctx context.Context, args map[string]interface{}) (interface{}, error) {
    // Validate required parameter
    query, ok := args["query"].(string)
    if !ok || query == "" {
        return nil, fmt.Errorf("invalid query parameter")
    }

    // Validate optional parameter
    var limit int = 10
    if l, ok := args["limit"].(float64); ok {
        if l < 1 || l > 100 {
            return nil, fmt.Errorf("limit must be between 1 and 100")
        }
        limit = int(l)
    }

    return search(query, limit)
}
```

## Security Best Practices

### Input Validation

```go
Function: func(ctx context.Context, args map[string]interface{}) (interface{}, error) {
    path := args["path"].(string)

    // Validate path
    if strings.Contains(path, "..") {
        return nil, fmt.Errorf("invalid path: directory traversal detected")
    }

    // Whitelist allowed paths
    if !strings.HasPrefix(path, "/allowed/directory/") {
        return nil, fmt.Errorf("access denied")
    }

    return readFile(path)
}
```

### Rate Limiting

```go
var rateLimiter = rate.NewLimiter(rate.Limit(10), 10) // 10 req/sec

Function: func(ctx context.Context, args map[string]interface{}) (interface{}, error) {
    // Check rate limit
    if !rateLimiter.Allow() {
        return nil, fmt.Errorf("rate limit exceeded")
    }

    // Perform operation
    return doExpensiveOperation(args)
}
```

### Authentication

```go
Function: func(ctx context.Context, args map[string]interface{}) (interface{}, error) {
    // Check context for auth
    userID := ctx.Value("user_id").(string)
    if userID == "" {
        return nil, fmt.Errorf("authentication required")
    }

    // Verify permissions
    if !hasPermission(userID, "search") {
        return nil, fmt.Errorf("insufficient permissions")
    }

    return performSearch(args)
}
```

## Testing Tools

### Unit Test

```go
func TestSearchTool(t *testing.T) {
    tool := &tools.Tool{
        Name:     "search",
        Function: searchFunction,
    }

    result, err := tool.Function(context.Background(), map[string]interface{}{
        "query": "test query",
    })

    if err != nil {
        t.Fatalf("unexpected error: %v", err)
    }

    // Verify result
    results := result.([]SearchResult)
    if len(results) == 0 {
        t.Error("expected search results")
    }
}
```

### Integration Test

```go
func TestToolWithSDK(t *testing.T) {
    registry := tools.NewRegistry()
    registry.Register("test_tool", testTool)

    manager, _ := sdk.NewConversationManager(
        sdk.WithProvider(mockProvider),
        sdk.WithToolRegistry(registry),
    )

    pack, _ := manager.LoadPack("./test.pack.json")
    conv, _ := manager.NewConversation(ctx, pack, config)

    resp, err := conv.Send(ctx, "Use the test tool")
    if err != nil {
        t.Fatalf("send failed: %v", err)
    }

    // Verify tool was called
    if !strings.Contains(resp.Content, "tool result") {
        t.Error("tool was not used")
    }
}
```

## Advanced Patterns

### Tool with State

```go
type SearchTool struct {
    cache map[string]interface{}
    mu    sync.RWMutex
}

func (s *SearchTool) Execute(ctx context.Context, args map[string]interface{}) (interface{}, error) {
    query := args["query"].(string)

    // Check cache
    s.mu.RLock()
    if cached, ok := s.cache[query]; ok {
        s.mu.RUnlock()
        return cached, nil
    }
    s.mu.RUnlock()

    // Perform search
    results := search(query)

    // Cache results
    s.mu.Lock()
    s.cache[query] = results
    s.mu.Unlock()

    return results, nil
}

// Register
searchTool := &SearchTool{cache: make(map[string]interface{})}
registry.Register("search", &tools.Tool{
    Name:     "search",
    Function: searchTool.Execute,
})
```

### Async Tool Execution

```go
Function: func(ctx context.Context, args map[string]interface{}) (interface{}, error) {
    taskID := generateTaskID()

    // Start async operation
    go func() {
        result := performLongOperation(args)
        saveResult(taskID, result)
    }()

    return map[string]interface{}{
        "task_id": taskID,
        "status":  "processing",
    }, nil
}
```

## Troubleshooting

### Tool Not Being Called

Check that:
1. Tool is registered before creating conversation
2. Tool description is clear
3. Provider supports function calling (GPT-4, Claude 3+, Gemini Pro)
4. Pack includes tool in available_tools

### Tool Errors

```go
// Add logging to tool
Function: func(ctx context.Context, args map[string]interface{}) (interface{}, error) {
    log.Printf("Tool called with args: %+v", args)

    result, err := performOperation(args)
    if err != nil {
        log.Printf("Tool error: %v", err)
        return nil, err
    }

    log.Printf("Tool result: %+v", result)
    return result, nil
}
```

## Next Steps

- **[Handle Tool Calls](handle-tool-calls.md)** - Process tool requests
- **[Human-in-the-Loop](hitl-workflows.md)** - Approval workflows
- **[Tutorial: Tool Integration](../tutorials/03-tool-integration.md)** - Complete guide

## See Also

- [ToolRegistry Reference](../reference/tool-registry.md)
- [Tools Package Documentation](https://pkg.go.dev/github.com/AltairaLabs/PromptKit/runtime/tools)
