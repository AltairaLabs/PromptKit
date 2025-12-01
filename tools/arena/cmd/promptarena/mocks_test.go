package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/AltairaLabs/PromptKit/tools/arena/engine"
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

func TestLoadRunResults_FilePath(t *testing.T) {
	tmp := t.TempDir()
	file := filepath.Join(tmp, "single.json")

	run := engine.RunResult{
		RunID:      "run-1",
		ScenarioID: "s1",
		ProviderID: "p1",
	}
	writeJSON(t, file, run)

	results, err := loadRunResults(file, nil, nil)
	if err != nil {
		t.Fatalf("loadRunResults error: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
}

func TestLoadRunResults_NoMatches(t *testing.T) {
	tmp := t.TempDir()
	file := filepath.Join(tmp, "single.json")
	run := engine.RunResult{
		RunID:      "run-1",
		ScenarioID: "s1",
		ProviderID: "p1",
	}
	writeJSON(t, file, run)

	_, err := loadRunResults(tmp, []string{"other"}, nil)
	if err == nil {
		t.Fatalf("expected error for unmatched filters")
	}
}

func TestLoadRunResults_SkipsNonResultJSON(t *testing.T) {
	tmp := t.TempDir()
	writeFile(t, filepath.Join(tmp, "index.json"), `{"not":"a-run"}`)
	runFile := filepath.Join(tmp, "run.json")
	run := engine.RunResult{
		RunID:      "run-1",
		ScenarioID: "s1",
		ProviderID: "p1",
	}
	writeJSON(t, runFile, run)

	results, err := loadRunResults(tmp, nil, nil)
	if err != nil {
		t.Fatalf("loadRunResults error: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 result after skipping non-run files, got %d", len(results))
	}
}

func TestLoadRunResults_NoJSONFiles(t *testing.T) {
	tmp := t.TempDir()
	writeFile(t, filepath.Join(tmp, "readme.txt"), "hello")

	if _, err := loadRunResults(tmp, nil, nil); err == nil {
		t.Fatalf("expected error when no JSON files present")
	}
}

func writeJSON(t *testing.T, path string, run engine.RunResult) {
	t.Helper()
	data, err := json.Marshal(run)
	if err != nil {
		t.Fatalf("marshal run: %v", err)
	}
	writeFile(t, path, string(data))
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}
