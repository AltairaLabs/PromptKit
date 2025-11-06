# Multimodal Basics Example

This example demonstrates the basic multimodal capabilities of PromptKit, showing how to configure prompts to accept and process images, audio, and video content.

## Overview

PromptKit's multimodal support allows prompts to process:
- **Images**: Photos, diagrams, screenshots (JPEG, PNG, WebP, GIF)
- **Audio**: Voice recordings, music, sound effects (MP3, WAV, OGG, WebM)
- **Video**: Video clips, recordings (MP4, WebM)

## Prompt Configurations

### 1. Image Analyzer (`image-analyzer.yaml`)

Analyzes images and provides detailed descriptions.

**Features:**
- Supports JPEG, PNG, WebP formats
- Maximum 20MB per image
- Up to 5 images per message
- High-detail analysis by default

**Example Usage:**
```yaml
parts:
  - type: text
    text: "What's in this image?"
  - type: image
    media:
      file_path: "./images/photo.jpg"
      mime_type: "image/jpeg"
      detail: "high"
```

### 2. Audio Transcriber (`audio-transcriber.yaml`)

Transcribes and analyzes audio content.

**Features:**
- Supports MP3, WAV, OGG, WebM formats
- Maximum 25MB per audio file
- Maximum 10 minutes duration
- Speaker identification
- Tone and emotion analysis

**Example Usage:**
```yaml
parts:
  - type: text
    text: "Please transcribe this recording"
  - type: audio
    media:
      file_path: "./audio/meeting.mp3"
      mime_type: "audio/mpeg"
```

### 3. Mixed Media Assistant (`mixed-media-assistant.yaml`)

Processes multiple media types together for comprehensive analysis.

**Features:**
- Supports images, audio, and video
- Identifies relationships between media
- Provides integrated insights
- Handles up to 100MB videos (5 minutes max)

**Example Usage:**
```yaml
parts:
  - type: text
    text: "Analyze this presentation"
  - type: image
    media:
      file_path: "./media/slide1.png"
      mime_type: "image/png"
  - type: audio
    media:
      file_path: "./media/narration.mp3"
      mime_type: "audio/mpeg"
```

## Media Configuration

The `media:` section in PromptConfig YAML controls multimodal behavior:

```yaml
media:
  enabled: true
  supported_types:
    - image
    - audio
    - video
  
  image:
    max_size_mb: 20
    allowed_formats: [jpeg, png, webp]
    default_detail: high
    max_images_per_msg: 5
  
  audio:
    max_size_mb: 25
    allowed_formats: [mp3, wav, ogg]
    max_duration_sec: 600
  
  video:
    max_size_mb: 100
    allowed_formats: [mp4, webm]
    max_duration_sec: 300
```

## Configuration Options

### Image Configuration

| Option | Description | Default |
|--------|-------------|---------|
| `max_size_mb` | Maximum image size in MB | 0 (unlimited) |
| `allowed_formats` | Allowed image formats | all supported |
| `default_detail` | Detail level: low/high/auto | high |
| `max_images_per_msg` | Max images per message | 0 (unlimited) |

### Audio Configuration

| Option | Description | Default |
|--------|-------------|---------|
| `max_size_mb` | Maximum audio size in MB | 0 (unlimited) |
| `allowed_formats` | Allowed audio formats | all supported |
| `max_duration_sec` | Max duration in seconds | 0 (unlimited) |
| `require_metadata` | Require duration/bitrate | false |

### Video Configuration

| Option | Description | Default |
|--------|-------------|---------|
| `max_size_mb` | Maximum video size in MB | 0 (unlimited) |
| `allowed_formats` | Allowed video formats | all supported |
| `max_duration_sec` | Max duration in seconds | 0 (unlimited) |
| `require_metadata` | Require resolution/fps | false |

## Media Sources

Content can be provided in three ways:

### 1. File Path (Relative)
```yaml
media:
  file_path: "./images/photo.jpg"
  mime_type: "image/jpeg"
```

### 2. URL
```yaml
media:
  url: "https://example.com/image.jpg"
  mime_type: "image/jpeg"
```

### 3. Base64 Data (Inline)
```yaml
media:
  data: "iVBORw0KGgoAAAANSUhEUgA..."
  mime_type: "image/png"
```

## Examples with Multimodal Parts

The `media.examples` section provides sample interactions:

```yaml
examples:
  - name: "image-analysis"
    description: "Single image analysis"
    role: user
    parts:
      - type: text
        text: "What's in this image?"
      - type: image
        media:
          file_path: "./test-images/sample.jpg"
          mime_type: "image/jpeg"
          detail: "high"
```

## Provider Compatibility

| Provider | Images | Audio | Video | Notes |
|----------|--------|-------|-------|-------|
| OpenAI GPT-4V | ✅ | ❌ | ❌ | Images only |
| Claude 3+ | ✅ | ❌ | ❌ | Images only |
| Gemini 2.0 | ✅ | ✅ | ✅ | Full multimodal |

## Best Practices

1. **File Sizes**: Keep media files reasonably sized for faster processing
2. **Detail Levels**: Use `low` detail for quick processing, `high` for detailed analysis
3. **Format Selection**: Use widely-supported formats (JPEG for images, MP3 for audio, MP4 for video)
4. **Context**: Provide text context with media to guide the analysis
5. **Validation**: Configure appropriate validators to ensure output quality

## Error Handling

Common media validation errors:

- **Invalid format**: Check `allowed_formats` in configuration
- **File too large**: Check `max_size_mb` limits
- **Duration exceeded**: Check `max_duration_sec` for audio/video
- **Too many items**: Check `max_images_per_msg` or similar limits

## Testing

To test these configurations:

1. Create test media files in appropriate directories
2. Use PromptArena to run scenarios with multimodal content
3. Validate output using assertions

## Next Steps

- Explore the [customer-support-multimodal](../customer-support-multimodal/) example for real-world usage
- See the [PromptPack specification](../../docs/pack-format-spec.md) for pack compilation
- Review provider-specific multimodal documentation

## Related Examples

- [assertions-test](../assertions-test/) - Testing multimodal outputs
- [customer-support-integrated](../customer-support-integrated/) - Tools with multimodal
- [mcp-filesystem-test](../mcp-filesystem-test/) - File-based media access
