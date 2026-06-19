package sandboxcapture_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/hooks"
	"github.com/AltairaLabs/PromptKit/tools/arena/sandboxcapture"
)

// knownZip is a minimal valid ZIP file (empty archive).
var knownZip = []byte{
	0x50, 0x4b, 0x05, 0x06, // end of central directory signature
	0x00, 0x00, 0x00, 0x00, // disk numbers
	0x00, 0x00, 0x00, 0x00, // entries
	0x00, 0x00, 0x00, 0x00, // central directory size
	0x00, 0x00, 0x00, 0x00, // central directory offset
	0x00, 0x00, // comment length
}

func TestHook_OnSessionEnd_CapturesZip(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/api/download", r.URL.Path)
		w.Header().Set("Content-Type", "application/zip")
		_, _ = w.Write(knownZip)
	}))
	defer ts.Close()

	outDir := t.TempDir()
	hook := sandboxcapture.New("", outDir)

	const sessionID = "test-session-001"
	ev := hooks.SessionEvent{
		SessionID:      sessionID,
		ConversationID: sessionID,
		Metadata: map[string]any{
			"sandbox_api_urls": map[string]string{
				"sandbox": ts.URL,
			},
		},
	}

	err := hook.OnSessionEnd(t.Context(), ev)
	require.NoError(t, err)

	dest := filepath.Join(outDir, "kit", sessionID, "sandbox.zip")
	data, readErr := os.ReadFile(dest)
	require.NoError(t, readErr)
	assert.Equal(t, knownZip, data)
}

func TestHook_OnSessionEnd_NoURLs_Noop(t *testing.T) {
	outDir := t.TempDir()
	hook := sandboxcapture.New("", outDir)

	ev := hooks.SessionEvent{
		SessionID:      "no-sandbox-session",
		ConversationID: "no-sandbox-session",
		Metadata:       map[string]any{},
	}

	err := hook.OnSessionEnd(t.Context(), ev)
	require.NoError(t, err)

	// Nothing should have been written.
	kitDir := filepath.Join(outDir, "kit")
	_, statErr := os.Stat(kitDir)
	assert.True(t, os.IsNotExist(statErr), "kit dir should not exist when no URLs provided")
}

func TestHook_OnSessionEnd_ServerFilter(t *testing.T) {
	captured := false
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		captured = true
		_, _ = w.Write(knownZip)
	}))
	defer ts.Close()

	outDir := t.TempDir()
	// Filter to "other-server" — should NOT hit our test server.
	hook := sandboxcapture.New("other-server", outDir)

	ev := hooks.SessionEvent{
		SessionID:      "filter-test",
		ConversationID: "filter-test",
		Metadata: map[string]any{
			"sandbox_api_urls": map[string]string{
				"sandbox": ts.URL, // different name — should be skipped
			},
		},
	}

	err := hook.OnSessionEnd(t.Context(), ev)
	require.NoError(t, err)
	assert.False(t, captured, "should not have captured when server name doesn't match")
}

func TestHook_Name(t *testing.T) {
	hook := sandboxcapture.New("", t.TempDir())
	assert.Equal(t, "sandbox-capture", hook.Name())
}

func TestHook_OnSessionStart_Noop(t *testing.T) {
	hook := sandboxcapture.New("", t.TempDir())
	err := hook.OnSessionStart(context.Background(), hooks.SessionEvent{})
	assert.NoError(t, err)
}

func TestHook_OnSessionUpdate_Noop(t *testing.T) {
	hook := sandboxcapture.New("", t.TempDir())
	err := hook.OnSessionUpdate(context.Background(), hooks.SessionEvent{})
	assert.NoError(t, err)
}

func TestHook_OnSessionEnd_HTTPError(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer ts.Close()

	outDir := t.TempDir()
	hook := sandboxcapture.New("", outDir)

	ev := hooks.SessionEvent{
		SessionID:      "err-session",
		ConversationID: "err-session",
		Metadata: map[string]any{
			"sandbox_api_urls": map[string]string{
				"sandbox": ts.URL,
			},
		},
	}

	err := hook.OnSessionEnd(t.Context(), ev)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "sandbox-capture")
	assert.Contains(t, err.Error(), "500")
}
