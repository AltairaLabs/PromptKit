package openai

import (
	"context"
	"encoding/base64"
	"encoding/binary"
	"math"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/AltairaLabs/PromptKit/runtime/logger"
	"github.com/AltairaLabs/PromptKit/runtime/providers"
	"github.com/AltairaLabs/PromptKit/runtime/types"
)

// generateTestWAV creates a minimal WAV file containing a 440Hz sine wave.
// Duration: 0.5 seconds, 16-bit PCM, 24kHz mono.
func generateTestWAV() []byte {
	sampleRate := 24000
	duration := 0.5
	numSamples := int(float64(sampleRate) * duration)
	dataSize := numSamples * 2 // 16-bit = 2 bytes per sample

	// WAV header (44 bytes)
	buf := make([]byte, 44+dataSize)
	copy(buf[0:4], "RIFF")
	binary.LittleEndian.PutUint32(buf[4:8], uint32(36+dataSize))
	copy(buf[8:12], "WAVE")
	copy(buf[12:16], "fmt ")
	binary.LittleEndian.PutUint32(buf[16:20], 16) // chunk size
	binary.LittleEndian.PutUint16(buf[20:22], 1)  // PCM
	binary.LittleEndian.PutUint16(buf[22:24], 1)  // mono
	binary.LittleEndian.PutUint32(buf[24:28], uint32(sampleRate))
	binary.LittleEndian.PutUint32(buf[28:32], uint32(sampleRate*2)) // byte rate
	binary.LittleEndian.PutUint16(buf[32:34], 2)                    // block align
	binary.LittleEndian.PutUint16(buf[34:36], 16)                   // bits per sample
	copy(buf[36:40], "data")
	binary.LittleEndian.PutUint32(buf[40:44], uint32(dataSize))

	// 440Hz sine wave
	for i := 0; i < numSamples; i++ {
		sample := int16(math.Sin(2*math.Pi*440*float64(i)/float64(sampleRate)) * 16000)
		binary.LittleEndian.PutUint16(buf[44+i*2:44+i*2+2], uint16(sample))
	}
	return buf
}

// TestOpenAIProvider_Contract runs the full provider contract test suite
// against the OpenAI base provider (no tools).
func TestOpenAIProvider_Contract(t *testing.T) {
	if os.Getenv("OPENAI_API_KEY") == "" {
		t.Skip("Skipping OpenAI contract tests - OPENAI_API_KEY not set")
	}

	logger.SetVerbose(true)
	defer logger.SetVerbose(false)

	provider := NewProvider(
		"openai-test",
		"gpt-4o-mini",
		"https://api.openai.com/v1",
		providers.ProviderDefaults{
			Temperature: 0.7,
			MaxTokens:   100,
		},
		false,
	)
	defer provider.Close()

	providers.RunProviderContractTests(t, providers.ProviderContractTests{
		Provider:                  provider,
		SupportsToolsExpected:     false,
		SupportsStreamingExpected: true,
	})
}

// TestToolProvider_Contract runs the full provider contract test suite
// against the OpenAI tool provider including tool-calling tests.
func TestToolProvider_Contract(t *testing.T) {
	if os.Getenv("OPENAI_API_KEY") == "" {
		t.Skip("Skipping OpenAI tool contract tests - OPENAI_API_KEY not set")
	}

	logger.SetVerbose(true)
	defer logger.SetVerbose(false)

	provider := NewToolProvider(
		"openai-tool-test",
		"gpt-4o-mini",
		"https://api.openai.com/v1",
		providers.ProviderDefaults{
			Temperature: 0.7,
			MaxTokens:   100,
		},
		false,
		nil,
		nil,
	)
	defer provider.Close()

	providers.RunProviderContractTests(t, providers.ProviderContractTests{
		Provider:                  provider,
		SupportsToolsExpected:     true,
		SupportsStreamingExpected: true,
	})
}

