package main

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/spf13/cobra"

	"github.com/AltairaLabs/PromptKit/tools/arena/templates"
)

// TestTemplateCommandDefinitions verifies that all template commands have correct metadata.
// This ensures command names, aliases, and descriptions are properly configured.
func TestTemplateCommandDefinitions(t *testing.T) {
	tests := []struct {
		name    string
		cmd     interface{ Use() string }
		use     string
		short   string
		aliases []string
	}{
		{"templatesCmd", cmdWrapper{templatesCmd}, "templates", "Manage PromptArena templates (list, fetch, render)", []string{"template"}},
		{"templatesListCmd", cmdWrapper{templatesListCmd}, "list", "List templates from an index", nil},
		{"templatesFetchCmd", cmdWrapper{templatesFetchCmd}, "fetch", "Fetch a template from an index into cache", nil},
		{"templatesUpdateCmd", cmdWrapper{templatesUpdateCmd}, "update", "Update all templates from an index into cache", nil},
		{"templatesRenderCmd", cmdWrapper{templatesRenderCmd}, "render", "Render a cached template to an output directory (dry-run only)", nil},
		{"templatesRepoCmd", cmdWrapper{templatesRepoCmd}, "repo", "Manage template repositories", nil},
		{"templatesRepoListCmd", cmdWrapper{templatesRepoListCmd}, "list", "List configured template repositories", nil},
		{"templatesRepoAddCmd", cmdWrapper{templatesRepoAddCmd}, "add", "Add or update a template repository", nil},
		{"templatesRepoRemoveCmd", cmdWrapper{templatesRepoRemoveCmd}, "remove", "Remove a template repository", nil},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w := tt.cmd.(cmdWrapper)
			if w.cmd.Use != tt.use {
				t.Errorf("Use = %q, want %q", w.cmd.Use, tt.use)
			}
			if w.cmd.Short != tt.short {
				t.Errorf("Short = %q, want %q", w.cmd.Short, tt.short)
			}
			if !reflect.DeepEqual(w.cmd.Aliases, tt.aliases) {
				t.Errorf("Aliases = %v, want %v", w.cmd.Aliases, tt.aliases)
			}
		})
	}
}

// cmdWrapper wraps cobra.Command to satisfy interface in table test
type cmdWrapper struct {
	cmd *cobra.Command
}

func (c cmdWrapper) Use() string { return c.cmd.Use }

func TestSplitTemplateRef_Additional(t *testing.T) {
	repo, name := templates.SplitTemplateRef("community/basic-app")
	if repo != "community" || name != "basic-app" {
		t.Fatalf("unexpected split: %s %s", repo, name)
	}
	repo, name = templates.SplitTemplateRef("solo")
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
	repo, name := templates.SplitTemplateRef("bad/")
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

func TestRunTemplatesRepoAdd_MissingFlags(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "repos.yaml")
	repoConfigPath = cfgPath
	cfg := &templates.RepoConfig{}
	if err := cfg.Save(cfgPath); err != nil {
		t.Fatalf("save cfg: %v", err)
	}

	// Test missing --name
	repoName = ""
	repoURL = "https://example.com"
	err := runTemplatesRepoAdd(templatesRepoAddCmd, nil)
	if err == nil || err.Error() != "--name and --url are required" {
		t.Fatalf("expected --name and --url error, got: %v", err)
	}

	// Test missing --url
	repoName = "test"
	repoURL = ""
	err = runTemplatesRepoAdd(templatesRepoAddCmd, nil)
	if err == nil || err.Error() != "--name and --url are required" {
		t.Fatalf("expected --name and --url error, got: %v", err)
	}
}

func TestRunTemplatesRepoRemove_MissingName(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "repos.yaml")
	repoConfigPath = cfgPath
	cfg := &templates.RepoConfig{}
	if err := cfg.Save(cfgPath); err != nil {
		t.Fatalf("save cfg: %v", err)
	}

	repoName = ""
	err := runTemplatesRepoRemove(templatesRepoRemoveCmd, nil)
	if err == nil || err.Error() != "--name is required" {
		t.Fatalf("expected --name error, got: %v", err)
	}
}

