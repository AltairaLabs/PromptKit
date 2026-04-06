//go:build integration

package tts_test

import (
	"context"
	"encoding/binary"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/AltairaLabs/PromptKit/runtime/providers"
	"github.com/AltairaLabs/PromptKit/runtime/providers/openai"
	"github.com/AltairaLabs/PromptKit/runtime/stt"
	"github.com/AltairaLabs/PromptKit/runtime/tts"
	"github.com/AltairaLabs/PromptKit/runtime/types"
)

// TestVoiceConversation_RealOpenAI proves the full voice conversation
// loop works end-to-end with real OpenAI APIs and retry wrappers:
//
//  1. TTS: simulate user speaking "What is the capital of France?"
//  2. STT: transcribe the user's audio → text
//  3. LLM: gpt-4o-mini answers the question → response text
//  4. TTS: synthesize the LLM's response → audio
//  5. STT: transcribe the response audio → verify it contains "Paris"
//
// Writes individual WAV files + a combined conversation.wav to a temp
// directory so the full exchange can be played back. Per-step latency
// is logged.
//
// Run with: OPENAI_API_KEY=... go test -tags=integration -v -run TestVoiceConversation ./tts/...
func TestVoiceConversation_RealOpenAI(t *testing.T) {
	apiKey := os.Getenv("OPENAI_API_KEY")
	if apiKey == "" {
		t.Skip("OPENAI_API_KEY not set")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	ttsSvc := tts.NewOpenAI(apiKey)
	sttSvc := stt.NewOpenAI(apiKey)
	llmProvider := openai.NewProvider("openai", "gpt-4o-mini", "https://api.openai.com/v1",
		providers.ProviderDefaults{Temperature: 0.3, MaxTokens: 100}, false)

	retryTTS := tts.DefaultRetryConfig()
	retrySTT := stt.DefaultRetryConfig()

	outDir := filepath.Join(os.TempDir(), "promptkit-voice-test")
	os.MkdirAll(outDir, 0o755)
	var totalStart = time.Now()

	// Voices: "echo" (male) for user, "nova" (female) for bot.
	const userVoice = "echo"
	const botVoice = "nova"

	// Step 1: Simulate user speaking.
	userQuestion := "What is the capital of France?"
	t.Logf("Step 1: TTS — user (%s) says: %q", userVoice, userQuestion)
	start := time.Now()
	userAudio, err := synthesizeVoice(ctx, ttsSvc, userQuestion, userVoice, retryTTS)
	step1 := time.Since(start)
	if err != nil {
		t.Fatalf("TTS (user speech): %v", err)
	}
	t.Logf("  → %d bytes, %v", len(userAudio), step1)

	userPath := filepath.Join(outDir, "01-user.wav")
	writeFile(t, userPath, userAudio)

	// Step 2: Transcribe the user's audio.
	t.Log("Step 2: STT — transcribing user audio...")
	start = time.Now()
	userTranscription, err := stt.TranscribeWithRetry(ctx, sttSvc, userAudio, stt.TranscriptionConfig{
		Format: stt.FormatWAV, Language: "en",
	}, retrySTT)
	step2 := time.Since(start)
	if err != nil {
		t.Fatalf("STT (user audio): %v", err)
	}
	t.Logf("  → %q, %v", userTranscription, step2)

	// Step 3: LLM responds.
	t.Log("Step 3: LLM — generating response...")
	start = time.Now()
	llmResponse, err := llmProvider.Predict(ctx, providers.PredictionRequest{
		Messages:    []types.Message{{Role: "user", Content: userTranscription}},
		MaxTokens:   100,
		Temperature: 0.3,
	})
	step3 := time.Since(start)
	if err != nil {
		t.Fatalf("LLM: %v", err)
	}
	responseText := llmResponse.Content
	t.Logf("  → %q, %v", responseText, step3)

	// Step 4: Synthesize the LLM's response with the bot voice.
	t.Logf("Step 4: TTS — bot (%s) says: %q", botVoice, responseText)
	start = time.Now()
	responseAudio, err := synthesizeVoice(ctx, ttsSvc, responseText, botVoice, retryTTS)
	step4 := time.Since(start)
	if err != nil {
		t.Fatalf("TTS (LLM response): %v", err)
	}
	t.Logf("  → %d bytes, %v", len(responseAudio), step4)

	responsePath := filepath.Join(outDir, "02-response.wav")
	writeFile(t, responsePath, responseAudio)

	// Step 5: Transcribe the response audio.
	t.Log("Step 5: STT — transcribing response audio...")
	start = time.Now()
	responseTranscription, err := stt.TranscribeWithRetry(ctx, sttSvc, responseAudio, stt.TranscriptionConfig{
		Format: stt.FormatWAV, Language: "en",
	}, retrySTT)
	step5 := time.Since(start)
	if err != nil {
		t.Fatalf("STT (response audio): %v", err)
	}
	t.Logf("  → %q, %v", responseTranscription, step5)

	// Write combined conversation WAV (user + 0.5s silence + response).
	combinedPath := filepath.Join(outDir, "conversation.wav")
	writeCombinedWAV(t, combinedPath, userAudio, responseAudio)

	// Verify coherence.
	if !strings.Contains(strings.ToLower(responseTranscription), "paris") {
		t.Errorf("response %q missing 'Paris'", responseTranscription)
	}

	totalTime := time.Since(totalStart)

	t.Log("")
	t.Log("=== VOICE CONVERSATION ===")
	t.Logf("User:  %q", userTranscription)
	t.Logf("LLM:   %q", responseText)
	t.Logf("Bot:   %q", responseTranscription)
	t.Log("")
	t.Log("=== LATENCY ===")
	t.Logf("Step 1 (TTS user):      %v", step1)
	t.Logf("Step 2 (STT user):      %v", step2)
	t.Logf("Step 3 (LLM):           %v", step3)
	t.Logf("Step 4 (TTS response):  %v", step4)
	t.Logf("Step 5 (STT verify):    %v", step5)
	t.Logf("Total:                  %v", totalTime)
	t.Logf("User-perceived latency: %v (steps 2+3+4)", step2+step3+step4)
	t.Log("")
	t.Log("=== RECORDINGS ===")
	t.Logf("User audio:     %s", userPath)
	t.Logf("Response audio: %s", responsePath)
	t.Logf("Combined:       %s", combinedPath)
	t.Logf("Play with:      afplay %s", combinedPath)
}

func synthesizeVoice(ctx context.Context, svc tts.Service, text, voice string, retry tts.RetryConfig) ([]byte, error) {
	reader, err := tts.SynthesizeWithRetry(ctx, svc, text, tts.SynthesisConfig{
		Voice: voice, Format: tts.FormatWAV,
	}, retry)
	if err != nil {
		return nil, fmt.Errorf("synthesis: %w", err)
	}
	defer reader.Close()
	data, err := io.ReadAll(reader)
	if err != nil {
		return nil, fmt.Errorf("reading audio: %w", err)
	}
	return data, nil
}

func writeFile(t *testing.T, path string, data []byte) {
	t.Helper()
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatalf("writing %s: %v", path, err)
	}
}

