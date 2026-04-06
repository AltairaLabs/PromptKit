//go:build integration

package tts_test

import (
	"context"
	"errors"
	"io"
	"os"
	"testing"
	"time"

	"github.com/AltairaLabs/PromptKit/runtime/tts"
)

// TestSynthesizeWithRetry_RealOpenAI_HappyPath hits the real OpenAI TTS
// API through the retry wrapper and verifies audio bytes come back.
func TestSynthesizeWithRetry_RealOpenAI_HappyPath(t *testing.T) {
	apiKey := os.Getenv("OPENAI_API_KEY")
	if apiKey == "" {
		t.Skip("OPENAI_API_KEY not set")
	}

	svc := tts.NewOpenAI(apiKey)
	result, err := tts.SynthesizeWithRetry(
		context.Background(),
		svc,
		"Hello, this is a retry test.",
		tts.SynthesisConfig{Voice: "alloy", Format: tts.FormatMP3},
		tts.DefaultRetryConfig(),
	)
	if err != nil {
		t.Fatalf("SynthesizeWithRetry failed: %v", err)
	}
	defer result.Close()

	audio, err := io.ReadAll(result)
	if err != nil {
		t.Fatalf("failed to read audio: %v", err)
	}
	if len(audio) == 0 {
		t.Fatal("expected non-empty audio output")
	}
	t.Logf("received %d bytes of audio from OpenAI TTS", len(audio))
}

// TestSynthesizeWithRetry_RealOpenAI_BadKey verifies that a 401 auth
// error from OpenAI is correctly classified as non-retryable — the
// wrapper must return immediately without retrying.
func TestSynthesizeWithRetry_RealOpenAI_BadKey(t *testing.T) {
	svc := tts.NewOpenAI("sk-invalid-key-for-testing")
	start := time.Now()
	_, err := tts.SynthesizeWithRetry(
		context.Background(),
		svc,
		"This should fail immediately.",
		tts.SynthesisConfig{Voice: "alloy", Format: tts.FormatMP3},
		tts.RetryConfig{
			MaxAttempts:  3,
			InitialDelay: 5 * time.Second, // deliberately long — should never wait
			MaxDelay:     10 * time.Second,
		},
	)
	elapsed := time.Since(start)

	if err == nil {
		t.Fatal("expected error with bad API key")
	}

	// Must NOT have retried — elapsed time should be well under the
	// 5-second initial delay. If it retried, we'd see >= 5 seconds.
	if elapsed > 3*time.Second {
		t.Errorf("took %v — retry fired on a non-retryable 401 error (should have returned immediately)", elapsed)
	}

	// Verify the error is a SynthesisError with Retryable: false.
	var se *tts.SynthesisError
	if !errors.As(err, &se) {
		t.Fatalf("expected SynthesisError, got %T: %v", err, err)
	}
	if se.Retryable {
		t.Errorf("401 error classified as Retryable=true — should be false")
	}
	t.Logf("correctly classified 401 as non-retryable: %v (elapsed %v)", err, elapsed)
}