func TestRunTemplatesRepoRemove_NotFound(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "repos.yaml")
	repoConfigPath = cfgPath
	cfg := &templates.RepoConfig{}
	if err := cfg.Save(cfgPath); err != nil {
		t.Fatalf("save cfg: %v", err)
	}

	repoName = "nonexistent"
	err := runTemplatesRepoRemove(templatesRepoRemoveCmd, nil)
	if err == nil || err.Error() != "repo nonexistent not found" {
		t.Fatalf("expected repo not found error, got: %v", err)
	}
}

func TestRunTemplatesRender_MissingFlags(t *testing.T) {
	// Test missing both --template and --file
	templateFile = ""
	templateName = ""
	err := runTemplatesRender(templatesRenderCmd, nil)
	if err == nil || err.Error() != "either --template or --file is required" {
		t.Fatalf("expected either --template or --file error, got: %v", err)
	}

	// Test missing --version when using --template
	templateFile = ""
	templateName = "test"
	templateVersion = ""
	err = runTemplatesRender(templatesRenderCmd, nil)
	if err == nil || err.Error() != "--version is required when rendering from cache" {
		t.Fatalf("expected --version error, got: %v", err)
	}
}

func TestRunTemplatesRepoList_WithRepos(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "repos.yaml")
	repoConfigPath = cfgPath
	cfg := &templates.RepoConfig{}
	cfg.Add("repo1", "https://example.com/1")
	cfg.Add("repo2", "https://example.com/2")
	if err := cfg.Save(cfgPath); err != nil {
		t.Fatalf("save cfg: %v", err)
	}

	// Execute through the command to get output
	buf := new(strings.Builder)
	templatesRepoListCmd.SetOut(buf)
	err := runTemplatesRepoList(templatesRepoListCmd, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	out := buf.String()
	if !strings.Contains(out, "repo1") || !strings.Contains(out, "repo2") {
		t.Fatalf("expected repos in output, got: %s", out)
	}
}

func TestRunTemplatesRepoAdd_Success(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "repos.yaml")
	repoConfigPath = cfgPath
	cfg := &templates.RepoConfig{}
	if err := cfg.Save(cfgPath); err != nil {
		t.Fatalf("save cfg: %v", err)
	}

	repoName = "newrepo"
	repoURL = "https://example.com/index.yaml"
	buf := new(strings.Builder)
	templatesRepoAddCmd.SetOut(buf)
	err := runTemplatesRepoAdd(templatesRepoAddCmd, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(buf.String(), "Added repo newrepo") {
		t.Fatalf("expected confirmation, got: %s", buf.String())
	}
}

func TestRunTemplatesRepoRemove_Success(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "repos.yaml")
	repoConfigPath = cfgPath
	cfg := &templates.RepoConfig{}
	cfg.Add("todelete", "https://example.com")
	if err := cfg.Save(cfgPath); err != nil {
		t.Fatalf("save cfg: %v", err)
	}

	repoName = "todelete"
	buf := new(strings.Builder)
	templatesRepoRemoveCmd.SetOut(buf)
	err := runTemplatesRepoRemove(templatesRepoRemoveCmd, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(buf.String(), "Removed repo todelete") {
		t.Fatalf("expected confirmation, got: %s", buf.String())
	}
}

func TestRunTemplatesList_EmptyRepoName(t *testing.T) {
	// Test with empty templateIndex - this should use DefaultRepoName
	dir := t.TempDir()
	indexPath := filepath.Join(dir, "index.yaml")
	indexContent := `apiVersion: promptkit.altairalabs.ai/v1
kind: TemplateIndex
spec:
  entries:
    - name: demo
      version: "1.0.0"
      description: "Test demo"
      source: ./demo.yaml`
	if err := os.WriteFile(indexPath, []byte(indexContent), 0o600); err != nil {
		t.Fatalf("write index: %v", err)
	}

	cfgPath := filepath.Join(dir, "repos.yaml")
	repoConfigPath = cfgPath
	cfg := &templates.RepoConfig{}
	cfg.Add(templates.DefaultRepoName, indexPath) // Use file path directly
	if err := cfg.Save(cfgPath); err != nil {
		t.Fatalf("save cfg: %v", err)
	}

	templateIndex = "" // Empty - should use default
	buf := new(strings.Builder)
	templatesListCmd.SetOut(buf)
	err := runTemplatesList(templatesListCmd, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	out := buf.String()
	if !strings.Contains(out, "demo") {
		t.Fatalf("expected demo in output, got: %s", out)
	}
}
