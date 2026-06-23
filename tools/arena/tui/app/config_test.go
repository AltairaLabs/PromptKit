package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestDiscoverConfig_FindsInDir verifies that DiscoverConfig finds
// config.arena.yaml when given a directory that contains it.
func TestDiscoverConfig_FindsInDir(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.arena.yaml")
	if err := os.WriteFile(cfgPath, []byte("# placeholder"), 0o644); err != nil {
		t.Fatalf("setup: %v", err)
	}

	got, found := DiscoverConfig(dir)
	if !found {
		t.Fatal("expected found=true for dir containing config.arena.yaml")
	}
	if got != cfgPath {
		t.Fatalf("path=%q, want %q", got, cfgPath)
	}
}

// TestDiscoverConfig_Missing verifies that DiscoverConfig returns !found
// when the directory does not contain config.arena.yaml.
func TestDiscoverConfig_Missing(t *testing.T) {
	dir := t.TempDir()

	_, found := DiscoverConfig(dir)
	if found {
		t.Fatal("expected found=false for empty dir")
	}
}

// TestDiscoverConfig_FileArg verifies that DiscoverConfig returns the file
// directly when given a path to a file (not a directory).
func TestDiscoverConfig_FileArg(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "my-config.yaml")
	if err := os.WriteFile(cfgPath, []byte("# placeholder"), 0o644); err != nil {
		t.Fatalf("setup: %v", err)
	}

	got, found := DiscoverConfig(cfgPath)
	if !found {
		t.Fatal("expected found=true when given a file path directly")
	}
	if got != cfgPath {
		t.Fatalf("path=%q, want %q", got, cfgPath)
	}
}

// TestAppContext_LoadConfig verifies that LoadConfig parses a minimal fixture,
// sets Config/ConfigPath on the context, and derives ResultsDir as out/ next
// to the config file.
func TestAppContext_LoadConfig(t *testing.T) {
	fixturePath := filepath.Join("testdata", "minimal-config", "config.arena.yaml")

	ctx := &AppContext{}
	if err := ctx.LoadConfig(fixturePath); err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	if ctx.Config == nil {
		t.Fatal("expected Config != nil after LoadConfig")
	}
	if ctx.ConfigPath != fixturePath {
		t.Fatalf("ConfigPath=%q, want %q", ctx.ConfigPath, fixturePath)
	}
	wantDir := filepath.Join(filepath.Dir(fixturePath), "out")
	if !strings.HasSuffix(ctx.ResultsDir, "out") {
		t.Fatalf("ResultsDir=%q, expected it to end with 'out'", ctx.ResultsDir)
	}
	if ctx.ResultsDir != wantDir {
		t.Fatalf("ResultsDir=%q, want %q", ctx.ResultsDir, wantDir)
	}
}
