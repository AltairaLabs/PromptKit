package tts

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/AltairaLabs/PromptKit/runtime/v2/providers/base"
)

// rateLimitedServer answers every request 429 with Retry-After: 7.
func rateLimitedServer(t *testing.T) string {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Retry-After", "7")
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = w.Write([]byte(`{"error":{"message":"slow down","code":"rate_limit"}}`))
	}))
	t.Cleanup(srv.Close)
	return srv.URL
}

// A 429's Retry-After reaches the SynthesisError, so SynthesizeWithRetry
// waits what the provider asked for, on both the OpenAI path and the shared
// JSON-POST path ElevenLabs, Cartesia and Gemini use.
func TestSynthesize_RateLimitCarriesRetryAfter(t *testing.T) {
	url := rateLimitedServer(t)
	for name, svc := range map[string]Service{
		"openai":     NewOpenAI("k", base.WithBaseURL(url)),
		"elevenlabs": NewElevenLabs("k", base.WithBaseURL(url)),
	} {
		t.Run(name, func(t *testing.T) {
			_, err := svc.Synthesize(context.Background(), "hi", SynthesisConfig{Voice: "v"})
			var se *SynthesisError
			if !errors.As(err, &se) {
				t.Fatalf("err = %v (%T), want a *SynthesisError", err, err)
			}
			if se.RetryAfter != 7*time.Second {
				t.Errorf("RetryAfter = %v, want 7s", se.RetryAfter)
			}
		})
	}
}
