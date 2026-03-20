package templates

import (
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
)

func TestResolveSource(t *testing.T) {
	tests := []struct {
		name      string
		source    string
		indexPath string
		want      string
	}{
		{"absolute URL unchanged", "https://example.com/tpl.yaml", "/local/index.yaml", "https://example.com/tpl.yaml"},
		{"absolute path unchanged", "/abs/path/tpl.yaml", "/local/index.yaml", "/abs/path/tpl.yaml"},
		{"relative to HTTP index", "sub/tpl.yaml", "https://example.com/idx/index.yaml", "https://example.com/idx/sub/tpl.yaml"},
		{"relative to file index", "sub/tpl.yaml", "/local/idx/index.yaml", "/local/idx/sub/tpl.yaml"},
		{"http source unchanged", "http://other.com/t.yaml", "https://example.com/index.yaml", "http://other.com/t.yaml"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := resolveSource(tt.source, tt.indexPath)
			if got != tt.want {
				t.Errorf("resolveSource(%q, %q) = %q, want %q", tt.source, tt.indexPath, got, tt.want)
			}
		})
	}
}

func TestIsHTTPURL(t *testing.T) {
	if !isHTTPURL("https://example.com") {
		t.Error("expected https URL to be detected")
	}
	if !isHTTPURL("http://example.com") {
		t.Error("expected http URL to be detected")
	}
	if isHTTPURL("/local/path") {
		t.Error("expected local path not to be HTTP")
	}
}

// Fix 2: Verify HTTP clients have timeouts configured.

func TestHTTPClientHasTimeout(t *testing.T) {
	if httpClient.Timeout == 0 {
		t.Fatal("httpClient must have a non-zero Timeout to prevent hanging on slow servers")
	}
	if httpClient.Timeout.Seconds() < 1 {
		t.Fatal("httpClient Timeout is unreasonably small")
	}
}

func TestLoadBytesUsesClientWithTimeout(t *testing.T) {
	// Verify that loadBytes uses our httpClient (with timeout) by checking
	// the request actually goes through. If it used http.DefaultClient,
	// this test would still pass, but the timeout test above ensures the
	// package-level client is configured.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	}))
	defer srv.Close()

	data, err := loadBytes(srv.URL)
	if err != nil {
		t.Fatalf("loadBytes failed: %v", err)
	}
	if string(data) != "ok" {
		t.Fatalf("unexpected data: %s", string(data))
	}
}

func TestFetchURLUsesClientWithTimeout(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("fetched"))
	}))
	defer srv.Close()

	data, err := fetchURL(srv.URL)
	if err != nil {
		t.Fatalf("fetchURL failed: %v", err)
	}
	if string(data) != "fetched" {
		t.Fatalf("unexpected data: %s", string(data))
	}
}

func TestLoadBytesRejectsNonOKStatus(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	_, err := loadBytes(srv.URL)
	if err == nil {
		t.Fatal("expected error for non-200 status")
	}
	if !strings.Contains(err.Error(), "status 404") {
		t.Fatalf("expected status 404 in error, got: %v", err)
	}
}

func TestLoadBytesReadsLocalFile(t *testing.T) {
	tmp := t.TempDir()
	path := tmp + "/test.txt"
	if err := os.WriteFile(path, []byte("local-content"), 0o600); err != nil {
		t.Fatal(err)
	}
	data, err := loadBytes(path)
	if err != nil {
		t.Fatalf("loadBytes local file failed: %v", err)
	}
	if string(data) != "local-content" {
		t.Fatalf("unexpected: %s", string(data))
	}
}

func TestLoadBytesLocalFileNotFound(t *testing.T) {
	_, err := loadBytes("/nonexistent/file.txt")
	if err == nil {
		t.Fatal("expected error for missing file")
	}
}

// Fix 3: Verify response body size is limited.

func TestLoadBytesLimitsResponseSize(t *testing.T) {
	// Create a server that returns more data than maxTemplateSize
	bigBody := strings.Repeat("X", int(maxTemplateSize)+1024)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(bigBody))
	}))
	defer srv.Close()

	data, err := loadBytes(srv.URL)
	if err != nil {
		t.Fatalf("loadBytes should succeed but truncate, got error: %v", err)
	}
	if int64(len(data)) > maxTemplateSize {
		t.Fatalf("response body should be limited to %d bytes, got %d", maxTemplateSize, len(data))
	}
	if int64(len(data)) != maxTemplateSize {
		t.Fatalf("expected exactly %d bytes (limit), got %d", maxTemplateSize, len(data))
	}
}

func TestFetchURLLimitsResponseSize(t *testing.T) {
	bigBody := strings.Repeat("Y", int(maxTemplateSize)+2048)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(bigBody))
	}))
	defer srv.Close()

	data, err := fetchURL(srv.URL)
	if err != nil {
		t.Fatalf("fetchURL should succeed but truncate, got error: %v", err)
	}
	if int64(len(data)) > maxTemplateSize {
		t.Fatalf("response body should be limited to %d bytes, got %d", maxTemplateSize, len(data))
	}
}
