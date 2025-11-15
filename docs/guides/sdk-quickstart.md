---
layout: default
title: "SDK Quickstart Tutorial"
parent: Guides
nav_order: 1
---
# PromptKit SDK - Quickstart Tutorial

> **Note**: This guide is being updated to reflect the current SDK API. For the most accurate API reference, see the [SDK API Documentation](../api/sdk.md).

The PromptKit SDK enables developers to build robust LLM applications with conversation management, provider abstraction, and advanced pipeline features. This tutorial will guide you through building your first LLM application.

## Installation

### Prerequisites

- Go 1.21 or later
- OpenAI API key (or other supported provider)

### Install SDK

```bash
# Initialize your Go project
mkdir my-llm-app
cd my-llm-app
go mod init my-llm-app

# Add PromptKit SDK
go get github.com/AltairaLabs/PromptKit/sdk@latest
```

## Your First LLM Application

### Basic Chat Application

Create a simple chat application that demonstrates core SDK features:

```go
// main.go
package main

import (
    "context"
    "fmt"
    "log"
    "os"

    "github.com/AltairaLabs/PromptKit/sdk"
)

func main() {
    // Initialize SDK with OpenAI provider
    config := sdk.Config{
        Provider: "openai",
        APIKey:   os.Getenv("OPENAI_API_KEY"),
        Model:    "gpt-4",
    }
    
    client, err := sdk.NewClient(config)
    if err != nil {
        log.Fatal("Failed to create client:", err)
    }
    defer client.Close()
    
    // Create a conversation
    conversation := client.NewConversation()
    
    // Set system prompt
    err = conversation.SetSystemPrompt("You are a helpful assistant that provides concise, accurate answers.")
    if err != nil {
        log.Fatal("Failed to set system prompt:", err)
    }
    
    // Send a message
    response, err := conversation.SendMessage(context.Background(), "What is the capital of France?")
    if err != nil {
        log.Fatal("Failed to send message:", err)
    }
    
    fmt.Printf("Assistant: %s\n", response.Content)
    
    // Continue the conversation
    response, err = conversation.SendMessage(context.Background(), "What's interesting about that city?")
    if err != nil {
        log.Fatal("Failed to send follow-up:", err)
    }
    
    fmt.Printf("Assistant: %s\n", response.Content)
}
```

### Run the Application

```bash
# Set your API key
export OPENAI_API_KEY="your-api-key-here"

# Run the application
go run main.go
```

Expected output:

```text
Assistant: The capital of France is Paris.
Assistant: Paris is fascinating for many reasons: it's known as the "City of Light," home to iconic landmarks like the Eiffel Tower and Louvre Museum, renowned for its art, fashion, cuisine, and rich history spanning over 2,000 years.
```

## Core Concepts

### 1. Clients and Providers

The SDK abstracts different LLM providers behind a unified interface:

```go
// OpenAI
client, err := sdk.NewClient(sdk.Config{
    Provider: "openai",
    APIKey:   os.Getenv("OPENAI_API_KEY"),
    Model:    "gpt-4",
})

// Anthropic
client, err := sdk.NewClient(sdk.Config{
    Provider: "anthropic", 
    APIKey:   os.Getenv("ANTHROPIC_API_KEY"),
    Model:    "claude-3-opus-20240229",
})

// Azure OpenAI
client, err := sdk.NewClient(sdk.Config{
    Provider: "azure-openai",
    APIKey:   os.Getenv("AZURE_OPENAI_API_KEY"),
    Endpoint: os.Getenv("AZURE_OPENAI_ENDPOINT"),
    Model:    "gpt-4",
})
```

### 2. Conversations

Conversations manage message history and context:

```go
// Create conversation
conversation := client.NewConversation()

// With options
conversation := client.NewConversationWithOptions(sdk.ConversationOptions{
    MaxTokens:     1000,
    Temperature:   0.7,
    SystemPrompt:  "You are a technical expert.",
    MaxHistory:    10, // Keep last 10 messages
})

// Get conversation history
history := conversation.GetHistory()
for _, msg := range history {
    fmt.Printf("%s: %s\n", msg.Role, msg.Content)
}

// Clear history
conversation.ClearHistory()
```

### 3. Messages and Responses

Rich message and response handling:

