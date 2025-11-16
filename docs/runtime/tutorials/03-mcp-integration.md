---
layout: docs
title: "Tutorial 3: MCP Integration"
parent: Runtime Tutorials
grand_parent: Runtime
nav_order: 3
---

# Tutorial 3: MCP Integration

Add external tools to your LLM application via Model Context Protocol.

**Time**: 30 minutes  
**Level**: Intermediate

## What You'll Build

A chatbot that can read and manipulate files using MCP filesystem tools.

## What You'll Learn

- Set up MCP servers
- Register external tools
- Enable automatic tool calling
- Handle tool execution
- Build tool-enabled agents

## Prerequisites

- Completed [Tutorial 2](02-multi-turn.md)
- Node.js (for MCP servers)

## Step 1: Install MCP Filesystem Server

The filesystem server provides file operations as tools:

```bash
# Test the MCP server
npx -y @modelcontextprotocol/server-filesystem /tmp/allowed
```

This starts an MCP server that can access files in `/tmp/allowed`.

## Step 2: Create Tool-Enabled Bot

```go
package main

import (
    "bufio"
    "context"
    "fmt"
    "log"
    "os"
    "strings"
    
    "github.com/AltairaLabs/PromptKit/runtime/mcp"
    "github.com/AltairaLabs/PromptKit/runtime/pipeline"
    "github.com/AltairaLabs/PromptKit/runtime/pipeline/middleware"
    "github.com/AltairaLabs/PromptKit/runtime/providers/openai"
    "github.com/AltairaLabs/PromptKit/runtime/tools"
)

func main() {
    // Create MCP registry
    mcpRegistry := mcp.NewRegistry()
    defer mcpRegistry.Close()
    
    // Register filesystem server
    err := mcpRegistry.RegisterServer(mcp.ServerConfig{
        Name:    "filesystem",
        Command: "npx",
        Args:    []string{"-y", "@modelcontextprotocol/server-filesystem", "/tmp/allowed"},
    })
    if err != nil {
        log.Fatal(err)
    }
    
    // Create tool registry
    toolRegistry := tools.NewRegistry()
    toolRegistry.RegisterExecutor(tools.NewMCPExecutor(mcpRegistry))
    
    // Discover and register MCP tools
    ctx := context.Background()
    serverTools, err := mcpRegistry.ListAllTools(ctx)
    if err != nil {
        log.Fatal(err)
    }
    
    for _, mcpTools := range serverTools {
        for _, mcpTool := range mcpTools {
            toolRegistry.Register(&tools.ToolDescriptor{
                Name:        mcpTool.Name,
                Description: mcpTool.Description,
                InputSchema: mcpTool.InputSchema,
                Mode:        "mcp",
            })
            fmt.Printf("Registered tool: %s\n", mcpTool.Name)
        }
    }
    
    // Create provider
    provider := openai.NewOpenAIProvider(
        "openai",
        "gpt-4o-mini",
        os.Getenv("OPENAI_API_KEY"),
        openai.DefaultProviderDefaults(),
        false,
    )
    defer provider.Close()
    
    // Build pipeline with tools
    pipe := pipeline.NewPipeline(
        middleware.ProviderMiddleware(provider, toolRegistry, &pipeline.ToolPolicy{
            ToolChoice: "auto",
            MaxRounds:  5,
        }, &middleware.ProviderMiddlewareConfig{
            MaxTokens:   1500,
            Temperature: 0.7,
        }),
    )
    defer pipe.Shutdown(context.Background())
    
    // Interactive loop
    fmt.Println("\nTool-enabled chatbot ready! Type 'exit' to quit.")
    fmt.Print("\nYou: ")
    
    scanner := bufio.NewScanner(os.Stdin)
    for scanner.Scan() {
        input := strings.TrimSpace(scanner.Text())
        
        if input == "exit" {
            break
        }
        
        if input == "" {
            fmt.Print("You: ")
            continue
        }
        
        result, err := pipe.Execute(ctx, "user", input)
        if err != nil {
            log.Printf("Error: %v\n", err)
            fmt.Print("You: ")
            continue
        }
        
        fmt.Printf("\nBot: %s\n\n", result.Response.Content)
        fmt.Print("You: ")
    }
    
    fmt.Println("Goodbye!")
}
```

## Step 3: Test with Files

Create test directory and files:

```bash
mkdir -p /tmp/allowed
echo "Hello from file!" > /tmp/allowed/test.txt
echo "Shopping: milk, eggs, bread" > /tmp/allowed/shopping.txt
```

Run the bot:

```bash
go run main.go
```

Try these prompts:

```
You: Read the file /tmp/allowed/test.txt
Bot: The file contains: "Hello from file!"

You: List all files in /tmp/allowed
Bot: There are 2 files: test.txt and shopping.txt

You: What's in my shopping list?
Bot: Your shopping list contains: milk, eggs, and bread

You: Create a new file called notes.txt with "Meeting at 3pm"
Bot: I've created notes.txt with your message.
```

The LLM automatically uses tools to complete tasks! 🎉

## Understanding Tool Calling

### How Tools Work

1. **Registration**: Tools are registered with descriptions
2. **LLM Decision**: LLM decides when to use tools
3. **Execution**: Runtime executes tool calls
4. **Response**: LLM sees results and responds to user

### Tool Policy

```go
policy := &pipeline.ToolPolicy{
    ToolChoice: "auto",    // "auto", "required", "none"
    MaxRounds:  5,         // Max back-and-forth with tools
}
```

- `auto`: LLM chooses when to use tools
- `required`: LLM must use at least one tool
- `none`: Tools disabled

