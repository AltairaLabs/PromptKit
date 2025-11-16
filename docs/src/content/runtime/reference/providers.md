---
title: Providers
docType: reference
order: 3
---
# Provider Reference

LLM provider implementations with unified API.

## Overview

PromptKit supports multiple LLM providers through a common interface:

- **OpenAI**: GPT-4, GPT-4o, GPT-3.5
- **Anthropic Claude**: Claude 3.5 Sonnet, Claude 3 Opus, Claude 3 Haiku
- **Google Gemini**: Gemini 1.5 Pro, Gemini 1.5 Flash
- **Mock**: Testing and development

All providers implement the `Provider` interface for text completion and `ToolSupport` interface for function calling.

## Core Interfaces

### Provider

```go
type Provider interface {
    ID() string
    Predict(ctx context.Context, req PredictionRequest) (PredictionResponse, error)
    
    // Streaming
    PredictStream(ctx context.Context, req PredictionRequest) (<-chan StreamChunk, error)
    SupportsStreaming() bool
    
    ShouldIncludeRawOutput() bool
    Close() error
    
    // Cost calculation
    CalculateCost(inputTokens, outputTokens, cachedTokens int) types.CostInfo
}
```

### ToolSupport

```go
type ToolSupport interface {
    Provider
    
    // Convert tools to provider format
    BuildTooling(descriptors []*ToolDescriptor) (interface{}, error)
    
    // Execute with tools
    PredictWithTools(
        ctx context.Context,
        req PredictionRequest,
        tools interface{},
        toolChoice string,
    ) (PredictionResponse, []types.MessageToolCall, error)
}
```

## Request/Response Types

### PredictionRequest

```go
type PredictionRequest struct {
    System      string
    Messages    []types.Message
    Temperature float32
    TopP        float32
    MaxTokens   int
    Seed        *int
    Metadata    map[string]interface{}
}
```

### PredictionResponse

```go
type PredictionResponse struct {
    Content    string
    Parts      []types.ContentPart  // Multimodal content
    CostInfo   *types.CostInfo
    Latency    time.Duration
    Raw        []byte               // Raw API response
    RawRequest interface{}          // Raw API request
    ToolCalls  []types.MessageToolCall
}
```

### ProviderDefaults

```go
type ProviderDefaults struct {
    Temperature float32
    TopP        float32
    MaxTokens   int
    Pricing     Pricing
}
```

### Pricing

```go
type Pricing struct {
    InputCostPer1K  float64  // Per 1K input tokens
    OutputCostPer1K float64  // Per 1K output tokens
}
```

## OpenAI Provider

### Constructor

```go
func NewOpenAIProvider(
    id string,
    model string,
    baseURL string,
    defaults ProviderDefaults,
    includeRawOutput bool,
) *OpenAIProvider
```

**Parameters**:
- `id`: Provider identifier (e.g., "openai-gpt4")
- `model`: Model name (e.g., "gpt-4o-mini", "gpt-4-turbo")
- `baseURL`: Custom API URL (empty for default `https://api.openai.com/v1`)
- `defaults`: Default parameters and pricing
- `includeRawOutput`: Include raw API response in output

**Environment**:
- `OPENAI_API_KEY`: Required API key

**Example**:
```go
provider := openai.NewOpenAIProvider(
    "openai",
    "gpt-4o-mini",
    "",  // Use default URL
    openai.DefaultProviderDefaults(),
    false,
)
defer provider.Close()
```

### Supported Models

| Model | Context | Cost (Input/Output per 1M tokens) |
|-------|---------|-----------------------------------|
| `gpt-4o` | 128K | $2.50 / $10.00 |
| `gpt-4o-mini` | 128K | $0.15 / $0.60 |
| `gpt-4-turbo` | 128K | $10.00 / $30.00 |
| `gpt-4` | 8K | $30.00 / $60.00 |
| `gpt-3.5-turbo` | 16K | $0.50 / $1.50 |

### Features

- ✅ Streaming support
- ✅ Function calling
- ✅ Multimodal (vision)
- ✅ JSON mode
- ✅ Seed for reproducibility
- ✅ Token counting

