package main

import (
	"strings"
	"testing"
)

func TestServeBindsToLocalhost(t *testing.T) {
	// The server address should bind to 127.0.0.1, not 0.0.0.0
	port := 8080
	addr := serveAddr(port)
	if !strings.HasPrefix(addr, "127.0.0.1:") {
		t.Fatalf("expected address to bind to 127.0.0.1, got %s", addr)
	}
}

func TestServeAddrFormat(t *testing.T) {
	addr := serveAddr(3000)
	if addr != "127.0.0.1:3000" {
		t.Fatalf("expected 127.0.0.1:3000, got %s", addr)
	}
}
