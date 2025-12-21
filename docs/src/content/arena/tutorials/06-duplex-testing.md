---
title: 'Tutorial 6: Duplex Voice Testing'
docType: tutorial
order: 6
---
# Tutorial 6: Duplex Voice Testing

Learn to test bidirectional voice conversations with real-time audio streaming.

## What You'll Learn

- Understand duplex streaming vs traditional audio testing
- Create duplex test scenarios with audio files
- Configure turn detection modes (VAD vs provider-native)
- Use self-play with TTS for automated voice testing
- Handle session resilience and error recovery

## Prerequisites

- Completed [Tutorial 1: Your First Test](01-first-test)
- A Gemini API key (duplex streaming requires Gemini Live API)
- Optional: OpenAI API key (for TTS in self-play mode)

## Understanding Duplex Streaming

Traditional audio testing sends entire audio files as blobs. **Duplex streaming** is different:

| Aspect | Traditional | Duplex |
|--------|-------------|--------|
| Audio delivery | Entire file at once | Streamed in chunks |
| Turn detection | Manual (per turn) | Dynamic (VAD or provider) |
| Response timing | After full upload | Real-time as audio streams |
| Use case | Transcription testing | Voice assistants, interviews |

Duplex mode enables testing of **real-time voice conversations** where timing and turn-taking matter.

## Step 1: Set Up Your Project

Create a new duplex testing project:

```bash
mkdir duplex-test
cd duplex-test
mkdir -p audio prompts providers scenarios
```

Create the arena configuration:

```yaml
# arena.yaml
apiVersion: promptkit.altairalabs.ai/v1alpha1
kind: ArenaConfig
metadata:
  name: duplex-test

spec:
  prompts_dir: ./prompts
  providers_dir: ./providers
  scenarios_dir: ./scenarios
  output_dir: ./out
```

## Step 2: Configure the Gemini Provider

Duplex streaming requires Gemini's Live API:

```yaml
# providers/gemini-live.provider.yaml
apiVersion: promptkit.altairalabs.ai/v1alpha1
kind: Provider
metadata:
  name: gemini-live

spec:
  id: gemini-live
  type: gemini
  api_key_env: GEMINI_API_KEY
  model: gemini-2.0-flash-exp

  # Enable streaming with audio output
  streaming:
    enabled: true
    response_modalities:
      - AUDIO
      - TEXT
```

Set your API key:

```bash
export GEMINI_API_KEY="your-api-key"
```

## Step 3: Create a Voice Assistant Prompt

```yaml
# prompts/voice-assistant.prompt.yaml
apiVersion: promptkit.altairalabs.ai/v1alpha1
kind: Prompt
metadata:
  name: voice-assistant

spec:
  id: voice-assistant
  task_type: voice-assistant

  system_prompt: |
    You are Nova, a friendly voice assistant. Keep responses brief
    and conversational since this is a voice interaction.

    Guidelines:
    - Speak naturally as if in conversation
    - Keep responses under 2-3 sentences
    - Be helpful and warm
```

## Step 4: Prepare Audio Files

You'll need PCM audio files for testing. Audio requirements:

| Parameter | Value |
|-----------|-------|
| Format | Raw PCM (no headers) |
| Sample Rate | 16000 Hz |
| Bit Depth | 16-bit |
| Channels | Mono |

Convert existing audio files using ffmpeg:

```bash
# Convert WAV to PCM
ffmpeg -i input.wav -f s16le -ar 16000 -ac 1 audio/greeting.pcm

# Convert MP3 to PCM
ffmpeg -i input.mp3 -f s16le -ar 16000 -ac 1 audio/question.pcm
```

Or record directly in the correct format:

```bash
# Record 5 seconds of audio (macOS)
rec -r 16000 -b 16 -c 1 -e signed-integer audio/greeting.pcm trim 0 5
```

## Step 5: Create a Basic Duplex Scenario

```yaml
# scenarios/basic-duplex.scenario.yaml
apiVersion: promptkit.altairalabs.ai/v1alpha1
kind: Scenario
metadata:
  name: basic-duplex

spec:
  id: basic-duplex
  task_type: voice-assistant
  description: "Basic duplex streaming test"

  # Enable duplex mode
  duplex:
    timeout: "30s"
    turn_detection:
      mode: asm  # Provider-native turn detection

  streaming: true

  turns:
    # Turn 1: Greeting
    - role: user
      parts:
        - type: audio
          media:
            file_path: audio/greeting.pcm
            mime_type: audio/L16
      assertions:
        - type: content_matches
          params:
            pattern: "(?i)(hello|hi|hey)"

    # Turn 2: Question
    - role: user
      parts:
        - type: audio
          media:
            file_path: audio/question.pcm
            mime_type: audio/L16
      assertions:
        - type: content_matches
          params:
            pattern: ".{10,}"  # At least 10 chars response
```

## Step 6: Run Your First Duplex Test

```bash
promptarena run --scenario basic-duplex --provider gemini-live
```

You should see real-time streaming output as audio is processed.

## Turn Detection Modes

Duplex supports two turn detection modes:

### ASM Mode (Provider-Native)

The provider (Gemini) handles turn detection internally:

```yaml
duplex:
  turn_detection:
    mode: asm
```

**Best for**: Simple tests, provider-specific behavior testing.

### VAD Mode (Voice Activity Detection)

