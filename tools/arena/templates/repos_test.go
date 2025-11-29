package templates

import (
	"path/filepath"
	"testing"
)

func TestLoadRepoConfigMissingCreatesDefaults(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "repos.yaml")
	cfg, err := LoadRepoConfig(path)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if cfg.Repos[DefaultRepoName] != DefaultGitHubIndex {
		t.Fatalf("default repo missing: %#v", cfg.Repos)
	}
}

func TestRepoConfigAddRemoveSave(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "repos.yaml")
	cfg := &RepoConfig{}
	cfg.Add(" custom ", "https://example.com/index.yaml")
	if err := cfg.Save(path); err != nil {
		t.Fatalf("save: %v", err)
	}
	// reload and remove
	loaded, err := LoadRepoConfig(path)
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	// ensure trimming occurs on load via ensureDefaults
	if _, ok := loaded.Repos["custom"]; !ok {
		t.Fatalf("key not trimmed after load: %#v", loaded.Repos)
	}
	loaded.Remove("custom")
	if _, ok := loaded.Repos["custom"]; ok {
		t.Fatalf("remove failed: %#v", loaded.Repos)
	}
}

func TestResolveIndex(t *testing.T) {
	cfg := &RepoConfig{Repos: map[string]string{"foo": "https://x/y.yaml"}}
	if got := ResolveIndex("foo", cfg); got != "https://x/y.yaml" {
		t.Fatalf("resolve short failed: %s", got)
	}
	if got := ResolveIndex("https://z/index.yaml", cfg); got != "https://z/index.yaml" {
		t.Fatalf("resolve passthrough failed: %s", got)
	}
	if got := ResolveIndex("", cfg); got == "" {
		t.Fatalf("empty resolve returned empty")
	}
}

func TestSaveNilConfig(t *testing.T) {
	var cfg *RepoConfig
	if err := cfg.Save(filepath.Join(t.TempDir(), "repos.yaml")); err == nil {
		t.Fatalf("expected error when saving nil config")
	}
}

func TestEnsureDefaultsCreatesMap(t *testing.T) {
	cfg := &RepoConfig{}
	// call through public Add to ensure map init
	cfg.Add("x", "y")
	if len(cfg.Repos) == 0 {
		t.Fatalf("repos map not initialized")
	}
}
