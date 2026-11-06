---
title: Preprocess Images
description: Resize and compress images in the SDK before they reach the provider
sidebar:
  order: 7
verified:
  commit: 8489bf3d95c7498f9de0f024769db9399c24e884
  sources:
    - runtime/media/image.go
    - runtime/pipeline/stage/stages_image_preprocess.go
    - sdk/examples/image-preprocessing/main.go
    - sdk/internal/pipeline/builder.go
    - sdk/options.go
---

Resize and compress images automatically before the SDK sends them to the provider, so large photos stay within provider size limits and use fewer tokens.

## Prerequisites

- A conversation that sends images, for example with `sdk.WithImageFile`.
- A vision-capable provider.

## Enable preprocessing

Pick one of three ways.

1. Use the defaults (max 1024x1024, quality 85):

   ```go
   conv, err := sdk.Open("./vision.pack.json", "analyst",
       sdk.WithImagePreprocessing(nil),
   )
   ```

2. Set only the dimensions:

   ```go
   conv, err := sdk.Open("./vision.pack.json", "analyst",
       sdk.WithAutoResize(2048, 2048), // max 2048x2048, other settings default
   )
   ```

3. Control everything. Start from `stage.DefaultImagePreprocessConfig()` and override what you need:

   ```go
   import "github.com/AltairaLabs/PromptKit/runtime/v2/pipeline/stage"

   cfg := stage.DefaultImagePreprocessConfig()
   cfg.Resize.MaxWidth = 2048
   cfg.Resize.MaxHeight = 2048
   cfg.Resize.MaxSizeBytes = 5 * 1024 * 1024 // 5MB limit
   cfg.Resize.Quality = 90

   conv, err := sdk.Open("./vision.pack.json", "analyst",
       sdk.WithImagePreprocessing(&cfg),
   )
   ```

Starting from the defaults matters. A `stage.ImagePreprocessConfig` literal has zero values for every field you omit: `EnableResize` is false (nothing is processed), `PreserveAspectRatio` is false (images are squashed to the maximum dimensions), and `SkipIfSmaller` is false (every image is re-encoded).

## Configuration fields

The defaults apply to `WithImagePreprocessing(nil)`, `WithAutoResize` and `stage.DefaultImagePreprocessConfig()`.

| Field | Default | Description |
|-------|---------|-------------|
| `Resize.MaxWidth` | 1024 | Maximum width in pixels (0 = no limit) |
| `Resize.MaxHeight` | 1024 | Maximum height in pixels (0 = no limit) |
| `Resize.MaxSizeBytes` | 0 | Maximum encoded size in bytes (0 = no limit) |
| `Resize.Quality` | 85 | JPEG quality, 1-100 |
| `Resize.Format` | `""` | Output format: `"jpeg"`, `"png"`, or empty to keep the original |
| `Resize.PreserveAspectRatio` | true | Keep the original aspect ratio |
| `Resize.SkipIfSmaller` | true | Leave images within the limits untouched |
| `EnableResize` | true | Turn processing on or off |

## Choose the output format

The SDK decodes JPEG, PNG, GIF and WebP input. It encodes JPEG or PNG output. With `Format` empty, a resized PNG stays PNG and any other input is re-encoded as JPEG. Any format other than PNG, requested or preserved, is written as JPEG, and the reported format and MIME type describe the bytes produced. A resized GIF or WebP is therefore always JPEG.

## Compress for constrained clients

For tight bandwidth, lower the dimensions and cap the size. When an image is still over `MaxSizeBytes` after resizing, the SDK lowers the quality in steps (down to a floor of 10) until it fits:

```go
cfg := stage.DefaultImagePreprocessConfig()
cfg.Resize.MaxWidth = 512
cfg.Resize.MaxHeight = 512
cfg.Resize.MaxSizeBytes = 100 * 1024 // 100KB limit
cfg.Resize.Quality = 70

conv, err := sdk.Open("./mobile.pack.json", "assistant",
    sdk.WithImagePreprocessing(&cfg),
)
```

## Send an image

Preprocessing is automatic. Send the image as usual:

```go
package main

import (
    "context"
    "fmt"
    "log"

    "github.com/AltairaLabs/PromptKit/sdk/v2"
)

func main() {
    conv, err := sdk.Open("./photo.pack.json", "analyst",
        sdk.WithAutoResize(2048, 2048),
    )
    if err != nil {
        log.Fatal(err)
    }
    defer conv.Close()

    resp, err := conv.Send(context.Background(), "Describe this photo in detail",
        sdk.WithImageFile("/path/to/large-photo.jpg"),
    )
    if err != nil {
        log.Fatal(err)
    }

    fmt.Println(resp.Text())
}
```

The preprocessing stage runs after the template stage (and the context builder, when you set a token budget) and before the provider stage. It decodes each image that has inline data or a file path, resizes it if it exceeds the limits, re-encodes it, and replaces the original in the message. Images supplied by URL are sent unchanged.

## Verify

Enable debug logging and send an image larger than your limits. The runtime logs `ImagePreprocessStage: resized image in message` with the original and new sizes and the new dimensions. The stage logs only the first resize it performs. An image already within the limits produces no log line and goes through untouched.

## Troubleshoot

- Images are unchanged: the config has `EnableResize` false, or the images are URLs. Start from `stage.DefaultImagePreprocessConfig()` and send files or bytes.
- Images look stretched: `PreserveAspectRatio` is false in your config.
- The log shows `Image preprocessing failed`: the image could not be decoded or read. Check that it is one of the supported formats.

## Related

- [Multimodal example](/sdk/examples/multimodal/)
- [Image preprocessing example](/sdk/examples/image-preprocessing/)
- [Send messages](/sdk/how-to/conversations/send-messages/)
