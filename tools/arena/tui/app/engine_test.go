package app

import (
	"path/filepath"
	"testing"
)

// TestEnsureEngine_NoConfig verifies that EnsureEngine returns an error and
// leaves ctx.Engine nil when no config has been loaded.
func TestEnsureEngine_NoConfig(t *testing.T) {
	ctx := &AppContext{}
	eng, err := ctx.EnsureEngine()
	if err == nil {
		t.Fatal("expected error when Config is nil, got nil")
	}
	if eng != nil {
		t.Fatalf("expected nil engine on error, got %v", eng)
	}
	if ctx.Engine != nil {
		t.Fatal("expected ctx.Engine to remain nil on error")
	}
}

// TestEnsureEngine_BuildsAndCaches verifies that EnsureEngine constructs an
// engine from the minimal fixture config, caches it, and sets StateStore.
// A second call returns the same *Engine pointer (no rebuild).
func TestEnsureEngine_BuildsAndCaches(t *testing.T) {
	fixturePath := filepath.Join("testdata", "minimal-config", "config.arena.yaml")

	ctx := &AppContext{}
	if err := ctx.LoadConfig(fixturePath); err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}

	eng1, err := ctx.EnsureEngine()
	if err != nil {
		t.Fatalf("EnsureEngine: %v", err)
	}
	if eng1 == nil {
		t.Fatal("expected non-nil engine")
	}
	if ctx.Engine == nil {
		t.Fatal("expected ctx.Engine to be set after EnsureEngine")
	}
	if ctx.StateStore == nil {
		t.Fatal("expected ctx.StateStore to be set after EnsureEngine")
	}

	// Second call must return the same pointer (cached).
	eng2, err := ctx.EnsureEngine()
	if err != nil {
		t.Fatalf("EnsureEngine (second call): %v", err)
	}
	if eng1 != eng2 {
		t.Fatal("expected EnsureEngine to return cached engine on second call")
	}
}
