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
			"modalities": []string{"text", "audio"},
			// Enable Whisper input transcription so the reorder path is exercised;
			// OpenAI delivers it late, and the pipeline reorders it ahead of the reply.
			"input_transcription": true,
		},
	}

	mic := audio.NewMemSource(audio.KindAudio, 512)
	speaker := audio.NewMemSink(audio.KindAudio)
	sess := &fakeAudioSession{sources: []audio.Source{mic}, sinks: []audio.Sink{speaker}}

	var obsAudio, obsText, obsTurnDone, obsUserTranscript atomic.Int64
	// assistantTextBeforeUser counts assistant text deltas seen before the user
	// transcript. With reordering, the user turn is emitted first, so this stays 0.
	var assistantTextBeforeUser atomic.Int64
	conv, err := OpenVoice(writeIngestionTestPack(t), "main",
		WithProvider(prov),
		WithStreamingConfig(cfg),
		WithSkipSchemaValidation(),
		WithAudioSession(sess),
		WithVoiceObserver(func(c providers.StreamChunk) {
			if c.MediaData != nil && len(c.MediaData.Data) > 0 {
				obsAudio.Add(1)
			}
			if t, _ := c.Metadata["input_transcription"].(string); t != "" {
				obsUserTranscript.Add(1)
			}
			if c.Delta != "" {
				obsText.Add(1)
				if obsUserTranscript.Load() == 0 {
					assistantTextBeforeUser.Add(1)
				}
			}
			if done, _ := c.Metadata["assistant_turn_complete"].(bool); done {
				obsTurnDone.Add(1)
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
	// -> speaker, and the session must stay alive throughout. Wait until both the
	// audio reply and its (reordered) transcript text have arrived — with
	// reordering the assistant text is held until the user transcript lands.
	const wantFrames = 25
	replyDeadline := time.Now().Add(60 * time.Second)
	for time.Now().Before(replyDeadline) && (len(speaker.Written()) < wantFrames || obsText.Load() == 0) {
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

	// Ordering guarantee: the user's transcript (delivered late by OpenAI's
	// Whisper) must be reordered AHEAD of the assistant's text for the turn, so the
	// transcript reads user-then-assistant. No assistant text delta may precede the
	// user transcript.
	require.Positive(t, obsUserTranscript.Load(),
		"user transcript was never observed — transcription/ordering path not exercised")
	require.Zerof(t, assistantTextBeforeUser.Load(),
		"%d assistant text deltas were shown before the user transcript — reordering failed",
		assistantTextBeforeUser.Load())
	t.Logf("reply reached the speaker: %d frames (audio=%d text=%d user_transcript=%d text_before_user=%d)",
		len(speaker.Written()), obsAudio.Load(), obsText.Load(), obsUserTranscript.Load(), assistantTextBeforeUser.Load())

	// Phase 2: prove the session survives WELL past the 30s pipeline idle timeout
	// while the mic keeps streaming — the original ~30s death. Hold to 45s total.
	surviveUntil := time.Now().Add(40 * time.Second)
	for time.Now().Before(surviveUntil) {
		dieIfSessionEnded("during the 40s idle-survival window")
		time.Sleep(500 * time.Millisecond)
	}
	// Note: assistant_turn_complete is asserted by the unit test
	// (TestStreamElementToStreamChunk_AssistantTurnComplete). End to end it flows
	// behind the paced audio, so it only lands once the reply has fully drained to
	// the speaker — later than this bounded window — hence it's logged, not gated.
	t.Logf("OK: session alive past the 30s idle timeout with a live mic; total speaker frames=%d turn_done=%d",
		len(speaker.Written()), obsTurnDone.Load())

	micStop()
	cancel()
	// Best-effort teardown: Close may block on the output pacing stage still
	// draining buffered audio at realtime (a pacing/drain timing interaction, not
	// this feature). Prompt-close is covered by the dedicated drain test; here we
	// only need the feature assertions above.
	_ = conv.Close()
}

// TestVoiceRealtime_ShortReplySurvives reproduces the reported termination on a
// SHORT exchange: with a brief reply, OpenAI's Whisper transcript arrives AFTER
// the assistant turn ends. The long-reply test never hits this. The session must
// stay alive after the short turn while the mic keeps streaming (it must not tear
// down). Runs with reordering auto-enabled (input_transcription on).
func TestVoiceRealtime_ShortReplySurvives(t *testing.T) {
	apiKey := os.Getenv("OPENAI_API_KEY")
	if apiKey == "" {
		t.Skip("OPENAI_API_KEY not set")
	}

	speech := synthesizePromptPCM24k(t, apiKey, "Say hi back in one very short sentence.")

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
			"input_transcription": true, // reorder auto-on
		},
	}

	mic := audio.NewMemSource(audio.KindAudio, 512)
	speaker := audio.NewMemSink(audio.KindAudio)
	sess := &fakeAudioSession{sources: []audio.Source{mic}, sinks: []audio.Sink{speaker}}

	var obsText, obsRealUser, sawPlaceholder, assistantTextBeforeUser atomic.Int64
	conv, err := OpenVoice(writeIngestionTestPack(t), "main",
		WithProvider(prov),
		WithStreamingConfig(cfg),
		WithSkipSchemaValidation(),
		WithAudioSession(sess),
		WithVoiceObserver(func(c providers.StreamChunk) {
			if tr, _ := c.Metadata["input_transcription"].(string); tr != "" {
				if tr == defaultTranscriptPlaceholder {
					sawPlaceholder.Add(1)
				} else {
					obsRealUser.Add(1)
				}
			}
			if c.Delta != "" {
				obsText.Add(1)
				if obsRealUser.Load() == 0 {
					assistantTextBeforeUser.Add(1)
				}
			}
		}),
	)
	require.NoError(t, err)

	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- conv.Start(ctx) }()

	micCtx, micStop := context.WithCancel(ctx)
	defer micStop()
	go pushMicUntilDone(micCtx, mic, speech)

	// Wait for the short reply to arrive (some assistant text or audio).
	replyBy := time.Now().Add(45 * time.Second)
	for time.Now().Before(replyBy) && obsText.Load() == 0 && len(speaker.Written()) == 0 {
		select {
		case e := <-done:
			t.Fatalf("session ended before any reply (%v)", e)
		default:
		}
		time.Sleep(200 * time.Millisecond)
	}

	// The reply happened. Now hold: the session must NOT tear down after this short
	// turn while the mic keeps streaming (the reported termination).
	holdUntil := time.Now().Add(25 * time.Second)
	for time.Now().Before(holdUntil) {
		select {
		case e := <-done:
			t.Fatalf("session terminated after the short turn (%v) — text=%d frames=%d",
				e, obsText.Load(), len(speaker.Written()))
		default:
		}
		time.Sleep(500 * time.Millisecond)
	}
	t.Logf("survived the short turn; text=%d frames=%d real_user=%d placeholder=%d text_before_user=%d",
		obsText.Load(), len(speaker.Written()), obsRealUser.Load(), sawPlaceholder.Load(), assistantTextBeforeUser.Load())

	// Correct-order guarantee for a SHORT reply, where the transcript arrives after
	// the reply ends. The reorder stage must hold for the late transcript and emit
	// it ahead of the assistant text — NOT fall back to the "[no transcription
	// available]" placeholder while the real transcript is still coming.
	require.Zerof(t, sawPlaceholder.Load(),
		"placeholder fired for a turn whose real transcript did arrive (%d real transcripts) — "+
			"reorder gave up before the late transcript landed", obsRealUser.Load())
	require.Positive(t, obsRealUser.Load(), "the user's real transcript was never surfaced")
	require.Zerof(t, assistantTextBeforeUser.Load(),
		"%d assistant text deltas were shown before the user transcript — short-reply ordering failed",
		assistantTextBeforeUser.Load())

	micStop()
	cancel()
	_ = conv.Close()
}
