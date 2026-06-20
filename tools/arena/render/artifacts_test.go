package render

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/AltairaLabs/PromptKit/tools/arena/engine"
)

func TestLoadRunArtifacts(t *testing.T) {
	dir := t.TempDir()
	artDir := filepath.Join(dir, "artifacts")
	if err := os.MkdirAll(artDir, 0o755); err != nil {
		t.Fatal(err)
	}
	mustWrite := func(name, body string) {
		if err := os.WriteFile(filepath.Join(artDir, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	mustWrite("run-1.json", `{"artifacts":[{"name":"Captured workspace","description":"the kit","path":"kit/run-1/sandbox"}]}`)
	mustWrite("run-2.json", `not json`)         // malformed -> skipped
	mustWrite("run-4.json", `{"artifacts":[]}`) // empty -> skipped

	results := []engine.RunResult{
		{RunID: "run-1"}, // valid manifest
		{RunID: "run-2"}, // malformed
		{RunID: "run-3"}, // no manifest
		{RunID: "run-4"}, // empty manifest
		{RunID: ""},      // no run id
	}

	got := loadRunArtifacts(dir, results)
	if len(got) != 1 {
		t.Fatalf("expected exactly run-1 to have artifacts, got %d (%v)", len(got), got)
	}
	arts := got["run-1"]
	if len(arts) != 1 || arts[0].Name != "Captured workspace" || arts[0].Path != "kit/run-1/sandbox" {
		t.Fatalf("unexpected artifacts for run-1: %+v", arts)
	}
	for _, skipped := range []string{"run-2", "run-3", "run-4", ""} {
		if _, ok := got[skipped]; ok {
			t.Errorf("run %q should have no artifacts", skipped)
		}
	}
}
