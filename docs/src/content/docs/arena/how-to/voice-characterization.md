---
title: Test expressive voice personas with characterization tags
description: Drive TTS providers with bracket-tagged expressive markup. Verify persona consistency across turns without coupling persona prompts to a specific provider's dialect.
---

This how-to walks through the expressive-persona path in `examples/voice-refund-demo/`. The TTS characterization tag dialect lets a persona emit `[shouts]`, `[sighs]`, `[whispers]`, etc.; each provider adapter lowers the tags into its native API (ElevenLabs v3 native, OpenAI `gpt-4o-mini-tts` instructions field, Cartesia emotion array, SSML for Google/Azure). One persona, every provider, no per-provider prompt forks.

## What it proves

Voice agents that all sound the same are uncanny. Voice tests that ignore expressiveness miss real failures: an agent stays calm when the situation calls for empathy, sounds the same across happy and aggressive callers, never modulates. Provider APIs already expose per-utterance expressiveness — but each in its own dialect, which makes persona prompts non-portable.

PromptArena's bracket-tag dialect collapses that:

- Personas emit canonical `[tag]` markup (`[shouts]`, `[laughs]`, `[pause:500ms]`, etc.).
- Each TTS provider adapter parses the tags and lowers them into its native format — pass-through for ElevenLabs v3, `instructions:` for `gpt-4o-mini-tts`, `emotion[]` for Cartesia, SSML for Google/Azure.
- Persona prompts stay provider-agnostic; the same persona drives every supported provider with identical bracket markup.

## Opt in via the persona

In `examples/voice-refund-demo/personas/aggressive-entitled.persona.yaml`:

```yaml
spec:
  id: aggressive-entitled
  voice: confident-man
  style:
    verbosity: short
    formality: casual
    challenge_level: high
    expressive: true   # ← teaches the persona to emit bracket tags
```

When `expressive: true` is set, the runtime prepends a short rubric to the persona's system prompt explaining the tag dialect. The LLM then emits text like:

```
[furious] I don't care about your warranty policy! [shouts] I want my money back!
```

The TTS adapter parses the bracket spans, lowers them into its native dialect, and synthesises audio. The strict-only-text content (no brackets) reaches downstream eval handlers — `content_matches`, `content_includes` etc. operate on the plain text.

## Run it

```bash
cd examples/voice-refund-demo
promptarena serve
```

Pick the `aggressive-refund` scenario. The persona's audio output exercises the characterization path — the agent under test (Gemini Live, OpenAI Realtime) hears an expressive caller and replies in voice.

Headless / CI:

```bash
promptarena run --scenario aggressive-refund --formats html,json
```

The HTML report shows each persona-side turn with the parsed tags adjacent to the spoken text, so you can see which expressive cues the LLM emitted.

## Persona-side authoring

Two patterns are useful:

**Per-trait personas**: each persona sticks to a narrow emotional band — `aggressive-entitled` shouts and demands; `anxious-recipient` worries and apologises. The expressive rubric encourages the LLM to keep within that band.

**Mood-shifting personas**: a single persona drifts across the call (calm → agitated → conciliatory). The system prompt includes a per-turn guidance: "turn 1: open friendly; turn 2: get frustrated; turn 3: escalate". The LLM emits different tags per turn; the same agent under test has to handle the modulation.

## Asserting persona consistency

The interesting tests are *cross-turn*: does the agent stay in role as the caller modulates? Two patterns:

- `llm_judge_session` with criteria about consistency: "did the agent maintain a professional tone across all turns?"
- `content_includes_any` / `content_excludes` per turn checking for explicit calmness-markers when the persona escalates.

```yaml
conversation_assertions:
  - type: assertion
    params:
      eval_type: llm_judge_session
      eval_params:
        criteria: |
          Did the agent maintain a calm, professional tone across all turns,
          even when the caller escalated? Score 1.0 if yes, 0.0 if the agent
          matched the caller's tone instead of staying composed.
        judge: default
      min_score: 0.8
```

## What's currently limited

`expressive: true` flows persona-side tags through to TTS. The reverse direction — asserting on the *agent's* audio output for warmth / professionalism / emotional appropriateness — needs richer audio-side metric capture. Currently the only audio-side assertions are structural (`audio_duration`, `audio_format`); content-of-audio assertions go through the LLM-judged path operating on the text representation of the agent's speech.

The reference list of supported tags per provider lives in `runtime/tts/markup/` — see the package doc for the canonical dialect and what each TTS adapter recognizes.

## Switching providers

Persona prompts are provider-agnostic. To run the same expressive persona through a different TTS:

```yaml
# scenarios/aggressive-refund.scenario.yaml — change the persona's voice...
turns:
  - role: selfplay-user
    persona: aggressive-entitled
    tts:
      provider: cartesia    # was: elevenlabs / openai
      voice: cartesia-confident-man
```

The persona's bracket tags reach the new provider; the adapter lowers them into its native format. No persona prompt changes needed.

## CI gate

The expressive path needs real provider keys (mock TTS strips tags). For CI, validate the configs without making provider calls:

```yaml
- name: Validate expressive scenarios
  working-directory: examples/voice-refund-demo
  run: ../../bin/promptarena validate config.arena.yaml
```

For real-provider gated runs, follow the standard pattern from the [voice customer support how-to](/arena/how-to/voice-customer-support/) — secret-gated `head.repo.full_name == github.repository` job, provider keys in `secrets:`.
