package json

import (
	"os"
	"path/filepath"
	"testing"
)

// Issue #2081: spec.name, when set, is the function name; metadata.name is
// only the fallback.
func TestJSONToolRepository_SpecNameIsFunctionName(t *testing.T) {
	tmpDir := t.TempDir()
	toolFile := filepath.Join(tmpDir, "save-order.json")
	content := `{
  "apiVersion": "promptkit.altairalabs.ai/v1alpha1",
  "kind": "Tool",
  "metadata": {"name": "save-order"},
  "spec": {
    "name": "save_order",
    "description": "Save an order",
    "input_schema": {"type": "object"},
    "mode": "mock"
  }
}`
	if err := os.WriteFile(toolFile, []byte(content), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	repo := NewJSONToolRepository(tmpDir)
	if err := repo.LoadToolFromFile(toolFile); err != nil {
		t.Fatalf("LoadToolFromFile: %v", err)
	}

	got, err := repo.LoadTool("save_order")
	if err != nil {
		t.Fatalf("LoadTool(save_order): %v", err)
	}
	if got.Name != "save_order" {
		t.Errorf("Name = %q, want save_order", got.Name)
	}
	if _, err := repo.LoadTool("save-order"); err == nil {
		t.Error("tool also registered under resource name save-order")
	}
}
