---
title: Providers
sidebar:
  order: 3
---
LLM provider implementations with unified API.

## Overview

PromptKit supports multiple LLM providers through a common interface:

- **OpenAI**: GPT-4, GPT-4o, GPT-3.5
- **Anthropic Claude**: Claude 3.5 Sonnet, Claude 3 Opus, Claude 3 Haiku
- **Google Gemini**: Gemini 1.5 Pro, Gemini 1.5 Flash
- **Ollama**: Local LLMs (Llama, Mistral, LLaVA, DeepSeek)
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

### MultimodalSupport

Providers that support images, audio, or video inputs implement `MultimodalSupport`:

```go
type MultimodalSupport interface {
    Provider
    
    // Get supported multimodal capabilities
    GetMultimodalCapabilities() MultimodalCapabilities
    
    // Execute with multimodal content
    PredictMultimodal(ctx context.Context, req PredictionRequest) (PredictionResponse, error)
    
    // Stream with multimodal content
    PredictMultimodalStream(ctx context.Context, req PredictionRequest) (<-chan StreamChunk, error)
}
```

**MultimodalCapabilities**:

```go
type MultimodalCapabilities struct {
    SupportsImages bool       // Can process image inputs
    SupportsAudio  bool       // Can process audio inputs
    SupportsVideo  bool       // Can process video inputs
    ImageFormats   []string   // Supported image MIME types
    AudioFormats   []string   // Supported audio MIME types
    VideoFormats   []string   // Supported video MIME types
    MaxImageSizeMB int        // Max image size (0 = unlimited/unknown)
    MaxAudioSizeMB int        // Max audio size (0 = unlimited/unknown)
    MaxVideoSizeMB int        // Max video size (0 = unlimited/unknown)
}
```

**Provider Multimodal Support**:

| Provider | Images | Audio | Video | Notes |
|----------|--------|-------|-------|-------|
| OpenAI GPT-4o/4o-mini | ✅ | ❌ | ❌ | JPEG, PNG, GIF, WebP |
| Anthropic Claude 3.5 | ✅ | ❌ | ❌ | JPEG, PNG, GIF, WebP |
| Google Gemini 1.5 | ✅ | ✅ | ✅ | Full multimodal support |
| Ollama (LLaVA, Llama 3.2 Vision) | ✅ | ❌ | ❌ | JPEG, PNG, GIF, WebP |

**Helper Functions**:

```go
// Check if provider supports multimodal
func SupportsMultimodal(p Provider) bool

// Get multimodal provider (returns nil if not supported)
func GetMultimodalProvider(p Provider) MultimodalSupport

// Check specific media type support
func HasImageSupport(p Provider) bool
func HasAudioSupport(p Provider) bool
func HasVideoSupport(p Provider) bool

// Check format compatibility
func IsFormatSupported(p Provider, contentType string, mimeType string) bool

// Validate message compatibility
func ValidateMultimodalMessage(p Provider, msg types.Message) error
```

**Usage Example**:

```go
// Check capabilities
if providers.HasImageSupport(provider) {
    caps := providers.GetMultimodalProvider(provider).GetMultimodalCapabilities()
    fmt.Printf("Max image size: %d MB\n", caps.MaxImageSizeMB)
}

// Send multimodal request
req := providers.PredictionRequest{
    System: "You are a helpful assistant.",
    Messages: []types.Message{
        {
            Role: "user",
            Parts: []types.ContentPart{
                {Type: "text", Text: "What's in this image?"},
                {
                    Type: "image",
                    Media: &types.MediaContent{
                        Type:     "image",
                        MIMEType: "image/jpeg",
                        Data:     imageBase64,
                    },
                },
            },
        },
    },
}

if mp := providers.GetMultimodalProvider(provider); mp != nil {
    resp, err := mp.PredictMultimodal(ctx, req)
}
```

### MultimodalToolSupport

Providers that support both multimodal content and function calling implement `MultimodalToolSupport`:

```go
type MultimodalToolSupport interface {
    MultimodalSupport
    ToolSupport
    
    // Execute with both multimodal content and tools
    PredictMultimodalWithTools(
        ctx context.Context,
        req PredictionRequest,
        tools interface{},
        toolChoice string,
    ) (PredictionResponse, []types.MessageToolCall, error)
}
```

**Usage Example**:

