---
layout: docs
title: Multimodal Examples
nav_order: 4
parent: Arena Examples
grand_parent: PromptArena
---

# Multimodal Examples

Learn to test LLM applications that work with images, audio, and other media types.

## Examples in this Category

### [multimodal-basics](multimodal-basics/)

**Purpose**: Foundation for testing multimodal prompt interactions

**What you'll learn:**
- Image input configuration
- Vision model testing
- Media file handling in scenarios
- Multimodal assertion patterns
- Cross-provider image support

**Difficulty**: Intermediate  
**Estimated time**: 35 minutes

**Featured capabilities:**
- Image description testing
- Visual question answering
- Image analysis validation
- Provider comparison for vision tasks

### [arena-media-test](arena-media-test/)

**Purpose**: Advanced media handling and validation

**What you'll learn:**
- Complex media scenarios
- Media format handling
- URL vs local file media
- Base64-encoded media
- Media-specific assertions

**Difficulty**: Advanced  
**Estimated time**: 45 minutes

**Featured patterns:**
- Multiple image handling
- Media format conversion
- Vision capability testing
- Media error handling

## Getting Started

### Prerequisites

```bash
# Install PromptArena
make install-arena

# Set up vision-capable provider API keys
export OPENAI_API_KEY="your-key"      # GPT-4o, GPT-4o-mini
export ANTHROPIC_API_KEY="your-key"   # Claude 3.5 Sonnet
export GOOGLE_API_KEY="your-key"      # Gemini Pro
```

### Vision-Capable Models

| Provider | Model | Vision Support | Best For |
|----------|-------|----------------|----------|
| OpenAI | gpt-4o | ✅ Excellent | General vision, fast |
| OpenAI | gpt-4o-mini | ✅ Good | Cost-effective vision |
| Anthropic | claude-3-5-sonnet | ✅ Excellent | Detailed analysis |
| Google | gemini-1.5-pro | ✅ Excellent | High-resolution images |
| Google | gemini-1.5-flash | ✅ Good | Fast vision tasks |

### Running Multimodal Examples

```bash
# Navigate to an example
cd docs/arena/examples/multimodal/multimodal-basics

# Ensure image files exist
ls -la images/

# Run tests
promptarena run

# Test with specific vision model
promptarena run --provider openai-gpt4o
```

## Key Concepts

### Image Input

Include images in scenarios:

```yaml
turns:
  - user: "Describe this image"
    image: "./images/sample.jpg"
    expected:
      - type: contains
        value: ["scene", "objects", "colors"]
```

### Multiple Images

Test with multiple images:

```yaml
turns:
  - user: "Compare these two images"
    images:
      - "./images/before.jpg"
      - "./images/after.jpg"
    expected:
      - type: contains
        value: ["difference", "changed", "similar"]
```

### Image URLs

Use remote images:

```yaml
turns:
  - user: "Analyze this image"
    image: "https://example.com/image.jpg"
    expected:
      - type: contains
        value: "content description"
```

### Base64 Encoding

Include inline images:

```yaml
turns:
  - user: "Describe this"
    image:
      data: "data:image/jpeg;base64,/9j/4AAQSkZJRg..."
      format: "jpeg"
    expected:
      - type: not_empty
```

## Multimodal Testing Patterns

### Image Description

```yaml
test_cases:
  - name: "Image Description Test"
    turns:
      - user: "Describe what you see in this image"
        image: "./test-image.jpg"
        expected:
          # Check for key elements
          - type: contains
            value: ["objects", "scene", "colors"]
          
          # Verify adequate detail
          - type: min_length
            value: 50
          
          # Check descriptive language
          - type: sentiment
            value: descriptive
```

### Visual Question Answering

```yaml
test_cases:
  - name: "VQA Test"
    turns:
      - user: "How many people are in this image?"
        image: "./people.jpg"
        expected:
          # Should provide numeric answer
          - type: regex
            value: "\\d+"
          
          # Should be confident
          - type: not_contains
            value: ["uncertain", "maybe", "possibly"]
```

