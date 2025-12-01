package main

import (
	"os"
	"path/filepath"
	"testing"
)

// Basic coverage for loadRunResults filtering using staged fixtures.
func TestLoadRunResults_Filtering(t *testing.T) {
	tmp := t.TempDir()

	fixtures := []string{
		filepath.Join("..", "..", "templates", "testdata", "2025-11-30T19-49Z_openai-gpt4o_default_hardware-faults_18c25790.json"),
		filepath.Join("..", "..", "templates", "testdata", "2025-11-30T19-49Z_openai-gpt4o_default_redteam-selfplay_83be345a.json"),
	}

	for _, src := range fixtures {
		base := filepath.Base(src)
		dst := filepath.Join(tmp, base)
		data, err := os.ReadFile(src)
		if err != nil {
			t.Fatalf("read fixture %s: %v", src, err)
		}
		if err := os.WriteFile(dst, data, 0o644); err != nil {
			t.Fatalf("write temp fixture %s: %v", dst, err)
		}
	}

	results, err := loadRunResults(tmp, []string{"hardware-faults"}, nil)
	if err != nil {
		t.Fatalf("loadRunResults error: %v", err)
	}

	if len(results) != 1 {
		t.Fatalf("expected 1 result after filtering, got %d", len(results))
	}

	if results[0].ScenarioID != "hardware-faults" {
		t.Fatalf("unexpected ScenarioID: %s", results[0].ScenarioID)
	}
}
