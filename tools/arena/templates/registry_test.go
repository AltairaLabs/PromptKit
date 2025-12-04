package templates

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func TestLoadIndexAndFindEntry(t *testing.T) {
	dir := t.TempDir()
	indexPath := filepath.Join(dir, "index.yaml")
	content := `
apiVersion: promptkit.altairalabs.ai/v1
kind: TemplateIndex
spec:
  entries:
    - name: demo
      version: "1.0.0"
      description: sample
      tags: [demo]
      source: ./pkg.yaml
      checksum: ""
`
	if err := os.WriteFile(indexPath, []byte(content), 0o644); err != nil {
		t.Fatalf("write index: %v", err)
	}
	idx, err := LoadIndex(indexPath)
	if err != nil {
		t.Fatalf("load index: %v", err)
	}
	entry, err := idx.FindEntry("demo", "1.0.0")
	if err != nil {
		t.Fatalf("find entry: %v", err)
	}
	if entry.Description != "sample" {
		t.Fatalf("wrong description")
	}
	if _, err := idx.FindEntry("missing", ""); err == nil {
		t.Fatalf("expected not found error")
	}
}

func TestLoadIndexValidationFailures(t *testing.T) {
	dir := t.TempDir()
	// missing kind/apiVersion
	bad := `spec: { entries: [] }`
	p := filepath.Join(dir, "bad.yaml")
	if err := os.WriteFile(p, []byte(bad), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	if _, err := LoadIndex(p); err == nil {
		t.Fatalf("expected error for missing fields")
	}

	// wrong version
	wrong := `apiVersion: v2\nkind: TemplateIndex\nspec: { entries: [{name: a, version: "1.0.0", source: ./x}]}`
	p2 := filepath.Join(dir, "wrong.yaml")
	if err := os.WriteFile(p2, []byte(wrong), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	if _, err := LoadIndex(p2); err == nil {
		t.Fatalf("expected error for unsupported version")
	}

	// wrong kind
	wrongKind := `apiVersion: promptkit.altairalabs.ai/v1\nkind: NotIndex\nspec: { entries: [{name: a, version: "1.0.0", source: ./x}]}`
	p3 := filepath.Join(dir, "wrongkind.yaml")
	if err := os.WriteFile(p3, []byte(wrongKind), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	if _, err := LoadIndex(p3); err == nil {
		t.Fatalf("expected error for wrong kind")
	}

	// empty entries
	empty := `apiVersion: promptkit.altairalabs.ai/v1\nkind: TemplateIndex\nspec: { entries: [] }`
	p4 := filepath.Join(dir, "empty.yaml")
	if err := os.WriteFile(p4, []byte(empty), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	if _, err := LoadIndex(p4); err == nil {
		t.Fatalf("expected error for empty entries")
	}
}

func TestLoadBytesHTTPNon200(t *testing.T) {
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTeapot)
	}))
	defer s.Close()
	if _, err := LoadIndex(s.URL); err == nil {
		t.Fatalf("expected error on non-200")
	}
}

func TestFetchTemplateChecksumMismatch(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "pkg.yaml")
	if err := os.WriteFile(src, []byte("files: []"), 0o644); err != nil {
		t.Fatalf("write pkg: %v", err)
	}
	entry := &IndexEntry{Name: "demo", Version: "1.0.0", Source: src, Checksum: "deadbeef"}
	if _, err := FetchTemplate(entry, filepath.Join(dir, "cache"), "", "test"); err == nil {
		t.Fatalf("expected checksum mismatch error")
	}
}

func TestValidateChecksum(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "file.txt")
	if err := os.WriteFile(p, []byte("hello"), 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}
	// sha256 of "hello"
	sum := "2cf24dba5fb0a30e26e83b2ac5b9e29e1b161e5c1fa7425e73043362938b9824"
	if err := ValidateChecksum(p, sum); err != nil {
		t.Fatalf("expected checksum match, got %v", err)
	}
	if err := ValidateChecksum(p, "deadbeef"); err == nil {
		t.Fatalf("expected checksum mismatch")
	}

	// bytes validation
	if err := validateChecksumBytes([]byte("hello"), sum); err != nil {
		t.Fatalf("bytes checksum failed: %v", err)
	}
}

func TestFetchTemplate(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "pkg.yaml")
	if err := os.WriteFile(src, []byte("files: []"), 0o644); err != nil {
		t.Fatalf("write pkg: %v", err)
	}
	entry := &IndexEntry{Name: "demo", Version: "1.0.0", Source: src}
	dest, err := FetchTemplate(entry, filepath.Join(dir, "cache"), "", "test")
	if err != nil {
		t.Fatalf("fetch: %v", err)
	}
	if _, err := os.Stat(dest); err != nil {
		t.Fatalf("cached file missing: %v", err)
	}
}

func TestDefaultCacheDirCreatesPath(t *testing.T) {
	// Reset package-level cache dir
	defaultCacheDir = ""
	dir := DefaultCacheDir()
	if dir == "" {
		t.Fatalf("expected default cache dir")
	}
}

func TestRenderDryRun(t *testing.T) {
	pkg := &TemplatePackage{
		Files: []TemplateFile{
			{Path: "arena.yaml", Content: "name: {{.project}}"},
			{Path: "README.md", Content: "Hello {{.project}}"},
		},
	}
	out := t.TempDir()
	err := RenderDryRun(pkg, map[string]string{"project": "demo"}, out)
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	data, err := os.ReadFile(filepath.Join(out, "arena.yaml"))
	if err != nil {
		t.Fatalf("read rendered: %v", err)
	}
	if string(data) != "name: demo" {
		t.Fatalf("unexpected render: %s", string(data))
	}
}

func TestLoadIndexFromHTTP(t *testing.T) {
	// simulate remote index
	index := `
apiVersion: promptkit.altairalabs.ai/v1
kind: TemplateIndex
spec:
  entries:
    - name: demo
      version: "1.0.0"
      description: remote
      source: http://example.com/template.yaml
`
	tpl := `
files:
  - path: README.md
    content: ok
`
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/index.yaml":
			_, _ = w.Write([]byte(index))
		case "/template.yaml":
			_, _ = w.Write([]byte(tpl))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer s.Close()

	idx, err := LoadIndex(s.URL + "/index.yaml")
	if err != nil {
		t.Fatalf("load remote index: %v", err)
	}
	entry, err := idx.FindEntry("demo", "1.0.0")
	if err != nil {
		t.Fatalf("find entry: %v", err)
	}
	entry.Source = s.URL + "/template.yaml"
	dest, err := FetchTemplate(entry, t.TempDir(), "", "test")
	if err != nil {
		t.Fatalf("fetch template: %v", err)
	}
	if dest == "" {
		t.Fatalf("expected dest path")
	}
}
