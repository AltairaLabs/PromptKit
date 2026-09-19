package sdk

import (
	"bytes"
	"errors"
	"log/slog"
	"strings"
	"testing"

	"encoding/json"

	"github.com/AltairaLabs/PromptKit/runtime/v2/logger"
	"github.com/AltairaLabs/PromptKit/runtime/v2/mcp"
)

// captureWarnLogs redirects the package logger to a buffer at Warn level for
// the duration of the test.
func captureWarnLogs(t *testing.T) *bytes.Buffer {
	t.Helper()
	var buf bytes.Buffer
	logger.SetLogger(slog.New(slog.NewJSONHandler(&buf, &slog.HandlerOptions{Level: slog.LevelWarn})))
	t.Cleanup(func() { logger.SetLogger(nil) })
	return &buf
}

func mcpRegistryWithOneTool() *mockMCPRegistry {
	reg := newMockMCPRegistry()
	_ = reg.RegisterServer(mcp.ServerConfig{Name: "server1"})
	reg.tools["server1"] = []mcp.Tool{{
		Name:        "mcp_tool",
		Description: "An MCP tool",
		InputSchema: json.RawMessage(`{"type":"object"}`),
	}}
	return reg
}

func TestRegisterMCPExecutors_EnumerationFailureIsLogged(t *testing.T) {
	buf := captureWarnLogs(t)

	conv := newTestConversation()
	reg := mcpRegistryWithOneTool()
	reg.listErr = errors.New("dial tcp 127.0.0.1:9000: connection refused")
	conv.mcpRegistry = reg

	conv.registerMCPExecutors()

	out := buf.String()
	if !strings.Contains(out, "connection refused") {
		t.Fatalf("enumeration failure was not logged at warn: %q", out)
	}
	if !strings.Contains(out, "server1") {
		t.Errorf("log line does not name the servers whose tools are missing: %q", out)
	}
}

func TestRegisterMCPExecutors_EnumerationFailureDoesNotLatchGuard(t *testing.T) {
	captureWarnLogs(t)

	conv := newTestConversation()
	reg := mcpRegistryWithOneTool()
	reg.listErr = errors.New("connection refused")
	conv.mcpRegistry = reg

	conv.registerMCPExecutors()
	if conv.toolRegistry.Get("mcp__server1__mcp_tool") != nil {
		t.Fatal("tool registered despite enumeration failure")
	}

	// The server recovers; the next pipeline build must try again.
	reg.listErr = nil
	conv.registerMCPExecutors()

	if reg.listCalls != 2 {
		t.Errorf("ListAllTools called %d times, want 2 — a failed enumeration must not latch the guard", reg.listCalls)
	}
	if conv.toolRegistry.Get("mcp__server1__mcp_tool") == nil {
		t.Error("tool still missing after the registry recovered")
	}
}

func TestRegisterMCPExecutors_SuccessLatchesGuard(t *testing.T) {
	captureWarnLogs(t)

	conv := newTestConversation()
	reg := mcpRegistryWithOneTool()
	conv.mcpRegistry = reg

	conv.registerMCPExecutors()
	conv.registerMCPExecutors()

	if reg.listCalls != 1 {
		t.Errorf("ListAllTools called %d times, want 1 — a successful enumeration must not be repeated", reg.listCalls)
	}
}
