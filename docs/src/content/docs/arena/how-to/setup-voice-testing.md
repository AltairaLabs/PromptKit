---
title: Set Up Voice Testing with Self-Play
---
Configure automated voice testing using self-play mode with TTS for multi-turn conversations.

## Prerequisites

- Gemini API key (for duplex streaming)
- OpenAI API key (for TTS, or use mock TTS)
- Audio files in PCM format (16kHz, 16-bit, mono)

## Quick Setup

### 1. Declare TTS Providers and Voices in the Arena Config

TTS is configured at the arena level. Declare one or more TTS provider files under
`tts_providers:`, then bind voice IDs in `voices:`. Personas and scenarios reference
those IDs — a single edit to `voices:` swaps between a real vendor and mock TTS for CI.

```yaml
# config.arena.yaml
spec:
  providers:
    - file: providers/gemini-live.provider.yaml

  tts_providers:
    - file: providers/openai-alloy.provider.yaml  # real TTS
    - file: providers/mock-tts.provider.yaml       # for CI

  voices:
    # Real-vendor mode: point to openai-alloy.
    # CI / keyless mode: change provider to mock-tts.
    - id: test-voice
      provider: openai-alloy
```

The provider files themselves declare the vendor details:

```yaml
# providers/openai-alloy.provider.yaml
apiVersion: promptkit.altairalabs.ai/v1alpha1
kind: Provider
metadata:
  name: openai-alloy
spec:
  id: openai-alloy
  type: openai
  capability: tts
  voice: alloy
  sample_rate: 24000
```

### 2. Create a Provider Configuration for the Duplex Model

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

### 3. Create a Persona for Self-Play

Assign a voice ID from the catalog to the persona. The runtime resolves it to the
correct TTS provider at run time.

```yaml
# prompts/personas/test-user.persona.yaml
apiVersion: promptkit.altairalabs.ai/v1alpha1
kind: Persona
metadata:
  name: test-user
spec:
  id: test-user
  voice: test-voice
  description: "Curious user asking follow-up questions"
  system_prompt: |
    You are testing a voice assistant. Ask natural follow-up
    questions based on the assistant's responses. Keep questions
    brief and conversational.
```

### 4. Create the Self-Play Scenario

The scenario references the persona by ID. No inline `tts:` block is needed — the
voice is resolved through the catalog.

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

    # Self-play generates follow-up turns; voice is resolved from the persona
    - role: selfplay-user
      persona: test-user
      turns: 3
```

### 5. Run the Test

```bash
export GEMINI_API_KEY="your-key"
export OPENAI_API_KEY="your-key"
promptarena run --scenario voice-selfplay --provider gemini-live
```

## CI vs Recording Mode

Because voice IDs are declared in one place (`voices:` in the arena config), switching
between real TTS and a mock is a single-line change:

```yaml
voices:
  # Recording mode (requires OPENAI_API_KEY):
  - id: test-voice
    provider: openai-alloy

  # CI / keyless mode — swap to:
  # - id: test-voice
  #   provider: mock-tts
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

- [Tutorial 6: Duplex Voice Testing](/arena/tutorials/06-duplex-testing/) - Complete learning path
- [Duplex Configuration Reference](/arena/reference/duplex-config/) - All configuration options
- [Duplex Architecture](/arena/explanation/duplex-architecture/) - How duplex streaming works
