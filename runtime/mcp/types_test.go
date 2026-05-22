package mcp

import (
	"testing"

	"github.com/stretchr/testify/assert"
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

func TestServerConfig_Transport_ExplicitStreamable(t *testing.T) {
	cfg := ServerConfig{Name: "x", URL: "http://h", TransportName: TransportStreamableHTTP}
	assert.Equal(t, TransportStreamableHTTP, cfg.Transport())
}

func TestServerConfig_Transport_ExplicitSSE(t *testing.T) {
	cfg := ServerConfig{Name: "x", URL: "http://h", TransportName: TransportSSE}
	assert.Equal(t, TransportSSE, cfg.Transport())
}

func TestServerConfig_Transport_URLDefaultsToSSE_ForBackCompat(t *testing.T) {
	cfg := ServerConfig{Name: "x", URL: "http://h"}
	assert.Equal(t, TransportSSE, cfg.Transport())
}

func TestServerConfig_Transport_ExplicitStdioWithURL_HonoursExplicit(t *testing.T) {
	// If a caller explicitly opts into stdio, that wins over URL inference.
	cfg := ServerConfig{Name: "x", URL: "http://h", Command: "./foo", TransportName: TransportStdio}
	assert.Equal(t, TransportStdio, cfg.Transport())
}