```go
// Send simple message
response, err := conversation.SendMessage(ctx, "Hello!")

// Send message with options
response, err := conversation.SendMessageWithOptions(ctx, "Explain quantum computing", sdk.MessageOptions{
    Temperature: 0.3,
    MaxTokens:   500,
})

// Access response details
fmt.Printf("Content: %s\n", response.Content)
fmt.Printf("Tokens: %d\n", response.TokensUsed)
fmt.Printf("Cost: $%.4f\n", response.Cost)
fmt.Printf("Latency: %v\n", response.Latency)
```

## Advanced Features

### Prompt Packs

Use compiled prompt packs for better organization and reusability:

```go
// Load a prompt pack
pack, err := sdk.LoadPack("./prompts/customer-support.json")
if err != nil {
    log.Fatal("Failed to load pack:", err)
}

// Get a prompt from the pack
prompt, err := pack.GetPrompt("greeting")
if err != nil {
    log.Fatal("Failed to get prompt:", err)
}

// Use prompt with variables
response, err := conversation.SendPrompt(ctx, prompt, map[string]interface{}{
    "customer_name": "Alice",
    "issue_type":    "billing",
})
```

### Tool Integration (MCP)

Integrate external tools using the Model Context Protocol:

```go
// Enable MCP tools
config := sdk.Config{
    Provider: "openai",
    APIKey:   os.Getenv("OPENAI_API_KEY"),
    Model:    "gpt-4",
    MCP: &sdk.MCPConfig{
        Servers: []sdk.MCPServer{
            {
                Name:    "filesystem",
                Command: "npx",
                Args:    []string{"-y", "@modelcontextprotocol/server-filesystem", "./data"},
            },
            {
                Name:    "memory",
                Command: "npx", 
                Args:    []string{"-y", "@modelcontextprotocol/server-memory"},
            },
        },
    },
}

client, err := sdk.NewClient(config)
if err != nil {
    log.Fatal(err)
}

// The model can now use file system and memory tools automatically
response, err := conversation.SendMessage(ctx, "What files are in the data directory?")
```

### Streaming Responses

Handle streaming responses for real-time applications:

```go
// Create streaming conversation
stream, err := conversation.SendMessageStream(ctx, "Write a long story about space exploration")
if err != nil {
    log.Fatal(err)
}

// Process chunks as they arrive
for chunk := range stream.Chunks() {
    fmt.Print(chunk.Content)
}

// Check for errors
if err := stream.Err(); err != nil {
    log.Fatal("Stream error:", err)
}

// Get final response
finalResponse := stream.Response()
fmt.Printf("\nTotal tokens: %d\n", finalResponse.TokensUsed)
```

### Pipeline Processing

Build complex processing pipelines:

```go
// Create a pipeline
pipeline := sdk.NewPipeline().
    AddStep("summarize", func(ctx context.Context, input string) (string, error) {
        return conversation.SendMessage(ctx, fmt.Sprintf("Summarize this text: %s", input))
    }).
    AddStep("translate", func(ctx context.Context, input string) (string, error) {
        return conversation.SendMessage(ctx, fmt.Sprintf("Translate to French: %s", input))
    }).
    AddStep("sentiment", func(ctx context.Context, input string) (string, error) {
        return conversation.SendMessage(ctx, fmt.Sprintf("Analyze sentiment: %s", input))
    })

// Execute pipeline
result, err := pipeline.Execute(ctx, "Long article text here...")
if err != nil {
    log.Fatal(err)
}

fmt.Println("Final result:", result)
```

## Error Handling

### Robust Error Handling

```go
// Handle different types of errors
response, err := conversation.SendMessage(ctx, "Hello")
if err != nil {
    switch {
    case sdk.IsRateLimitError(err):
        fmt.Println("Rate limited, waiting...")
        time.Sleep(time.Minute)
        // Retry logic
    
    case sdk.IsAuthenticationError(err):
        log.Fatal("Authentication failed, check API key")
    
    case sdk.IsQuotaExceededError(err):
        log.Fatal("API quota exceeded")
    
    case sdk.IsTemporaryError(err):
        fmt.Println("Temporary error, retrying...")
        // Retry with backoff
    
    default:
        log.Fatal("Unexpected error:", err)
    }
}
```

### Retry Configuration

```go
// Configure automatic retries
config := sdk.Config{
    Provider: "openai",
    APIKey:   os.Getenv("OPENAI_API_KEY"),
    Model:    "gpt-4",
    Retry: &sdk.RetryConfig{
        MaxAttempts: 3,
        InitialDelay: time.Second,
        MaxDelay:     time.Minute,
        Multiplier:   2.0,
    },
}
```

