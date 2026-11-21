package stt_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/AltairaLabs/PromptKit/runtime/v2/providers/base"
	"github.com/AltairaLabs/PromptKit/runtime/v2/stt"
)

// A 429's Retry-After reaches the TranscriptionError, so TranscribeWithRetry
// waits what the provider asked for, on the OpenAI path and on the shared
// batch-upload path the other vendors use.
func TestTranscribe_RateLimitCarriesRetryAfter(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Retry-After", "7")
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = w.Write([]byte(`{"error":{"message":"slow down","code":"rate_limit"}}`))
	}))
	defer srv.Close()

	for name, svc := range map[string]stt.Service{
		"openai":   stt.NewOpenAI("k", base.WithBaseURL(srv.URL)),
		"cartesia": stt.NewCartesia("k", base.WithBaseURL(srv.URL)),
	} {
		t.Run(name, func(t *testing.T) {
			_, err := svc.TranscribeBytes(context.Background(), pcmTurn(4800), stt.TranscriptionConfig{
				Format: stt.FormatPCM, SampleRate: 24000,
			})
			var te *stt.TranscriptionError
			if !errors.As(err, &te) {
				t.Fatalf("err = %v (%T), want a *TranscriptionError", err, err)
			}
			if te.RetryAfter != 7*time.Second {
				t.Errorf("RetryAfter = %v, want 7s", te.RetryAfter)
			}
		})
	}
}
