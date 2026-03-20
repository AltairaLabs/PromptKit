package httputil_test

import (
	"crypto/tls"
	"net/http"
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

func TestNewHTTPClient_TLSAndConnectionLimits(t *testing.T) {
	t.Parallel()

	client := httputil.NewHTTPClient(10 * time.Second)
	if client.Transport == nil {
		t.Fatal("expected non-nil Transport")
	}

	transport, ok := client.Transport.(*http.Transport)
	if !ok {
		t.Fatal("expected Transport to be *http.Transport")
	}

	// Verify TLS 1.2 minimum
	if transport.TLSClientConfig == nil {
		t.Fatal("expected non-nil TLSClientConfig")
	}
	if transport.TLSClientConfig.MinVersion != tls.VersionTLS12 {
		t.Fatalf("expected TLS 1.2 minimum, got %d", transport.TLSClientConfig.MinVersion)
	}

	// Verify connection pool limits are set (non-zero)
	if transport.MaxIdleConns <= 0 {
		t.Fatalf("expected positive MaxIdleConns, got %d", transport.MaxIdleConns)
	}
	if transport.MaxIdleConnsPerHost <= 0 {
		t.Fatalf("expected positive MaxIdleConnsPerHost, got %d", transport.MaxIdleConnsPerHost)
	}
}
