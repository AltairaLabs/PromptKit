package stage_test

import (
	"context"
	"io"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/AltairaLabs/PromptKit/runtime/pipeline/stage"
	"github.com/AltairaLabs/PromptKit/runtime/stt"
	"github.com/AltairaLabs/PromptKit/runtime/tts"
)

// TestTTSStage_RetryOnTransientFailure proves that when a TTS provider
// returns a retryable error (429 rate limit, 503 server error), the
// pipeline stage retries and still produces audio output instead of
// silence. This is the end-to-end proof that the retry wrappers from
// tts.SynthesizeWithRetry are correctly wired into the stage.
func TestTTSStage_RetryOnTransientFailure(t *testing.T) {
	t.Parallel()

	var calls atomic.Int32
	svc := &mockTTSService{
		synthesizeFunc: func(_ context.Context, _ string, _ tts.SynthesisConfig) (io.ReadCloser, error) {
			n := calls.Add(1)
			if n <= 2 {
				// First two calls: transient 429/503.
				return nil, tts.NewSynthesisError("mock-tts", "429", "rate limited", nil, true)
			}
			// Third call: success.
			return io.NopCloser(strings.NewReader(generateTestPCM(100))), nil
		},
	}

	cfg := stage.DefaultTTSStageWithInterruptionConfig()
	cfg.Retry = tts.RetryConfig{
		MaxAttempts:  3,
		InitialDelay: time.Millisecond,
		MaxDelay:     5 * time.Millisecond,
	}
	s := stage.NewTTSStageWithInterruption(svc, cfg)

	inputs := []stage.StreamElement{makeTextElement("Hello world")}
	results := runStage(t, s, inputs, 5*time.Second)

	// The stage must produce output despite two transient failures.
	if len(results) == 0 {
		t.Fatal("expected at least one output element, got 0 (TTS retry did not recover)")
	}

	// The output must have audio — not an error element.
	gotAudio := false
	for _, r := range results {
		if r.Audio != nil && len(r.Audio.Samples) > 0 {
			gotAudio = true
		}
		if r.Error != nil {
			t.Errorf("unexpected error element: %v (retry should have recovered)", r.Error)
		}
	}
	if !gotAudio {
		t.Error("expected audio output from TTS stage after retry, got none")
	}

	// The service must have been called 3 times (2 failures + 1 success).
	if c := calls.Load(); c != 3 {
		t.Errorf("TTS service called %d times, want 3 (2 retries + 1 success)", c)
	}
}

// TestTTSStage_NonRetryableErrorStillFails proves that non-retryable
// errors (401 auth, 400 bad request) are NOT retried — the stage
// propagates the error immediately.
func TestTTSStage_NonRetryableErrorStillFails(t *testing.T) {
	t.Parallel()

	var calls atomic.Int32
	svc := &mockTTSService{
		synthesizeFunc: func(_ context.Context, _ string, _ tts.SynthesisConfig) (io.ReadCloser, error) {
			calls.Add(1)
			return nil, tts.NewSynthesisError("mock-tts", "401", "unauthorized", nil, false)
		},
	}

	cfg := stage.DefaultTTSStageWithInterruptionConfig()
	cfg.Retry = tts.RetryConfig{
		MaxAttempts:  3,
		InitialDelay: time.Millisecond,
		MaxDelay:     5 * time.Millisecond,
	}
	s := stage.NewTTSStageWithInterruption(svc, cfg)

	inputs := []stage.StreamElement{makeTextElement("Hello")}
	results := runStage(t, s, inputs, 5*time.Second)

	// Should have been called exactly once — no retry on 401.
	if c := calls.Load(); c != 1 {
		t.Errorf("TTS service called %d times, want 1 (no retry on non-retryable)", c)
	}

	// The output should contain an error element.
	gotError := false
	for _, r := range results {
		if r.Error != nil {
			gotError = true
		}
	}
	if !gotError {
		t.Error("expected error element for non-retryable failure, got none")
	}
}

// TestSTTStage_RetryOnTransientFailure proves that when an STT provider
// returns a retryable error, the pipeline stage retries and still
// produces transcribed text instead of silently dropping the speech.
func TestSTTStage_RetryOnTransientFailure(t *testing.T) {
	t.Parallel()

	var calls atomic.Int32
	svc := &mockSTTService{
		transcribeFunc: func(_ context.Context, _ []byte, _ stt.TranscriptionConfig) (string, error) {
			n := calls.Add(1)
			if n <= 2 {
				return "", stt.NewTranscriptionError("mock-stt", "503", "service unavailable", nil, true)
			}
			return "Hello world", nil
		},
	}

	cfg := stage.DefaultSTTStageConfig()
	cfg.Retry = stt.RetryConfig{
		MaxAttempts:  3,
		InitialDelay: time.Millisecond,
		MaxDelay:     5 * time.Millisecond,
	}
	s := stage.NewSTTStage(svc, cfg)

	inputs := []stage.StreamElement{
		makeAudioElement(generateTestPCMAudio(3200), 16000),
	}
	results := runStage(t, s, inputs, 5*time.Second)

	if len(results) == 0 {
		t.Fatal("expected at least one output element, got 0 (STT retry did not recover)")
	}

	gotText := false
	for _, r := range results {
		if r.Text != nil && *r.Text == "Hello world" {
			gotText = true
		}
		if r.Error != nil {
			t.Errorf("unexpected error element: %v (retry should have recovered)", r.Error)
		}
	}
	if !gotText {
		t.Error("expected transcribed text from STT stage after retry, got none")
	}

	if c := calls.Load(); c != 3 {
		t.Errorf("STT service called %d times, want 3 (2 retries + 1 success)", c)
	}
}
