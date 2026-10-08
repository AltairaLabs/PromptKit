package gemini

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/AltairaLabs/PromptKit/runtime/v2/providers"
	"github.com/AltairaLabs/PromptKit/runtime/v2/types"
)

// A media part that cannot be loaded is an error, and nothing is sent: the
// message used to go out as its text alone, so the model answered about media
// it never saw (#2216).
func TestUnloadableMedia_IsAnErrorNotATextFallback(t *testing.T) {
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hits.Add(1)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()
	p := NewProvider("g", "gemini-3.8-flash", srv.URL, providers.ProviderDefaults{}, false)

	ref, text := "no-store-ref", "describe it"
	req := providers.PredictionRequest{MaxTokens: 64, Messages: []types.Message{{Role: "user", Parts: []types.ContentPart{
		{Type: types.ContentTypeText, Text: &text},
		{Type: types.ContentTypeImage, Media: &types.MediaContent{StorageReference: &ref, MIMEType: "image/png"}},
	}}}}

	_, err := p.Predict(context.Background(), req)
	assert.ErrorContains(t, err, "message 0")
	_, err = p.PredictStream(context.Background(), req)
	assert.ErrorContains(t, err, "message 0")
	assert.Zero(t, hits.Load(), "a request went out without its image")
}