### Object Detection

```yaml
test_cases:
  - name: "Object Detection"
    turns:
      - user: "List all objects visible in this image"
        image: "./scene.jpg"
        expected:
          # Should identify multiple objects
          - type: list_length
            min: 3
          
          # Should use object vocabulary
          - type: contains_any
            value: ["chair", "table", "lamp", "window"]
```

### Image Comparison

```yaml
test_cases:
  - name: "Image Comparison"
    turns:
      - user: "What are the key differences between these images?"
        images:
          - "./before.jpg"
          - "./after.jpg"
        expected:
          # Should mention differences
          - type: contains
            value: ["difference", "changed", "different"]
          
          # Should be specific
          - type: contains_any
            value: ["color", "position", "size", "added", "removed"]
```

### OCR (Text in Images)

```yaml
test_cases:
  - name: "Text Extraction"
    turns:
      - user: "What text appears in this image?"
        image: "./text-image.jpg"
        expected:
          # Should extract text
          - type: contains
            value: "expected text content"
          
          # Should preserve formatting
          - type: maintains_structure
            value: true
```

## Advanced Patterns

### Multi-Turn with Images

Reference images across turns:

```yaml
test_cases:
  - name: "Multi-Turn Vision"
    turns:
      # Turn 1: Initial analysis
      - user: "Describe this image"
        image: "./product.jpg"
        expected:
          - type: contains
            value: "product"
      
      # Turn 2: Follow-up without re-sending image
      - user: "What color is it?"
        expected:
          # Should remember image from turn 1
          - type: contains
            value: "color"
          - type: references_previous_image
            value: true
```

### Image + Context

Combine images with textual context:

```yaml
test_cases:
  - name: "Image with Context"
    context:
      product_info: |
        Product ID: ABC123
        Category: Electronics
        Price: $299.99
    
    turns:
      - user: "Does the product in this image match our product info?"
        image: "./product.jpg"
        expected:
          - type: contains
            value: "match"
          - type: references_context
            value: true
```

### Error Handling

Test image error scenarios:

```yaml
test_cases:
  - name: "Invalid Image Handling"
    turns:
      # Missing image
      - user: "Describe this image"
        image: "./nonexistent.jpg"
        expected:
          - type: error
            message: "Image file not found"
      
      # Corrupted image
      - user: "Analyze this"
        image: "./corrupted.jpg"
        expected:
          - type: contains
            value: ["cannot", "unable", "error"]
```

### Provider Comparison

Test vision capabilities across providers:

```yaml
test_cases:
  - name: "Cross-Provider Vision"
    providers: [openai-gpt4o, claude-sonnet, gemini-pro]
    
    turns:
      - user: "Describe this image in detail"
        image: "./test-scene.jpg"
        expected:
          # Common requirement for all providers
          - type: min_length
            value: 100
          
          # Provider-specific thresholds
          - type: semantic_similarity
            baseline: "detailed scene description"
            threshold:
              openai-gpt4o: 0.85
              claude-sonnet: 0.90  # More detailed
              gemini-pro: 0.85
```

## Multimodal Assertions

### Vision-Specific Validations

```yaml
expected:
  # Image was processed
  - type: image_processed
    value: true
  
  # Contains visual elements
  - type: mentions_visual_elements
    min_elements: 3
  
  # Spatial awareness
  - type: contains_spatial_terms
    value: ["left", "right", "above", "below", "center"]
  
  # Color recognition
  - type: contains_colors
    value: ["red", "blue", "green"]
```

### Semantic Visual Validation

```yaml
expected:
  # Visual similarity to expected description
  - type: visual_semantic_similarity
    baseline: "A red car parked on a street"
    threshold: 0.85
  
  # Scene understanding
  - type: scene_classification
    expected_scenes: ["outdoor", "urban", "daytime"]
```

## Best Practices

### Image Preparation

