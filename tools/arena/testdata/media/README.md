# Test Media Assets

This directory contains minimal valid media files for testing Arena's media loading functionality.

## Files

### Images
- `test-image-small.jpg` - Minimal valid JPEG (1x1 red pixel, ~200 bytes)
- `test-image-small.png` - Minimal valid PNG (1x1 transparent pixel, ~67 bytes)
- `test-image-small.gif` - Minimal valid GIF (1x1 white pixel, ~35 bytes)
- `test-image-small.webp` - Minimal valid WebP (1x1 pixel, ~42 bytes)
- `test-image-large.jpg` - Large file for size limit testing (1MB)

### Audio
- `test-audio-small.wav` - Minimal valid WAV (1 sample of silence, ~44 bytes)
- `test-audio-small.ogg` - Minimal valid OGG (header only, ~28 bytes)

## Usage

These files are used in integration tests to verify:
- Media loading from local file paths
- MIME type detection and validation
- File size limit enforcement
- Security validation (path traversal, symlinks)

## Notes

- Small files are intentionally minimal to keep the repository size down
- Files contain valid headers to pass MIME type detection
- Large file is used to test size limit enforcement (50MB default limit)
