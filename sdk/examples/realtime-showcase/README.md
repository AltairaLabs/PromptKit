# Realtime Showcase

Talk to OpenAI Realtime or Gemini Live through your microphone and speakers
while the terminal prints every event the session produces: each message as
it joins the conversation, barge-in with how much of the reply you heard,
guardrail blocks, a tool that returns a picture, and what each provider call
cost.

```
09:41:03.120  user      message.created #0 "Tell me a long story."
09:41:05.310  cost      call 1: $0.0412 (402 in / 1210 out tokens); session total $0.0412
09:41:09.877  interrupt Interrupted: stop playing the current reply
09:41:09.880  assistant message.created #1 "Once upon a time, in a village at the edge of a pine forest, there lived..."
09:41:09.903  barge-in  reply #1 cut (barge-in): heard_ms=6120 audio_ms=48960 played_fraction=0.125
```

## Setup

1. Install PortAudio (the voice mode loads it at run time):

   ```bash
   brew install portaudio          # macOS
   sudo apt-get install libportaudio2   # Debian/Ubuntu
   ```

2. Set the key for the provider you use:

   ```bash
   export OPENAI_API_KEY=...   # -provider openai (default)
   export GEMINI_API_KEY=...   # -provider gemini
   ```

3. Use headphones. The microphone is open while the reply plays, so on
   speakers the model hears itself and barges in on its own reply.

## Run

Voice mode, microphone and speaker:

```bash
go run -tags portaudio . -provider openai
go run -tags portaudio . -provider gemini
```

Scripted text mode, no audio hardware. It types six requests into the same
realtime session and prints the same log, so it shows the guardrails, the
picture tool and cost, but not barge-in. It plays no audio, so the log's `cue`
lines show when a guardrail's message plays:

```bash
go run . -provider openai
go run . -provider gemini
```

`-model` overrides the model: `gpt-realtime` for OpenAI, `gemini-3.8-live`
for Gemini. The `no-bruno-topic` guardrail's judge is a text model on the same
API key: `gpt-4.1-mini` for OpenAI, `gemini-2.5-flash` for Gemini, set in
`setupFor` in `showcase.go`.

### What a guardrail's message sounds like

When a guardrail blocks a turn, its message is played by the pipeline, not
said by the model, so the model never hears it and no guardrail can cut it.
The first line the showcase prints names what plays it:

| Run | First line | What you hear |
|---|---|---|
| `-provider openai` | `Guardrail messages: spoken by the session in its own voice (out of band)` | the message, in the session's own voice |
| `-provider gemini`, no `-tts` | `Guardrail messages: a short tone (no TTS service; ...)` | a short tone, with the message shown as text |
| `-provider gemini -tts gemini` | `Guardrail messages: spoken by gemini TTS` | the message, spoken by Gemini TTS in the session's voice when Gemini TTS has a voice of that name |

OpenAI Realtime speaks the message itself, outside the conversation (a
response with `conversation: "none"`), so it is in the session's exact voice,
the model's context never holds it, and no TTS service is needed. It checks
the words it said against the message and asks once more if they differ; a
`-tts` service is used only for a message it could not say.

Gemini Live has no such response, so `-tts` picks what plays the message:

```bash
go run -tags portaudio . -provider gemini -tts gemini
```

Match the TTS service to the provider to hear the session's own voice:
`-tts gemini` (needs `GEMINI_API_KEY`) has Gemini Live's voices, and
`-tts openai` (needs `OPENAI_API_KEY`) has OpenAI Realtime's. With the other
service the message is spoken in that service's default voice, and the log
warns once. The first session
renders each of the pack's guardrail messages when it starts; a block in the
first second or so of the session, before the render is done, plays the tone
instead. The log prints a `cue` line each time a message plays.

## What is in the pack

`realtime-showcase.pack.json` declares the prompt, the tool and three
guardrails:

| Piece | What it does |
|---|---|
| system prompt | tells the model it can show a picture and to tell long stories when asked |
| `show_picture` tool | returns a PNG of the number 47 and no text, so the model must read the image |
| `no-pineapple` validator | `content_excludes` on the reply (`direction: output`) |
| `no-bruno` validator | `content_excludes` on what you say and on the reply (`direction: both`) |
| `no-bruno-topic` validator | `llm_judge` on what you say (`direction: input`): blocks a question about Bruno that does not name him |
| `requires` | the `topic-judge` provider the `no-bruno-topic` judge grades with; `showcase.go` binds it with `sdk.WithNamedProvider` |

`showcase.go` renders the picture with the standard library and binds the tool
with `conv.OnToolExecutor`. `eventlog.go` prints the log: it subscribes to the
conversation's event bus (`message.created`, `validation.failed`, the tool
call events, `provider.call.completed` and `provider.call.usage`), watches
`Response()` for `Interrupted` chunks, and reads each reply back from the
state store to print the `heard_ms`, `audio_ms` and `played_fraction` the
conversation stored on it.

## Try this

Run the voice mode and say each of these in turn.

