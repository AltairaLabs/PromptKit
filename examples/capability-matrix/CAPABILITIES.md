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
| gpt-audio-1.5 | ✅ | ✅ | ❌ | ✅ | ❌ | ✅ | ✅ | GA audio (replaces gpt-4o-audio-preview, retired 2026-05-07) |
| gpt-audio-mini | ✅ | ✅ | ❌ | ✅ | ❌ | ✅ | ✅ | GA audio mini (replaces gpt-4o-mini-audio-preview) |
| gpt-4.1 | ✅ | ✅ | ✅ | ❌ | ❌ | ✅ | ✅ | Updated GPT-4 |
| gpt-4.1-mini | ✅ | ✅ | ✅ | ❌ | ❌ | ✅ | ✅ | |
| gpt-4.1-nano | ✅ | ✅ | ✅ | ❌ | ❌ | ✅ | ✅ | Smallest 4.1 variant |
| gpt-5 | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | Full multimodal (Aug 2025) |
| gpt-5-mini | ✅ | ✅ | ✅ | ✅ | ❌ | ✅ | ✅ | Smaller GPT-5 |
| gpt-5-nano | ✅ | ✅ | ✅ | ❌ | ❌ | ✅ | ✅ | Smallest GPT-5 |
| gpt-5-pro | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | Enhanced reasoning |
| gpt-5.1 | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | Updated GPT-5 |
| gpt-5.2 | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | Dec 2025 |
| gpt-5.2-pro | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | Pro variant |
| gpt-5.4 | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | |
| gpt-5.4-mini | ✅ | ✅ | ❌ | ❌ | ❌ | ✅ | ✅ | Cost-efficient |
| gpt-5.5 | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | Current frontier |
| o1 | ✅ | ✅ | ✅ | ❌ | ❌ | ✅ | ✅ | Reasoning model — deprecated, retires 2026-10-23 |
| o1-pro | ✅ | ✅ | ✅ | ❌ | ❌ | ✅ | ✅ | Deprecated, retires 2026-10-23 |
| o3 | ✅ | ✅ | ✅ | ❌ | ❌ | ✅ | ✅ | Deprecated, retires 2026-10-23 |
| o3-mini | ✅ | ✅ | ❌ | ❌ | ❌ | ✅ | ✅ | No vision; deprecated, retires 2026-10-23 |
| o4-mini | ✅ | ✅ | ✅ | ❌ | ❌ | ✅ | ✅ | Deprecated, retires 2026-10-23 |

## Anthropic/Claude Models

| Model ID | Text | Stream | Vision | Audio | Video | Tools | JSON | Notes |
|----------|------|--------|--------|-------|-------|-------|------|-------|
| claude-3.5-sonnet | ✅ | ✅ | ✅ | ❌ | ❌ | ✅ | ✅ | |
| claude-3.5-haiku | ✅ | ✅ | ✅ | ❌ | ❌ | ✅ | ✅ | Fast, efficient |
| claude-3.7-sonnet | ✅ | ✅ | ✅ | ❌ | ❌ | ✅ | ✅ | Extended thinking |
| claude-sonnet-4 | ✅ | ✅ | ✅ | ❌ | ❌ | ✅ | ✅ | Deprecated, retires 2026-06-15 |
| claude-sonnet-4.5 | ✅ | ✅ | ✅ | ❌ | ❌ | ✅ | ✅ | |
| claude-sonnet-4.6 | ✅ | ✅ | ✅ | ❌ | ❌ | ✅ | ✅ | |
| claude-opus-4 | ✅ | ✅ | ✅ | ❌ | ❌ | ✅ | ✅ | Deprecated, retires 2026-06-15 |
| claude-opus-4.1 | ✅ | ✅ | ✅ | ❌ | ❌ | ✅ | ✅ | |
| claude-opus-4.5 | ✅ | ✅ | ✅ | ❌ | ❌ | ✅ | ✅ | |
| claude-opus-4.6 | ✅ | ✅ | ✅ | ❌ | ❌ | ✅ | ✅ | |
| claude-opus-4.7 | ✅ | ✅ | ✅ | ❌ | ❌ | ✅ | ✅ | |
| claude-opus-4.8 | ✅ | ✅ | ✅ | ❌ | ❌ | ✅ | ✅ | Latest Opus |
| claude-haiku-4.5 | ✅ | ✅ | ✅ | ❌ | ❌ | ✅ | ✅ | Fast, cost-effective |

**Note:** Claude API does not support native audio input. Voice features in consumer apps use external STT/TTS.

## Google Gemini Models

| Model ID | Text | Stream | Vision | Audio | Video | Tools | JSON | Notes |
|----------|------|--------|--------|-------|-------|-------|------|-------|
| gemini-2.5-flash | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | Deprecated, retires 2026-10-16 |
| gemini-2.5-flash-lite | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | Deprecated, retires 2026-10-16 |
| gemini-2.5-pro | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | Deprecated, retires 2026-10-16 |
| gemini-3.1-flash-lite | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | Replaces gemini-2.0-flash-lite (retired 2026-06-01) |
| gemini-3.5-flash | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | Replaces gemini-3-flash-preview |
| gemini-3.1-pro-preview | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | Replaces gemini-3-pro-preview (retired) |

**Audio formats supported:** wav, mp3, aiff, aac, ogg, flac (up to 9.5 hours, 20MB inline)

## Implementation Status

### Fully Implemented
- **Text, Streaming, Vision, Tools, JSON** - All providers
- **Audio Input** - Gemini only (inline data up to 20MB)
- **Video Input** - Gemini only

### Partially Implemented
- **OpenAI Audio Models** (`gpt-audio-1.5`, `gpt-audio-mini`)
  - The earlier `gpt-4o-audio-preview` / `gpt-4o-mini-audio-preview` models were retired 2026-05-07
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

2026-06-05
