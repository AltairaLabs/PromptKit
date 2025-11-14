# Google Imagen Provider

Google Imagen 4.0 provider for image generation using the Gemini API.

## Requirements

✅ **Works with AI Studio API keys** - No Google Cloud billing required!

- Google AI Studio API key (same as Gemini)
- Access via `generativelanguage.googleapis.com` endpoint
- Model: `imagen-4.0-generate-001`

## Configuration

```yaml
apiVersion: promptkit.altairalabs.ai/v1alpha1
kind: Provider
metadata:
  name: imagen-provider
spec:
  type: imagen
  model: imagen-4.0-generate-001
```

**Note**: Project ID and location are no longer required when using the Gemini API endpoint.

## Environment Variables

- `GOOGLE_API_KEY` or `GEMINI_API_KEY` - Required for authentication

## Cost

- **$0.04 per image** (fixed cost, not token-based)
- No token counting

## API Details

- Endpoint: `https://generativelanguage.googleapis.com/v1beta/models/{model}:predict`
- Authentication: `x-goog-api-key` header (same as Gemini)
- Method: POST
- Request format: `{"instances": [{"prompt": "..."}], "parameters": {"sampleCount": 1, ...}}`
- Response format: `{"predictions": [{"bytesBase64Encoded": "...", "mimeType": "image/png"}]}`

## Limitations

- No streaming support
- **Works with AI Studio API keys** (no GCP billing required!)
- Only supports image generation (no analysis or multimodal understanding)
- **Always generates PNG format** - cannot generate JPEG, WebP, or other formats
- Cannot convert or manipulate existing images
- Generates one image per request (even if prompt asks for multiple)

## Alternative for Testing

For testing image generation scenarios without GCP billing:
- Use the `mock` provider with pre-generated images
- Or implement an OpenAI DALL-E provider (requires OpenAI API key)
