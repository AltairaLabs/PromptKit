---
title: Multimodal Basics Example
description: Example demonstrating multimodal-basics
sidebar:
  order: 100
---
This example demonstrates how to configure and use multimodal media support in PromptKit, with runnable PromptArena tests using both real models and mock providers.

## Quick Start

### 1. Setup

```bash
# Copy environment template
cp .env.example .env

# Add your Gemini API key to .env (for real model testing)
echo "GEMINI_API_KEY=your_actual_key" > .env
```

### 2. Run Tests

```bash
# Run with mock provider (no API keys needed - perfect for CI/CD)
promptarena run arena.yaml --provider mock-vision

# Run with real Gemini vision model (requires API key)
promptarena run arena.yaml --provider gemini-vision

# Run all providers and scenarios
promptarena run arena.yaml
```

### 3. View Results

```bash
# View HTML report
open out/multimodal-report.html

# Or check JSON results
cat out/results.json | jq
```

## Structure

```
multimodal-basics/
├── arena.yaml                  # Arena configuration
├── prompts/
│   ├── image-analyzer.yaml     # Multimodal prompt with MediaConfig
│   ├── audio-transcriber.yaml  # Audio analysis example
│   └── mixed-media-assistant.yaml  # Combined media types
├── providers/
│   ├── gemini-vision.yaml      # Real Gemini model configuration
│   └── mock-vision.yaml        # Mock provider for testing
├── scenarios/
│   ├── image-analysis.yaml     # Single image test
│   └── image-comparison.yaml   # Multiple images test
├── mock-responses.yaml         # Deterministic mock responses
└── .env.example                # Environment template
```

## Features

### Runnable Scenarios

✅ **Image Analysis** (`scenarios/image-analysis.yaml`)
- Tests single image description capabilities
- Validates response includes image-related content
- Checks length constraints (50-2000 characters)
- Tests color and composition analysis

✅ **Image Comparison** (`scenarios/image-comparison.yaml`)
- Tests comparing multiple images
- Validates comparative analysis
- Checks professional assessment capabilities
- Tests detailed technical descriptions

### Provider Support

✅ **Mock Provider** (`providers/mock-vision.yaml`)
- Deterministic testing without API calls
- Scenario-specific responses in `mock-responses.yaml`
- Turn-by-turn scripted responses
- Perfect for CI/CD testing and development

✅ **Gemini Vision** (`providers/gemini-vision.yaml`)
- Real multimodal model testing
- Requires `GEMINI_API_KEY` in `.env`
- Tests actual vision capabilities
- Validates MediaConfig constraints work with real models

## MediaConfig Structure

Each prompt includes a `media:` section that defines capabilities:

```yaml
media:
  enabled: true
  supported_types:
    - image
  
  image:
    max_size_mb: 20
    allowed_formats: [jpeg, png, webp]
    default_detail: high
    max_images_per_msg: 5
  
  examples:
    - name: "single-image-analysis"
      role: user
      parts:
        - type: text
          text: "What's in this image?"
        - type: image
          media:
            file_path: "./test-images/sample.jpg"
            mime_type: "image/jpeg"
```

## Running Specific Tests

```bash
# Test only image analysis scenario
promptarena run arena.yaml --scenario image-analysis

# Test only mock provider
promptarena run arena.yaml --provider mock-vision

# Increase concurrency for faster testing
promptarena run arena.yaml --concurrency 5

# Generate only HTML output
promptarena run arena.yaml --format html
```

## Mock Responses

The `mock-responses.yaml` file provides deterministic responses for testing:

```yaml
scenarios:
  image-analysis:
    turns:
      1: "I can see a landscape with mountains..."
      2: "The color palette is predominantly warm..."
      
  image-comparison:
    turns:
      1: "Comparing these two images, I can identify..."
      2: "Image 2 appears more professional because..."
```

Benefits of mock responses:
- ✅ Predictable test results
- ✅ No API costs during development
- ✅ Fast iteration on prompt logic
- ✅ CI/CD testing without secrets
- ✅ Scenario-specific responses

## Architecture Notes

### Media Bundling: NOT Implemented

MediaConfig defines **capabilities only** (types, sizes, formats supported).

Actual media files are:
- ✅ Provided by users as input at runtime
- ✅ Returned by models as output  
- ❌ NOT bundled into prompt packs

Benefits:
- Lightweight packs (no embedded media)
- Clear separation of prompt logic and user data
- Dynamic media handling at runtime
- No storage/versioning concerns for media

