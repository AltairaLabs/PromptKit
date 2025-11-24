---
title: Types
docType: reference
order: 7
---
# Types Reference

Core data structures used throughout the Runtime.

## Message Types

### Message

```go
type Message struct {
    Role               string
    Content            string
    Parts              []ContentPart         // Multimodal content
    ToolCalls          []MessageToolCall     // Tools called
    ToolCallResponseID string                // For tool result messages
    Name               string                // For function/tool messages
}
```

Primary message structure for conversations.

**Example**:
```go
// Text message
msg := types.Message{
    Role:    "user",
    Content: "Hello, how are you?",
}

// Multimodal message
msg := types.Message{
    Role:    "user",
    Content: "Describe this image",
    Parts: []types.ContentPart{
        {
            Type: "image",
            ImageURL: &types.ImageURL{
                URL: "data:image/jpeg;base64,...",
            },
        },
    },
}

// Tool response message
msg := types.Message{
    Role:               "tool",
    Content:            `{"temp": 72}`,
    ToolCallResponseID: "call_abc123",
    Name:               "get_weather",
}
```

### ContentPart

```go
type ContentPart struct {
    Type      string      // "text", "image", "audio", "video"
    Text      string      // Text content
    ImageURL  *ImageURL   // Image reference
    AudioData *AudioData  // Audio content
    VideoData *VideoData  // Video content
}
```

Multimodal content component.

**Example**:
```go
// Text part
part := types.ContentPart{
    Type: "text",
    Text: "Analyze this image:",
}

// Image part
part := types.ContentPart{
    Type: "image",
    ImageURL: &types.ImageURL{
        URL: "https://example.com/image.jpg",
    },
}
```

### MessageToolCall

```go
type MessageToolCall struct {
    ID        string
    Name      string
    Arguments json.RawMessage
}
```

Tool/function call in a message.

**Example**:
```go
toolCall := types.MessageToolCall{
    ID:        "call_123",
    Name:      "get_weather",
    Arguments: json.RawMessage(`{"location": "SF"}`),
}
```

### MessageToolResult

```go
type MessageToolResult struct {
    ID      string
    Name    string
    Content json.RawMessage
    IsError bool
}
```

Result of tool execution.

**Example**:
```go
result := types.MessageToolResult{
    ID:      "call_123",
    Name:    "get_weather",
    Content: json.RawMessage(`{"temp": 72, "conditions": "sunny"}`),
    IsError: false,
}
```

## Cost Types

### CostInfo

```go
type CostInfo struct {
    InputTokens  int
    OutputTokens int
    CachedTokens int
    InputCost    float64
    OutputCost   float64
    CachedCost   float64
    TotalCost    float64
}
```

Token usage and cost breakdown.

**Example**:
```go
costInfo := types.CostInfo{
    InputTokens:  150,
    OutputTokens: 75,
    CachedTokens: 50,
    InputCost:    0.000225,  // $0.15 per 1M tokens
    OutputCost:   0.000450,  // $0.60 per 1M tokens
    CachedCost:   0.000000,  // Cached tokens free
    TotalCost:    0.000675,
}

fmt.Printf("Total cost: $%.6f\n", costInfo.TotalCost)
```

## Tool Types

### ToolDef

```go
type ToolDef struct {
    Name        string
    Description string
    Parameters  json.RawMessage  // JSON Schema
}
```

Tool definition for provider.

**Example**:
```go
tool := types.ToolDef{
    Name:        "get_weather",
    Description: "Get current weather",
    Parameters: json.RawMessage(`{
        "type": "object",
        "properties": {
            "location": {"type": "string"}
        },
        "required": ["location"]
    }`),
}
```

## Multimodal Types

### ImageURL

```go
type ImageURL struct {
    URL    string
    Detail string  // "auto", "low", "high"
}
```

Image reference for vision models.

**Example**:
```go
image := types.ImageURL{
    URL:    "data:image/jpeg;base64,/9j/4AAQSkZJRg...",
    Detail: "high",
}
```

### AudioData

