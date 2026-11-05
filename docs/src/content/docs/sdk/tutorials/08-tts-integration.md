---
title: "Tutorial 8: TTS integration"
sidebar:
  order: 8
verified:
  commit: 885cafa787a84bfba66f158fe51cda15a077ec23
  sources:
    - runtime/tts/openai.go
    - runtime/tts/service.go
    - sdk/conversation.go
    - sdk/options.go
    - sdk/response.go
---
## What we will build

In this tutorial we build programs that speak. We synthesize a sentence to an MP3 file with OpenAI text-to-speech, pick a voice, speak a model's reply from a text conversation, and give the voice session from Tutorial 7 a different voice.

## Prerequisites

- Completed [Tutorial 7: Audio sessions](/sdk/tutorials/07-audio-sessions/), with its module, `voice-assistant.pack.json` and `voice/main.go`
- `OPENAI_API_KEY` set in the environment
- `ffplay` (part of FFmpeg) to play the audio

## Step 1: Synthesize a sentence

Create `speak/main.go`:

```go
package main

import (
	"context"
	"io"
	"log"
	"os"

	"github.com/AltairaLabs/PromptKit/runtime/v2/tts"
)

func main() {
	speaker := tts.NewOpenAI(os.Getenv("OPENAI_API_KEY"))

	cfg := tts.SynthesisConfig{
		Voice:  "alloy",
		Format: tts.FormatMP3,
		Speed:  1.0,
	}

	audio, err := speaker.Synthesize(context.Background(), "Hello, how can I help you?", cfg)
	if err != nil {
		log.Fatal(err)
	}
	defer audio.Close()

	mp3, err := io.ReadAll(audio)
	if err != nil {
		log.Fatal(err)
	}
	if err := os.WriteFile("greeting.mp3", mp3, 0o644); err != nil {
		log.Fatal(err)
	}
}
```

`tts.NewOpenAI` makes no network call. The request goes out when `Synthesize` runs, and the returned reader streams the audio.

Run it and play the file:

```bash
go run ./speak
ffplay -autoexit greeting.mp3
```

`greeting.mp3` holds the sentence, spoken in the `alloy` voice.

## Step 2: List the voices

Create `voices/main.go`:

```go
package main

import (
	"fmt"
	"os"

	"github.com/AltairaLabs/PromptKit/runtime/v2/tts"
)

func main() {
	speaker := tts.NewOpenAI(os.Getenv("OPENAI_API_KEY"))

	for _, voice := range speaker.SupportedVoices() {
		fmt.Printf("%s: %s (%s, %s)\n", voice.ID, voice.Name, voice.Language, voice.Gender)
	}
}
```

Run it:

```bash
go run ./voices
```

The program prints the six OpenAI voices:

```text
alloy: Alloy (en, neutral)
echo: Echo (en, male)
fable: Fable (en, female)
onyx: Onyx (en, male)
nova: Nova (en, female)
shimmer: Shimmer (en, female)
```

## Step 3: Change the voice, speed and model

In `speak/main.go`, change the config to use the `nova` voice, a slightly faster rate and the higher-quality model:

```go
	cfg := tts.SynthesisConfig{
		Voice:  "nova",
		Format: tts.FormatMP3,
		Speed:  1.1,
		Model:  tts.ModelTTS1HD,
	}
```

Run it and play the file again:

```bash
go run ./speak
ffplay -autoexit greeting.mp3
```

The same sentence now plays in the `nova` voice, a little faster. Without `Model`, the service uses `tts-1`.

## Step 4: Speak a model's reply

Create `reply/main.go`. It opens a text conversation on the Tutorial 7 pack, sends a message, and synthesizes the reply:

```go
package main

import (
	"context"
	"fmt"
	"io"
	"log"
	"os"

	"github.com/AltairaLabs/PromptKit/runtime/v2/tts"
	"github.com/AltairaLabs/PromptKit/sdk/v2"
)

func main() {
	ctx := context.Background()

	conv, err := sdk.Open("./voice-assistant.pack.json", "assistant")
	if err != nil {
		log.Fatal(err)
	}
	defer conv.Close()

	conv.SetVar("user_name", "Alice")

	resp, err := conv.Send(ctx, "Tell me a joke")
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println(resp.Text())

	speaker := tts.NewOpenAI(os.Getenv("OPENAI_API_KEY"))
	audio, err := speaker.Synthesize(ctx, resp.Text(), tts.SynthesisConfig{Voice: "nova", Format: tts.FormatMP3})
	if err != nil {
		log.Fatal(err)
	}
	defer audio.Close()

	mp3, err := io.ReadAll(audio)
	if err != nil {
		log.Fatal(err)
	}
	if err := os.WriteFile("joke.mp3", mp3, 0o644); err != nil {
		log.Fatal(err)
	}
}
```

Run it and play the joke:

```bash
go run ./reply
ffplay -autoexit joke.mp3
```

The program prints the joke, which differs from run to run, and `joke.mp3` speaks it in the `nova` voice.

## Step 5: Give the voice session a voice

A VAD voice session synthesizes its replies with the same TTS service. Its voice and speed come from `VADModeConfig`. In `voice/main.go` from Tutorial 7, set them after the sample rate:

```go
	vadCfg := sdk.DefaultVADModeConfig()
	vadCfg.SampleRate = sampleRate
	vadCfg.Voice = "nova"
	vadCfg.Speed = 1.1
```

Run the session and play its reply:

```bash
go run ./voice
ffplay -autoexit -f s16le -ar 24000 reply.pcm
```

The reply to the spoken question now plays in the `nova` voice instead of `alloy`.

## Next steps

- [Configure text-to-speech](/sdk/how-to/voice/configure-tts/) covers the ElevenLabs and Cartesia services, streaming synthesis, formats, error handling and caching.
- [Tutorial 9: Variable providers](/sdk/tutorials/09-variable-providers/) injects dynamic context into prompts.
- The [`tts` package reference](/runtime/reference/tts/) lists every type and function.
