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
	dest, err := FetchTemplate(entry, filepath.Join(dir, "cache"))
	if err != nil {
		t.Fatalf("fetch: %v", err)
	}
	if _, err := os.Stat(dest); err != nil {
		t.Fatalf("cached file missing: %v", err)
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
	dest, err := FetchTemplate(entry, t.TempDir())
	if err != nil {
		t.Fatalf("fetch template: %v", err)
	}
	if dest == "" {
		t.Fatalf("expected dest path")
	}
}
