package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestTemplatesListAndFetchAndRender(t *testing.T) {
	dir := t.TempDir()

	// Create template package
	tplContent := `
files:
  - path: README.md
    content: "Hello {{.name}}"
`
	tplPath := filepath.Join(dir, "pkg.yaml")
	if err := os.WriteFile(tplPath, []byte(tplContent), 0o644); err != nil {
		t.Fatalf("write tpl: %v", err)
	}

	// Index file
	index := `
entries:
  - name: demo
    version: "1.0.0"
    description: sample
    source: "` + tplPath + `"
`
	indexPath := filepath.Join(dir, "index.yaml")
	if err := os.WriteFile(indexPath, []byte(index), 0o644); err != nil {
		t.Fatalf("write index: %v", err)
	}

	templateCache = filepath.Join(dir, "cache")
	templateIndex = indexPath

	// List
	buf := &bytes.Buffer{}
	templatesListCmd.SetOut(buf)
	if err := templatesListCmd.RunE(templatesListCmd, nil); err != nil {
		t.Fatalf("list run: %v", err)
	}
	if !strings.Contains(buf.String(), "demo") {
		t.Fatalf("expected demo in list output")
	}

	// Fetch
	templatesFetchCmd.Flags().Set("template", "demo")
	templatesFetchCmd.Flags().Set("version", "1.0.0")
	if err := templatesFetchCmd.RunE(templatesFetchCmd, nil); err != nil {
		t.Fatalf("fetch run: %v", err)
	}

	// Render from cache
	templatesRenderCmd.Flags().Set("template", "demo")
	templatesRenderCmd.Flags().Set("version", "1.0.0")
	templatesRenderCmd.Flags().Set("out", filepath.Join(dir, "out"))
	valsFile := filepath.Join(dir, "values.yaml")
	if err := os.WriteFile(valsFile, []byte("name: world\n"), 0o644); err != nil {
		t.Fatalf("write values: %v", err)
	}

	templatesRenderCmd.Flags().Set("values", valsFile)
	if err := templatesRenderCmd.RunE(templatesRenderCmd, nil); err != nil {
		t.Fatalf("render run: %v", err)
	}
	data, err := os.ReadFile(filepath.Join(dir, "out", "README.md"))
	if err != nil {
		t.Fatalf("read rendered: %v", err)
	}
	if string(data) != "Hello world" {
		t.Fatalf("unexpected render: %s", string(data))
	}
}