## Configuration Management

### Environment-based Configuration

```go
// config.go
package main

import (
    "os"
    "strconv"
    "time"
)

type AppConfig struct {
    OpenAI struct {
        APIKey string
        Model  string
    }
    Anthropic struct {
        APIKey string
        Model  string
    }
    Features struct {
        EnableMCP     bool
        EnableRetries bool
        MaxTokens     int
    }
}

func LoadConfig() *AppConfig {
    config := &AppConfig{}
    
    // OpenAI configuration
    config.OpenAI.APIKey = os.Getenv("OPENAI_API_KEY")
    config.OpenAI.Model = getEnvOrDefault("OPENAI_MODEL", "gpt-4")
    
    // Anthropic configuration  
    config.Anthropic.APIKey = os.Getenv("ANTHROPIC_API_KEY")
    config.Anthropic.Model = getEnvOrDefault("ANTHROPIC_MODEL", "claude-3-opus-20240229")
    
    // Feature flags
    config.Features.EnableMCP = getBoolEnv("ENABLE_MCP", true)
    config.Features.EnableRetries = getBoolEnv("ENABLE_RETRIES", true)
    config.Features.MaxTokens = getIntEnv("MAX_TOKENS", 1000)
    
    return config
}

func getEnvOrDefault(key, defaultValue string) string {
    if value := os.Getenv(key); value != "" {
        return value
    }
    return defaultValue
}

func getBoolEnv(key string, defaultValue bool) bool {
    if value := os.Getenv(key); value != "" {
        parsed, _ := strconv.ParseBool(value)
        return parsed
    }
    return defaultValue
}

func getIntEnv(key string, defaultValue int) int {
    if value := os.Getenv(key); value != "" {
        parsed, _ := strconv.Atoi(value)
        return parsed
    }
    return defaultValue
}
```

### Configuration File Support

```yaml
# config.yaml
providers:
  openai:
    api_key_env: "OPENAI_API_KEY"
    model: "gpt-4"
    max_tokens: 1000
    temperature: 0.7
  
  anthropic:
    api_key_env: "ANTHROPIC_API_KEY"
    model: "claude-3-opus-20240229"
    max_tokens: 1000
    temperature: 0.7

features:
  enable_mcp: true
  enable_retries: true
  enable_streaming: true

mcp:
  servers:
    - name: "filesystem"
      command: "npx"
      args: ["-y", "@modelcontextprotocol/server-filesystem", "./data"]
    
    - name: "memory"
      command: "npx"
      args: ["-y", "@modelcontextprotocol/server-memory"]

logging:
  level: "info"
  format: "json"
```

```go
// Load YAML configuration
import "gopkg.in/yaml.v2"

func LoadYAMLConfig(filename string) (*Config, error) {
    data, err := os.ReadFile(filename)
    if err != nil {
        return nil, err
    }
    
    var config Config
    err = yaml.Unmarshal(data, &config)
    return &config, err
}
```

## Testing

### Unit Testing Conversations

```go
// conversation_test.go
package main

import (
    "context"
    "testing"
    
    "github.com/AltairaLabs/PromptKit/sdk"
    "github.com/stretchr/testify/assert"
    "github.com/stretchr/testify/require"
)

func TestBasicConversation(t *testing.T) {
    // Create mock client for testing
    client := sdk.NewMockClient()
    
    // Configure expected responses
    client.ExpectResponse("Hello", "Hello! How can I help you today?")
    client.ExpectResponse("What's 2+2?", "2+2 equals 4.")
    
    conversation := client.NewConversation()
    
    // Test first message
    response, err := conversation.SendMessage(context.Background(), "Hello")
    require.NoError(t, err)
    assert.Equal(t, "Hello! How can I help you today?", response.Content)
    
    // Test second message
    response, err = conversation.SendMessage(context.Background(), "What's 2+2?")
    require.NoError(t, err)
    assert.Equal(t, "2+2 equals 4.", response.Content)
    
    // Verify all expectations were met
    client.AssertExpectationsMet(t)
}

func TestConversationHistory(t *testing.T) {
    client := sdk.NewMockClient()
    client.ExpectResponse("Test message", "Test response")
    
    conversation := client.NewConversation()
    conversation.SetSystemPrompt("You are a test assistant.")
    
    _, err := conversation.SendMessage(context.Background(), "Test message")
    require.NoError(t, err)
    
    history := conversation.GetHistory()
    assert.Len(t, history, 3) // System + User + Assistant
    assert.Equal(t, "system", history[0].Role)
    assert.Equal(t, "user", history[1].Role)
    assert.Equal(t, "assistant", history[2].Role)
}
```

