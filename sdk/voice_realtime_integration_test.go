//go:build integration
// +build integration

package sdk

import (
	"context"
	"io"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/AltairaLabs/PromptKit/runtime/audio"
	"github.com/AltairaLabs/PromptKit/runtime/providers"
	"github.com/AltairaLabs/PromptKit/runtime/providers/openai"
	"github.com/AltairaLabs/PromptKit/runtime/tts"
	"github.com/AltairaLabs/PromptKit/runtime/types"
	"github.com/stretchr/testify/require"
)

// realtimeSampleRate is the OpenAI Realtime PCM16 rate.
const realtimeSampleRate = 24000

// synthesizePromptPCM24k turns text into raw 24kHz PCM16 mono speech via OpenAI
// TTS. Real speech (not a sine wave) reliably trips the Realtime API's
// server-side VAD, so the model actually answers.
func synthesizePromptPCM24k(t *testing.T, apiKey, text string) []byte {
	t.Helper()
	svc := tts.NewOpenAI(apiKey)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	rc, err := svc.Synthesize(ctx, text, tts.SynthesisConfig{
		Voice:  "echo",
		Format: tts.AudioFormat{Name: "pcm"}, // raw 24kHz 16-bit mono
	})
	require.NoError(t, err, "TTS synthesize prompt")
	defer func() { _ = rc.Close() }()
	data, err := io.ReadAll(rc)
	require.NoError(t, err, "read TTS audio")
	require.NotEmpty(t, data, "TTS returned no audio")
	return data
}

// pushMicUntilDone feeds PCM16 into a MemSource as 20ms frames at real time —
// the spoken prompt first, then a continuous stream of silence frames until ctx
// is canceled — exactly how a real always-on microphone drives the session. The
// continuous stream is deliberate: it is what keeps a live conversation alive
// past the pipeline's 30s idle timeout (each inbound frame resets it), so this
// exercises the idle-reset fix under realistic mic behavior.
func pushMicUntilDone(ctx context.Context, mic *audio.MemSource, pcm []byte) {
	const frameBytes = realtimeSampleRate / 100 * 2 // 20ms @ 24kHz PCM16 = 480 samples = 960 bytes
	fmtA := audio.Format{SampleRate: realtimeSampleRate, Channels: 1}
	push := func(b []byte) bool {
		select {
		case <-ctx.Done():
			return false
		default:
		}
		mic.Push(audio.MediaFrame{Kind: audio.KindAudio, Data: b, Format: fmtA})
		select {
		case <-ctx.Done():
			return false
		case <-time.After(20 * time.Millisecond):
			return true
		}
	}
	for i := 0; i < len(pcm); i += frameBytes {
		end := i + frameBytes
		if end > len(pcm) {
			end = len(pcm)
		}
		frame := make([]byte, end-i)
		copy(frame, pcm[i:end])
		if !push(frame) {
			return
		}
	}
	silent := make([]byte, frameBytes)
	for {
		if !push(append([]byte(nil), silent...)) {
			return
		}
	}
}

