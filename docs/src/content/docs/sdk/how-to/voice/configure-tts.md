---
title: Configure text-to-speech
description: Pick a TTS service (OpenAI, ElevenLabs or Cartesia), set the voice and format, stream synthesis, handle errors and cache audio.
sidebar:
  order: 3
verified:
  commit: 8489bf3d95c7498f9de0f024769db9399c24e884
  sources:
    - runtime/audio/chunk.go
    - runtime/tts/cartesia.go
    - runtime/tts/cartesia_interactive.go
    - runtime/tts/elevenlabs.go
    - runtime/tts/errors.go
    - runtime/tts/http_helpers.go
    - runtime/tts/openai.go
    - runtime/tts/service.go
---
Set up a text-to-speech service for a voice session or for your own synthesis calls.

## Prerequisites

- An API key for the TTS service: `OPENAI_API_KEY`, `ELEVENLABS_API_KEY` or `CARTESIA_API_KEY`.
- The `github.com/AltairaLabs/PromptKit/runtime/v2/tts` package.

To declare the service in configuration instead of code, see [Declarative TTS and STT providers](/sdk/how-to/providers/declarative-tts-stt-providers/).

## Step 1: Create the service

Every constructor takes the API key and returns a `tts.Service`, whose methods are `Name`, `Synthesize`, `SupportedVoices` and `SupportedFormats`:

```go
openaiTTS := tts.NewOpenAI(os.Getenv("OPENAI_API_KEY"))
elevenLabsTTS := tts.NewElevenLabs(os.Getenv("ELEVENLABS_API_KEY"))
cartesiaTTS := tts.NewCartesia(os.Getenv("CARTESIA_API_KEY"))
```

No constructor makes a network call.

## Step 2: Set the voice, format and model

Pass a `tts.SynthesisConfig` to `Synthesize`:

```go
config := tts.SynthesisConfig{
	Voice:    "nova",          // voice ID; empty uses the service's default voice
	Format:   tts.FormatMP3,   // output format
	Speed:    1.0,             // speech rate (0.25-4.0)
	Language: "en-US",         // language code
	Model:    tts.ModelTTS1HD, // model (provider-specific)
}
```

- Voice IDs differ per service. `SupportedVoices()` lists them.
- Each service supports a subset of the formats `FormatMP3`, `FormatOpus`, `FormatAAC`, `FormatFLAC`, `FormatPCM16` and `FormatWAV`. `SupportedFormats()` lists them with their sample rates. OpenAI supports all six, ElevenLabs MP3 and PCM, and Cartesia PCM, MP3 and WAV.
- `SynthesisConfig.Pitch` has no effect on the built-in services. The OpenAI, ElevenLabs and Cartesia speech APIs take no pitch parameter.
- The OpenAI models are `tts.ModelTTS1` (the default), `tts.ModelTTS1HD` and `tts.ModelGPT4oMiniTTS`.

For a VAD voice session, set the voice and speed on `sdk.VADModeConfig` instead. See [Choose a voice mode](/sdk/how-to/voice/choose-voice-mode/).

## Step 3: Stream synthesis

A service that implements `tts.StreamingService` returns audio in chunks as it is generated. Cartesia implements it:

```go
var service tts.Service = tts.NewCartesia(os.Getenv("CARTESIA_API_KEY"))

streaming, ok := service.(tts.StreamingService)
if !ok {
	log.Fatal("service does not support streaming")
}

chunks, err := streaming.SynthesizeStream(ctx, "Hello, how can I help you?", tts.SynthesisConfig{Format: tts.FormatPCM16})
if err != nil {
	log.Fatal(err)
}
for chunk := range chunks {
	if chunk.Error != nil {
		log.Printf("synthesis error: %v", chunk.Error)
		break
	}
	playAudioChunk(chunk.Data) // your playback code
	if chunk.Final {
		break
	}
}
```

## Step 4: Handle errors

Empty text returns `tts.ErrEmptyText` before any request. A failed request returns a `*tts.SynthesisError`: `Retryable` is true for rate limits, server errors and requests that never got a response, and `errors.Is` matches its cause, such as `tts.ErrInvalidVoice` or `tts.ErrRateLimited`:

```go
reader, err := service.Synthesize(ctx, text, config)
if err != nil {
	var synthErr *tts.SynthesisError
	switch {
	case errors.Is(err, tts.ErrEmptyText):
		log.Print("nothing to say")
	case errors.Is(err, tts.ErrInvalidVoice):
		log.Printf("voice %q not supported", config.Voice)
	case errors.As(err, &synthErr) && synthErr.Retryable:
		log.Printf("transient failure, retry later: %v", err)
	default:
		log.Printf("synthesis failed: %v", err)
	}
	return
}
defer reader.Close()
```

## Step 5: Cache repeated phrases

Synthesis costs a request each time. Cache the audio for phrases you repeat, keyed by everything that changes the audio:

```go
var cache = make(map[string][]byte)

func synthesizeWithCache(ctx context.Context, service tts.Service, text string, config tts.SynthesisConfig) ([]byte, error) {
	key := text + "|" + config.Voice + "|" + config.Format.Name
	if cached, ok := cache[key]; ok {
		return cached, nil
	}

	reader, err := service.Synthesize(ctx, text, config)
	if err != nil {
		return nil, err
	}
	defer reader.Close()

	data, err := io.ReadAll(reader)
	if err != nil {
		return nil, err
	}

	cache[key] = data
	return data, nil
}
```

Call `synthesizeWithCache` at startup for fixed prompts such as greetings, so they play without a request.

## Verify

Write the bytes from `Synthesize` to a file with the format's extension and play it, for example `ffplay -autoexit out.mp3`. For PCM, pass the rate from `SupportedFormats()`: `ffplay -autoexit -f s16le -ar 24000 out.pcm`.

## Related

- [Tutorial 8: TTS integration](/sdk/tutorials/08-tts-integration/)
- [Choose a voice mode](/sdk/how-to/voice/choose-voice-mode/)
- [`tts` package reference](/runtime/reference/tts/)
