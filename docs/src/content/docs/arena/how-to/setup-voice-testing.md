---
title: Set Up Voice Testing with Self-Play
sidebar:
  order: 8
---
Configure automated voice testing using self-play mode with TTS for multi-turn conversations.

## Prerequisites

- Gemini API key (for duplex streaming)
- OpenAI API key (for TTS, or use mock TTS)
- Audio files in PCM format (16kHz, 16-bit, mono)

## Quick Setup

### 1. Create the Provider Configuration

```yaml
# providers/gemini-live.provider.yaml
apiVersion: promptkit.altairalabs.ai/v1alpha1
kind: Provider
metadata:
  name: gemini-live
spec:
  id: gemini-live
  type: gemini
  model: gemini-2.0-flash-exp
  additional_config:
    audio_enabled: true
    response_modalities:
      - AUDIO
```

### 2. Create a Persona for Self-Play

```yaml
# prompts/personas/test-user.persona.yaml
apiVersion: promptkit.altairalabs.ai/v1alpha1
kind: Persona
metadata:
  name: test-user
spec:
  id: test-user
  description: "Curious user asking follow-up questions"
  system_prompt: |
    You are testing a voice assistant. Ask natural follow-up
    questions based on the assistant's responses. Keep questions
    brief and conversational.
```

### 3. Create the Self-Play Scenario

```yaml
# scenarios/voice-selfplay.scenario.yaml
apiVersion: promptkit.altairalabs.ai/v1alpha1
kind: Scenario
metadata:
  name: voice-selfplay
spec:
  id: voice-selfplay
  task_type: voice-assistant
  streaming: true

  duplex:
    timeout: "5m"
    turn_detection:
      mode: vad
      vad:
        silence_threshold_ms: 1200  # Longer for TTS pauses
        min_speech_ms: 500
    resilience:
      partial_success_min_turns: 2
      ignore_last_turn_session_end: true

  turns:
    # Initial audio turn
    - role: user
      parts:
        - type: audio
          media:
            file_path: audio/greeting.pcm
            mime_type: audio/L16

    # Self-play generates follow-up turns
    - role: selfplay-user
      persona: test-user
      turns: 3
      tts:
        provider: openai
        voice: alloy
```

### 4. Run the Test

```bash
export GEMINI_API_KEY="your-key"
export OPENAI_API_KEY="your-key"
promptarena run --scenario voice-selfplay --provider gemini-live
```

## Using Mock TTS

For faster testing without OpenAI costs, use pre-recorded audio:

```yaml
turns:
  - role: selfplay-user
    persona: test-user
    turns: 3
    tts:
      provider: mock
      audio_files:
        - audio/question1.pcm
        - audio/question2.pcm
        - audio/question3.pcm
      sample_rate: 16000
```

## Tuning Turn Detection

If turns are cutting off early or late, adjust VAD settings:

| Issue | Solution |
|-------|----------|
| Cuts off mid-sentence | Increase `silence_threshold_ms` to 1500-2000 |
| Long pauses before response | Decrease `silence_threshold_ms` to 800-1000 |
| Short utterances ignored | Decrease `min_speech_ms` to 200-300 |

## Adding Assertions

Validate responses with turn-level assertions:

```yaml
turns:
  - role: selfplay-user
    persona: test-user
    turns: 3
    tts:
      provider: openai
      voice: alloy
    assertions:
      - type: content_matches
        params:
          pattern: ".{20,}"  # At least 20 characters
      - type: content_includes
        params:
          patterns:
            - "help"
            - "assist"
```

## See Also

- [Tutorial 6: Duplex Voice Testing](../tutorials/06-duplex-testing) - Complete learning path
- [Duplex Configuration Reference](../reference/duplex-config) - All configuration options
- [Duplex Architecture](../explanation/duplex-architecture) - How duplex streaming works