### Tool Support

```go
// Create tool provider
toolProvider := openai.NewOpenAIToolProvider(
    "openai",
    "gpt-4o-mini",
    "",
    openai.DefaultProviderDefaults(),
    false,
    nil,  // Additional config
)

// Build tools in OpenAI format
tools, err := toolProvider.BuildTooling(toolDescriptors)
if err != nil {
    log.Fatal(err)
}

// Execute with tools
response, toolCalls, err := toolProvider.PredictWithTools(
    ctx,
    req,
    tools,
    "auto",  // Tool choice: "auto", "required", "none", or specific tool
)
```

## Anthropic Claude Provider

### Constructor

```go
func NewClaudeProvider(
    id string,
    model string,
    baseURL string,
    defaults ProviderDefaults,
    includeRawOutput bool,
) *ClaudeProvider
```

**Environment**:
- `ANTHROPIC_API_KEY`: Required API key

**Example**:
```go
provider := claude.NewClaudeProvider(
    "claude",
    "claude-3-5-sonnet-20241022",
    "",  // Use default URL
    claude.DefaultProviderDefaults(),
    false,
)
defer provider.Close()
```

### Supported Models

| Model | Context | Cost (Input/Output per 1M tokens) |
|-------|---------|-----------------------------------|
| `claude-3-5-sonnet-20241022` | 200K | $3.00 / $15.00 |
| `claude-3-opus-20240229` | 200K | $15.00 / $75.00 |
| `claude-3-haiku-20240307` | 200K | $0.25 / $1.25 |

### Features

- ✅ Streaming support
- ✅ Tool calling
- ✅ Multimodal (vision)
- ✅ Extended context (200K tokens)
- ✅ Prompt caching
- ✅ System prompts

### Tool Support

```go
toolProvider := claude.NewClaudeToolProvider(
    "claude",
    "claude-3-5-sonnet-20241022",
    "",
    claude.DefaultProviderDefaults(),
    false,
)

response, toolCalls, err := toolProvider.PredictWithTools(ctx, req, tools, "auto")
```

## Google Gemini Provider

### Constructor

```go
func NewGeminiProvider(
    id string,
    model string,
    baseURL string,
    defaults ProviderDefaults,
    includeRawOutput bool,
) *GeminiProvider
```

**Environment**:
- `GEMINI_API_KEY`: Required API key

**Example**:
```go
provider := gemini.NewGeminiProvider(
    "gemini",
    "gemini-1.5-flash",
    "",
    gemini.DefaultProviderDefaults(),
    false,
)
defer provider.Close()
```

### Supported Models

| Model | Context | Cost (Input/Output per 1M tokens) |
|-------|---------|-----------------------------------|
| `gemini-1.5-pro` | 2M | $1.25 / $5.00 |
| `gemini-1.5-flash` | 1M | $0.075 / $0.30 |

### Features

- ✅ Streaming support
- ✅ Function calling
- ✅ Multimodal (vision, audio, video)
- ✅ Extended context (up to 2M tokens)
- ✅ Grounding with Google Search

### Tool Support

```go
toolProvider := gemini.NewGeminiToolProvider(
    "gemini",
    "gemini-1.5-flash",
    "",
    gemini.DefaultProviderDefaults(),
    false,
)

response, toolCalls, err := toolProvider.PredictWithTools(ctx, req, tools, "auto")
```

## Mock Provider

For testing and development.

### Constructor

```go
func NewMockProvider(
    id string,
    model string,
    includeRawOutput bool,
) *MockProvider
```

**Example**:
```go
provider := mock.NewMockProvider("mock", "test-model", false)

// Configure responses
provider.AddResponse("Hello", "Hi there!")
provider.AddResponse("What is 2+2?", "4")
```

### With Repository

```go
// Custom response repository
repo := &CustomMockRepository{
    responses: map[string]string{
        "hello": "Hello! How can I help?",
        "bye":   "Goodbye!",
    },
}

provider := mock.NewMockProviderWithRepository("mock", "test-model", false, repo)
```

