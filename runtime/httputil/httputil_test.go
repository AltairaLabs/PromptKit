package httputil_test

import (
	"testing"
	"time"

	"github.com/AltairaLabs/PromptKit/runtime/httputil"
)

func TestDefaultConstants(t *testing.T) {
	t.Parallel()

	if httputil.DefaultProviderTimeout != 60*time.Second {
		t.Fatalf("expected 60s, got %v", httputil.DefaultProviderTimeout)
	}
	if httputil.DefaultToolTimeout != 30*time.Second {
		t.Fatalf("expected 30s, got %v", httputil.DefaultToolTimeout)
	}
	if httputil.DefaultStreamingTimeout != 300*time.Second {
		t.Fatalf("expected 300s, got %v", httputil.DefaultStreamingTimeout)
	}
}

func TestNewHTTPClient(t *testing.T) {
	t.Parallel()

	client := httputil.NewHTTPClient(5 * time.Second)
	if client == nil {
		t.Fatal("expected non-nil client")
	}
	if client.Timeout != 5*time.Second {
		t.Fatalf("expected 5s timeout, got %v", client.Timeout)
	}
}
