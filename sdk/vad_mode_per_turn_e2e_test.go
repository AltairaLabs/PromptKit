//go:build e2e

package sdk

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/AltairaLabs/PromptKit/runtime/providers"
	"github.com/AltairaLabs/PromptKit/runtime/providers/claude"
	"github.com/AltairaLabs/PromptKit/runtime/stt"
	"github.com/AltairaLabs/PromptKit/runtime/tts"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// OpenAI TTS emits headerless PCM16 mono at 24 kHz. Running the whole pipeline
// at that rate keeps the audio byte-exact end to end: AudioTurnStage stamps
// SampleRate onto the audio element and STTStage forwards it as the
// "sample_rate" hint, so no resampling step can misdescribe the samples.
const liveSampleRate = 24000

// liveFrame is the wall-clock chunk size speech is fed at. AudioTurnStage
// measures silence in audio-sample time but only re-evaluates on element
// arrival, so audio must arrive paced roughly like a live microphone.
const liveFrame = 20 * time.Millisecond

// requireLiveKeys skips unless both real providers are configured. e2e_init_test.go
// loads .env before this runs.
func requireLiveKeys(t *testing.T) string {
	t.Helper()
	openAIKey := os.Getenv("OPENAI_API_KEY")
	if openAIKey == "" {
		t.Skip("OPENAI_API_KEY not set; skipping live VAD-mode test")
	}
	if os.Getenv("ANTHROPIC_API_KEY") == "" {
		t.Skip("ANTHROPIC_API_KEY not set; skipping live VAD-mode test")
	}
	return openAIKey
}

// writeVADLivePack writes a pack whose system prompt keeps replies short, so a
// turn's audio is a second or two rather than a paragraph.
func writeVADLivePack(t *testing.T) string {
	t.Helper()
	packFile := filepath.Join(t.TempDir(), "voice.pack.json")
	content := `{
		"name": "vad-live-pack",
		"version": "v1",
		"prompts": {
			"main": {
				"system_template": "You are a voice assistant. Answer in one short sentence, naming the place directly."
			}
		}
	}`
	require.NoError(t, os.WriteFile(packFile, []byte(content), 0o600))
	return packFile
}

// synthesizeSpeech renders text to headerless PCM16 at liveSampleRate using the
// real OpenAI TTS service — genuine speech, not a synthetic tone, so the VAD and
// Whisper both see what a caller would actually produce.
func synthesizeSpeech(t *testing.T, svc tts.Service, text string) []byte {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	rc, err := svc.Synthesize(ctx, text, tts.SynthesisConfig{
		Voice:  tts.VoiceAlloy,
		Format: tts.FormatPCM16,
		Speed:  1.0,
		Model:  tts.ModelTTS1,
	})
	require.NoError(t, err, "TTS synthesis of %q", text)
	defer func() { _ = rc.Close() }()

	pcm, err := io.ReadAll(rc)
	require.NoError(t, err)
	require.NotEmpty(t, pcm, "TTS returned no audio for %q", text)
	return pcm
}

// replyCollector accumulates the audio the pipeline speaks back, so a test can
// tell whether a reply arrived *while the session was still open*.
type replyCollector struct {
	mu    sync.Mutex
	audio []byte
	text  strings.Builder
}

func (c *replyCollector) consume(ch <-chan providers.StreamChunk) {
	for chunk := range ch {
		c.mu.Lock()
		if chunk.MediaData != nil && len(chunk.MediaData.Data) > 0 {
			c.audio = append(c.audio, chunk.MediaData.Data...)
		}
		if chunk.Delta != "" {
			c.text.WriteString(chunk.Delta)
		}
		c.mu.Unlock()
	}
}

func (c *replyCollector) reset() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.audio = nil
	c.text.Reset()
}

func (c *replyCollector) snapshot() ([]byte, string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := make([]byte, len(c.audio))
	copy(out, c.audio)
	return out, c.text.String()
}

// waitForReply polls until at least minBytes of spoken reply have arrived.
// A quarter second of speech is far more than any stray artifact.
func (c *replyCollector) waitForReply(minBytes int, timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		audio, _ := c.snapshot()
		if len(audio) >= minBytes {
			return true
		}
		time.Sleep(20 * time.Millisecond)
	}
	return false
}

