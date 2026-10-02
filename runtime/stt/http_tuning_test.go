package stt_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/v2/credentials"
	"github.com/AltairaLabs/PromptKit/runtime/v2/providers/base"
	"github.com/AltairaLabs/PromptKit/runtime/v2/stt"
)

func TestSTTCreateFromSpec_OpenAIRequestTimeoutApplies(t *testing.T) {
	release := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-time.After(300 * time.Millisecond):
		case <-release:
		}
		_, _ = w.Write([]byte(`{"text":"late"}`))
	}))
	defer srv.Close()
	defer close(release)

	svc, err := stt.CreateFromSpec(stt.ProviderSpec{
		Type: "openai", BaseURL: srv.URL,
		Credential: credentials.NewAPIKeyCredential("sk-test"),
		Tuning:     base.HTTPTuning{RequestTimeout: 50 * time.Millisecond},
	})
	require.NoError(t, err)

	start := time.Now()
	_, err = svc.Transcribe(context.Background(),
		base.STTRequest{Audio: []byte{1, 2, 3, 4}, MIMEType: "audio/wav"})
	elapsed := time.Since(start)
	require.Error(t, err)
	assert.Less(t, elapsed, 250*time.Millisecond, "the 50ms request_timeout must cut the call short")
}