// wavDataOffset finds the "data" chunk in a WAV file and returns the
// offset where PCM samples begin. OpenAI WAV files may have extra
// chunks before "data" so a fixed 44-byte header assumption is wrong.
func wavDataOffset(wav []byte) int {
	// Search for "data" marker after the initial RIFF header (12 bytes).
	for i := 12; i < len(wav)-8; i++ {
		if string(wav[i:i+4]) == "data" {
			return i + 8 // skip "data" + 4-byte size
		}
	}
	return 44 // fallback
}

// writeCombinedWAV concatenates two WAV files with 0.5s silence between
// them into a single WAV file. Both inputs must be the same format
// (OpenAI TTS outputs 24kHz 16-bit mono WAV).
func writeCombinedWAV(t *testing.T, path string, wav1, wav2 []byte) {
	t.Helper()

	pcm1 := wav1[wavDataOffset(wav1):]
	pcm2 := wav2[wavDataOffset(wav2):]

	// 0.5 seconds of silence at 24kHz 16-bit mono = 24000 samples × 2 bytes.
	const sampleRate = 24000
	silenceBytes := sampleRate // 0.5s × 24000 × 2 bytes / 2 = 24000 bytes
	silence := make([]byte, silenceBytes)

	totalPCM := len(pcm1) + len(silence) + len(pcm2)

	// Build a clean WAV header with correct sizes.
	const headerSize = 44
	header := make([]byte, headerSize)
	copy(header[0:4], "RIFF")
	binary.LittleEndian.PutUint32(header[4:8], uint32(36+totalPCM))
	copy(header[8:12], "WAVE")
	copy(header[12:16], "fmt ")
	binary.LittleEndian.PutUint32(header[16:20], 16) // fmt chunk size
	binary.LittleEndian.PutUint16(header[20:22], 1)  // PCM format
	binary.LittleEndian.PutUint16(header[22:24], 1)  // mono
	binary.LittleEndian.PutUint32(header[24:28], sampleRate)
	binary.LittleEndian.PutUint32(header[28:32], sampleRate*2) // byte rate
	binary.LittleEndian.PutUint16(header[32:34], 2)            // block align
	binary.LittleEndian.PutUint16(header[34:36], 16)           // bits per sample
	copy(header[36:40], "data")
	binary.LittleEndian.PutUint32(header[40:44], uint32(totalPCM))

	f, err := os.Create(path)
	if err != nil {
		t.Fatalf("creating %s: %v", path, err)
	}
	defer f.Close()
	f.Write(header)
	f.Write(pcm1)
	f.Write(silence)
	f.Write(pcm2)
}
