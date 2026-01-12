# Realtime Video Streaming Example

This example demonstrates how to stream video frames to an LLM for real-time vision analysis using PromptKit's duplex session capabilities.

## Features Demonstrated

- **OpenDuplex with video**: Opening a bidirectional streaming session for video
- **WithStreamingVideo**: Configuring frame rate limiting and preprocessing
- **SendFrame()**: Sending individual image frames to the session
- **Frame rate limiting**: Automatic dropping of excess frames to match LLM processing speed

## Use Cases

- **Webcam analysis**: Real-time description of what a webcam sees
- **Screen sharing**: Analyzing screen content as it changes
- **Security monitoring**: Continuous analysis of camera feeds
- **Accessibility**: Describing visual content for users with visual impairments

## Usage

```bash
export GEMINI_API_KEY=your-key
go run .
```

## How It Works

### 1. Open a Duplex Session with Video Config

```go
conv, err := sdk.OpenDuplex(
    "./pack.json",
    "vision-stream",
    sdk.WithStreamingVideo(&sdk.VideoStreamConfig{
        TargetFPS:    1.0,   // Process 1 frame per second
        MaxWidth:     1024,  // Resize large frames
        MaxHeight:    1024,
        Quality:      85,    // JPEG quality
        EnableResize: true,
    }),
)
```

### 2. Send Frames

```go
frame := &session.ImageFrame{
    Data:      jpegBytes,
    MIMEType:  "image/jpeg",
    Width:     640,
    Height:    480,
    FrameNum:  frameCount,
    Timestamp: time.Now(),
}
err = conv.SendFrame(ctx, frame)
```

### 3. Receive Responses

```go
for chunk := range conv.Response() {
    if chunk.Content != "" {
        fmt.Print(chunk.Content)
    }
}
```

## Frame Rate Limiting

The `TargetFPS` setting automatically drops excess frames:

- **Webcam at 30 FPS** → With `TargetFPS: 1.0`, only ~1 frame/second reaches the LLM
- **No frame loss**: The most recent frame is kept, older frames are dropped
- **Reduces costs**: Fewer frames = fewer tokens = lower API costs

## Video Chunk Streaming

For encoded video segments (H.264, VP8, etc.), use `SendVideoChunk()`:

```go
chunk := &session.VideoChunk{
    Data:       h264Data,
    MIMEType:   "video/h264",
    ChunkIndex: 0,
    IsKeyFrame: true,
    Timestamp:  time.Now(),
}
err = conv.SendVideoChunk(ctx, chunk)
```

## Real-World Integration

In a real application, replace the simulated frames with actual capture:

### Using gocv (OpenCV for Go)

```go
import "gocv.io/x/gocv"

webcam, _ := gocv.VideoCaptureDevice(0)
defer webcam.Close()

mat := gocv.NewMat()
defer mat.Close()

for webcam.Read(&mat) {
    // Encode to JPEG
    buf, _ := gocv.IMEncode(".jpg", mat)

    frame := &session.ImageFrame{
        Data:      buf.GetBytes(),
        MIMEType:  "image/jpeg",
        Timestamp: time.Now(),
    }
    conv.SendFrame(ctx, frame)
}
```

## Provider Support

Currently, realtime video streaming works best with providers that support bidirectional streaming:

- **Gemini Live API**: Full support for real-time vision (when available)
- **OpenAI Realtime**: Audio-focused, video support may be added later

The SDK is provider-agnostic - video frames flow through when the provider supports them.
