// Package sandboxcapture captures the sandbox workspace after a session ends.
// It reads the sandbox_api_urls map from the SessionEvent metadata and issues
// an HTTP GET <url>/api/download for each entry, streaming the response body
// to out/<outBase>/kit/<sessionID>/<serverName>.zip.
//
// This is out-of-band capture via the sandbox HTTP API — no MCP tool, no LLM.
// The capture runs in OnSessionEnd while the container is still alive (cleanup
// runs after the session hook chain).
package sandboxcapture

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/AltairaLabs/PromptKit/runtime/hooks"
)

const (
	captureTimeout = 60 * time.Second
	dirPerm        = 0o750
)

// Hook implements hooks.SessionHook and captures the sandbox workspace zip
// from each session-scoped docker source that exposes an HTTP API.
type Hook struct {
	// server, when non-empty, limits capture to the named sandbox server.
	// When empty, all entries in sandbox_api_urls are captured.
	server string
	// outBase is the base output directory (same as --out flag). Zips land at
	// <outBase>/kit/<sessionID>/<serverName>.zip.
	outBase string
}

// New returns a Hook that captures the sandbox workspace zip at session end.
//
// server filters which sandbox server to capture (empty = capture all).
// outBase is the arena output directory (the --out flag value).
func New(server, outBase string) *Hook {
	return &Hook{server: server, outBase: outBase}
}

// Name returns the hook name.
func (h *Hook) Name() string { return "sandbox-capture" }

// OnSessionStart is a no-op; capture happens at session end.
func (h *Hook) OnSessionStart(_ context.Context, _ hooks.SessionEvent) error { return nil }

// OnSessionUpdate is a no-op; capture happens at session end.
func (h *Hook) OnSessionUpdate(_ context.Context, _ hooks.SessionEvent) error { return nil }

// OnSessionEnd downloads the workspace zip from each sandbox server that
// exposes an API URL. The container is still alive at this point — cleanup
// of the MCP source (docker stop) runs after the hook chain completes.
//
// Errors from individual downloads are collected and returned as a joined
// error so all servers are attempted regardless of individual failures.
func (h *Hook) OnSessionEnd(ctx context.Context, ev hooks.SessionEvent) error {
	urls, _ := ev.Metadata["sandbox_api_urls"].(map[string]string)
	if len(urls) == 0 {
		return nil
	}

	captureCtx, cancel := context.WithTimeout(ctx, captureTimeout)
	defer cancel()

	var errs []error
	for name, apiURL := range urls {
		if h.server != "" && h.server != name {
			continue
		}
		if err := h.download(captureCtx, ev.SessionID, name, apiURL); err != nil {
			errs = append(errs, fmt.Errorf("sandbox-capture %q: %w", name, err))
		}
	}
	return errors.Join(errs...)
}

// download fetches <apiURL>/api/download and writes the response body to
// <outBase>/kit/<sessionID>/<name>.zip.
func (h *Hook) download(ctx context.Context, sessionID, name, apiURL string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, apiURL+"/api/download", http.NoBody)
	if err != nil {
		return fmt.Errorf("build request: %w", err)
	}
	// The sandbox API's identity middleware (WithIdentity) rejects requests
	// without a trusted identity header. We're the trusted local caller, so
	// supply a placeholder X-Forwarded-Sub to satisfy it (any non-empty value
	// is accepted; the sandbox does not verify it).
	req.Header.Set("X-Forwarded-Sub", "arena-capture")

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("GET /api/download: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("GET /api/download returned %d", resp.StatusCode)
	}

	dest := filepath.Join(h.outBase, "kit", sessionID, name+".zip")
	destDir := filepath.Dir(dest)
	if mkErr := os.MkdirAll(destDir, dirPerm); mkErr != nil {
		return fmt.Errorf("mkdir %s: %w", destDir, mkErr)
	}

	f, createErr := os.Create(dest) //nolint:gosec // dest is built from trusted config values
	if createErr != nil {
		return fmt.Errorf("create %s: %w", dest, createErr)
	}
	defer func() { _ = f.Close() }()

	if _, copyErr := io.Copy(f, resp.Body); copyErr != nil {
		return fmt.Errorf("write %s: %w", dest, copyErr)
	}
	return nil
}
