---
title: Duplex Streaming Example
description: Example demonstrating duplex-streaming
sidebar:
  order: 100
---


This example demonstrates Arena's **duplex streaming** capabilities for testing real-time, bidirectional audio conversations with LLMs.

## What is Duplex Streaming?

Duplex streaming enables simultaneous input and output audio streams, allowing for natural voice conversations where:

- **User speaks** → Audio is streamed to the LLM in real-time
- **LLM responds** → Audio is streamed back while the user might still be speaking
- **Natural interruptions** → The system handles turn-taking using voice activity detection (VAD)

This is ideal for testing voice assistants, customer support bots, and any real-time conversational AI.

## Features Demonstrated

| Feature | Description |
|---------|-------------|
| **Duplex Mode** | Bidirectional audio streaming with configurable timeouts |
| **VAD Turn Detection** | Voice activity detection for natural conversation flow |
| **Self-Play with TTS** | LLM-generated user messages converted to audio via TTS |
| **Multiple Providers** | Test across Gemini 2.0 Flash and OpenAI GPT-4o Realtime |
| **Mock Mode** | CI-friendly testing without API keys |

## Prerequisites

### For Local Testing (Real Providers)

```bash
# Set your API keys
export GEMINI_API_KEY="your-gemini-api-key"
export OPENAI_API_KEY="your-openai-api-key"
```

### For CI Testing (Mock Provider)

No API keys required - uses deterministic mock responses.

## Quick Start

### Run with Mock Provider (CI Mode)

```bash
# Navigate to the example directory
cd examples/duplex-streaming

# Run all scenarios with mock provider
promptarena run --provider mock-duplex

# Run a specific scenario
promptarena run --scenario duplex-basic --provider mock-duplex
```

### Run with Real Providers (Local Testing)

```bash
# Run with Gemini 2.0 Flash (requires GEMINI_API_KEY)
promptarena run --provider gemini-2-flash

# Run with OpenAI GPT-4o Realtime (requires OPENAI_API_KEY)
promptarena run --provider openai-gpt4o-realtime

# Run specific scenario
promptarena run --scenario duplex-selfplay --provider gemini-2-flash
```

## Scenarios

### 1. `duplex-basic` - Basic Duplex Streaming

Simple scripted conversation to verify duplex functionality:
- 3 scripted user turns
- Tests greeting, Q&A, and follow-up
- Validates response patterns

```yaml
duplex:
  timeout: "5m"
  turn_detection:
    mode: vad
    vad:
      silence_threshold_ms: 500
      min_speech_ms: 1000
```

### 2. `duplex-selfplay` - Self-Play with TTS

Demonstrates automated conversation testing using self-play:
- LLM generates user messages
- TTS converts generated text to audio
- Audio is fed back into the duplex stream

```yaml
turns:
  - role: selfplay-user
    persona: curious-customer
    turns: 2
    tts:
      provider: openai
      voice: alloy
```

### 3. `duplex-interactive` - Interactive Technical Support

Extended conversation simulating a support call:
- Multiple self-play turns with different personas
- Comprehensive assertion testing
- Tests natural conversation flow

## Configuration Reference

### Duplex Configuration

```yaml
spec:
  duplex:
    # Maximum session duration
    timeout: "10m"

    # Turn detection settings
    turn_detection:
      mode: vad  # "vad" or "asm" (provider-native)
      vad:
        # Silence duration to trigger turn end (ms)
        silence_threshold_ms: 500
        # Minimum speech before silence counts (ms)
        min_speech_ms: 1000
```

### TTS Configuration (Self-Play)

```yaml
turns:
  - role: selfplay-user
    persona: curious-customer
    tts:
      provider: openai    # "openai", "elevenlabs", "cartesia"
      voice: alloy        # Provider-specific voice ID
```

### Available TTS Voices

| Provider | Voices |
|----------|--------|
| OpenAI | `alloy`, `echo`, `fable`, `onyx`, `nova`, `shimmer` |
| ElevenLabs | Use voice IDs from your ElevenLabs account |
| Cartesia | Use voice IDs from your Cartesia account |