```go
// Use images with tool calls
tools, _ := provider.BuildTooling(toolDescriptors)

resp, toolCalls, err := provider.PredictMultimodalWithTools(
    ctx,
    multimodalRequest,
    tools,
    "auto",
)

// Response contains both text and any tool calls
fmt.Println(resp.Content)
for _, call := range toolCalls {
    fmt.Printf("Tool called: %s\n", call.Name)
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

## Ollama Provider

Run local LLMs with zero API costs using [Ollama](https://ollama.ai/). Uses the OpenAI-compatible `/v1/chat/completions` endpoint.

### Constructor

```go
func NewOllamaProvider(
    id string,
    model string,
    baseURL string,
    defaults ProviderDefaults,
    includeRawOutput bool,
    additionalConfig map[string]interface{},
) *OllamaProvider
```

**Parameters**:
- `id`: Provider identifier (e.g., "ollama-llama")
- `model`: Model name (e.g., "llama3.2:1b", "mistral", "llava")
- `baseURL`: Ollama server URL (default: `http://localhost:11434`)
- `defaults`: Default parameters and pricing (typically zero cost)
- `includeRawOutput`: Include raw API response in output
- `additionalConfig`: Extra options including `keep_alive` for model persistence

**Environment**:
- No API key required (local inference)
- `OLLAMA_HOST`: Optional, alternative to `baseURL` parameter

**Example**:
```go
provider := ollama.NewOllamaProvider(
    "ollama",
    "llama3.2:1b",
    "http://localhost:11434",
    ollama.DefaultProviderDefaults(),
    false,
    map[string]interface{}{
        "keep_alive": "5m",  // Keep model loaded for 5 minutes
    },
)
defer provider.Close()
```

### Supported Models

Any model available via `ollama pull`. Common models include:

| Model | Context | Cost |
|-------|---------|------|
| `llama3.2:1b` | 128K | Free (local) |
| `llama3.2:3b` | 128K | Free (local) |
| `llama3.1:8b` | 128K | Free (local) |
| `mistral` | 32K | Free (local) |
| `deepseek-r1:8b` | 64K | Free (local) |
| `phi3:mini` | 128K | Free (local) |
| `llava` | 4K | Free (local) |
| `llama3.2-vision` | 128K | Free (local) |

Run `ollama list` to see installed models, or `ollama pull <model>` to download new ones.

### Features

- ✅ Streaming support
- ✅ Function calling (tool use)
- ✅ Multimodal (vision) - LLaVA, Llama 3.2 Vision
- ✅ Zero cost (local inference)
- ✅ Model persistence (`keep_alive` parameter)
- ✅ OpenAI-compatible API
- ❌ No API key required

### Tool Support

```go
toolProvider := ollama.NewOllamaToolProvider(
    "ollama",
    "llama3.2:1b",
    "http://localhost:11434",
    ollama.DefaultProviderDefaults(),
    false,
    map[string]interface{}{"keep_alive": "5m"},
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
    "auto",  // Tool choice: "auto", "required", "none"
)
```

### Configuration via YAML

```yaml
spec:
  id: "ollama-llama"
  type: ollama
  model: llama3.2:1b
  base_url: "http://localhost:11434"
  additional_config:
    keep_alive: "5m"
```

### Docker Setup

Run Ollama with Docker Compose:

```yaml
services:
  ollama:
    image: ollama/ollama:latest
    ports:
      - "11434:11434"
    volumes:
      - ollama_data:/root/.ollama
    healthcheck:
      test: ["CMD-SHELL", "ollama list || exit 1"]
      interval: 10s
      timeout: 30s
      retries: 5
      start_period: 30s

volumes:
  ollama_data:
```

