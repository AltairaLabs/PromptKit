package sdk

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/v2/providers"
	"github.com/AltairaLabs/PromptKit/runtime/v2/providers/claude"
)

// A Send whose message carries more media than providers.MaxMediaItems fails
// with an error callers can match, and nothing reaches the provider (#2468).
func TestSend_OverMediaItemCapFailsBeforeTheProviderCall(t *testing.T) {
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hits.Add(1)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{}`))
	}))
	t.Cleanup(srv.Close)

	t.Setenv("ANTHROPIC_API_KEY", "k")
	provider := claude.NewToolProvider("caps", "claude-sonnet-4-6", srv.URL,
		providers.ProviderDefaults{MaxTokens: 64}, false)
	conv, err := Open(writeTestPack(t), "chat", WithProvider(provider), WithSkipSchemaValidation())
	require.NoError(t, err)
	t.Cleanup(func() { _ = conv.Close() })

	opts := make([]SendOption, 0, providers.MaxMediaItems+1)
	for i := 0; i <= providers.MaxMediaItems; i++ {
		opts = append(opts, WithImageData([]byte(fmt.Sprintf("image-%d", i)), "image/png"))
	}
	_, err = conv.Send(context.Background(), "describe these", opts...)
	require.ErrorIs(t, err, providers.ErrMediaItemLimitExceeded)
	assert.Zero(t, hits.Load(), "a request went out over the media item cap")
}
