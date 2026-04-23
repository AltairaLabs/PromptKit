package mcp

import (
	"testing"
)

func TestServerConfig_Transport_Stdio(t *testing.T) {
	cfg := ServerConfig{Name: "x", Command: "./foo"}
	if got := cfg.Transport(); got != TransportStdio {
		t.Errorf("Transport() = %q, want %q", got, TransportStdio)
	}
}

func TestServerConfig_Transport_SSE(t *testing.T) {
	cfg := ServerConfig{Name: "x", URL: "https://x"}
	if got := cfg.Transport(); got != TransportSSE {
		t.Errorf("Transport() = %q, want %q", got, TransportSSE)
	}
}

func TestServerConfig_Transport_URLTakesPrecedence(t *testing.T) {
	// Belt-and-suspenders: if both are somehow set (validator should reject),
	// URL wins — SSE is the higher-intent transport.
	cfg := ServerConfig{Name: "x", Command: "./foo", URL: "https://x"}
	if got := cfg.Transport(); got != TransportSSE {
		t.Errorf("Transport() = %q, want %q", got, TransportSSE)
	}
}

func TestServerConfig_Transport_Unknown(t *testing.T) {
	cfg := ServerConfig{Name: "x"}
	if got := cfg.Transport(); got != TransportUnknown {
		t.Errorf("Transport() = %q, want %q", got, TransportUnknown)
	}
}