// speak streams pre-rendered speech at real time, then trailing silence long
// enough to close the VAD turn.
func speak(t *testing.T, conv *Conversation, pcm []byte, trailingSilence time.Duration) {
	t.Helper()
	ctx := context.Background()
	bytesPerFrame := int(float64(liveSampleRate)*liveFrame.Seconds()) * 2

	send := func(chunk []byte) {
		require.NoError(t, conv.SendChunk(ctx, &providers.StreamChunk{
			MediaData: &providers.StreamMediaData{MIMEType: "audio/pcm", Data: chunk},
		}))
		time.Sleep(liveFrame)
	}

	for off := 0; off < len(pcm); off += bytesPerFrame {
		end := off + bytesPerFrame
		if end > len(pcm) {
			end = len(pcm)
		}
		send(pcm[off:end])
	}
	silence := make([]byte, bytesPerFrame)
	for elapsed := time.Duration(0); elapsed < trailingSilence; elapsed += liveFrame {
		send(silence)
	}
}

// transcribe runs the spoken reply back through real Whisper so the test can
// assert on what the assistant actually said, not merely that it said something.
func transcribe(t *testing.T, svc *stt.OpenAIService, pcm []byte) string {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	text, err := svc.TranscribeBytes(ctx, pcm, stt.TranscriptionConfig{
		Format:     stt.FormatPCM,
		SampleRate: liveSampleRate,
		Channels:   1,
		BitDepth:   16,
		Language:   "en",
	})
	require.NoError(t, err)
	return strings.ToLower(text)
}

// TestVADModeLiveRepliesMidSessionEndToEnd is #1644 proven against real
// providers: synthesized speech in through OpenAI TTS, real Whisper STT, a real
// Claude turn, real TTS back — and the reply must be spoken while the caller is
// still on the line. Before the fix this produced silence until Close, which no
// mock can demonstrate is a *product* failure rather than a wiring detail.
//
// Two utterances run back to back, because one early reply could in principle
// come from a single flush; a second reply to a second question can only come
// from a pipeline that stayed open and kept listening.
func TestVADModeLiveRepliesMidSessionEndToEnd(t *testing.T) {
	if testing.Short() {
		t.Skip("live voice round trip against real providers")
	}
	openAIKey := requireLiveKeys(t)

	ttsSvc := tts.NewOpenAI(openAIKey)
	sttSvc := stt.NewOpenAI(openAIKey)
	llm := claude.NewProvider(
		"claude-vad-live",
		"claude-haiku-4-5-20251001",
		"https://api.anthropic.com/v1",
		providers.ProviderDefaults{Temperature: 0.2, MaxTokens: 100},
		false,
	)
	t.Cleanup(func() { _ = llm.Close() })

	// Render both questions up front: synthesis latency must not eat into the
	// paced streaming that the VAD turn boundaries depend on.
	francePCM := synthesizeSpeech(t, ttsSvc, "What is the capital of France?")
	germanyPCM := synthesizeSpeech(t, ttsSvc, "And what is the capital of Germany?")

	conv, err := OpenDuplex(writeVADLivePack(t), "main",
		WithProvider(llm),
		WithSkipSchemaValidation(),
		WithVADMode(sttSvc, ttsSvc, &VADModeConfig{
			SilenceDuration:   500 * time.Millisecond,
			MinSpeechDuration: 200 * time.Millisecond,
			MaxTurnDuration:   15 * time.Second,
			SampleRate:        liveSampleRate,
			Language:          "en",
			Voice:             "alloy",
			Speed:             1.0,
		}),
	)
	require.NoError(t, err)
	t.Cleanup(func() { _ = conv.Close() })

	responseCh, err := conv.Response()
	require.NoError(t, err)
	replies := &replyCollector{}
	go replies.consume(responseCh)

	// A quarter second of 16-bit mono speech.
	const minReplyBytes = liveSampleRate / 4 * 2
	const replyTimeout = 45 * time.Second

	// Turn one.
	speak(t, conv, francePCM, time.Second)
	require.True(t, replies.waitForReply(minReplyBytes, replyTimeout),
		"assistant must speak a reply during the session, before Close")

	firstAudio, firstText := replies.snapshot()
	firstSpoken := transcribe(t, sttSvc, firstAudio)
	t.Logf("turn 1 reply: transcript=%q streamed_text=%q audio_bytes=%d",
		firstSpoken, firstText, len(firstAudio))
	assert.Contains(t, firstSpoken, "paris", "the mid-session reply must answer the question asked")

	// Turn two, on the same open session.
	replies.reset()
	speak(t, conv, germanyPCM, time.Second)
	require.True(t, replies.waitForReply(minReplyBytes, replyTimeout),
		"a second utterance must be answered too — the session must stay open and keep listening")

	secondAudio, secondText := replies.snapshot()
	secondSpoken := transcribe(t, sttSvc, secondAudio)
	t.Logf("turn 2 reply: transcript=%q streamed_text=%q audio_bytes=%d",
		secondSpoken, secondText, len(secondAudio))
	assert.Contains(t, secondSpoken, "berlin", "the second turn must be answered on its own merits")
}
