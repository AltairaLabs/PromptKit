package sdk

import (
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/logger"
	"github.com/AltairaLabs/PromptKit/runtime/providers/mock"
	"github.com/AltairaLabs/PromptKit/runtime/statestore"
)

// captureHandler collects slog records for assertions.
type captureHandler struct {
	mu      sync.Mutex
	records []slog.Record
}

func (h *captureHandler) Enabled(context.Context, slog.Level) bool { return true }
func (h *captureHandler) Handle(_ context.Context, r slog.Record) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.records = append(h.records, r.Clone())
	return nil
}
func (h *captureHandler) WithAttrs([]slog.Attr) slog.Handler { return h }
func (h *captureHandler) WithGroup(string) slog.Handler      { return h }

func (h *captureHandler) messages() []string {
	h.mu.Lock()
	defer h.mu.Unlock()
	out := make([]string, 0, len(h.records))
	for _, r := range h.records {
		out = append(out, r.Message)
	}
	return out
}

// withCapturedLogs swaps in a capturing logger for the duration of the test.
func withCapturedLogs(t *testing.T) *captureHandler {
	t.Helper()
	h := &captureHandler{}
	logger.SetLogger(slog.New(h))
	t.Cleanup(func() { logger.SetLogger(nil) })
	return h
}

const inertWarning = "WithConversationID has no cross-process effect without a state store"

// TestWithConversationID_WarnsWhenInert covers the reported trap: an
// application sets a conversation id, gets no error, and gets no continuity
// between Open calls, because each Open without a configured store gets its own
// private memory store.
func TestWithConversationID_WarnsWhenInert(t *testing.T) {
	h := withCapturedLogs(t)

	warnIfConversationIDInert(&config{conversationID: "order-123"})

	assert.Contains(t, h.messages(), inertWarning,
		"an explicitly set conversation id with no store must not fail silently")
}

// TestWithConversationID_SilentWhenAStoreIsConfigured — the id is meaningful
// then, which is the whole point of supplying a store.
func TestWithConversationID_SilentWhenAStoreIsConfigured(t *testing.T) {
	h := withCapturedLogs(t)

	warnIfConversationIDInert(&config{
		conversationID: "order-123",
		stateStore:     statestore.NewMemoryStore(),
	})

	assert.NotContains(t, h.messages(), inertWarning)
}

// TestWithConversationID_SilentWhenNotSet guards against warning on the common
// path. A generated id is not a promise of continuity, so there is nothing to
// warn about.
func TestWithConversationID_SilentWhenNotSet(t *testing.T) {
	h := withCapturedLogs(t)

	warnIfConversationIDInert(&config{})

	assert.NotContains(t, h.messages(), inertWarning)
}

const inertProbePackJSON = `{
  "$schema": "https://promptpack.org/schema/latest/promptpack.schema.json",
  "id": "inert-probe",
  "name": "Inert Probe Pack",
  "version": "1.0.0",
  "description": "Minimal pack for the WithConversationID wiring test",
  "template_engine": {"version": "v1", "syntax": "{{variable}}"},
  "prompts": {
    "chat": {
      "id": "chat",
      "name": "Chat",
      "version": "1.0.0",
      "system_template": "You are helpful."
    }
  }
}`

// TestWithConversationID_WarningIsWiredIntoOpen is the wiring test.
//
// The three tests above call warnIfConversationIDInert directly, so they all
// still pass if the call site is deleted — the helper would just be dead code,
// caught only by the linter. This one goes through the real Open path, so it
// fails if the warning is never reached. That distinction is what the
// inert-declaration bugs in this repo keep turning on: a complete implementation
// with no producer.
func TestWithConversationID_WarningIsWiredIntoOpen(t *testing.T) {
	h := withCapturedLogs(t)

	dir := t.TempDir()
	packPath := filepath.Join(dir, "inert.pack.json")
	require.NoError(t, os.WriteFile(packPath, []byte(inertProbePackJSON), 0o600))

	conv, err := Open(packPath, "chat",
		WithProvider(mock.NewProvider("mock-inert", "mock-model", false)),
		WithConversationID("order-123"),
	)
	require.NoError(t, err)
	t.Cleanup(func() { _ = conv.Close() })

	assert.Contains(t, h.messages(), inertWarning,
		"Open must reach the warning; without the call site the helper is dead code")
}