### Audio File Input

For testing with pre-recorded audio files, use the `parts` field with media content:

```yaml
turns:
  # Turn 1: Greeting - "Hello, can you hear me?"
  - role: user
    parts:
      - type: audio
        media:
          file_path: audio/greeting.pcm
          mime_type: audio/L16
```

In duplex mode, the audio from `parts` is streamed directly to the model. Use comments to document what each audio file contains.

**Supported audio formats:**
- PCM (audio/L16) - Raw 16-bit PCM at 16kHz mono
- Opus (audio/opus) - Compressed audio
- WAV (audio/wav) - Uncompressed WAV files

## File Structure

```
duplex-streaming/
├── config.arena.yaml           # Main arena configuration
├── README.md                   # This file
├── mock-responses.yaml         # Mock responses for CI testing
├── audio/                      # Pre-recorded audio fixtures
│   ├── greeting.pcm            # "Hello, can you hear me?"
│   ├── question.pcm            # "What's your name?"
│   └── funfact.pcm             # "Tell me a fun fact"
├── providers/
│   ├── gemini-2-flash.provider.yaml
│   ├── openai-gpt4o-realtime.provider.yaml
│   └── mock-duplex.provider.yaml
├── scenarios/
│   ├── duplex-basic.scenario.yaml
│   ├── duplex-selfplay.scenario.yaml
│   └── duplex-interactive.scenario.yaml
├── prompts/
│   └── voice-assistant.prompt.yaml
├── personas/
│   ├── curious-customer.persona.yaml
│   └── technical-user.persona.yaml
└── out/                        # Test results output
```

## Current Status

Duplex streaming requires providers that support bidirectional audio streaming.

### Provider Requirements

Duplex mode requires providers to implement `StreamInputSupport` interface, which enables:
- Streaming audio input to the model
- Streaming audio output from the model
- Bidirectional, real-time conversation

**Supported providers:**
- Gemini 2.0 Flash (with audio enabled)
- OpenAI GPT-4o Realtime
- Mock provider (for CI/testing)

**Not supported:**
- Standard text-only providers

When running with unsupported providers, you'll see:
```
Error: provider does not support streaming input
```

## CI/CD Integration

### Using Mock Provider

The mock provider fully supports duplex streaming, enabling CI testing without API keys:

```yaml
# GitHub Actions example - run duplex tests
- name: Run Duplex Streaming Tests
  run: |
    cd examples/duplex-streaming
    promptarena run --provider mock-duplex
```

For schema validation only:

```yaml
# GitHub Actions example - validate configuration
- name: Validate Duplex Streaming Config
  run: |
    cd examples/duplex-streaming
    promptarena validate config.arena.yaml
```

### Audio Fixtures

Pre-recorded PCM audio files are included in the `audio/` directory for testing:
- `greeting.pcm` - Simple greeting (~2.5s)
- `question.pcm` - Basic question (~1.5s)
- `funfact.pcm` - Follow-up request (~2.3s)

These can be used to test audio streaming without TTS dependencies.

## Troubleshooting

### "Provider does not support streaming"

Ensure you're using a provider that supports duplex mode:
- Gemini 2.0 Flash with audio enabled
- OpenAI GPT-4o Realtime
- Mock provider (mock-duplex)

### "TTS provider not configured"

For self-play scenarios with TTS, ensure:
1. The TTS provider API key is set (e.g., `OPENAI_API_KEY`)
2. The voice ID is valid for the chosen provider

### "VAD timeout"

If turn detection isn't working:
- Increase `silence_threshold_ms` for longer pauses
- Decrease `min_speech_ms` if speech is being cut off

## Learn More

- [Tutorial: Duplex Voice Testing](/arena/tutorials/06-duplex-testing) - Step-by-step learning guide
- [Duplex Configuration Reference](/arena/reference/duplex-config) - Complete configuration options
- [Duplex Architecture](/arena/explanation/duplex-architecture) - How duplex streaming works
- [Set Up Voice Testing with Self-Play](/arena/how-to/setup-voice-testing) - Quick-start guide
