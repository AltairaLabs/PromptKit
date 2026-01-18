---
title: Multimodal SDK Example
description: Example demonstrating multimodal
sidebar:
  order: 100
---


This example demonstrates multimodal (vision) capabilities using the PromptKit SDK with streaming responses.

## Features

- **Image Analysis**: Send images with text prompts for visual analysis
- **Streaming Responses**: Get real-time streaming output as the model analyzes images
- **Conversation Context**: Follow-up questions maintain context about previously analyzed images
- **Multiple Input Methods**: Support for image URLs, file paths, and raw image data

## Prerequisites

1. A Google Gemini API key (for vision capabilities)
2. Go 1.21 or later

## Setup

```bash
export GEMINI_API_KEY=your-gemini-api-key
```

## Running the Example

```bash
cd sdk/examples/multimodal
go run .
```

## How It Works

### Opening a Multimodal Conversation

```go
conv, err := sdk.Open("./multimodal.pack.json", "vision-analyst")
if err != nil {
    log.Fatalf("Failed to open pack: %v", err)
}
defer conv.Close()
```

### Streaming Image Analysis

```go
for chunk := range conv.Stream(ctx, "What do you see in this image?",
    sdk.WithImageURL("https://example.com/image.jpg"),
) {
    if chunk.Error != nil {
        log.Printf("Error: %v", chunk.Error)
        break
    }
    if chunk.Type == sdk.ChunkDone {
        break
    }
    fmt.Print(chunk.Text)
}
```

### Non-Streaming Image Analysis

```go
resp, err := conv.Send(ctx, "Describe this image",
    sdk.WithImageURL("https://example.com/image.jpg"),
)
if err != nil {
    log.Fatalf("Error: %v", err)
}
fmt.Println(resp.Text())
```

## Image Input Options

The SDK supports multiple ways to provide images:

### From URL
```go
sdk.WithImageURL("https://example.com/image.jpg")
```

### From File
```go
sdk.WithImageFile("/path/to/local/image.png")
```

### From Raw Data
```go
sdk.WithImageData(imageBytes, "image/png")
```

## Document Input Options

The SDK supports PDF document analysis:

### From File
```go
sdk.WithDocumentFile("/path/to/document.pdf")
```

### From Raw Data
```go
sdk.WithDocumentData(pdfBytes, "application/pdf")
```

### Document Analysis Example

```go
resp, err := conv.Send(ctx, "Summarize this document",
    sdk.WithDocumentFile("./report.pdf"),
)
if err != nil {
    log.Fatalf("Error: %v", err)
}
fmt.Println(resp.Text())
```

### Provider Support

- **Claude**: Supports PDFs up to 32MB (Haiku, Sonnet, Opus)
- **Gemini**: Supports PDFs up to 20MB (Flash, Pro)
- **OpenAI**: Document support varies by model

## Supported Providers

Multimodal capabilities require a provider that supports vision:

- **Gemini** (recommended): Full multimodal support with streaming
- **OpenAI GPT-4V**: Vision capabilities with GPT-4 Vision models
- **Claude**: Vision support with Claude 3 models

## Pack Configuration

The pack file configures the vision analyst prompt:

```json
{
  "prompts": {
    "vision-analyst": {
      "id": "vision-analyst",
      "name": "Vision Analyst",
      "system_template": "You are an expert visual analyst...",
      "parameters": {
        "temperature": 0.7,
        "max_tokens": 1024
      }
    }
  }
}
```

## Image Preprocessing

For large images, enable automatic preprocessing to resize before sending:

```go
conv, err := sdk.Open("./multimodal.pack.json", "vision-analyst",
    sdk.WithAutoResize(2048, 2048), // Max 2048x2048
)
```

This automatically resizes large images while preserving aspect ratio, reducing token usage and preventing provider size limit errors.

See [Preprocess Images](../how-to/preprocess-images) for full configuration options.

## Notes

- Image analysis typically requires more tokens than text-only requests
- Large images may be resized by the provider for processing
- Some providers have limits on image size and format
- Use `sdk.WithImagePreprocessing()` to handle large images automatically
