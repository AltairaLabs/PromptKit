---
title: Preprocess Images
sidebar:
  order: 7
---

Learn how to automatically resize and optimize images before sending to LLM providers.

## Why Preprocess Images?

Large images can cause issues with LLM providers:
- **Token limits**: Large images consume more tokens, leaving less room for conversation
- **Size limits**: Some providers reject images over certain sizes
- **Latency**: Larger images take longer to process
- **Cost**: More tokens = higher API costs

The SDK can automatically resize and optimize images before sending them.

## Enable Image Preprocessing

### With Default Settings

Enable preprocessing with sensible defaults (max 1024x1024, 85% quality):

```go
conv, err := sdk.Open("./vision.pack.json", "analyst",
    sdk.WithImagePreprocessing(nil),
)
```

### With Custom Dimensions

For simple dimension limits, use the convenience option:

```go
conv, err := sdk.Open("./vision.pack.json", "analyst",
    sdk.WithAutoResize(2048, 2048), // Max 2048x2048
)
```

### With Full Configuration

For complete control over preprocessing:

```go
import "github.com/AltairaLabs/PromptKit/runtime/pipeline/stage"

conv, err := sdk.Open("./vision.pack.json", "analyst",
    sdk.WithImagePreprocessing(&stage.ImagePreprocessConfig{
        Resize: stage.ImageResizeStageConfig{
            MaxWidth:            2048,
            MaxHeight:           2048,
            MaxSizeBytes:        5 * 1024 * 1024, // 5MB limit
            Quality:             90,
            PreserveAspectRatio: true,
            SkipIfSmaller:       true,
        },
        EnableResize: true,
    }),
)
```

## Configuration Options

| Option | Default | Description |
|--------|---------|-------------|
| `MaxWidth` | 1024 | Maximum width in pixels |
| `MaxHeight` | 1024 | Maximum height in pixels |
| `MaxSizeBytes` | 0 | Maximum file size (0 = no limit) |
| `Quality` | 85 | JPEG/WebP quality (1-100) |
| `PreserveAspectRatio` | true | Maintain original aspect ratio |
| `SkipIfSmaller` | true | Don't process images within limits |
| `EnableResize` | true | Enable/disable processing |

## How It Works

When preprocessing is enabled:

1. Images in your message are decoded
2. If dimensions exceed limits, the image is resized
3. Aspect ratio is preserved (if configured)
4. Image is re-encoded with the specified quality
5. If still over `MaxSizeBytes`, quality is reduced iteratively
6. Processed image replaces the original in the message

## Supported Formats

**Input formats:**
- JPEG
- PNG
- GIF
- WebP

**Output formats:**
- JPEG (default)
- PNG

## Example: High-Resolution Photo Analysis

```go
package main

import (
    "context"
    "fmt"
    "log"

    "github.com/AltairaLabs/PromptKit/sdk"
)

func main() {
    // Enable preprocessing for large photos
    conv, err := sdk.Open("./photo.pack.json", "analyst",
        sdk.WithAutoResize(2048, 2048),
    )
    if err != nil {
        log.Fatal(err)
    }
    defer conv.Close()

    ctx := context.Background()

    // Send a large image - it will be automatically resized
    resp, err := conv.Send(ctx, "Describe this photo in detail",
        sdk.WithImageFile("/path/to/large-photo.jpg"),
    )
    if err != nil {
        log.Fatal(err)
    }

    fmt.Println(resp.Text())
}
```

## Example: Mobile App with Bandwidth Constraints

```go
import "github.com/AltairaLabs/PromptKit/runtime/pipeline/stage"

// Aggressive compression for mobile
conv, err := sdk.Open("./mobile.pack.json", "assistant",
    sdk.WithImagePreprocessing(&stage.ImagePreprocessConfig{
        Resize: stage.ImageResizeStageConfig{
            MaxWidth:     512,
            MaxHeight:    512,
            MaxSizeBytes: 100 * 1024, // 100KB limit
            Quality:      70,
        },
        EnableResize: true,
    }),
)
```

## Pipeline Integration

Image preprocessing runs as a pipeline stage before the provider stage:

```
StateStoreLoad → PromptAssembly → Template → ContextBuilder → ImagePreprocess → Provider → StateStoreSave
```

This means images are processed after context management but before sending to the LLM.

## See Also

- [Multimodal Example](../examples/multimodal)
- [Send Messages](send-messages)