// TestVoiceRealtime_LongReplyThroughOpenVoice is the integration test that was
// missing: it drives the REAL OpenAI Realtime provider through the actual
// OpenVoice path — mic pump → DuplexProviderStage → output AudioPacingStage →
// speaker pump — the same pipeline (and the same idle-reset, drain, and pacing
// changes) that ships in the openai-realtime example. It proves the mic path
// works end to end against a real provider: a spoken prompt provokes a long
// reply, that reply streams back to the speaker through the paced pipeline, and
// the session stays alive the whole time (does not tear down after the turn).
//
// Run: OPENAI_API_KEY=... go test -tags integration -run TestVoiceRealtime ./sdk/... -v
func TestVoiceRealtime_LongReplyThroughOpenVoice(t *testing.T) {
	apiKey := os.Getenv("OPENAI_API_KEY")
	if apiKey == "" {
		t.Skip("OPENAI_API_KEY not set")
	}

	// A prompt engineered to make the model talk for a while — the long reply is
	// the burst that stresses output pacing and the session lifetime.
	speech := synthesizePromptPCM24k(t, apiKey,
		"Please tell me a detailed story about a lighthouse keeper and the sea. "+
			"Take your time and speak for at least thirty seconds.")

	prov := openai.NewProvider(
		"openai-realtime", "gpt-realtime", "https://api.openai.com",
		providers.ProviderDefaults{}, false,
	)
	cfg := &providers.StreamingInputConfig{
		Config: types.StreamingMediaConfig{
			Type: types.ContentTypeAudio, ChunkSize: 3200,
			SampleRate: realtimeSampleRate, Channels: 1, BitDepth: 16, Encoding: "pcm16",
		},
		Metadata: map[string]interface{}{
			"modalities":          []string{"text", "audio"},
			"input_transcription": false,
		},
	}

	mic := audio.NewMemSource(audio.KindAudio, 512)
	speaker := audio.NewMemSink(audio.KindAudio)
	sess := &fakeAudioSession{sources: []audio.Source{mic}, sinks: []audio.Sink{speaker}}

	var obsAudio, obsText atomic.Int64
	conv, err := OpenVoice(writeIngestionTestPack(t), "main",
		WithProvider(prov),
		WithStreamingConfig(cfg),
		WithSkipSchemaValidation(),
		WithAudioSession(sess),
		WithVoiceObserver(func(c providers.StreamChunk) {
			if c.MediaData != nil && len(c.MediaData.Data) > 0 {
				obsAudio.Add(1)
			}
			if c.Delta != "" {
				obsText.Add(1)
			}
		}),
	)
	require.NoError(t, err)

	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- conv.Start(ctx) }()

	// Continuous always-on mic: speak the prompt, then keep streaming (silence)
	// so server-VAD commits the turn AND the session stays fed past 30s.
	micCtx, micStop := context.WithCancel(ctx)
	defer micStop()
	go pushMicUntilDone(micCtx, mic, speech)

	// dieIfSessionEnded fails the test if Start returned (session torn down).
	dieIfSessionEnded := func(where string) {
		select {
		case startErr := <-done:
			if startErr != nil && (strings.Contains(startErr.Error(), "invalid_api_key") ||
				strings.Contains(startErr.Error(), "websocket: close")) {
				t.Skipf("Skipping (connection): %v", startErr)
			}
			t.Fatalf("session ended %s (%v) — mic path/lifetime broke; speaker frames=%d observer audio=%d text=%d",
				where, startErr, len(speaker.Written()), obsAudio.Load(), obsText.Load())
		default:
		}
	}

	// Phase 1: a long reply must round-trip mic -> real provider -> paced pipeline
	// -> speaker, and the session must stay alive throughout.
	const wantFrames = 25
	replyDeadline := time.Now().Add(60 * time.Second)
	for time.Now().Before(replyDeadline) && len(speaker.Written()) < wantFrames {
		dieIfSessionEnded("before the reply arrived")
		time.Sleep(200 * time.Millisecond)
	}
	require.GreaterOrEqualf(t, len(speaker.Written()), wantFrames,
		"expected a long spoken reply through the paced OpenVoice pipeline; got %d speaker frames "+
			"(observer audio=%d text=%d) — the real provider reply did not reach the speaker",
		len(speaker.Written()), obsAudio.Load(), obsText.Load())
	// The assistant's spoken words must also reach the observer as live transcript
	// text (StreamChunk.Delta), not just audio — otherwise a voice UI plays sound
	// but shows nothing. Regression guard for the "no assistant transcript" bug.
	require.Positivef(t, obsText.Load(),
		"assistant transcript did not reach the observer (audio frames=%d) — the spoken reply "+
			"produced no live text deltas", obsAudio.Load())
	t.Logf("reply reached the speaker: %d frames (observer audio=%d text=%d)",
		len(speaker.Written()), obsAudio.Load(), obsText.Load())

	// Phase 2: prove the session survives WELL past the 30s pipeline idle timeout
	// while the mic keeps streaming — the original ~30s death. Hold to 45s total.
	surviveUntil := time.Now().Add(40 * time.Second)
	for time.Now().Before(surviveUntil) {
		dieIfSessionEnded("during the 40s idle-survival window")
		time.Sleep(500 * time.Millisecond)
	}
	t.Logf("OK: session alive past the 30s idle timeout with a live mic; total speaker frames=%d", len(speaker.Written()))

	micStop()
	cancel()
	require.NoError(t, conv.Close())
}