```go
type AudioData struct {
    Data   string  // Base64 encoded
    Format string  // "mp3", "wav", etc.
}
```

Audio content for audio-capable models.

**Example**:
```go
audio := types.AudioData{
    Data:   "base64-encoded-audio-data",
    Format: "mp3",
}
```

### VideoData

```go
type VideoData struct {
    Data   string  // Base64 encoded or URL
    Format string  // "mp4", "webm", etc.
}
```

Video content for video-capable models.

### MediaContent

```go
type MediaContent struct {
    Type             string                      // "image", "audio", "video"
    MimeType         string                      // "image/png", "audio/mp3", etc.
    Data             string                      // Base64 encoded data (may be empty if externalized)
    FilePath         string                      // Local file path
    URL              string                      // HTTP/HTTPS URL
    StorageReference *storage.StorageReference   // External storage reference
}
```

Unified media container supporting multiple sources. Part of the media externalization system.

**Fields:**

- **Type**: Media type ("image", "audio", "video")
- **MimeType**: MIME type (e.g., "image/png", "audio/mp3")
- **Data**: Base64-encoded media data (empty if externalized)
- **FilePath**: Path to local file (alternative to Data)
- **URL**: HTTP/HTTPS URL (alternative to Data)
- **StorageReference**: Reference to externally stored media

**Source Priority:**

Media is loaded in this order:

1. **Data** - If present, use inline base64 data
2. **StorageReference** - Load from storage backend
3. **FilePath** - Load from local file system
4. **URL** - Fetch from HTTP/HTTPS

Use `MediaLoader.GetBase64Data()` to load transparently from any source.

**Examples:**

Inline data:

```go
media := &types.MediaContent{
    Type:     "image",
    MimeType: "image/png",
    Data:     "iVBORw0KGgoAAAANSUhEUg...",
}
```

Externalized to storage:

```go
media := &types.MediaContent{
    Type:     "image",
    MimeType: "image/png",
    Data:     "", // Cleared after externalization
    StorageReference: &storage.StorageReference{
        ID:      "abc123-def456-ghi789",
        Backend: "file",
        Metadata: map[string]string{
            "path": "/media/session-xyz/conv-abc/abc123-def456-ghi789.png",
        },
    },
}
```

From local file:

```go
media := &types.MediaContent{
    Type:     "image",
    MimeType: "image/jpeg",
    FilePath: "/path/to/user-upload.jpg",
}
```

From URL:

```go
media := &types.MediaContent{
    Type:     "image",
    MimeType: "image/jpeg",
    URL:      "https://example.com/photo.jpg",
}
```

**Media Externalization:**

When media exceeds the size threshold, it's automatically externalized:

```go
// Before externalization (from LLM response)
media := &types.MediaContent{
    Type:     "image",
    MimeType: "image/png",
    Data:     "iVBORw0..." // 2 MB base64 string
}

// After externalization (by MediaExternalizer middleware)
media := &types.MediaContent{
    Type:             "image",
    MimeType:         "image/png",
    Data:             "", // Cleared - memory saved!
    StorageReference: &storage.StorageReference{
        ID:      "abc123-def456-ghi789",
        Backend: "file",
    },
}
```

**Loading Media:**

Use MediaLoader for unified access:

```go
import "github.com/AltairaLabs/PromptKit/runtime/providers"

loader := providers.NewMediaLoader(providers.MediaLoaderConfig{
    StorageService: fileStore,
})

// Works with any source
data, err := loader.GetBase64Data(ctx, media)
if err != nil {
    log.Fatal(err)
}

// Now have base64 data regardless of original source
fmt.Printf("Loaded %d bytes\n", len(data))
```

**See Also:**