### Tool Support

```go
toolProvider := mock.NewMockToolProvider("mock", "test-model", false, nil)

// Configure tool call responses
toolProvider.ConfigureToolResponse("get_weather", `{"temp": 72, "conditions": "sunny"}`)
```

## Usage Examples

### Basic Completion

```go
import (
    "context"
    "github.com/AltairaLabs/PromptKit/runtime/providers/openai"
)

provider := openai.NewOpenAIProvider(
    "openai",
    "gpt-4o-mini",
    "",
    openai.DefaultProviderDefaults(),
    false,
)
defer provider.Close()

req := providers.PredictionRequest{
    System:      "You are a helpful assistant.",
    Messages:    []types.Message{
        {Role: "user", Content: "What is 2+2?"},
    },
    Temperature: 0.7,
    MaxTokens:   100,
}

ctx := context.Background()
response, err := provider.Predict(ctx, req)
if err != nil {
    log.Fatal(err)
}

fmt.Printf("Response: %s\n", response.Content)
fmt.Printf("Cost: $%.6f\n", response.CostInfo.TotalCost)
fmt.Printf("Latency: %v\n", response.Latency)
```

### Streaming Completion

```go
streamChan, err := provider.PredictStream(ctx, req)
if err != nil {
    log.Fatal(err)
}

var fullContent string
for chunk := range streamChan {
    if chunk.Error != nil {
        log.Printf("Stream error: %v\n", chunk.Error)
        break
    }
    
    if chunk.Delta != "" {
        fullContent += chunk.Delta
        fmt.Print(chunk.Delta)
    }
    
    if chunk.Done {
        fmt.Printf("\n\nComplete! Tokens: %d\n", chunk.TokenCount)
    }
}
```

### With Function Calling

```go
toolProvider := openai.NewOpenAIToolProvider(
    "openai",
    "gpt-4o-mini",
    "",
    openai.DefaultProviderDefaults(),
    false,
    nil,
)

// Define tools
toolDescs := []*providers.ToolDescriptor{
    {
        Name:        "get_weather",
        Description: "Get current weather for a location",
        InputSchema: json.RawMessage(`{
            "type": "object",
            "properties": {
                "location": {"type": "string", "description": "City name"}
            },
            "required": ["location"]
        }`),
    },
}

// Build tools in provider format
tools, err := toolProvider.BuildTooling(toolDescs)
if err != nil {
    log.Fatal(err)
}

// Execute with tools
req.Messages = []types.Message{
    {Role: "user", Content: "What's the weather in San Francisco?"},
}

response, toolCalls, err := toolProvider.PredictWithTools(ctx, req, tools, "auto")
if err != nil {
    log.Fatal(err)
}

// Process tool calls
for _, call := range toolCalls {
    fmt.Printf("Tool: %s\n", call.Name)
    fmt.Printf("Args: %s\n", call.Arguments)
}
```

### Multimodal (Vision)

```go
// Create message with image
msg := types.Message{
    Role:    "user",
    Content: "What's in this image?",
    Parts: []types.ContentPart{
        {
            Type: "image",
            ImageURL: &types.ImageURL{
                URL: "data:image/jpeg;base64,/9j/4AAQSkZJRg...",
            },
        },
    },
}

req.Messages = []types.Message{msg}
response, err := provider.Predict(ctx, req)
```

### Cost Calculation

```go
// Manual cost calculation
costInfo := provider.CalculateCost(
    1000,  // Input tokens
    500,   // Output tokens
    0,     // Cached tokens
)

fmt.Printf("Input cost: $%.6f\n", costInfo.InputCost)
fmt.Printf("Output cost: $%.6f\n", costInfo.OutputCost)
fmt.Printf("Total cost: $%.6f\n", costInfo.TotalCost)
```

### Custom Provider Configuration

