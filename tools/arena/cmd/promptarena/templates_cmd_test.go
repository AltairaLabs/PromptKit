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
apiVersion: v1
kind: TemplateIndex
spec:
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
	repoConfigPath = filepath.Join(dir, "repos.yaml")

	// List
	buf := &bytes.Buffer{}
	templatesListCmd.SetOut(buf)
	if err := templatesListCmd.RunE(templatesListCmd, nil); err != nil {
		t.Fatalf("list run: %v", err)
	}
	out := buf.String()
	if !strings.Contains(out, "TEMPLATE") || !strings.Contains(out, "VERSION") || !strings.Contains(out, "demo") {
		t.Fatalf("expected demo in list output, got: %q", out)
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

	templatesUpdateCmd.Flags().Set("index", indexPath)
	templatesUpdateCmd.Flags().Set("cache-dir", templateCache)
	if err := templatesUpdateCmd.RunE(templatesUpdateCmd, nil); err != nil {
		t.Fatalf("update run: %v", err)
	}
}

func TestTemplatesRepoCommands(t *testing.T) {
	dir := t.TempDir()
	repoConfigPath = filepath.Join(dir, "repos.yaml")

	// Add repo
	repoName = "local"
	repoURL = "https://example.com/index.yaml"
	buf := &bytes.Buffer{}
	templatesRepoAddCmd.SetOut(buf)
	if err := templatesRepoAddCmd.RunE(templatesRepoAddCmd, nil); err != nil {
		t.Fatalf("repo add: %v", err)
	}

	// List repos
	buf.Reset()
	templatesRepoListCmd.SetOut(buf)
	if err := templatesRepoListCmd.RunE(templatesRepoListCmd, nil); err != nil {
		t.Fatalf("repo list: %v", err)
	}
	output := buf.String()
	if !strings.Contains(output, "REPO") || !strings.Contains(output, "local") || !strings.Contains(output, "example.com") {
		t.Fatalf("unexpected list output: %s", output)
	}

	// Remove repo
	repoName = "local"
	buf.Reset()
	templatesRepoRemoveCmd.SetOut(buf)
	if err := templatesRepoRemoveCmd.RunE(templatesRepoRemoveCmd, nil); err != nil {
		t.Fatalf("repo remove: %v", err)
	}
}
