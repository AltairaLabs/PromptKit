---
title: 'Tutorial 1: First Pipeline'
sidebar:
  order: 1
---
Learn the basics by building a simple LLM application.

**Time**: 15 minutes  
**Level**: Beginner

## What You'll Build

A command-line application that sends prompts to an LLM and displays responses.

## What You'll Learn

- Create a pipeline
- Configure an LLM provider
- Execute requests
- Handle responses
- Track costs

## Prerequisites

- Go 1.21+
- OpenAI API key (get one at [platform.openai.com](https://platform.openai.com))

## Step 1: Set Up Your Project

Create a new Go module:

```bash
mkdir my-llm-app
cd my-llm-app
go mod init my-llm-app
```

Install PromptKit:

```bash
go get github.com/AltairaLabs/PromptKit/runtime@latest
```

## Step 2: Set Your API Key

Export your OpenAI API key:

```bash
export OPENAI_API_KEY="sk-..."
```

## Step 3: Create Your First Pipeline

Create `main.go`:

```go
package main

import (
    "context"
    "fmt"
    "log"
    "os"
    
    "github.com/AltairaLabs/PromptKit/runtime/pipeline"
    "github.com/AltairaLabs/PromptKit/runtime/pipeline/middleware"
    "github.com/AltairaLabs/PromptKit/runtime/providers/openai"
)

func main() {
    // Step 1: Create provider
    provider := openai.NewOpenAIProvider(
        "openai",
        "gpt-4o-mini",
        os.Getenv("OPENAI_API_KEY"),
        openai.DefaultProviderDefaults(),
        false,
    )
    defer provider.Close()
    
    // Step 2: Build pipeline
    pipe := pipeline.NewPipeline(
        middleware.ProviderMiddleware(provider, nil, nil, &middleware.ProviderMiddlewareConfig{
            MaxTokens:   500,
            Temperature: 0.7,
        }),
    )
    defer pipe.Shutdown(context.Background())
    
    // Step 3: Execute request
    ctx := context.Background()
    result, err := pipe.Execute(ctx, "user", "What is artificial intelligence?")
    if err != nil {
        log.Fatal(err)
    }
    
    // Step 4: Display response
    fmt.Printf("Response: %s\n", result.Response.Content)
    fmt.Printf("Tokens: %d\n", result.Response.Usage.TotalTokens)
    fmt.Printf("Cost: $%.6f\n", result.Cost.TotalCost)
}
```

## Step 4: Run Your Application

```bash
go run main.go
```

You should see output like:

```
Response: Artificial intelligence (AI) refers to the simulation of human intelligence...
Tokens: 152
Cost: $0.000023
```

## Understanding the Code

### 1. Create Provider

```go
provider := openai.NewOpenAIProvider(
    "openai",           // Provider name
    "gpt-4o-mini",      // Model (cost-effective)
    os.Getenv("OPENAI_API_KEY"),  // API key
    openai.DefaultProviderDefaults(),  // Default settings
    false,              // Debug mode off
)
```

The provider connects to OpenAI's API. We use `gpt-4o-mini` for cost-effectiveness.

### 2. Build Pipeline

```go
pipe := pipeline.NewPipeline(
    middleware.ProviderMiddleware(provider, nil, nil, config),
)
```

The pipeline processes requests through middleware. `ProviderMiddleware` sends requests to the LLM.

### 3. Execute Request

```go
result, err := pipe.Execute(ctx, "user", "What is artificial intelligence?")
```

`Execute()` takes:
- Context for cancellation
- Role (`"user"` for user messages)
- Content (your prompt)

### 4. Handle Response

```go
fmt.Printf("Response: %s\n", result.Response.Content)
fmt.Printf("Tokens: %d\n", result.Response.Usage.TotalTokens)
fmt.Printf("Cost: $%.6f\n", result.Cost.TotalCost)
```

The result contains:
- `Response.Content`: LLM's response text
- `Response.Usage`: Token counts
- `Cost.TotalCost`: Cost in dollars

## Experiment

Try modifying your application:

### 1. Change the Model

Use a more powerful model:

```go
provider := openai.NewOpenAIProvider(
    "openai",
    "gpt-4o",  // More capable, higher cost
    os.Getenv("OPENAI_API_KEY"),
    openai.DefaultProviderDefaults(),
    false,
)
```

### 2. Adjust Temperature

Make responses more creative:

```go
config := &middleware.ProviderMiddlewareConfig{
    MaxTokens:   500,
    Temperature: 1.0,  // More creative (0.0 = deterministic, 2.0 = very creative)
}
```

### 3. Limit Response Length

Reduce costs by limiting tokens:

```go
config := &middleware.ProviderMiddlewareConfig{
    MaxTokens:   100,  // Shorter responses
    Temperature: 0.7,
}
```

### 4. Multiple Questions

Ask several questions:

```go
questions := []string{
    "What is AI?",
    "What is machine learning?",
    "What is deep learning?",
}

for _, question := range questions {
    result, err := pipe.Execute(ctx, "user", question)
    if err != nil {
        log.Printf("Error: %v\n", err)
        continue
    }
    
    fmt.Printf("\nQ: %s\n", question)
    fmt.Printf("A: %s\n", result.Response.Content)
    fmt.Printf("Cost: $%.6f\n\n", result.Cost.TotalCost)
}
```

## Common Issues

### "authentication failed"

**Problem**: Invalid API key.

**Solution**: Check your API key is set:
```bash
echo $OPENAI_API_KEY
```

### "context deadline exceeded"

**Problem**: Request took too long.

**Solution**: Increase timeout:
```go
ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
defer cancel()
```

### High costs

**Problem**: Using expensive model or many tokens.

**Solution**: 
- Use `gpt-4o-mini` instead of `gpt-4o`
- Reduce `MaxTokens` to 100-300
- Monitor costs with `result.Cost.TotalCost`

## Complete Example

Here's the full application with better structure:

```go
package main

import (
    "context"
    "fmt"
    "log"
    "os"
    "time"
    
    "github.com/AltairaLabs/PromptKit/runtime/pipeline"
    "github.com/AltairaLabs/PromptKit/runtime/pipeline/middleware"
    "github.com/AltairaLabs/PromptKit/runtime/providers/openai"
)

func main() {
    // Validate API key
    apiKey := os.Getenv("OPENAI_API_KEY")
    if apiKey == "" {
        log.Fatal("OPENAI_API_KEY environment variable not set")
    }
    
    // Create provider
    provider := openai.NewOpenAIProvider(
        "openai",
        "gpt-4o-mini",
        apiKey,
        openai.DefaultProviderDefaults(),
        false,
    )
    defer provider.Close()
    
    // Build pipeline
    config := &middleware.ProviderMiddlewareConfig{
        MaxTokens:   500,
        Temperature: 0.7,
    }
    
    pipe := pipeline.NewPipeline(
        middleware.ProviderMiddleware(provider, nil, nil, config),
    )
    defer pipe.Shutdown(context.Background())
    
    // Create context with timeout
    ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
    defer cancel()
    
    // Get prompt from command line or use default
    prompt := "What is artificial intelligence?"
    if len(os.Args) > 1 {
        prompt = os.Args[1]
    }
    
    // Execute request
    fmt.Printf("Prompt: %s\n\n", prompt)
    result, err := pipe.Execute(ctx, "user", prompt)
    if err != nil {
        log.Fatalf("Request failed: %v", err)
    }
    
    // Display results
    fmt.Printf("Response:\n%s\n\n", result.Response.Content)
    fmt.Printf("--- Metrics ---\n")
    fmt.Printf("Input tokens:  %d\n", result.Response.Usage.PromptTokens)
    fmt.Printf("Output tokens: %d\n", result.Response.Usage.CompletionTokens)
    fmt.Printf("Total tokens:  %d\n", result.Response.Usage.TotalTokens)
    fmt.Printf("Cost:          $%.6f\n", result.Cost.TotalCost)
}
```

Run with custom prompt:

```bash
go run main.go "Explain quantum computing in simple terms"
```

## What You've Learned

✅ Create and configure a pipeline  
✅ Connect to an LLM provider  
✅ Execute basic requests  
✅ Handle responses  
✅ Track token usage and costs  
✅ Adjust model parameters  

## Next Steps

Continue to [Tutorial 2: Multi-Turn Conversations](02-multi-turn) to add conversation state and build a chatbot.

## See Also

- [Configure Pipeline](../how-to/configure-pipeline) - More configuration options
- [Setup Providers](../how-to/setup-providers) - Other LLM providers
- [Pipeline Reference](../reference/pipeline) - Complete API
