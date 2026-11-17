---
title: Handle turns, barge-in and transcripts
description: Stream live audio into a duplex voice session, tune when a turn ends, flush playback on barge-in, and read transcripts and turn boundaries.
sidebar:
  order: 2
verified:
  commit: 885cafa787a84bfba66f158fe51cda15a077ec23
  sources:
    - runtime/audio/silence.go
    - runtime/pipeline/stage/playout_hold.go
    - runtime/pipeline/stage/stages_speech.go
    - sdk/conversation.go
    - sdk/options.go
    - sdk/session/duplex_session.go
---
Stream live microphone audio through a duplex voice session and handle what comes back: turn ends, interruptions and transcripts.

## Prerequisites

- A duplex conversation opened in VAD or ASM mode. See [Choose a voice mode](/sdk/how-to/voice/choose-voice-mode/).
- Audio capture and playback code that produces and plays 16-bit mono PCM at the session's sample rate.

## Step 1: Send and receive audio

Wrap each block of captured PCM in a `StreamChunk` and pass it to `SendChunk`. Read the reply audio from the channel `conv.Response()` returns. Drain that channel for the life of the session: an unread channel back-pressures the pipeline.

```go
responses, err := conv.Response()
if err != nil {
	log.Fatal(err)
}

// Send audio chunks from the microphone.
go func() {
	for ctx.Err() == nil {
		pcm := readFromMicrophone() // your audio capture code
		err := conv.SendChunk(ctx, &providers.StreamChunk{
			MediaData: &providers.StreamMediaData{
				MIMEType: "audio/pcm",
				Data:     pcm,
			},
		})
		if err != nil {
			log.Printf("send error: %v", err)
			return
		}
	}
}()

// Play audio output.
for chunk := range responses {
	if chunk.Error != nil {
		log.Printf("audio error: %v", chunk.Error)
		continue
	}
	if chunk.MediaData != nil {
		playAudio(chunk.MediaData.Data) // your audio playback code
	}
}
```

## Step 2: Tune when a turn ends

In VAD mode, once a turn holds at least `MinSpeechDuration` of speech, it ends when any of these holds, measured in audio time (samples received):

- `SilenceDuration` of quiet has passed.
- The turn reaches `MaxTurnDuration`.
- A turn detector passed with `WithTurnDetector` reports that the speaker is not speaking.

Set the durations on `VADModeConfig`, or add a detector:

```go
detector := audio.NewSilenceDetector(500 * time.Millisecond)

conv, err := sdk.OpenDuplex("./voice-assistant.pack.json", "assistant",
	sdk.WithTurnDetector(detector),
	sdk.WithVADMode(sttService, ttsService, nil),
)
if err != nil {
	log.Fatal(err)
}
```

`WithVAD` replaces the voice activity detector that decides what counts as speech.

## Step 3: Flush playback on barge-in

In VAD mode, speech counts as barge-in from the moment the user's turn ends until
the reply's audio would have finished playing. That covers the model generating
the reply, synthesis, and playback, where the session assumes your player starts
each audio chunk as it arrives. The pipeline cancels whatever is still in flight
and drops the reply's remaining audio. Audio already delivered is in your
player's buffer, so the session tells you to flush it: a chunk on `Response()`
arrives with `Interrupted` set.

In ASM mode the same window applies. OpenAI Realtime and Gemini Live send a
reply's audio faster than it plays, so most of a long reply is still playing
after the model has finished it. The session estimates the playout the same
way, and speech the provider hears before it ends is barge-in, with the same
`Interrupted` chunk. OpenAI Realtime reports speech as it starts, and the
session also cuts the model's copy of the reply to the audio the caller heard.
Gemini Live reports nothing until it has transcribed some of the speech, so its
`Interrupted` chunk arrives a moment later. A reply that OpenAI Realtime cuts
short while still generating it ends with `finish_reason` `interrupted`, and an
`Interrupted` chunk arrives right before that end.