Then pull a model:
```bash
docker exec ollama ollama pull llama3.2:1b
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

**Ollama**:
```go
func DefaultProviderDefaults() ProviderDefaults {
    return ProviderDefaults{
        Temperature: 0.7,
        TopP:        0.9,
        MaxTokens:   2048,
        Pricing: Pricing{
            InputCostPer1K:  0.0,  // Local inference - free
            OutputCostPer1K: 0.0,
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
- `OLLAMA_HOST`: Ollama server URL (default: `http://localhost:11434`)

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

- [MediaLoader](#medialoader) - Unified media loading
- [Pipeline Reference](pipeline) - Using providers in pipelines
- [Tools Reference](tools) - Function calling
- [Provider How-To](../how-to/configure-providers) - Configuration guide
- [Provider Explanation](../explanation/provider-architecture) - Architecture details

## MediaLoader

Unified interface for loading media content from various sources (inline data, storage references, file paths, URLs).

### Overview

`MediaLoader` abstracts media access, allowing providers to load media transparently regardless of where it's stored. This is essential for media externalization, where large media is stored on disk instead of being kept in memory.

```go
import "github.com/AltairaLabs/PromptKit/runtime/providers"

loader := providers.NewMediaLoader(providers.MediaLoaderConfig{
    StorageService: fileStore,
    HTTPTimeout:    30 * time.Second,
    MaxURLSizeBytes: 50 * 1024 * 1024, // 50 MB
})
```

### Type Definition

```go
type MediaLoader struct {
    // Unexported fields
}
```

### Constructor

#### NewMediaLoader

Creates a new MediaLoader with the specified configuration.

```go
func NewMediaLoader(config MediaLoaderConfig) *MediaLoader
```

**Parameters:**

- `config` - MediaLoaderConfig with storage service and options

**Returns:**

- `*MediaLoader` - Ready-to-use media loader

**Example:**

```go
import (
    "github.com/AltairaLabs/PromptKit/runtime/providers"
    "github.com/AltairaLabs/PromptKit/runtime/storage/local"
)

fileStore := local.NewFileStore(local.FileStoreConfig{
    BaseDir: "./media",
})

loader := providers.NewMediaLoader(providers.MediaLoaderConfig{
    StorageService: fileStore,
    HTTPTimeout:    30 * time.Second,
    MaxURLSizeBytes: 50 * 1024 * 1024,
})
```

### Configuration

#### MediaLoaderConfig

Configuration for MediaLoader instances.

```go
type MediaLoaderConfig struct {
    StorageService   storage.MediaStorageService // Required for storage references
    HTTPTimeout      time.Duration               // Timeout for URL fetches
    MaxURLSizeBytes  int64                       // Max size for URL content
}
```

**Fields:**

**StorageService** - Media storage backend

- Required if loading from storage references
- Typically a FileStore or cloud storage backend
- Set to nil if not using media externalization

**HTTPTimeout** - HTTP request timeout for URLs

- Default: 30 seconds
- Applies to URL fetches only
- Set to 0 for no timeout

**MaxURLSizeBytes** - Maximum size for URL content

- Default: 50 MB
- Prevents downloading huge files
- Returns error if content larger

### Methods

#### GetBase64Data

Loads media content from any source and returns base64-encoded data.

```go
func (l *MediaLoader) GetBase64Data(
    ctx context.Context,
    media *types.MediaContent,
) (string, error)
```

**Parameters:**

- `ctx` - Context for cancellation and timeout
- `media` - MediaContent with one or more sources

**Returns:**

- `string` - Base64-encoded media data
- `error` - Load errors (not found, timeout, size limit, etc.)

**Source Priority:**

Media is loaded from the first available source in this order:

1. **Data** - Inline base64 data (if present)
2. **StorageReference** - External storage (requires StorageService)
3. **FilePath** - Local file system path
4. **URL** - HTTP/HTTPS URL (with timeout and size limits)

**Example:**

```go
// Load from any source
data, err := loader.GetBase64Data(ctx, media)
if err != nil {
    log.Printf("Failed to load media: %v", err)
    return err
}

// Use the data
fmt.Printf("Loaded %d bytes\n", len(data))
```

### Usage Examples

#### Basic Usage

```go
// Media with inline data
media := &types.MediaContent{
    Type:     "image",
    MimeType: "image/png",
    Data:     "iVBORw0KGgoAAAANSUhEUg...", // Base64
}

data, err := loader.GetBase64Data(ctx, media)
// Returns media.Data immediately (already inline)
```

#### Load from Storage

```go
// Media externalized to storage
media := &types.MediaContent{
    Type:     "image",
    MimeType: "image/png",
    StorageReference: &storage.StorageReference{
        ID:      "abc123-def456-ghi789",
        Backend: "file",
    },
}

data, err := loader.GetBase64Data(ctx, media)
// Loads from disk via StorageService
```

#### Load from File Path

```go
// Media from local file
media := &types.MediaContent{
    Type:     "image",
    MimeType: "image/jpeg",
    FilePath: "/path/to/image.jpg",
}

data, err := loader.GetBase64Data(ctx, media)
// Reads file and converts to base64
```

#### Load from URL

```go
// Media from HTTP URL
media := &types.MediaContent{
    Type:     "image",
    MimeType: "image/png",
    URL:      "https://example.com/image.png",
}

data, err := loader.GetBase64Data(ctx, media)
// Fetches URL with timeout and size checks
```

#### Provider Integration

```go
// Provider using MediaLoader
type MyProvider struct {
    mediaLoader *providers.MediaLoader
}

func (p *MyProvider) Predict(ctx context.Context, req providers.PredictionRequest) (providers.PredictionResponse, error) {
    // Load media from messages
    for _, msg := range req.Messages {
        for _, content := range msg.Content {
            if content.Media != nil {
                // Load media transparently
                data, err := p.mediaLoader.GetBase64Data(ctx, content.Media)
                if err != nil {
                    return providers.PredictionResponse{}, err
                }
                
                // Use data in API call
                // ...
            }
        }
    }
    
    // Call LLM API
    // ...
}
```

### Error Handling

MediaLoader returns specific errors:

```go
data, err := loader.GetBase64Data(ctx, media)
if err != nil {
    switch {
    case errors.Is(err, providers.ErrNoMediaSource):
        // No source available (no Data, StorageReference, FilePath, or URL)
    case errors.Is(err, providers.ErrMediaNotFound):
        // Storage reference or file path not found
    case errors.Is(err, providers.ErrMediaTooLarge):
        // URL content exceeds MaxURLSizeBytes
    case errors.Is(err, context.DeadlineExceeded):
        // HTTP timeout or context cancelled
    default:
        // Other errors (permission, network, etc.)
    }
}
```

### Performance Considerations

#### Caching

MediaLoader does not cache loaded media. For repeated access:

```go
// Cache loaded media yourself
mediaCache := make(map[string]string)

data, ok := mediaCache[media.StorageReference.ID]
if !ok {
    var err error
    data, err = loader.GetBase64Data(ctx, media)
    if err != nil {
        return err
    }
    mediaCache[media.StorageReference.ID] = data
}
```

#### Async Loading

For loading multiple media items in parallel:

```go
type loadResult struct {
    data string
    err  error
}

// Load media concurrently
results := make([]loadResult, len(mediaItems))
var wg sync.WaitGroup

for i, media := range mediaItems {
    wg.Add(1)
    go func(idx int, m *types.MediaContent) {
        defer wg.Done()
        data, err := loader.GetBase64Data(ctx, m)
        results[idx] = loadResult{data, err}
    }(i, media)
}

wg.Wait()

// Check results
for i, result := range results {
    if result.err != nil {
        log.Printf("Failed to load media %d: %v", i, result.err)
    }
}
```

### Best Practices

**✅ Do:**

- Create one MediaLoader per application (reuse)
- Set reasonable HTTP timeout (30s is good default)
- Set MaxURLSizeBytes to prevent abuse
- Handle errors gracefully (media may be unavailable)
- Use context for cancellation support

**❌ Don't:**

- Don't create MediaLoader per request (expensive)
- Don't ignore errors (media may be corrupted/missing)
- Don't set timeout too low (large images take time)
- Don't allow unlimited URL sizes (DoS risk)
- Don't cache without bounds (memory leak)

### Integration with Media Storage

MediaLoader and media storage work together:

```go
// 1. Set up storage
fileStore := local.NewFileStore(local.FileStoreConfig{
    BaseDir: "./media",
})

// 2. Create loader with storage
loader := providers.NewMediaLoader(providers.MediaLoaderConfig{
    StorageService: fileStore,
})

// 3. Use in SDK
manager, _ := sdk.NewConversationManager(
    sdk.WithProvider(provider),
    sdk.WithMediaStorage(fileStore), // Externalizes media
)

// 4. Provider automatically uses loader
// Media externalized by MediaExternalizer middleware
// Provider loads via MediaLoader when needed
// Application code remains unchanged
```

**Flow:**

```text
1. User sends image → inline Data
2. LLM returns generated image → inline Data
3. MediaExternalizer → externalizes to storage, clears Data, adds StorageReference
4. State saved → only reference in Redis/Postgres
5. Next turn: Provider needs image → MediaLoader loads from StorageReference
6. Transparent to application code
```

### See Also

- [Storage Reference](storage) - Media storage backends
- [Types Reference](types#mediacontent) - MediaContent structure
- [How-To: Configure Media Storage](../../sdk/how-to/configure-media-storage) - Setup guide
- [Explanation: Media Storage](../../sdk/explanation/media-storage) - Design and architecture
