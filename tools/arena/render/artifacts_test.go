package render

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/AltairaLabs/PromptKit/tools/arena/engine"
)

func TestLoadRunArtifacts(t *testing.T) {
	dir := t.TempDir()

	// Manifests live at <outDir>/artifacts/<runID>/manifest.json and record only
	// {name, description, filename}; the loader joins Arena's base path.
	writeManifest := func(runID, body string) {
		d := filepath.Join(dir, "artifacts", runID)
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(d, "manifest.json"), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	writeManifest("run-1", `{"artifacts":[{"name":"Captured workspace","description":"the kit","filename":"sandbox"}]}`)
	writeManifest("run-2", `not json`)                     // malformed -> skipped
	writeManifest("run-4", `{"artifacts":[]}`)             // empty -> skipped
	writeManifest("run-5", `{"artifacts":[{"name":"x"}]}`) // no filename -> entry skipped

	results := []engine.RunResult{
		{RunID: "run-1"}, // valid
		{RunID: "run-2"}, // malformed
		{RunID: "run-3"}, // no manifest
		{RunID: "run-4"}, // empty
		{RunID: "run-5"}, // entry without filename
		{RunID: ""},      // no run id
	}

	got := loadRunArtifacts(dir, results)
	if len(got) != 1 {
		t.Fatalf("expected exactly run-1 to have artifacts, got %d (%v)", len(got), got)
	}
	arts := got["run-1"]
	// Arena owns the base: the report link is artifacts/<runID>/<filename>.
	if len(arts) != 1 || arts[0].Name != "Captured workspace" || arts[0].Path != "artifacts/run-1/sandbox" {
		t.Fatalf("unexpected artifacts for run-1: %+v", arts)
	}
	for _, skipped := range []string{"run-2", "run-3", "run-4", "run-5", ""} {
		if _, ok := got[skipped]; ok {
			t.Errorf("run %q should have no artifacts", skipped)
		}
	}
}
