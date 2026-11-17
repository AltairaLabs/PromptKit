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

Scripted text mode, no audio hardware. It types four requests into the same
realtime session and prints the same log, so it shows the guardrails, the
picture tool and cost, but not barge-in:

```bash
go run . -provider openai
go run . -provider gemini
```

`-model` overrides the model: `gpt-realtime` for OpenAI, `gemini-3.8-live`
for Gemini.

## What is in the pack

`realtime-showcase.pack.json` declares the prompt, the tool and two
guardrails:

| Piece | What it does |
|---|---|
| system prompt | tells the model it can show a picture and to tell long stories when asked |
| `show_picture` tool | returns a PNG of the number 47 and no text, so the model must read the image |
| `no-pineapple` validator | `content_excludes` on the reply (`direction: output`) |
| `no-banana` validator | `content_excludes` on what you say (`direction: input`) |

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
   guardrail's message follows on `Response()` as text, and the turn ends
   with finish reason `safety`: the log prints `turn ended (safety); text
   after the cut: "Sorry, I can't say that word."`. The reply is stored as
   that message, and a `playout` line gives `playout_cut` `guardrail` with
   `heard_ms`, how much of the reply played before the cut. The session goes
   on: ask "what is the capital of France?" and it answers.
3. **Input guardrail.** Say something with "banana" in it. The guardrail
   judges your words once your speech is transcribed, which is after the
   model has heard them, so the reply may start. The log prints a
   `guardrail` line for `no-banana` naming your input, an `Interrupted`
   line, and the turn ends (safety) with "Let's not talk about bananas."
   A hook that denies without enforcing ends the turn the same way, with
   the default blocked message, whether the input was typed or spoken.
4. **A tool that returns an image.** Say "show me the picture". The log
   prints `show_picture called`, `returned an image; image sent to the
   model (image/png)`, the tool-call and tool-result messages, and the
   reply, in which the model reads out 47.
5. **Cost.** Every provider call prints a `cost` line with its tokens and
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
- **Transcript lag.** Gemini Live transcribes your speech later than OpenAI
  Realtime, so your `message.created` line and the `no-banana` guardrail
  arrive later, and more of the blocked reply plays first.
- **Late usage.** Gemini Live reports a tool-call turn's usage after its tool
  calls. The log prints it as a `late usage for call N` line, and the session
  total includes it.
- **The picture.** Gemini Live sends a tool's images to Gemini 3 and later
  models; on Gemini 2.x the model does not see the picture.

## Tests

```bash
go test ./...
```

The test runs the example's pack, tool and event log on a scripted mock
realtime session (no audio, no network) and checks the log lines for a
barge-in, both guardrails, the picture tool and cost.
