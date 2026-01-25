# Provider Capability Matrix

This document tracks expected capabilities for each provider/model based on official documentation.

## Capability Legend

| Capability | Description |
|------------|-------------|
| text | Basic text completion |
| streaming | Streaming responses |
| vision | Image input understanding |
| audio | Audio input understanding |
| video | Video input understanding |
| tools | Function/tool calling |
| json | JSON mode / structured output |

## OpenAI Models

| Model ID | Text | Stream | Vision | Audio | Video | Tools | JSON | Notes |
|----------|------|--------|--------|-------|-------|-------|------|-------|
| gpt-4o | ✅ | ✅ | ✅ | ❌ | ❌ | ✅ | ✅ | Standard multimodal (no audio) |
| gpt-4o-mini | ✅ | ✅ | ✅ | ❌ | ❌ | ✅ | ✅ | Smaller, faster version |
| gpt-4o-audio-preview | ✅ | ✅ | ❌ | ✅ | ❌ | ✅ | ✅ | Audio-specific model |
| gpt-4o-mini-audio-preview | ✅ | ✅ | ❌ | ✅ | ❌ | ✅ | ✅ | Smaller audio model |
| gpt-4.1 | ✅ | ✅ | ✅ | ❌ | ❌ | ✅ | ✅ | Updated GPT-4 |
| gpt-4.1-mini | ✅ | ✅ | ✅ | ❌ | ❌ | ✅ | ✅ | |
| gpt-4.1-nano | ✅ | ✅ | ✅ | ❌ | ❌ | ✅ | ✅ | Smallest 4.1 variant |
| gpt-5 | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | Full multimodal (Aug 2025) |
| gpt-5-mini | ✅ | ✅ | ✅ | ✅ | ❌ | ✅ | ✅ | Smaller GPT-5 |
| gpt-5-nano | ✅ | ✅ | ✅ | ❌ | ❌ | ✅ | ✅ | Smallest GPT-5 |
| gpt-5-pro | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | Enhanced reasoning |
| gpt-5.1 | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | Updated GPT-5 |
| gpt-5.2 | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | Latest (Dec 2025) |
| gpt-5.2-pro | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | Pro variant |
| o1 | ✅ | ✅ | ✅ | ❌ | ❌ | ✅ | ✅ | Reasoning model |
| o1-pro | ✅ | ✅ | ✅ | ❌ | ❌ | ✅ | ✅ | Enhanced reasoning |
| o3 | ✅ | ✅ | ✅ | ❌ | ❌ | ✅ | ✅ | Latest reasoning |
| o3-mini | ✅ | ✅ | ❌ | ❌ | ❌ | ✅ | ✅ | No vision support |
| o4-mini | ✅ | ✅ | ✅ | ❌ | ❌ | ✅ | ✅ | |

## Anthropic/Claude Models

| Model ID | Text | Stream | Vision | Audio | Video | Tools | JSON | Notes |
|----------|------|--------|--------|-------|-------|-------|------|-------|
| claude-3.5-sonnet | ✅ | ✅ | ✅ | ❌ | ❌ | ✅ | ✅ | |
| claude-3.5-haiku | ✅ | ✅ | ✅ | ❌ | ❌ | ✅ | ✅ | Fast, efficient |
| claude-3.7-sonnet | ✅ | ✅ | ✅ | ❌ | ❌ | ✅ | ✅ | Extended thinking |
| claude-sonnet-4 | ✅ | ✅ | ✅ | ❌ | ❌ | ✅ | ✅ | |
| claude-sonnet-4.5 | ✅ | ✅ | ✅ | ❌ | ❌ | ✅ | ✅ | |
| claude-opus-4 | ✅ | ✅ | ✅ | ❌ | ❌ | ✅ | ✅ | Most capable |
| claude-opus-4.1 | ✅ | ✅ | ✅ | ❌ | ❌ | ✅ | ✅ | |
| claude-opus-4.5 | ✅ | ✅ | ✅ | ❌ | ❌ | ✅ | ✅ | Latest Opus |
| claude-haiku-4.5 | ✅ | ✅ | ✅ | ❌ | ❌ | ✅ | ✅ | Fast, cost-effective |

**Note:** Claude API does not support native audio input. Voice features in consumer apps use external STT/TTS.

## Google Gemini Models

| Model ID | Text | Stream | Vision | Audio | Video | Tools | JSON | Notes |
|----------|------|--------|--------|-------|-------|-------|------|-------|
| gemini-2.0-flash | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | Fast multimodal |
| gemini-2.0-flash-lite | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | Lighter version |
| gemini-2.5-flash | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | |
| gemini-2.5-flash-lite | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | |
| gemini-2.5-pro | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | Most capable |
| gemini-3.0-flash | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | Latest generation |
| gemini-3.0-pro | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | |

**Audio formats supported:** wav, mp3, aiff, aac, ogg, flac (up to 9.5 hours, 20MB inline)

## Implementation Status

### Fully Implemented
- **Text, Streaming, Vision, Tools, JSON** - All providers
- **Audio Input** - Gemini only (inline data up to 20MB)
- **Video Input** - Gemini only

### Partially Implemented
- **OpenAI Audio Models** (`gpt-4o-audio-preview`, `gpt-4o-mini-audio-preview`)
  - Implemented via Chat Completions API with `modalities: ["text", "audio"]` parameter
  - Requires `api_mode: completions` in provider config (Responses API doesn't support audio)
  - Supports WAV and MP3 formats only
  - See: https://platform.openai.com/docs/guides/audio

### Not Yet Implemented
- **OpenAI Realtime API** (WebSocket-based live audio)
  - Different API endpoint and protocol entirely
  - Not applicable for batch testing

### Known Limitations
- Claude/Anthropic has no native audio input support in the API
- OpenAI standard models (gpt-4o, o1, etc.) don't support audio input
- Audio input requires specific model variants or Gemini

## Sources

- [OpenAI Models](https://platform.openai.com/docs/models)
- [OpenAI Audio Guide](https://platform.openai.com/docs/guides/audio)
- [Anthropic Models](https://docs.anthropic.com/en/docs/about-claude/models)
- [Gemini Audio Understanding](https://ai.google.dev/gemini-api/docs/audio)
- [GPT-5 Announcement](https://openai.com/index/introducing-gpt-5/)

## Last Updated

2026-01-25
