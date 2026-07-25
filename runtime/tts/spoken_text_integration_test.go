//go:build integration

package tts_test

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/AltairaLabs/PromptKit/runtime/providers/base"
	"github.com/AltairaLabs/PromptKit/runtime/stt"
	"github.com/AltairaLabs/PromptKit/runtime/tts"
)

// liveTTS is a TTS service that also reports the text it will actually speak.
// Every shipped provider satisfies both halves.
type liveTTS interface {
	tts.Service
	tts.SpokenTextReporter
}

// TestSpokenText_MatchesRealSpeech is the live, cross-provider proof for #1657:
// the value a provider reports via tts.SpokenTextReporter must be what the model
// actually speaks after markup lowering — a unit test with a mock cannot prove
// the model does not vocalize the bracket markup, only a real synth → STT
// round-trip can.
//
// For every shipped TTS provider it feeds a markup-tagged line
// ("[whispers]The capital of France is Paris."), asserts SpokenText reports the
// provider's expected lowering, synthesizes against the live API, transcribes
// the audio with OpenAI STT, and asserts the transcription speaks the sentence
// and never the tag word "whisper" — i.e. the tag was lowered (stripped, moved
// to an instructions field, or interpreted by the model), not read aloud.
//
// [whispers] is chosen because it is both a strippable markup tag (OpenAI tts-1,
// Cartesia, older ElevenLabs) and a native ElevenLabs-v3 directive — so the same
// input exercises every provider's lowering path. The strip/instructions paths
// (which are PromptKit's own logic and where markup could leak into audio) all
// yield the markup-free sentence; ElevenLabs v3 passes the tag through verbatim
// and interprets it, so its reported SpokenText keeps the tag while the audio
// still does not speak it.
//
// STT verification always uses OpenAI Whisper (audio-format agnostic), so the
// whole test needs OPENAI_API_KEY plus each provider's own key; any provider
// whose key is unset is skipped rather than failed.
//
// Run with:
//
//	OPENAI_API_KEY=... CARTESIA_API_KEY=... ELEVENLABS_API_KEY=... \
//	  go test -tags=integration -v -run TestSpokenText_MatchesRealSpeech ./tts/...
func TestSpokenText_MatchesRealSpeech(t *testing.T) {
	openaiKey := os.Getenv("OPENAI_API_KEY")
	if openaiKey == "" {
		t.Skip("OPENAI_API_KEY not set (required for STT verification)")
	}

	// "Confident Man", the voice the voice-refund-demo pins for Cartesia.
	const cartesiaVoice = "bf991597-6c13-47e4-8411-91ec2de5c466"
	const input = "[whispers]The capital of France is Paris."
	const strippedSpoken = "The capital of France is Paris."

	sttSvc := stt.NewOpenAI(openaiKey)
	retryTTS := tts.DefaultRetryConfig()
	retrySTT := stt.DefaultRetryConfig()

	cases := []struct {
		name       string
		ttsKeyEnv  string
		voice      string
		sttMIME    string
		wantSpoken string
		newSvc     func(key string) liveTTS
		// skipOnSynthErr treats a synthesis error as a skip rather than a
		// failure — used for ElevenLabs v3, whose model access is gated.
		skipOnSynthErr bool
	}{
		{
			name:       "openai tts-1 strips tags",
			ttsKeyEnv:  "OPENAI_API_KEY",
			voice:      "nova",
			sttMIME:    "audio/wav",
			wantSpoken: strippedSpoken,
			newSvc:     func(k string) liveTTS { return tts.NewOpenAI(k, base.WithModel(tts.ModelTTS1)) },
		},
		{
			name:       "openai gpt-4o-mini-tts moves tags to instructions",
			ttsKeyEnv:  "OPENAI_API_KEY",
			voice:      "nova",
			sttMIME:    "audio/wav",
			wantSpoken: strippedSpoken,
			newSvc:     func(k string) liveTTS { return tts.NewOpenAI(k, base.WithModel(tts.ModelGPT4oMiniTTS)) },
		},
		{
			name:       "cartesia strips tags from the transcript",
			ttsKeyEnv:  "CARTESIA_API_KEY",
			voice:      cartesiaVoice,
			sttMIME:    "audio/wav", // Cartesia FormatWAV → real WAV container
			wantSpoken: strippedSpoken,
			newSvc:     func(k string) liveTTS { return tts.NewCartesia(k) },
		},
		{
			name:       "elevenlabs non-v3 strips tags",
			ttsKeyEnv:  "ELEVENLABS_API_KEY",
			voice:      "",           // default voice (Rachel)
			sttMIME:    "audio/mpeg", // ElevenLabs returns MP3 for a WAV request
			wantSpoken: strippedSpoken,
			newSvc:     func(k string) liveTTS { return tts.NewElevenLabs(k, base.WithModel(tts.ElevenLabsModelMultilingual)) },
		},
		{
			name:           "elevenlabs v3 passes tags through and interprets them",
			ttsKeyEnv:      "ELEVENLABS_API_KEY",
			voice:          "",
			sttMIME:        "audio/mpeg",
			wantSpoken:     input, // v3 keeps inline tags verbatim
			newSvc:         func(k string) liveTTS { return tts.NewElevenLabs(k, base.WithModel(tts.ElevenLabsModelV3)) },
			skipOnSynthErr: true,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			key := os.Getenv(tc.ttsKeyEnv)
			if key == "" {
				t.Skipf("%s not set; skipping %s", tc.ttsKeyEnv, tc.name)
			}

			ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
			defer cancel()

			svc := tc.newSvc(key)

			// (1) The caption we would surface as spoken_text.
			if spoken := svc.SpokenText(input, tts.SynthesisConfig{}); spoken != tc.wantSpoken {
				t.Fatalf("SpokenText(%q) = %q, want %q", input, spoken, tc.wantSpoken)
			}

			// (2) Synthesize the markup input against the live API.
			audio, err := synthesizeVoice(ctx, svc, input, tc.voice, retryTTS)
			if err != nil {
				if tc.skipOnSynthErr {
					t.Skipf("live synthesis unavailable (%v); skipping %s", err, tc.name)
				}
				t.Fatalf("live synthesis: %v", err)
			}
			if len(audio) == 0 {
				t.Fatal("live synthesis returned no audio")
			}
			t.Logf("%s: synthesized %d bytes", tc.name, len(audio))

			// (3) Transcribe and prove the audio speaks the sentence, not the tag.
			resp, err := stt.TranscribeWithRetry(ctx, sttSvc, base.STTRequest{
				Audio:    audio,
				MIMEType: tc.sttMIME,
				Hints:    map[string]string{"language": "en"},
			}, retrySTT)
			if err != nil {
				t.Fatalf("STT transcription: %v", err)
			}
			lower := strings.ToLower(resp.Text)
			t.Logf("%s: transcript=%q", tc.name, resp.Text)

			if !strings.Contains(lower, "capital") || !strings.Contains(lower, "paris") {
				t.Errorf("transcript %q does not speak the expected sentence", resp.Text)
			}
			if strings.Contains(lower, "whisper") {
				t.Errorf("markup leaked into the audio: transcript %q contains the tag word 'whisper'", resp.Text)
			}
		})
	}
}
