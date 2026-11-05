package tts

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/AltairaLabs/PromptKit/runtime/v2/providers/base"
)

func TestQuotaErrors_MapToErrQuotaExceeded(t *testing.T) {
	cases := []struct {
		name      string
		provider  string
		status    int
		body      string
		wantQuota bool
		wantRate  bool
		retryable bool
	}{
		{"openai insufficient_quota", providerOpenAI, 429,
			`{"error":{"message":"You exceeded your current quota","type":"insufficient_quota","code":"insufficient_quota"}}`,
			true, false, true},
		{"openai rate_limit_exceeded", providerOpenAI, 429,
			`{"error":{"message":"slow down","type":"requests","code":"rate_limit_exceeded"}}`,
			false, true, true},
		{"elevenlabs 402 insufficient_credits", providerElevenLabs, 402,
			`{"detail":{"code":"insufficient_credits","message":"no credits"}}`, true, false, false},
		{"elevenlabs legacy quota_exceeded", providerElevenLabs, 401,
			`{"detail":{"status":"quota_exceeded","message":"quota"}}`, true, false, false},
		{"elevenlabs 429 concurrent", providerElevenLabs, 429,
			`{"detail":{"code":"concurrent_limit_exceeded","message":"busy"}}`, false, true, true},
		{"cartesia 402", providerCartesia, 402,
			`{"error":"payment_required","message":"out of credits"}`, true, false, false},
		{"cartesia 429", providerCartesia, 429,
			`{"error":"rate_limited","message":"too many"}`, false, true, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(tc.status)
				_, _ = w.Write([]byte(tc.body))
			}))
			defer srv.Close()

			var err error
			switch tc.provider {
			case providerOpenAI:
				_, err = NewOpenAI("k", base.WithBaseURL(srv.URL)).Synthesize(context.Background(), "hi", SynthesisConfig{})
			case providerElevenLabs:
				_, err = NewElevenLabs("k", base.WithBaseURL(srv.URL)).Synthesize(context.Background(), "hi", SynthesisConfig{})
			default:
				_, err = NewCartesia("k", base.WithBaseURL(srv.URL)).Synthesize(context.Background(), "hi", SynthesisConfig{})
			}
			if err == nil {
				t.Fatal("expected error")
			}
			if got := errors.Is(err, ErrQuotaExceeded); got != tc.wantQuota {
				t.Errorf("Is(ErrQuotaExceeded) = %v, want %v (err: %v)", got, tc.wantQuota, err)
			}
			if got := errors.Is(err, ErrRateLimited); got != tc.wantRate {
				t.Errorf("Is(ErrRateLimited) = %v, want %v", got, tc.wantRate)
			}
			var se *SynthesisError
			if !errors.As(err, &se) || se.Retryable != tc.retryable {
				t.Errorf("SynthesisError retryable = %+v, want %v", se, tc.retryable)
			}
		})
	}
}

func TestCartesiaWSErrorFrame_QuotaMapping(t *testing.T) {
	s := NewCartesia("k")
	_, err := s.processWSResponse(&cartesiaWSResponse{StatusCode: 402, Error: "insufficient credits"}, 0)
	if !errors.Is(err, ErrQuotaExceeded) {
		t.Errorf("402 frame: want ErrQuotaExceeded, got %v", err)
	}
	_, err = s.processWSResponse(&cartesiaWSResponse{StatusCode: 429, Error: "too many"}, 0)
	if !errors.Is(err, ErrRateLimited) || errors.Is(err, ErrQuotaExceeded) {
		t.Errorf("429 frame: want ErrRateLimited only, got %v", err)
	}
	var se *SynthesisError
	if !errors.As(err, &se) || !se.Retryable {
		t.Errorf("429 frame should be retryable")
	}
}