Client-side VAD with configurable thresholds:

```yaml
duplex:
  turn_detection:
    mode: vad
    vad:
      silence_threshold_ms: 600   # Silence to end turn
      min_speech_ms: 200          # Minimum speech duration
```

**Best for**: Precise control over turn boundaries, testing interruption handling.

## Step 7: Add Self-Play with TTS

For fully automated testing, use self-play mode where an LLM generates user responses converted to audio via TTS:

```yaml
# scenarios/selfplay-duplex.scenario.yaml
apiVersion: promptkit.altairalabs.ai/v1alpha1
kind: Scenario
metadata:
  name: selfplay-duplex

spec:
  id: selfplay-duplex
  task_type: voice-assistant
  description: "Automated voice testing with TTS"

  duplex:
    timeout: "5m"
    turn_detection:
      mode: vad
      vad:
        silence_threshold_ms: 1200  # Longer for TTS pauses
        min_speech_ms: 800
    resilience:
      max_retries: 2
      partial_success_min_turns: 2
      ignore_last_turn_session_end: true

  streaming: true

  turns:
    # Initial audio greeting
    - role: user
      parts:
        - type: audio
          media:
            file_path: audio/greeting.pcm
            mime_type: audio/L16
      assertions:
        - type: content_matches
          params:
            pattern: "(?i)(help|assist)"

    # Self-play: LLM generates questions, TTS converts to audio
    - role: selfplay-user
      persona: curious-customer
      turns: 3  # Generate 3 follow-up turns
      tts:
        provider: openai
        voice: alloy
      assertions:
        - type: content_matches
          params:
            pattern: ".{10,}"

  context:
    goal: "Test multi-turn voice conversation"
    user_type: "potential customer"
```

Create the persona:

```yaml
# prompts/personas/curious-customer.persona.yaml
apiVersion: promptkit.altairalabs.ai/v1alpha1
kind: Persona
metadata:
  name: curious-customer

spec:
  id: curious-customer
  description: "A curious customer asking follow-up questions"

  system_prompt: |
    You are a curious customer exploring a product or service.
    Ask natural follow-up questions based on the assistant's responses.
    Keep questions brief and conversational.
```

Configure the TTS provider:

```yaml
# providers/openai-tts.provider.yaml
apiVersion: promptkit.altairalabs.ai/v1alpha1
kind: Provider
metadata:
  name: openai-tts

spec:
  id: openai
  type: openai
  api_key_env: OPENAI_API_KEY
  model: gpt-4o-mini
```

Run the self-play test:

```bash
export OPENAI_API_KEY="your-openai-key"
promptarena run --scenario selfplay-duplex --provider gemini-live
```

## Session Resilience Configuration

Voice sessions can be interrupted by network issues or provider limits. Configure resilience:

```yaml
duplex:
  resilience:
    # Retry failed conversations
    max_retries: 2
    retry_delay_ms: 2000

    # Delay between turns
    inter_turn_delay_ms: 500
    selfplay_inter_turn_delay_ms: 1000

    # Accept partial success
    partial_success_min_turns: 2

    # Don't fail on final turn session end
    ignore_last_turn_session_end: true
```

## Assertions for Voice Testing

Common assertions for duplex tests:

```yaml
assertions:
  # Content pattern matching
  - type: content_matches
    params:
      pattern: "(?i)(hello|greeting)"

  # Must include certain phrases
  - type: content_includes
    params:
      patterns:
        - "welcome"
        - "help"

  # Response length check
  - type: content_matches
    params:
      pattern: ".{20,}"  # At least 20 characters

  # Sentiment analysis
  - type: sentiment
    params:
      expected: positive
```

## Debugging Duplex Tests

### Enable Verbose Logging

```bash
promptarena run --scenario basic-duplex --provider gemini-live --verbose
```

### Check Audio Format

Ensure your audio files are correct:

```bash
# Check file info
ffprobe audio/greeting.pcm

# Play back (requires sox)
play -r 16000 -b 16 -c 1 -e signed-integer audio/greeting.pcm
```

### Common Issues

| Issue | Solution |
|-------|----------|
| "Session ended early" | Increase `partial_success_min_turns` |
| "Empty response" | Check audio quality, increase `silence_threshold_ms` |
| "Turn interrupted" | Increase `inter_turn_delay_ms` |
| TTS pauses causing issues | Increase `silence_threshold_ms` to 1200ms+ |

## Complete Example Project

```
duplex-test/
├── arena.yaml
├── audio/
│   ├── greeting.pcm
│   └── question.pcm
├── prompts/
│   ├── voice-assistant.prompt.yaml
│   └── personas/
│       └── curious-customer.persona.yaml
├── providers/
│   ├── gemini-live.provider.yaml
│   └── openai-tts.provider.yaml
└── scenarios/
    ├── basic-duplex.scenario.yaml
    └── selfplay-duplex.scenario.yaml
```

## Next Steps

- [Multi-Provider Testing](02-multi-provider) - Test across providers
- [CI/CD Integration](05-ci-integration) - Automate voice tests
- [Duplex Reference](../reference/duplex-config) - Full configuration options

## See Also

- [Arena CLI Reference](../reference/cli-commands) - Command options
- [Assertions Reference](../reference/assertions) - All assertion types
- [Validators Reference](../reference/validators) - Output validation
