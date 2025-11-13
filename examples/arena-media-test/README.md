# Arena Media Testing Examples

This directory contains example test scenarios demonstrating how to use media content (images, audio, video) with Arena's scripted test executor and media validation assertions.

## Example Scenarios

### Media Input Examples

1. **image-analysis.yaml** - Basic image analysis with local files
2. **image-url.yaml** - Loading images from HTTP URLs
3. **multimodal-mixed.yaml** - Combining text, images, and other media types
4. **audio-transcription.yaml** - Audio file processing examples

### Media Validation Examples (Phase 1)

1. **image-validation.yaml** - Image format and dimension validation
   - Validates image formats (PNG, JPEG, WebP, etc.)
   - Tests exact dimensions, min/max ranges
   - Demonstrates combined format + dimension assertions

2. **audio-validation.yaml** - Audio format and duration validation
   - Validates audio formats (MP3, WAV, OGG, etc.)
   - Tests duration constraints (min/max seconds)
   - Examples for short clips, podcasts, and longer content

3. **video-validation.yaml** - Video resolution and duration validation
   - Validates video resolutions (720p, 1080p, 4K presets)
   - Tests duration constraints for video content
   - Demonstrates aspect ratio and dimension validation
   - Examples for short-form, long-form, and vertical video

## Running Examples

To run these examples with Arena:

```bash
# Run a specific scenario
promptarena run examples/arena-media-test/image-analysis.yaml

# Run all scenarios in this directory
promptarena run examples/arena-media-test/*.yaml
```

## Media File References

Examples reference test media files from `tools/arena/testdata/media/`. In your own tests, you can:

- Use relative paths to your media files
- Use absolute paths
- Use HTTP/HTTPS URLs to remote media
- Embed base64-encoded media inline (for small files)

## Configuration Notes

- **file_path**: Path to local media file (relative or absolute)
- **url**: HTTP/HTTPS URL to remote media
- **data**: Base64-encoded media data (inline)
- **mime_type**: MIME type of the media (required for inline data)
- **detail**: Image detail level - "low", "high", or "auto" (images only)

## Media Assertions

Arena Phase 1 includes six media validation assertions that validate media content in assistant responses:

### Image Assertions

**image_format** - Validates image format/MIME type

```yaml
assertions:
  - type: image_format
    params:
      formats:
        - png
        - jpeg
        - webp
```

**image_dimensions** - Validates image dimensions (width/height)

```yaml
assertions:
  - type: image_dimensions
    params:
      width: 1920          # Exact width
      height: 1080         # Exact height
      min_width: 1280      # Minimum width
      max_width: 3840      # Maximum width
      min_height: 720      # Minimum height
      max_height: 2160     # Maximum height
```

### Audio Assertions

**audio_format** - Validates audio format/MIME type

```yaml
assertions:
  - type: audio_format
    params:
      formats:
        - mp3
        - wav
        - ogg
```

**audio_duration** - Validates audio duration in seconds

```yaml
assertions:
  - type: audio_duration
    params:
      min_seconds: 10      # Minimum duration
      max_seconds: 60      # Maximum duration
```

### Video Assertions

**video_resolution** - Validates video resolution

```yaml
assertions:
  - type: video_resolution
    params:
      presets:            # Use named presets
        - 720p
        - 1080p
        - 4k
      # Or use explicit dimensions
      min_width: 1280
      min_height: 720
```

**video_duration** - Validates video duration in seconds

```yaml
assertions:
  - type: video_duration
    params:
      min_seconds: 30
      max_seconds: 120
```

Supported resolution presets: `480p`, `sd`, `720p`, `hd`, `1080p`, `fhd`, `full_hd`, `1440p`, `2k`, `qhd`, `2160p`, `4k`, `uhd`, `4320p`, `8k`

## Size Limits

Default limits:

- HTTP downloads: 50MB maximum
- Timeout: 30 seconds for HTTP requests

These can be configured in the Arena engine settings.

## Mock Provider Multimodal Support

The MockProvider now supports returning multimodal content in responses, enabling realistic testing of media validators without requiring actual LLM API calls. This is particularly useful for:

- Fast, deterministic testing in CI/CD pipelines
- Testing without API costs
- Offline development
- Testing edge cases with specific media configurations

### Configuration Format

Mock responses can include multimodal content parts in `providers/mock-responses.yaml`:

```yaml
scenarios:
  image-analysis-local:
    turns:
      1:
        text: "I can see a test image."
        parts:
          - type: text
            text: "I can see a test image with simple geometric shapes."
          - type: image
            image_url:
              url: "mock://test-shapes.png"
            metadata:
              format: "PNG"
              width: 800
              height: 600
              size: 51200
```

### Supported Content Types

**Text Parts:**

```yaml
- type: text
  text: "Your text content here"
```

**Image Parts:**

```yaml
- type: image
  image_url:
    url: "mock://test-image.png"  # or https://, data:, file path
    detail: "high"  # optional: low, high, auto
  metadata:
    format: "PNG"
    width: 1920
    height: 1080
```

**Audio Parts:**

```yaml
- type: audio
  audio_url:
    url: "mock://test-audio.mp3"
  metadata:
    format: "MP3"
    duration_seconds: 30
    channels: 2
```

**Video Parts:**

```yaml
- type: video
  video_url:
    url: "mock://test-video.mp4"
  metadata:
    format: "MP4"
    width: 1920
    height: 1080
    duration_seconds: 60
    fps: 30
```

### URL Schemes

- **mock://**: Mock URLs for test fixtures (e.g., `mock://test-image.png`)
- **https:// / http://**: External URLs (e.g., `https://picsum.photos/200`)
- **data:**: Inline base64-encoded data
- **file paths**: Local file paths

### Backward Compatibility

Text-only responses continue to work:

```yaml
scenarios:
  simple-text:
    turns:
      1: "Simple text response"  # Old format
      2:
        text: "New format without parts"  # Also works
```

See `providers/mock-responses.yaml` for complete examples.
