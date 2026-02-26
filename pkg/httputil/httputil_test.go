package httputil_test

import (
	"testing"
	"time"

	"github.com/AltairaLabs/PromptKit/pkg/httputil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDefaultConstants(t *testing.T) {
	t.Parallel()

	assert.Equal(t, 60*time.Second, httputil.DefaultProviderTimeout, "provider timeout should be 60s")
	assert.Equal(t, 30*time.Second, httputil.DefaultToolTimeout, "tool timeout should be 30s")
}

func TestNewHTTPClient(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		timeout time.Duration
	}{
		{"provider timeout", httputil.DefaultProviderTimeout},
		{"tool timeout", httputil.DefaultToolTimeout},
		{"custom timeout", 5 * time.Second},
		{"zero timeout", 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			client := httputil.NewHTTPClient(tt.timeout)
			require.NotNil(t, client, "returned client must not be nil")
			assert.Equal(t, tt.timeout, client.Timeout, "client timeout must match requested value")
		})
	}
}