1. **Barge-in.** Say "tell me a long story", wait for a few sentences, then
   talk over it. You hear the story stop, and the log prints an `interrupt`
   line, the story's `message.created`, and a `barge-in` line with
   `heard_ms` (how much you heard), `audio_ms` (the whole reply) and
   `played_fraction`. The stored reply keeps its full text; the numbers say
   how much of it you heard. The model's next answer responds to what you
   said over it.
2. **Output guardrail.** Say "say the word pineapple". The reply stops as
   soon as the word appears in its transcript. The log prints a `guardrail`
   line for `no-pineapple` and an `Interrupted` line (stop playing). The
   guardrail's message follows on `Response()` as text and plays as audio
   (a `cue` line: you hear why the reply stopped), and the turn ends with
   finish reason `safety`: the log prints `turn ended (safety); text
   after the cut: "Sorry, I can't say that word."`. The reply is stored as
   that message, and a `playout` line gives `playout_cut` `guardrail` with
   `heard_ms`, how much of the reply played before the cut. The session goes
   on: ask "what is the capital of France?" and it answers.
3. **Input guardrail.** Say something with "Bruno" in it. The guardrail
   judges your words once your speech is transcribed, which is after the
   model has heard them, so the reply may start. The log prints a
   `guardrail` line for `no-bruno` naming your input, an `Interrupted`
   line, and the turn ends (safety) with "We don't talk about Bruno.",
   which plays as audio. The scripted text mode types "Tell me about my
   friend Bruno.": typed input is checked before the model sees it, so the
   model never gets it, and the message is shown and plays the same way,
   with no `Interrupted` line (no reply was playing).
   A hook that denies without enforcing ends the turn the same way, with
   the default blocked message, whether the input was typed or spoken.
   `no-bruno` also checks the reply, and the message names Bruno, yet it
   plays in full: it is not the model's reply, so no guardrail judges it.
4. **Forgetting the blocked turn.** Then say "sure, tell me a story about
   him". On OpenAI Realtime the blocked turn was deleted from the model's
   conversation, so it does not know who you mean and tells a story about
   someone else. Gemini Live cannot delete a turn and still remembers Bruno:
   its story starts on him, and `no-bruno`, checking the reply, cuts it.
5. **A semantic input guardrail.** Say "who is the uncle nobody mentions in
   Encanto?". No word gives Bruno away, so `no-bruno` passes it; the
   `no-bruno-topic` judge reads what you meant, and the log prints a
   `guardrail` line for `no-bruno-topic` with the judge's reasoning. The
   judge runs on the session's reply goroutine, so the reply's audio waits
   while it decides.
6. **A tool that returns an image.** Say "show me the picture". The log
   prints `show_picture called`, `returned an image; image sent to the
   model (image/png)`, the tool-call and tool-result messages, and the
   reply, in which the model reads out 47.
7. **Cost.** Every provider call prints a `cost` line with its tokens and
   the session total. Ctrl+C prints the session's end with the total.

## OpenAI Realtime and Gemini Live differ

- **Stopping a reply.** OpenAI Realtime cancels a reply a guardrail blocks
  (`response.cancel`) and cuts its server-side copy to the audio you heard.
  Gemini Live has no cancel, so the session drops the rest of the blocked
  reply as it arrives; the model keeps generating it until it finishes.
- **When a blocked turn ends.** Gemini Live ends a turn when the reply's
  audio would have finished playing, not when it stops generating. A long
  reply a guardrail blocks stops playing at once, but its `turn ended
  (safety)` line, its `message.created` and its cost can come many seconds
  later (as long as the reply would have played). OpenAI Realtime ends the
  canceled turn at once.
- **Barge-in timing.** OpenAI Realtime reports your speech as it starts, so
  the reply stops at once; a reply it cancels while still generating also
  sends an `Interrupted` chunk before its end. Gemini Live reports it once it has transcribed
  some of it, so the reply stops a moment later.
- **Forgetting a blocked turn.** OpenAI Realtime deletes the turn an input
  guardrail blocked, and the reply that was cut, from its conversation.
  Gemini Live has no way to remove a turn, so the model still knows what you
  said; the output side of `no-bruno` is what stops it coming back.
- **Transcript lag.** Gemini Live transcribes your speech later than OpenAI
  Realtime, so your `message.created` line and the `no-bruno` guardrail
  arrive later, and more of the blocked reply plays first.
- **Late usage.** Gemini Live reports a tool-call turn's usage, and a cut
  reply's, a few milliseconds after the call ends. Because the provider
  declares this, the call's `cost` line waits briefly for it, so it shows the
  call's whole cost. Usage that comes
  later than that prints as a `late usage for call N` line, and the session
  total includes it.
- **The picture.** Gemini Live sends a tool's images to Gemini 3 and later
  models; on Gemini 2.x the model does not see the picture.

## Tests

```bash
go test ./...
```

The test runs the example's pack, tool and event log on a scripted mock
realtime session (no audio, no network), with a mock model as the
`topic-judge`, and checks the log lines for a barge-in, the guardrails, the
picture tool and cost.