### Available Filesystem Tools

- `read_file`: Read file contents
- `write_file`: Write to file
- `list_directory`: List directory contents
- `create_directory`: Create directory
- `move_file`: Move/rename file
- `delete_file`: Delete file

## Add Memory Server

Install memory MCP server for persistent key-value storage:

```go
// Register memory server
mcpRegistry.RegisterServer(mcp.ServerConfig{
    Name:    "memory",
    Command: "npx",
    Args:    []string{"-y", "@modelcontextprotocol/server-memory"},
})
```

Memory tools:
- `store_memory`: Save data by key
- `retrieve_memory`: Get data by key

Try:
```
You: Remember that my favorite color is blue
Bot: I've stored that information.

You: What's my favorite color?
Bot: Your favorite color is blue.
```

## Complete Example with Multiple Servers

```go
package main

import (
    "bufio"
    "context"
    "fmt"
    "log"
    "os"
    "strings"
    
    "github.com/AltairaLabs/PromptKit/runtime/mcp"
    "github.com/AltairaLabs/PromptKit/runtime/pipeline"
    "github.com/AltairaLabs/PromptKit/runtime/pipeline/middleware"
    "github.com/AltairaLabs/PromptKit/runtime/providers/openai"
    "github.com/AltairaLabs/PromptKit/runtime/statestore"
    "github.com/AltairaLabs/PromptKit/runtime/tools"
)

func main() {
    // MCP setup
    mcpRegistry := mcp.NewRegistry()
    defer mcpRegistry.Close()
    
    // Register servers
    servers := []mcp.ServerConfig{
        {
            Name:    "filesystem",
            Command: "npx",
            Args:    []string{"-y", "@modelcontextprotocol/server-filesystem", "/tmp/allowed"},
        },
        {
            Name:    "memory",
            Command: "npx",
            Args:    []string{"-y", "@modelcontextprotocol/server-memory"},
        },
    }
    
    for _, config := range servers {
        if err := mcpRegistry.RegisterServer(config); err != nil {
            log.Fatalf("Failed to register %s: %v", config.Name, err)
        }
        fmt.Printf("Registered MCP server: %s\n", config.Name)
    }
    
    // Tool registry setup
    toolRegistry := tools.NewRegistry()
    toolRegistry.RegisterExecutor(tools.NewMCPExecutor(mcpRegistry))
    
    // Discover tools
    ctx := context.Background()
    serverTools, _ := mcpRegistry.ListAllTools(ctx)
    for serverName, mcpTools := range serverTools {
        fmt.Printf("\n%s tools:\n", serverName)
        for _, tool := range mcpTools {
            toolRegistry.Register(&tools.ToolDescriptor{
                Name:        tool.Name,
                Description: tool.Description,
                InputSchema: tool.InputSchema,
                Mode:        "mcp",
            })
            fmt.Printf("  - %s: %s\n", tool.Name, tool.Description)
        }
    }
    
    // Provider and pipeline
    provider := openai.NewOpenAIProvider(
        "openai",
        "gpt-4o-mini",
        os.Getenv("OPENAI_API_KEY"),
        openai.DefaultProviderDefaults(),
        false,
    )
    defer provider.Close()
    
    store := statestore.NewInMemoryStateStore()
    
    pipe := pipeline.NewPipeline(
        middleware.StateMiddleware(store),
        middleware.ProviderMiddleware(provider, toolRegistry, &pipeline.ToolPolicy{
            ToolChoice: "auto",
            MaxRounds:  5,
        }, &middleware.ProviderMiddlewareConfig{
            MaxTokens:   1500,
            Temperature: 0.7,
        }),
    )
    defer pipe.Shutdown(ctx)
    
    sessionID := "tool-session"
    
    fmt.Println("\n=== Tool-Enabled Agent ===")
    fmt.Println("Available: filesystem and memory tools")
    fmt.Println("Type 'exit' to quit\n")
    
    scanner := bufio.NewScanner(os.Stdin)
    fmt.Print("You: ")
    
    for scanner.Scan() {
        input := strings.TrimSpace(scanner.Text())
        
        if input == "exit" {
            break
        }
        
        if input == "" {
            fmt.Print("You: ")
            continue
        }
        
        result, err := pipe.ExecuteWithContext(ctx, sessionID, "user", input)
        if err != nil {
            log.Printf("\nError: %v\n\n", err)
            fmt.Print("You: ")
            continue
        }
        
        fmt.Printf("\nAgent: %s\n\n", result.Response.Content)
        fmt.Printf("[Cost: $%.6f]\n\n", result.Cost.TotalCost)
        fmt.Print("You: ")
    }
    
    fmt.Println("Goodbye!")
}
```

## Common Issues

### MCP server won't start

**Problem**: `npx` command fails.

**Solution**: Install Node.js or specify full path to npx.

### Tools not working

**Problem**: LLM doesn't use tools.

**Solution**: Use clear prompts: "Read the file test.txt" not "What's in that file?"

### Permission errors

**Problem**: Can't access files.

**Solution**: Ensure path is within allowed directory: `/tmp/allowed/file.txt`

## What You've Learned

✅ Set up MCP servers  
✅ Register external tools  
✅ Enable automatic tool calling  
✅ Handle tool execution  
✅ Build tool-enabled agents  
✅ Use multiple MCP servers  

## Next Steps

Continue to [Tutorial 4: Validation & Guardrails](04-validation-guardrails.md) to add content safety.

## See Also

- [Integrate MCP](../how-to/integrate-mcp.md) - More MCP patterns
- [Tools & MCP Reference](../reference/tools-mcp.md) - Complete API
