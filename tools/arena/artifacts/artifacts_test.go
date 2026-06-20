package artifacts

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestLocalStore(t *testing.T) {
	ref, err := NewLocal().Store(context.Background(), "run-1", "sandbox", "/anywhere/run-1/sandbox")
	if err != nil {
		t.Fatal(err)
	}
	if ref != "artifacts/run-1/sandbox" {
		t.Fatalf("ref = %q, want artifacts/run-1/sandbox", ref)
	}
}

func writeStaged(t *testing.T, dir, body string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, manifestName), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestIngest_ResolvesStagedManifest(t *testing.T) {
	dir := t.TempDir()
	writeStaged(t, dir, `{"artifacts":[`+
		`{"name":"Captured workspace","description":"the kit","filename":"sandbox"},`+
		`{"name":"no file","description":"skipped","filename":""}]}`)

	if err := Ingest(context.Background(), NewLocal(), dir, "run-1"); err != nil {
		t.Fatalf("Ingest: %v", err)
	}

	raw, err := os.ReadFile(filepath.Join(dir, manifestName))
	if err != nil {
		t.Fatal(err)
	}
	var resolved ResolvedManifest
	if err := json.Unmarshal(raw, &resolved); err != nil {
		t.Fatalf("resolved manifest is not valid: %v", err)
	}
	if len(resolved.Artifacts) != 1 {
		t.Fatalf("expected 1 resolved artifact (empty filename dropped), got %d", len(resolved.Artifacts))
	}
	a := resolved.Artifacts[0]
	if a.Name != "Captured workspace" || a.Ref != "artifacts/run-1/sandbox" {
		t.Fatalf("unexpected resolved entry: %+v", a)
	}
}

func TestIngest_NoManifestIsNoop(t *testing.T) {
	if err := Ingest(context.Background(), NewLocal(), t.TempDir(), "run-x"); err != nil {
		t.Fatalf("missing manifest should be a no-op, got %v", err)
	}
}

func TestIngest_MalformedManifestErrors(t *testing.T) {
	dir := t.TempDir()
	writeStaged(t, dir, `not json`)
	if err := Ingest(context.Background(), NewLocal(), dir, "run-1"); err == nil {
		t.Fatal("expected error on malformed manifest")
	}
}
