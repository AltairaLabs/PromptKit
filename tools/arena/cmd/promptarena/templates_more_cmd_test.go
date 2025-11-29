package main

import (
	"os"
	"path/filepath"
	"testing"
)

// Covers: templatesUpdateCmd error when a fetch fails, templatesRenderCmd missing version error,
// loadValuesFile parse error and mergeValues behavior via render.
func TestTemplates_Update_FetchError(t *testing.T) {
	t.Parallel()
	tmp := t.TempDir()
	// Create an index with a single entry pointing to a non-existent source.
	idxPath := filepath.Join(tmp, "index.yaml")
	idxContent := "apiVersion: promptkit.altairalabs.ai/v1\nkind: TemplateIndex\nspec:\n  entries:\n    - name: bad\n      version: 1.0.0\n      source: " + filepath.Join(tmp, "missing.yaml") + "\n"
	if err := os.WriteFile(idxPath, []byte(idxContent), 0o600); err != nil {
		t.Fatalf("write index: %v", err)
	}
	// Point flags
	templateIndex = idxPath
	templateCache = t.TempDir()
	// Run update expecting error due to missing source.
	if err := templatesUpdateCmd.RunE(templatesUpdateCmd, nil); err == nil {
		t.Fatalf("expected error from update when fetch fails")
	}
}

func TestTemplates_Render_MissingVersionFromCache(t *testing.T) {
	t.Parallel()
	templateName = "repo/name"
	templateVersion = ""
	templateCache = t.TempDir()
	// Expect error about required --version when rendering from cache
	if err := templatesRenderCmd.RunE(templatesRenderCmd, nil); err == nil {
		t.Fatalf("expected error requiring version when rendering from cache")
	}
}

func TestLoadValuesFile_ParseError(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	bad := filepath.Join(dir, "values.yaml")
	if err := os.WriteFile(bad, []byte("x: [broken"), 0o600); err != nil {
		t.Fatalf("write bad values: %v", err)
	}
	if _, err := loadValuesFile(bad); err == nil {
		t.Fatalf("expected parse error from values file")
	}
}

func TestMergeValues_OverrideWins(t *testing.T) {
	t.Parallel()
	base := map[string]string{"a": "1", "b": "2"}
	override := map[string]string{"b": "3", "c": "4"}
	out := mergeValues(base, override)
	if out["a"] != "1" || out["b"] != "3" || out["c"] != "4" {
		t.Fatalf("mergeValues unexpected: %#v", out)
	}
}
