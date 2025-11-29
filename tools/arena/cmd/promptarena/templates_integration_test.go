package main

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

// Integration-style test that uses a file-based index to exercise init with remote templates.
func TestInitWithRemoteTemplateIndex(t *testing.T) {
	dir := t.TempDir()

	// Build a minimal template package in the temp dir
	tplContent := `apiVersion: promptkit.altairalabs.ai/v1alpha1
kind: Template
metadata:
  name: demo
spec:
  variables:
    - name: project_name
      required: true
      default: demo-project
  files:
    - path: arena.yaml
      content: "apiVersion: promptkit.altairalabs.ai/v1alpha1\nkind: Arena\nspec:\n  providers: []\n  scenarios: []\n  metadata:\n    name: {{ .project_name }}\n"
`
	tplPath := filepath.Join(dir, "template.yaml")
	if err := os.WriteFile(tplPath, []byte(tplContent), 0o644); err != nil {
		t.Fatalf("write template: %v", err)
	}

	index := `apiVersion: promptkit.altairalabs.ai/v1
kind: TemplateIndex
spec:
  entries:
    - name: demo
      version: "1.0.0"
      description: demo
      source: ` + tplPath + `
`
	indexPath := filepath.Join(dir, "index.yaml")
	if err := os.WriteFile(indexPath, []byte(index), 0o644); err != nil {
		t.Fatalf("write index: %v", err)
	}

	// Point defaults to our file-based index/cache
	initTemplate = "demo"
	initTemplateIndex = indexPath
	initTemplateCache = filepath.Join(dir, "cache")
	initQuick = true
	initOutputDir = filepath.Join(dir, "out")

	if err := runInit(initCmd, []string{"demo-project"}); err != nil {
		t.Fatalf("init with remote index failed: %v", err)
	}

	// Verify arena.yaml was rendered
	arenaPath := filepath.Join(initOutputDir, "demo-project", "arena.yaml")
	data, err := os.ReadFile(arenaPath)
	if err != nil {
		t.Fatalf("read arena: %v", err)
	}
	if !bytes.Contains(data, []byte("demo-project")) {
		t.Fatalf("rendered arena missing project name")
	}
}