The speech that interrupted the reply is the user's next turn, and it is
transcribed in full. Speech that starts after the reply has finished playing is
an ordinary turn, with no `Interrupted` chunk.

In ASM mode, when the reply audio is played to a listener in real time, the
conversation's transcript records each spoken reply as the caller heard it.
That is the case when the conversation drives a speaker itself (`OpenVoice`,
`WithAudioSession`), or when your code plays the audio from `Response()` as it
arrives and opens the conversation with `sdk.WithPlayoutTracking()`. A reply
with audio then reaches the state store, the recording and `message.created`
once its audio has played; everything after it waits behind it, so the order
holds. The chunks on `Response()` are not delayed.

If the caller barges in, or the session ends while the reply still plays, the
stored reply keeps its full text and its `FinishReason`, so it stays in model
context with any tool calls it made. A reply a guardrail blocks is stored as the
guardrail's message, marked `safety`. Its `Meta` gains:

| Key | Value |
|---|---|
| `heard_ms` | how much of the reply's audio had played, in milliseconds |
| `audio_ms` | the reply's audio length, in milliseconds |
| `played_fraction` | `heard_ms` over `audio_ms`, from 0 to 1 |
| `playout_cut` | `barge-in`, `guardrail`, or `session-closed` |

The text is not cut to the words heard: neither provider's transcript of the
reply audio carries timing, so it cannot be aligned with the audio. A reply
without audio is stored as soon as it completes. Without a listener (a
headless or test consumer that does not play the audio) nothing is held, and
replies are stored as soon as they complete.

```go
for chunk := range responses {
	if chunk.Interrupted {
		player.Flush() // stop playing the reply the user talked over
		continue
	}
	if chunk.MediaData != nil {
		player.Write(chunk.MediaData.Data)
	}
}
```

:::caution
Barge-in during playback means the microphone must not pick up the reply itself. Use headphones, or capture audio with echo cancellation (browsers and phones provide it).
:::

## Step 4: Read transcripts and turn boundaries

VAD and ASM mode mark turns on `Response()` the same way, through chunk metadata:

- `Metadata["input_transcription"]` (a string) carries what the user said, once per user turn. In VAD mode it is the STT stage's transcript.
- `Delta` carries the reply's text as the provider produces it: the transcript of a spoken reply, or the text of a text-only reply (OpenAI Realtime with `modalities` `["text"]`). A turn's deltas add up to the reply the conversation records.
- `Metadata["assistant_turn_complete"]` (a bool) marks the end of each assistant turn. In VAD mode that includes a reply cut short by barge-in, whose boundary arrives before its `Interrupted` chunk. A reply interrupted before the model produced any of it has no assistant turn to close, so only the `Interrupted` chunk arrives.
- `Metadata["finish_reason"]` (a string), on the same chunk, says why the assistant turn ended, in the same values as `Message.FinishReason` on a text turn: `stop`, `max_output_tokens`, `safety`, `interrupted` and so on. It is absent when the provider reported no reason.

None of these sets `FinishReason`, so a loop over `Response()` keeps reading across turns.

```go
for chunk := range responses {
	if t, ok := chunk.Metadata["input_transcription"].(string); ok {
		fmt.Println("you:", t)
	}
	if chunk.Delta != "" {
		fmt.Print(chunk.Delta)
	}
	if done, _ := chunk.Metadata["assistant_turn_complete"].(bool); done {
		fmt.Println()
	}
}
```

## Verify

Speak, pause, and speak again over the reply. The loop prints a `you:` line per turn, the reply text, and a line break at each turn end, and your player flushes when you talk over a reply.

## Related

- [Choose a voice mode](/sdk/how-to/voice/choose-voice-mode/)
- [Tutorial 7: Audio sessions](/sdk/tutorials/07-audio-sessions/)
- The [voice interview example](https://github.com/AltairaLabs/PromptKit/tree/main/sdk/examples/voice-interview) captures from a microphone and handles barge-in.
- [Audio package reference](/runtime/reference/audio/)