// TestAudioModel_Predict_TextResponse verifies that sending audio input to an
// audio model via the Chat Completions API returns a text response describing
// the audio content.
func TestAudioModel_Predict_TextResponse(t *testing.T) {
	if os.Getenv("OPENAI_API_KEY") == "" {
		t.Skip("OPENAI_API_KEY not set")
	}

	logger.SetVerbose(true)
	defer logger.SetVerbose(false)

	provider := NewProviderWithConfig(
		"audio-test", "gpt-4o-audio-preview", "https://api.openai.com/v1",
		providers.ProviderDefaults{
			Temperature: 0.1,
			MaxTokens:   200,
		},
		false,
		map[string]any{"api_mode": "completions"},
	)
	defer provider.Close()

	// Generate a small WAV with a sine tone — content doesn't matter,
	// we just need the model to accept and respond to audio input.
	wavData := generateTestWAV()
	wavB64 := base64.StdEncoding.EncodeToString(wavData)
	req := providers.PredictionRequest{
		System: "Describe what you hear. Be concise. Just say what the audio sounds like.",
		Messages: []types.Message{{
			Role: "user",
			Parts: []types.ContentPart{
				types.NewTextPart("What does this audio contain?"),
				{
					Type: types.ContentTypeAudio,
					Media: &types.MediaContent{
						MIMEType: types.MIMETypeAudioWAV,
						Data:     &wavB64,
					},
				},
			},
		}},
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	resp, err := provider.Predict(ctx, req)
	if err != nil {
		t.Fatalf("Predict failed: %v", err)
	}

	if resp.Content == "" {
		t.Fatal("Predict returned empty content")
	}

	t.Logf("Response: %q", resp.Content)

	// The model should describe something about the audio (tone, beep, sound, etc.)
	lower := strings.ToLower(resp.Content)
	if !strings.Contains(lower, "tone") && !strings.Contains(lower, "beep") &&
		!strings.Contains(lower, "sound") && !strings.Contains(lower, "audio") &&
		!strings.Contains(lower, "sine") && !strings.Contains(lower, "hz") &&
		!strings.Contains(lower, "pitch") && !strings.Contains(lower, "frequency") {
		t.Logf("Warning: response may not describe the audio tone: %q", resp.Content)
	}

	// Should have Parts
	if len(resp.Parts) == 0 {
		t.Error("Expected non-empty Parts in response")
	}

	// Cost info should be present
	if resp.CostInfo == nil {
		t.Error("Expected CostInfo in response")
	}
}

// TestAudioModel_Predict_AudioOutputWithTranscript verifies that when
// modalities include "audio", the response contains both audio data and
// a transcript.
func TestAudioModel_Predict_AudioOutputWithTranscript(t *testing.T) {
	if os.Getenv("OPENAI_API_KEY") == "" {
		t.Skip("OPENAI_API_KEY not set")
	}

	logger.SetVerbose(true)
	defer logger.SetVerbose(false)

	provider := NewProviderWithConfig(
		"audio-test", "gpt-4o-audio-preview", "https://api.openai.com/v1",
		providers.ProviderDefaults{
			Temperature: 0.1,
			MaxTokens:   200,
		},
		false,
		map[string]any{
			"api_mode":     "completions",
			"modalities":   []any{"text", "audio"},
			"voice":        "alloy",
			"audio_format": "wav",
		},
	)
	defer provider.Close()

	// Audio model requires audio in input or output. Send a short audio clip
	// and request audio output via modalities config.
	wavData := generateTestWAV()
	wavB64 := base64.StdEncoding.EncodeToString(wavData)
	req := providers.PredictionRequest{
		Messages: []types.Message{{
			Role: "user",
			Parts: []types.ContentPart{
				types.NewTextPart("Say hello in exactly three words."),
				{
					Type: types.ContentTypeAudio,
					Media: &types.MediaContent{
						MIMEType: types.MIMETypeAudioWAV,
						Data:     &wavB64,
					},
				},
			},
		}},
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	resp, err := provider.Predict(ctx, req)
	if err != nil {
		t.Fatalf("Predict failed: %v", err)
	}

	// Content should be the transcript
	if resp.Content == "" {
		t.Fatal("Predict returned empty content (expected transcript)")
	}
	t.Logf("Transcript: %q", resp.Content)

	// Parts should contain both text (transcript) and audio
	var hasText, hasAudio bool
	for _, p := range resp.Parts {
		switch p.Type {
		case types.ContentTypeText:
			hasText = true
		case types.ContentTypeAudio:
			hasAudio = true
			if p.Media == nil || p.Media.Data == nil || *p.Media.Data == "" {
				t.Error("Audio part has no data")
			} else {
				t.Logf("Audio data length: %d bytes (base64)", len(*p.Media.Data))
			}
			if p.Media.MIMEType != types.MIMETypeAudioWAV {
				t.Errorf("Audio MIME = %q, want %q", p.Media.MIMEType, types.MIMETypeAudioWAV)
			}
		}
	}

	if !hasText {
		t.Error("Expected a text part (transcript) in response")
	}
	if !hasAudio {
		t.Error("Expected an audio part in response")
	}
}
