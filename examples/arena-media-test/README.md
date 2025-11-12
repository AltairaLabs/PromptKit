# Arena Media Testing Examples

This directory contains example test scenarios demonstrating how to use media content (images, audio, video) with Arena's scripted test executor.

## Example Scenarios

1. **image-analysis.yaml** - Basic image analysis with local files
2. **image-url.yaml** - Loading images from HTTP URLs
3. **multimodal-mixed.yaml** - Combining text, images, and other media types
4. **audio-transcription.yaml** - Audio file processing examples
5. **video-processing.yaml** - Video file handling
6. **error-handling.yaml** - Examples of error conditions and validation

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

## Size Limits

Default limits:
- HTTP downloads: 50MB maximum
- Timeout: 30 seconds for HTTP requests

These can be configured in the Arena engine settings.
