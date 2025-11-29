package main

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/AltairaLabs/PromptKit/tools/arena/templates"
)

func TestSplitTemplateRef_Additional(t *testing.T) {
	repo, name := splitTemplateRef("community/basic-app")
	if repo != "community" || name != "basic-app" {
		t.Fatalf("unexpected split: %s %s", repo, name)
	}
	repo, name = splitTemplateRef("solo")
	if repo != "" || name != "solo" {
		t.Fatalf("unexpected split no repo: %s %s", repo, name)
	}
}

func TestResolveIndexPath_NoMatchReturnsPassthrough(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "repos.yaml")
	repoConfigPath = cfgPath
	// empty config
	cfg := &templates.RepoConfig{}
	if err := cfg.Save(cfgPath); err != nil {
		t.Fatalf("save cfg: %v", err)
	}
	templateIndex = "https://example.com/custom.yaml"
	path, repoName, err := resolveIndexPath()
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if repoName != "" || path != "https://example.com/custom.yaml" {
		t.Fatalf("unexpected: repo=%s path=%s", repoName, path)
	}
}

func TestMergeValues(t *testing.T) {
	base := map[string]string{"a": "1", "b": "2"}
	over := map[string]string{"b": "3", "c": "4"}
	got := mergeValues(base, over)
	want := map[string]string{"a": "1", "b": "3", "c": "4"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("merge mismatch: %#v", got)
	}
}

func TestMergeValuesOverridesEmptyAndAddsNew(t *testing.T) {
	base := map[string]string{"a": "1", "b": ""}
	over := map[string]string{"b": "2", "c": "3"}
	got := mergeValues(base, over)
	want := map[string]string{"a": "1", "b": "2", "c": "3"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("merge mismatch: %#v", got)
	}
}

func TestSplitTemplateRefMalformed(t *testing.T) {
	repo, name := splitTemplateRef("bad/")
	if repo != "" || name != "bad/" {
		t.Fatalf("unexpected handling of malformed ref: %s/%s", repo, name)
	}
}

func TestLoadValuesFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "values.yaml")
	content := []byte("a: 1\nb: true\nc: hello")
	if err := os.WriteFile(path, content, 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	vals, err := loadValuesFile(path)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if vals["a"] != "1" || vals["b"] != "true" || vals["c"] != "hello" {
		t.Fatalf("unexpected values: %#v", vals)
	}
}

func TestLoadValuesFileErrors(t *testing.T) {
	// non-existent file
	if _, err := loadValuesFile("/path/does/not/exist.yaml"); err == nil {
		t.Fatalf("expected error for missing file")
	}
	// invalid yaml
	dir := t.TempDir()
	p := filepath.Join(dir, "bad.yaml")
	if err := os.WriteFile(p, []byte(": : :"), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	if _, err := loadValuesFile(p); err == nil {
		t.Fatalf("expected parse error")
	}
}

func TestExtractPlaceholders(t *testing.T) {
	pkg := &templates.TemplatePackage{Files: []templates.TemplateFile{
		{Path: "a.txt", Content: "{{ .name }} and {{ .count }}"},
		{Path: "b.txt", Content: "no placeholders"},
		{Path: "c.txt", Content: "{{ .name }} again"},
	}}
	keys := extractPlaceholders(pkg)
	// order not guaranteed; use set membership
	set := map[string]bool{}
	for _, k := range keys {
		set[k] = true
	}
	if !set["name"] || !set["count"] || len(set) != 2 {
		t.Fatalf("unexpected keys: %#v", set)
	}
}

func TestPromptForMissing_NoPromptWhenAllProvided(t *testing.T) {
	pkg := &templates.TemplatePackage{Files: []templates.TemplateFile{{Path: "a", Content: "hi {{ .name }}"}}}
	vars := map[string]string{"name": "world"}
	out, err := promptForMissing(vars, pkg)
	if err != nil {
		t.Fatalf("promptForMissing: %v", err)
	}
	if !reflect.DeepEqual(out, vars) {
		t.Fatalf("vars changed unexpectedly: %#v", out)
	}
}

func TestPromptForMissing_Canceled(t *testing.T) {
	// When pkg has a missing var, promptui will run; we can't simulate interactive input here.
	// Provide nil pkg to ensure early return and no prompt side effects.
	vars := map[string]string{"name": "x"}
	out, err := promptForMissing(vars, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !reflect.DeepEqual(out, vars) {
		t.Fatalf("vars changed: %#v", out)
	}
}

func TestResolveIndexPath_DefaultRepo(t *testing.T) {
	// Use temp config path with default only
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "repos.yaml")
	// Ensure default path is used by setting flag variable
	repoConfigPath = cfgPath
	templateIndex = templates.DefaultRepoName
	cfg := &templates.RepoConfig{}
	cfg.Add(templates.DefaultRepoName, templates.DefaultGitHubIndex)
	if err := cfg.Save(cfgPath); err != nil {
		t.Fatalf("save cfg: %v", err)
	}
	path, repoName, err := resolveIndexPath()
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if repoName != templates.DefaultRepoName || path != templates.DefaultGitHubIndex {
		t.Fatalf("unexpected: repo=%s path=%s", repoName, path)
	}
}

func TestResolveIndexPath_CustomNamePassthrough(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "repos.yaml")
	repoConfigPath = cfgPath
	cfg := &templates.RepoConfig{}
	cfg.Add("myrepo", "https://example.com/index.yaml")
	if err := cfg.Save(cfgPath); err != nil {
		t.Fatalf("save cfg: %v", err)
	}
	templateIndex = "myrepo"
	path, repoName, err := resolveIndexPath()
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if repoName != "myrepo" || path != "https://example.com/index.yaml" {
		t.Fatalf("unexpected: repo=%s path=%s", repoName, path)
	}
}