### Provider Implementation

Multimodal support requires provider-level implementation:
- **Gemini 2.0**: Images, audio, video ✅
- **GPT-4V**: Images ✅
- **Claude 3**: Images ✅

MediaConfig advertises what the prompt supports; actual handling depends on provider capabilities.

## Validation

MediaConfig is validated at compile time by PackC:

```bash
# Compile and validate
packc compile prompts/image-analyzer.yaml -o image-analyzer.pack.json

# Check media configuration in compiled pack
jq '.prompts[].media' image-analyzer.pack.json
```

Validation checks:
- ✅ Supported types are valid (`image`, `audio`, `video`)
- ✅ Size limits are positive numbers
- ✅ Formats are from allowed lists
- ✅ Examples have proper structure (role, parts, media)
- ⚠️  Warns about missing example files (non-blocking)

## Testing

### Unit Tests

```bash
# Test media validation logic
cd ../../runtime/prompt
go test -v -run TestValidateMediaConfig

# Test pack loading with MediaConfig
go test -v -run TestLoadPackWithMediaConfig
```

Test coverage:
- 53 comprehensive test cases
- 77.8-100% coverage across media validation functions
- Tests for all media types and configurations
- Edge cases and error conditions

### Integration Tests (Arena)

```bash
# Run all scenarios with both providers
promptarena run arena.yaml

# Run with verbose output
promptarena run arena.yaml -v

# Generate detailed HTML report
promptarena run arena.yaml --out ./test-results
```

## Expected Output

### Successful Mock Run

```
Running Arena: multimodal-basics
Loaded 1 prompt configs
Loaded 2 providers
Loaded 2 scenarios

Running scenario: image-analysis (mock-vision)
  Turn 1: ✓ Passed (content_includes: "image", "see")
  Turn 1: ✓ Passed (min_length: 50 chars)
  Turn 1: ✓ Passed (max_length: 2000 chars)
  Turn 2: ✓ Passed (content_includes: "color")
  Turn 2: ✓ Passed (min_length: 30 chars)

Running scenario: image-comparison (mock-vision)
  Turn 1: ✓ Passed (content_includes: "image", "difference")
  Turn 1: ✓ Passed (min_length: 100 chars)
  Turn 2: ✓ Passed (content_includes: "professional")

Summary:
  Total Tests: 8
  Passed: 8
  Failed: 0
  Success Rate: 100%

Report saved to: out/multimodal-report.html
```

## Troubleshooting

### Missing API Key

```
Error: GEMINI_API_KEY not set
Solution: cp .env.example .env && edit .env with your key
```

### Provider Not Found

```
Error: Provider 'gemini-vision' not found
Solution: Check that providers/gemini-vision.yaml exists and arena.yaml references it correctly
```

### Assertion Failures

Check the HTML report for detailed failure information:
```bash
open out/multimodal-report.html
# Look for red X marks and click for details
```

### Mock Responses Not Working

```
Error: Mock provider returning default response
Solution: Check that scenario name in arena.yaml matches key in mock-responses.yaml
```

## Next Steps

### 1. Add Real Images

Create test images for actual multimodal testing:

```bash
mkdir test-images
# Add sample images: sample-photo.jpg, before.jpg, after.jpg
```

### 2. Extend Scenarios

Add more complex test cases:
- OCR text extraction from images
- Object detection validation
- Style transfer comparison
- Multi-step image reasoning

### 3. Add Audio/Video Support

Test other media types:

```bash
# Copy and modify audio-transcriber.yaml
# Add audio scenarios
# Configure mock responses for audio tests
```

### 4. CI/CD Integration

Use mock provider in automated tests:

```yaml
# .github/workflows/test.yml
- name: Test Multimodal
  run: |
    cd examples/multimodal-basics
    promptarena run arena.yaml --provider mock-vision
```

## Related Documentation

- [MediaConfig Specification](../../docs/guides/multimodal-support.md)
- [PromptArena User Guide](../../docs/guides/arena-user-guide.md)
- [Provider Configuration](../../docs/guides/provider-configuration.md)
- [Mock Provider Usage](../assertions-test/MOCK_PROVIDER_USAGE.md)

## Related Examples

- [assertions-test/](../assertions-test/) - Testing with assertions and mock providers
- [customer-support-integrated/](../customer-support-integrated/) - Complex real-world scenario
- [guardrails-test/](../guardrails-test/) - Content validation examples
