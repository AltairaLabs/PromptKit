# Image Preprocessing Example

This example demonstrates the image preprocessing capabilities of the PromptKit SDK for optimizing images before sending them to vision models.

## Features Demonstrated

- **WithAutoResize**: Simple option to automatically resize large images
- **WithImagePreprocessing**: Full control over preprocessing configuration
- **Quality optimization**: Configure JPEG quality for the best balance of size and clarity
- **Streaming support**: Preprocessing works with both `Send()` and `Stream()`

## Why Image Preprocessing?

1. **Cost reduction**: Smaller images = fewer tokens = lower API costs
2. **Faster responses**: Less data to transmit and process
3. **Consistent quality**: Ensure images meet model requirements
4. **Automatic handling**: No manual image manipulation needed

## Usage

```bash
export GEMINI_API_KEY=your-key
go run .
```

## Configuration Options

### Simple: WithAutoResize

```go
conv, err := sdk.Open(
    "./pack.json",
    "vision",
    sdk.WithAutoResize(1024, 1024), // Max dimensions
)
```

### Advanced: WithImagePreprocessing

```go
conv, err := sdk.Open(
    "./pack.json",
    "vision",
    sdk.WithImagePreprocessing(&stage.ImagePreprocessConfig{
        Resize: stage.ImageResizeStageConfig{
            MaxWidth:  800,
            MaxHeight: 600,
            Quality:   90,  // JPEG quality (1-100)
        },
        EnableResize: true,
    }),
)
```

## How It Works

1. When you call `Send()` or `Stream()` with an image, the SDK:
2. Downloads or reads the image data
3. Checks if resizing is needed based on your configuration
4. Resizes maintaining aspect ratio if the image exceeds limits
5. Re-encodes as JPEG with the specified quality
6. Sends the optimized image to the vision model

## Best Practices

- **1024x1024** is a good default for most vision models
- **Quality 85** provides a good balance of size and clarity
- For detailed analysis, use higher max dimensions (2048x2048)
- For quick classification tasks, smaller sizes (512x512) work well
