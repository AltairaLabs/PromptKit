//go:build integration
// +build integration

package openai

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/AltairaLabs/PromptKit/runtime/v2/audio/cueclip"
	"github.com/AltairaLabs/PromptKit/runtime/v2/hooks"
	"github.com/AltairaLabs/PromptKit/runtime/v2/pipeline/stage"
	"github.com/AltairaLabs/PromptKit/runtime/v2/providers"
	"github.com/AltairaLabs/PromptKit/runtime/v2/stt"
	"github.com/AltairaLabs/PromptKit/runtime/v2/types"
)

// liveOOBRenders is how many times the live test renders the message: the
// model's behavior is stochastic, so one render proves little.
const liveOOBRenders = 6

// Live: a guardrail's message rendered out of band with no TTS service is
// the session's own audio, saying the message (checked by its transcript
// and by Whisper); a following reply asked what was just said does not
// quote it, since the message never entered the conversation. The test
// reports the transcript miss rate rather than failing on one miss: the
// cue source retries a miss and then falls to the next source.
func TestRealtimeIntegration_SpeakOutOfBand(t *testing.T) {
	apiKey := os.Getenv("OPENAI_API_KEY")
	if apiKey == "" {
		t.Skip("OPENAI_API_KEY not set")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	session, err := newRealtimeProvider().CreateStreamSession(ctx, &providers.StreamingInputConfig{
		Config: types.StreamingMediaConfig{Type: types.ContentTypeAudio, ChunkSize: 3200, SampleRate: DefaultRealtimeSampleRate,
			Channels: DefaultRealtimeChannels, BitDepth: DefaultRealtimeBitDepth, Encoding: "pcm16"},
		SystemInstruction: "You are a helpful voice assistant. Answer briefly.",
		Metadata:          map[string]interface{}{"modalities": []string{"text", "audio"}},
	})
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	defer session.Close()
	speaker, ok := session.(providers.OutOfBandSpeaker)
	if !ok {
		t.Fatal("the provider's session does not speak out of band")
	}

	// Nothing reaches Response() while the renders run.
	leaked := make(chan providers.StreamChunk, 64)
	stopWatch := make(chan struct{})
	watched := make(chan struct{})
	go func() {
		defer close(watched)
		for {
			select {
			case c, ok := <-session.Response():
				if !ok {
					return
				}
				leaked <- c
			case <-stopWatch:
				return
			}
		}
	}()

	whisper := stt.NewOpenAI(apiKey)
	misses, whisperMisses := 0, 0
	for i := 0; i < liveOOBRenders; i++ {
		start := time.Now()
		sp, err := speaker.SpeakOutOfBand(ctx, brunoLine)
		if err != nil {
			t.Fatalf("render %d: %v", i, err)
		}
		if len(sp.Audio) == 0 || sp.SampleRate != DefaultRealtimeSampleRate {
			t.Fatalf("render %d: %d bytes at %d Hz", i, len(sp.Audio), sp.SampleRate)
		}
		if sp.Cost == nil || sp.Cost.TotalCost <= 0 {
			t.Errorf("render %d: no cost reported: %+v", i, sp.Cost)
		}
		heard, err := whisper.TranscribeBytes(ctx, sp.Audio, stt.TranscriptionConfig{Format: "pcm",
			SampleRate: sp.SampleRate, Channels: 1, BitDepth: 16, Language: "en"})
		if err != nil {
			t.Fatalf("transcribe render %d: %v", i, err)
		}
		match := normalizedEqual(sp.Transcript, brunoLine)
		if !match {
			misses++
		}
		t.Logf("render %d: %v, %.2fs audio, transcript %q (match %v), whisper heard %q, cost $%.5f", i,
			time.Since(start).Round(time.Millisecond), float64(len(sp.Audio))/2/float64(sp.SampleRate),
			sp.Transcript, match, heard, sp.Cost.TotalCost)
		// Whisper is a second, independent ear, and it mishears too ("We
		// don't talk about burnout."): its misses are counted, not failed one
		// by one.
		if !strings.Contains(strings.ToLower(heard), "bruno") {
			whisperMisses++
		}
	}
	t.Logf("transcript miss rate: %d of %d; Whisper miss rate: %d of %d", misses, liveOOBRenders,
		whisperMisses, liveOOBRenders)
	if whisperMisses*2 > liveOOBRenders {
		t.Errorf("Whisper did not hear the message in %d of %d renders", whisperMisses, liveOOBRenders)
	}
	if misses == liveOOBRenders {
		t.Errorf("no render said exactly the message")
	}
	close(stopWatch)
	<-watched
	select {
	case c := <-leaked:
		t.Fatalf("an out-of-band chunk reached Response(): %+v", c)
	default:
	}

	// The message never entered the conversation.
	if err := session.SendText(ctx, "What did you just say? Quote your last words exactly."); err != nil {
		t.Fatalf("send text: %v", err)
	}
	reply, _, finished := collectResponse(t, session, 30*time.Second)
	t.Logf("reply to %q: %q", "what did you just say?", reply)
	if !finished {
		t.Fatal("no reply")
	}
	if strings.Contains(strings.ToLower(reply), "bruno") {
		t.Errorf("the reply quotes the out-of-band message: %q", reply)
	}
}

// Live: on an OpenAI Realtime session with no TTS service configured, a
// guardrail's message cued through the cue stage plays the session's own
// out-of-band render of it (not the earcon), and Whisper hears the message.
func TestRealtimeIntegration_CueStageSpeaksOutOfBand(t *testing.T) {
	apiKey := os.Getenv("OPENAI_API_KEY")
	if apiKey == "" {
		t.Skip("OPENAI_API_KEY not set")
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	duplex := stage.NewDuplexProviderStage(newRealtimeProvider(), &providers.StreamingInputConfig{
		Config: types.StreamingMediaConfig{Type: types.ContentTypeAudio, ChunkSize: 3200, SampleRate: DefaultRealtimeSampleRate,
			Channels: DefaultRealtimeChannels, BitDepth: DefaultRealtimeBitDepth, Encoding: "pcm16"},
		Metadata: map[string]interface{}{"modalities": []string{"text", "audio"}},
	})
	duplex.SetHookRegistry(hooks.NewRegistry(hooks.WithProviderHook(messageHook{brunoLine})))
	cue := stage.NewDuplexCueStage(duplex, stage.CueOptions{Cache: cueclip.NewMemoryCache(1 << 24)})

	in := make(chan stage.StreamElement, 4)
	out := make(chan stage.StreamElement, 256)
	done := make(chan error, 1)
	go func() { done <- duplex.Process(ctx, in, out) }()
	in <- stage.NewAudioElement(&stage.AudioData{Samples: make([]byte, 4800), SampleRate: 24000, Channels: 1,
		Format: stage.AudioFormatPCM16})
	time.Sleep(10 * time.Second) // the session connects and renders the message

	cueIn := make(chan stage.StreamElement, 1)
	cueOut := make(chan stage.StreamElement, 256)
	text := brunoLine
	cueIn <- stage.StreamElement{Text: &text, Cue: &stage.Cue{Kind: stage.CueGuardrailMessage, Text: text}}
	close(cueIn)
	if err := cue.Process(ctx, cueIn, cueOut); err != nil {
		t.Fatalf("cue stage: %v", err)
	}
	var played []byte
	for e := range cueOut {
		if e.Audio != nil {
			played = append(played, e.Audio.Samples...)
		}
	}
	seconds := float64(len(played)) / 2 / DefaultRealtimeSampleRate
	heard, err := stt.NewOpenAI(apiKey).TranscribeBytes(ctx, played, stt.TranscriptionConfig{Format: "pcm",
		SampleRate: DefaultRealtimeSampleRate, Channels: 1, BitDepth: 16, Language: "en"})
	if err != nil {
		t.Fatalf("transcribe the cue: %v", err)
	}
	t.Logf("cue played %.2fs; Whisper heard %q", seconds, heard)
	if seconds < 0.5 {
		t.Errorf("the cue played %.2fs: the earcon, not the session's render", seconds)
	}
	if !strings.Contains(strings.ToLower(heard), "bruno") {
		t.Errorf("Whisper did not hear the message in the cue: %q", heard)
	}

	close(in)
	cancel()
	<-done
	for e := range out {
		if e.Audio != nil || e.Text != nil {
			t.Errorf("the provider stage emitted output with no input turn: %+v", e)
		}
	}
}

// normalizedEqual is the cue source's transcript check.
func normalizedEqual(a, b string) bool {
	norm := func(s string) string {
		var out []rune
		for _, r := range strings.ToLower(s) {
			if r == ' ' || r == '\t' || r == '\n' || (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
				out = append(out, r)
			}
		}
		return strings.Join(strings.Fields(string(out)), " ")
	}
	return norm(a) == norm(b)
}