```bash
# Ensure images are in supported formats
# JPEG, PNG, GIF, WebP

# Optimize image size for API limits
# OpenAI: 20MB max
# Anthropic: 5MB max per image, 25 images max
# Google: 20MB max

# Use relative paths from arena.yaml location
images/
  sample.jpg
  test-scene.png
```

### Testing Strategy

```yaml
# Start with basic description
test_cases:
  - name: "Basic Image Test"
    turns:
      - user: "What do you see?"
        image: "./simple.jpg"
        expected:
          - type: not_empty

# Add specific validations
test_cases:
  - name: "Detailed Image Test"
    turns:
      - user: "Describe in detail"
        image: "./complex.jpg"
        expected:
          - type: contains
            value: ["specific", "elements"]
          - type: min_length
            value: 100
```

### Cost Management

Vision API calls are more expensive:

```yaml
# Use mini/flash models for development
providers:
  - type: openai
    model: gpt-4o-mini  # More affordable

# Limit image resolution
image_config:
  max_resolution: 1024x1024
  
# Use mock for rapid iteration
providers:
  - type: mock
    vision_enabled: true
```

### Provider Selection

Choose providers based on needs:

```yaml
# OpenAI: Fast, balanced
openai-gpt4o-mini:
  use_for: ["rapid testing", "cost optimization"]

# Claude: Detailed analysis
claude-sonnet:
  use_for: ["detailed descriptions", "complex scenes"]

# Gemini: High-resolution, fast
gemini-pro:
  use_for: ["high-res images", "fast processing"]
```

## Troubleshooting

### Image Not Loading

```bash
# Check file exists
ls -la images/sample.jpg

# Verify path is relative to arena.yaml
# ✅ ./images/sample.jpg
# ❌ /absolute/path/images/sample.jpg

# Check file permissions
chmod 644 images/sample.jpg

# Verify file format
file images/sample.jpg
```

### Vision Not Working

1. Verify provider supports vision (gpt-4o, claude-3.5-sonnet, gemini-pro)
2. Check image file size limits
3. Ensure image format is supported
4. Review API error messages
5. Test with simple image first

### Poor Image Analysis

If analysis quality is low:

1. Try higher-resolution image
2. Use more capable model (gpt-4o vs mini)
3. Improve prompt specificity
4. Provide additional context
5. Compare across providers

## Example Use Cases

### Product Quality Control

```yaml
test_cases:
  - name: "Quality Inspection"
    turns:
      - user: "Inspect this product for defects"
        image: "./product-photo.jpg"
        expected:
          - type: contains_any
            value: ["defect", "acceptable", "quality"]
```

### Content Moderation

```yaml
test_cases:
  - name: "Image Moderation"
    turns:
      - user: "Is this image appropriate for general audiences?"
        image: "./user-upload.jpg"
        expected:
          - type: safety_classification
            value: "safe"
          - type: not_contains
            value: ["inappropriate", "unsafe"]
```

### Document Processing

```yaml
test_cases:
  - name: "Document OCR"
    turns:
      - user: "Extract all text and key information"
        image: "./invoice.jpg"
        expected:
          - type: contains
            value: ["invoice", "date", "amount"]
          - type: json_valid  # Structured extraction
```

## Next Steps

After mastering multimodal testing:

1. **Real-World Integration**: Apply to production use cases
2. **Advanced Analysis**: Document processing, visual QA
3. **Multi-Modal + MCP**: Combine vision with tools
4. **Performance Optimization**: Balance quality and cost

## Additional Resources

- [OpenAI Vision Guide](https://platform.openai.com/docs/guides/vision)
- [Claude Vision Guide](https://docs.anthropic.com/en/docs/vision)
- [Gemini Vision Guide](https://ai.google.dev/gemini-api/docs/vision)
- [How-To: Configure Providers](../../how-to/configure-providers.md)
- [Explanation: Provider Comparison](../../explanation/provider-comparison.md)