```go
// Custom pricing
customDefaults := providers.ProviderDefaults{
    Temperature: 0.8,
    TopP:        0.95,
    MaxTokens:   2000,
    Pricing: providers.Pricing{
        InputCostPer1K:  0.0001,
        OutputCostPer1K: 0.0002,
    },
}

provider := openai.NewOpenAIProvider(
    "custom-openai",
    "gpt-4o-mini",
    "",
    customDefaults,
    true,  // Include raw output
)
```

## Configuration

### Default Provider Settings

**OpenAI**:
```go
func DefaultProviderDefaults() ProviderDefaults {
    return ProviderDefaults{
        Temperature: 0.7,
        TopP:        1.0,
        MaxTokens:   2000,
        Pricing: Pricing{
            InputCostPer1K:  0.00015,  // gpt-4o-mini
            OutputCostPer1K: 0.0006,
        },
    }
}
```

**Claude**:
```go
func DefaultProviderDefaults() ProviderDefaults {
    return ProviderDefaults{
        Temperature: 0.7,
        TopP:        1.0,
        MaxTokens:   4096,
        Pricing: Pricing{
            InputCostPer1K:  0.003,    // claude-3-5-sonnet
            OutputCostPer1K: 0.015,
        },
    }
}
```

**Gemini**:
```go
func DefaultProviderDefaults() ProviderDefaults {
    return ProviderDefaults{
        Temperature: 0.7,
        TopP:        0.95,
        MaxTokens:   8192,
        Pricing: Pricing{
            InputCostPer1K:  0.000075,  // gemini-1.5-flash
            OutputCostPer1K: 0.0003,
        },
    }
}
```

### Environment Variables

All providers support environment variable configuration:

- `OPENAI_API_KEY`: OpenAI authentication
- `ANTHROPIC_API_KEY`: Anthropic authentication
- `GEMINI_API_KEY`: Google Gemini authentication
- `OPENAI_BASE_URL`: Custom OpenAI-compatible endpoint
- `ANTHROPIC_BASE_URL`: Custom Claude endpoint
- `GEMINI_BASE_URL`: Custom Gemini endpoint

## Best Practices

### 1. Resource Management

```go
// Always close providers
provider := openai.NewOpenAIProvider(...)
defer provider.Close()
```

### 2. Error Handling

```go
response, err := provider.Predict(ctx, req)
if err != nil {
    // Check for specific error types
    if strings.Contains(err.Error(), "rate_limit_exceeded") {
        // Implement backoff
        time.Sleep(time.Second * 5)
        return retry()
    }
    return err
}
```

### 3. Cost Monitoring

```go
// Track costs across requests
var totalCost float64
for _, result := range results {
    totalCost += result.CostInfo.TotalCost
}
fmt.Printf("Total spend: $%.6f\n", totalCost)
```

### 4. Timeout Management

```go
// Use context timeout
ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
defer cancel()

response, err := provider.Predict(ctx, req)
```

### 5. Streaming Best Practices

```go
// Always drain channel
streamChan, err := provider.PredictStream(ctx, req)
if err != nil {
    return err
}

for chunk := range streamChan {
    if chunk.Error != nil {
        // Handle error but continue draining
        log.Printf("Error: %v", chunk.Error)
        continue
    }
    processChunk(chunk)
}
```

## Performance Considerations

### Latency

- **OpenAI**: 200-500ms TTFT, 1-3s total for short responses
- **Claude**: 300-600ms TTFT, similar total latency
- **Gemini**: 150-400ms TTFT, faster for simple queries

### Throughput

- **Rate limits**: Vary by provider and tier
  - OpenAI: 3,500-10,000 RPM
  - Claude: 4,000-50,000 RPM
  - Gemini: 2,000-15,000 RPM

### Cost Optimization

- Use mini/flash models for simple tasks
- Implement caching for repeated queries
- Use streaming for better UX (doesn't reduce cost)
- Monitor token usage and set appropriate `MaxTokens`

## See Also

- [Pipeline Reference](pipeline) - Using providers in pipelines
- [Tools Reference](tools) - Function calling
- [Provider How-To](../how-to/configure-providers) - Configuration guide
- [Provider Explanation](../explanation/provider-architecture) - Architecture details