### Integration Testing

```go
// integration_test.go
func TestRealProvider(t *testing.T) {
    if testing.Short() {
        t.Skip("Skipping integration test in short mode")
    }
    
    apiKey := os.Getenv("OPENAI_API_KEY")
    if apiKey == "" {
        t.Skip("OPENAI_API_KEY not set, skipping integration test")
    }
    
    client, err := sdk.NewClient(sdk.Config{
        Provider: "openai",
        APIKey:   apiKey,
        Model:    "gpt-3.5-turbo", // Use cheaper model for testing
    })
    require.NoError(t, err)
    defer client.Close()
    
    conversation := client.NewConversation()
    response, err := conversation.SendMessage(context.Background(), "Say 'test successful'")
    require.NoError(t, err)
    assert.Contains(t, strings.ToLower(response.Content), "test successful")
}
```

## Production Deployment

### Logging and Monitoring

```go
// main.go with structured logging
import (
    "log/slog"
    "os"
)

func main() {
    // Configure structured logging
    logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
        Level: slog.LevelInfo,
    }))
    slog.SetDefault(logger)
    
    // SDK configuration with logging
    config := sdk.Config{
        Provider: "openai",
        APIKey:   os.Getenv("OPENAI_API_KEY"),
        Model:    "gpt-4",
        Logger:   logger,
        Metrics:  true, // Enable metrics collection
    }
    
    client, err := sdk.NewClient(config)
    if err != nil {
        logger.Error("Failed to create client", "error", err)
        os.Exit(1)
    }
    defer client.Close()
    
    // Log conversation metrics
    conversation := client.NewConversation()
    response, err := conversation.SendMessage(ctx, "Hello")
    if err != nil {
        logger.Error("Conversation failed", 
            "error", err,
            "tokens_used", response.TokensUsed,
            "cost", response.Cost,
            "latency", response.Latency,
        )
        return
    }
    
    logger.Info("Conversation successful",
        "tokens_used", response.TokensUsed, 
        "cost", response.Cost,
        "latency", response.Latency,
    )
}
```

### Health Checks

```go
// health.go
func HealthCheck(client *sdk.Client) error {
    ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
    defer cancel()
    
    conversation := client.NewConversation()
    _, err := conversation.SendMessage(ctx, "test")
    return err
}

// HTTP health endpoint
func healthHandler(client *sdk.Client) http.HandlerFunc {
    return func(w http.ResponseWriter, r *http.Request) {
        if err := HealthCheck(client); err != nil {
            w.WriteHeader(http.StatusServiceUnavailable)
            json.NewEncoder(w).Encode(map[string]string{
                "status": "unhealthy",
                "error":  err.Error(),
            })
            return
        }
        
        json.NewEncoder(w).Encode(map[string]string{
            "status": "healthy",
        })
    }
}
```

### Graceful Shutdown

```go
// main.go with graceful shutdown
func main() {
    client, err := sdk.NewClient(config)
    if err != nil {
        log.Fatal(err)
    }
    
    // Handle shutdown signals
    sigChan := make(chan os.Signal, 1)
    signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
    
    go func() {
        <-sigChan
        slog.Info("Shutting down gracefully...")
        
        // Close SDK client
        client.Close()
        
        os.Exit(0)
    }()
    
    // Your application logic here
    runApplication(client)
}
```

## Next Steps

1. **Explore Examples**: Check out the `examples/` directory for more complex applications
2. **Read API Documentation**: Review the generated API docs at `docs/api/sdk.md`
3. **Join the Community**: Contribute to PromptKit development on GitHub
4. **Build Tools**: Use Arena for testing and PackC for prompt management
5. **Deploy**: Follow production best practices for robust LLM applications

For more advanced topics, see:

- [Arena Testing Guide](./arena-user-guide.md)
- [PackC Compilation Guide](./packc-user-guide.md)  
- [API Reference](../api/sdk.md)
- [Architecture Overview](../architecture/system-overview.md)
