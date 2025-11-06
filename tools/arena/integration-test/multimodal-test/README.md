# Multimodal Integration Test

This integration test verifies that PromptArena correctly handles multimodal content (text, images, audio) in scenario turns.

## Test Coverage

The test scenario `multimodal-mock.yaml` includes:

1. **Turn 1**: Text-only content (backward compatibility test)
2. **Turn 2**: Image from URL
3. **Turn 3**: Image from local file
4. **Turn 4**: Audio from local file  
5. **Turn 5**: Mixed multimodal content (multiple images + text)

## Running the Test

### With Mock Provider

```bash
cd tools/arena/integration-test/multimodal-test
../../../../bin/promptarena run --config arena.yaml --mock-provider --mock-config mock-responses.yaml
```

### With Real Provider

The real provider test uses an actual multimodal LLM (OpenAI GPT-4o-mini) to verify end-to-end functionality.

**Requirements**:

- Valid OpenAI API key set in environment: `export OPENAI_API_KEY=your-key-here`

**Run the test**:

```bash
cd tools/arena/integration-test/multimodal-test
../../../../bin/promptarena run --config arena-real.yaml
```

Note: This makes actual API calls and will incur small costs (~$0.001 per run).

## Test Files

### Mock Provider Test

- `arena.yaml`: Arena configuration for mock provider test
- `scenarios/multimodal-mock.yaml`: Test scenario with 5 multimodal turns
- `providers/mock-multimodal-provider.yaml`: Mock provider configuration
- `mock-responses.yaml`: Mock responses for each turn
- `test-media/`: Directory containing test media files
  - `test-image.jpg`: Fake image file for testing
  - `test-audio.mp3`: Fake audio file for testing

### Real Provider Test

- `arena-real.yaml`: Arena configuration for real provider test
- `scenarios/multimodal-real.yaml`: Test scenario with real image URL (2 turns)
- `providers/openai-gpt-4o-mini-real.yaml`: OpenAI GPT-4o-mini provider configuration

## Implementation Notes

### OpenAI Provider Multimodal Support

The OpenAI provider's `Chat()` method now automatically handles multimodal content. When messages contain `Parts` instead of just `Content`, the provider:

1. Detects multimodal content via `msg.IsMultimodal()`
2. Converts each `ContentPart` to OpenAI's format:
   - Text parts: `{"type": "text", "text": "..."}`
   - Image parts: `{"type": "image_url", "image_url": {"url": "data:image/jpeg;base64,..."}}`
3. Handles both URLs and base64-encoded file data
4. Applies detail level settings for images

This means Arena scenarios with `Parts` work seamlessly with OpenAI providers without requiring any special configuration.

## Expected Results

- **Total runs**: 1
- **Successful**: 1  
- **Errors**: 0
- **Messages**: 10 (5 turns × 2 messages per turn)

The test validates:

- Text-only content still works (backward compatibility)
- Images from URLs are processed
- Images from local files are loaded and base64-encoded
- Audio files are loaded and base64-encoded
- Mixed multimodal content (multiple parts) is handled correctly
- All assertions pass