- [Storage Reference](storage) - MediaStorageService interface
- [Providers Reference](providers#medialoader) - MediaLoader documentation
- [How-To: Configure Media Storage](../../sdk/how-to/configure-media-storage) - Setup guide
- [Explanation: Media Storage](../../sdk/explanation/media-storage) - Design and architecture

## Usage Examples

### Building Conversations

```go
conversation := []types.Message{
    {
        Role:    "system",
        Content: "You are a helpful assistant.",
    },
    {
        Role:    "user",
        Content: "What is 2+2?",
    },
    {
        Role:    "assistant",
        Content: "2+2 equals 4.",
    },
    {
        Role:    "user",
        Content: "Now multiply that by 3.",
    },
}
```

### Multimodal Messages

```go
msg := types.Message{
    Role:    "user",
    Content: "What's in these images?",
    Parts: []types.ContentPart{
        {
            Type: "image",
            ImageURL: &types.ImageURL{
                URL:    "https://example.com/img1.jpg",
                Detail: "high",
            },
        },
        {
            Type: "image",
            ImageURL: &types.ImageURL{
                URL:    "https://example.com/img2.jpg",
                Detail: "high",
            },
        },
    },
}
```

### Tool Conversations

```go
conversation := []types.Message{
    {
        Role:    "user",
        Content: "What's the weather in SF?",
    },
    {
        Role:    "assistant",
        Content: "",
        ToolCalls: []types.MessageToolCall{
            {
                ID:        "call_123",
                Name:      "get_weather",
                Arguments: json.RawMessage(`{"location": "San Francisco"}`),
            },
        },
    },
    {
        Role:               "tool",
        Content:            `{"temp": 65, "conditions": "foggy"}`,
        ToolCallResponseID: "call_123",
        Name:               "get_weather",
    },
    {
        Role:    "assistant",
        Content: "It's 65°F and foggy in San Francisco.",
    },
}
```

### Cost Tracking

```go
var totalCost float64
var totalTokens int

for _, result := range results {
    totalCost += result.CostInfo.TotalCost
    totalTokens += result.CostInfo.InputTokens + result.CostInfo.OutputTokens
}

fmt.Printf("Total: %d tokens, $%.6f\n", totalTokens, totalCost)
```

## Type Conversions

### Message to JSON

```go
data, err := json.Marshal(message)
if err != nil {
    log.Fatal(err)
}
```

### JSON to Message

```go
var message types.Message
err := json.Unmarshal(data, &message)
if err != nil {
    log.Fatal(err)
}
```

### Tool Arguments

```go
// Parse tool arguments
var args map[string]interface{}
err := json.Unmarshal(toolCall.Arguments, &args)
if err != nil {
    log.Fatal(err)
}

location := args["location"].(string)
```

## Best Practices

### 1. Message Validation

```go
func ValidateMessage(msg types.Message) error {
    if msg.Role == "" {
        return fmt.Errorf("role is required")
    }
    if msg.Content == "" && len(msg.Parts) == 0 && len(msg.ToolCalls) == 0 {
        return fmt.Errorf("message must have content, parts, or tool calls")
    }
    return nil
}
```

### 2. Safe Type Assertions

```go
// Safe JSON unmarshaling
var result map[string]interface{}
if err := json.Unmarshal(toolResult.Content, &result); err != nil {
    return fmt.Errorf("invalid tool result: %w", err)
}

// Safe type assertion with check
if temp, ok := result["temperature"].(float64); ok {
    fmt.Printf("Temperature: %.0f\n", temp)
}
```

### 3. Image URL Handling

```go
// Use data URLs for small images
func ImageToDataURL(imageBytes []byte, format string) string {
    encoded := base64.StdEncoding.EncodeToString(imageBytes)
    return fmt.Sprintf("data:image/%s;base64,%s", format, encoded)
}

// Use regular URLs for large images
func CreateImageMessage(imageURL string) types.Message {
    return types.Message{
        Role:    "user",
        Content: "Analyze this image",
        Parts: []types.ContentPart{
            {
                Type: "image",
                ImageURL: &types.ImageURL{
                    URL:    imageURL,
                    Detail: "high",
                },
            },
        },
    }
}
```

## See Also

- [Pipeline Reference](pipeline) - Using types in pipelines
- [Provider Reference](providers) - Provider-specific types
- [Tools Reference](tools-mcp) - Tool-related types
