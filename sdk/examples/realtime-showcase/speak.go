package main

// speak.go is how the scripted mode sends its requests and waits for the
// replies. It builds without PortAudio, so the tests drive it. In ASM mode a
// request is typed (SendText). VAD mode takes the caller's audio, not text,
// so each request is synthesized to speech with OpenAI TTS and sent as the
// caller's audio: the session's VAD ends the turn on the silence after it and
// its STT service transcribes it, as it does a microphone's.

import (
	"context"
	"errors"
	"fmt"
	"io"
	"time"

	"github.com/AltairaLabs/PromptKit/runtime/v2/audio"
	"github.com/AltairaLabs/PromptKit/runtime/v2/providers"
	"github.com/AltairaLabs/PromptKit/runtime/v2/tts"
	"github.com/AltairaLabs/PromptKit/sdk/v2"
)

const (
	// callerVoice is the OpenAI TTS voice that speaks the scripted requests:
	// not the default (alloy), so the caller and the assistant sound
	// different.
	callerVoice = "echo"
	// speechChunk is how much audio each SendChunk carries.
	speechChunk = 100 * time.Millisecond
	// leadSilence comes before the speech; trailSilence after it is longer
	// than VAD mode's default silence duration (800 ms), so the turn ends.
	leadSilence  = 300 * time.Millisecond
	trailSilence = 1500 * time.Millisecond
)

// chunkSender is the part of a duplex conversation speech is sent to.
type chunkSender interface {
	SendChunk(ctx context.Context, chunk *providers.StreamChunk) error
}

// synthesizeRequest renders text as 16-bit mono PCM at vadSampleRate.
func synthesizeRequest(ctx context.Context, svc tts.Service, text string) ([]byte, error) {
	format := tts.FormatPCM16
	format.SampleRate = vadSampleRate
	r, err := svc.Synthesize(ctx, text, tts.SynthesisConfig{Voice: callerVoice, Format: format})
	if err != nil {
		return nil, fmt.Errorf("synthesize %q: %w", text, err)
	}
	defer r.Close()
	pcm, err := io.ReadAll(r)
	if err != nil {
		return nil, fmt.Errorf("read the speech for %q: %w", text, err)
	}
	return pcm, nil
}

// sendSpeech sends pcm (16-bit mono at vadSampleRate) as the caller's
// speech: silence, the speech, then silence long enough to end the turn, in
// speechChunk pieces. VAD mode times turns by the speech's own length, so the
// chunks are sent back to back.
func sendSpeech(ctx context.Context, conv chunkSender, pcm []byte) error {
	speech := append(silence(leadSilence), pcm...)
	speech = append(speech, silence(trailSilence)...)
	step := pcmBytes(speechChunk)
	for start := 0; start < len(speech); start += step {
		end := min(start+step, len(speech))
		if err := conv.SendChunk(ctx, &providers.StreamChunk{MediaData: &providers.StreamMediaData{
			MIMEType: "audio/pcm", SampleRate: vadSampleRate, Channels: 1, Data: speech[start:end],
		}}); err != nil {
			return err
		}
	}
	return nil
}

// listen sends d of silence in real time, as an open microphone in a quiet
// room does while the reply plays. A session with no input for its idle
// timeout (30 s) ends, and a long reply plays for longer than that.
func listen(ctx context.Context, conv chunkSender, d time.Duration) error {
	tick := time.NewTicker(speechChunk)
	defer tick.Stop()
	quiet := silence(speechChunk)
	for end := time.Now().Add(d); time.Now().Before(end); <-tick.C {
		if err := conv.SendChunk(ctx, &providers.StreamChunk{MediaData: &providers.StreamMediaData{
			MIMEType: "audio/pcm", SampleRate: vadSampleRate, Channels: 1, Data: quiet,
		}}); err != nil {
			return err
		}
	}
	return nil
}

// silence is d of 16-bit mono silence at vadSampleRate.
func silence(d time.Duration) []byte { return make([]byte, pcmBytes(d)) }

// pcmBytes is the size of d of 16-bit mono PCM at vadSampleRate.
func pcmBytes(d time.Duration) int { return int(d * vadSampleRate / time.Second * 2) }

// turnTimeout bounds how long the script waits for each reply.
const turnTimeout = 90 * time.Second

// send gives the session one scripted request: typed in ASM mode, spoken in
// VAD mode.
func send(setup *providerSetup, conv *sdk.Conversation, elog *eventLog, line string) error {
	ctx := context.Background()
	if setup.mode != modeVAD {
		elog.logf("you type", "%q", line)
		return conv.SendText(ctx, line)
	}
	pcm, err := synthesizeRequest(ctx, setup.callerTTS, line)
	if err != nil {
		return err
	}
	elog.logf("you say", "%q (%.1f s of speech, synthesized by %s TTS)",
		line, audio.PCM16Duration(len(pcm), vadSampleRate, 1).Seconds(), setup.callerTTS.Name())
	return sendSpeech(ctx, conv, pcm)
}

// reply is a finished reply: its finish reason and how long its audio plays.
type reply struct {
	finish string
	audio  time.Duration
}

// readReplies reads Response(), shows each chunk to the log, and reports
// each finished reply on turns. A tool-call turn is not a reply: the model
// answers after the tool's result.
func readReplies(resp <-chan providers.StreamChunk, elog *eventLog, turns chan<- reply) {
	var played time.Duration
	for c := range resp {
		elog.observe(c)
		if m := c.MediaData; m != nil {
			played += audio.PCM16Duration(len(m.Data), m.SampleRate, 1)
		}
		if done, _ := c.Metadata["assistant_turn_complete"].(bool); !done {
			continue
		}
		if fr, _ := c.Metadata["finish_reason"].(string); fr != "tool_use" {
			select {
			case turns <- reply{finish: fr, audio: played}:
			default: // nobody is waiting (the script is done)
			}
			played = 0
		}
	}
	close(turns)
}

// waitReply waits for the next reply to finish.
func waitReply(turns <-chan reply) (reply, error) {
	select {
	case r, ok := <-turns:
		if !ok {
			return reply{}, errors.New("the session ended")
		}
		// Let the reply's message.created and cost lines print before the
		// next request.
		time.Sleep(time.Second)
		return r, nil
	case <-time.After(turnTimeout):
		return reply{}, errors.New("no reply within the timeout")
	}
}
