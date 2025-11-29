package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/AltairaLabs/PromptKit/tools/arena/templates"
)

// Test listing templates from a local index file.
func TestTemplatesList_LocalIndex(t *testing.T) {
	dir := t.TempDir()
	indexPath := filepath.Join(dir, "index.yaml")
	content := "apiVersion: promptkit.altairalabs.ai/v1\nkind: TemplateIndex\nspec:\n  entries:\n    - name: demo\n      version: \"1.0.0\"\n      description: local\n      source: ./template.yaml\n"
	if err := os.WriteFile(indexPath, []byte(content), 0o600); err != nil {
		t.Fatalf("write index: %v", err)
	}

	// Point repo config to a temp file with custom name mapping
	repoConfigPath = filepath.Join(dir, "repos.yaml")
	cfg := &templates.RepoConfig{}
	cfg.Add("local", indexPath)
	if err := cfg.Save(repoConfigPath); err != nil {
		t.Fatalf("save cfg: %v", err)
	}

	// Run list using the mapped repo name
	cmd := templatesListCmd
	cmd.SetArgs([]string{})
	templateIndex = "local"
	if err := cmd.RunE(cmd, []string{}); err != nil {
		t.Fatalf("list run: %v", err)
	}
}

// Test fetch uses cache dir and writes expected file.
func TestTemplatesFetch_LocalIndex(t *testing.T) {
	dir := t.TempDir()
	indexPath := filepath.Join(dir, "index.yaml")
	tplPath := filepath.Join(dir, "template.yaml")
	tpl := "files:\n  - path: README.md\n    content: ok\n"
	content := "apiVersion: promptkit.altairalabs.ai/v1\nkind: TemplateIndex\nspec:\n  entries:\n    - name: demo\n      version: \"1.0.0\"\n      description: local\n      source: " + tplPath + "\n"
	if err := os.WriteFile(indexPath, []byte(content), 0o600); err != nil {
		t.Fatalf("write index: %v", err)
	}
	if err := os.WriteFile(tplPath, []byte(tpl), 0o600); err != nil {
		t.Fatalf("write tpl: %v", err)
	}

	repoConfigPath = filepath.Join(dir, "repos.yaml")
	cfg := &templates.RepoConfig{}
	cfg.Add("local", indexPath)
	if err := cfg.Save(repoConfigPath); err != nil {
		t.Fatalf("save cfg: %v", err)
	}

	templateIndex = "local"
	templateName = "demo"
	templateVersion = "1.0.0"
	templateCache = filepath.Join(dir, "cache")
	cmd := templatesFetchCmd
	cmd.SetArgs([]string{})
	if err := cmd.RunE(cmd, []string{}); err != nil {
		t.Fatalf("fetch run: %v", err)
	}
	cached := filepath.Join(templateCache, "demo", "1.0.0", "template.yaml")
	if _, err := os.Stat(cached); err != nil {
		t.Fatalf("cached file missing: %v", err)
	}
}

// Test render from local file (non-interactive path, with values provided).
func TestTemplatesRender_FromFile(t *testing.T) {
	dir := t.TempDir()
	tplPath := filepath.Join(dir, "template.yaml")
	tpl := "files:\n  - path: README.md\n    content: Hello {{.name}}\n"
	if err := os.WriteFile(tplPath, []byte(tpl), 0o600); err != nil {
		t.Fatalf("write tpl: %v", err)
	}

	templateFile = tplPath
	outputDir = filepath.Join(dir, "out")
	templateValues = map[string]string{"name": "world"}
	promptMissing = false
	cmd := templatesRenderCmd
	cmd.SetArgs([]string{})
	if err := cmd.RunE(cmd, []string{}); err != nil {
		t.Fatalf("render run: %v", err)
	}
	data, err := os.ReadFile(filepath.Join(outputDir, "README.md"))
	if err != nil {
		t.Fatalf("read rendered: %v", err)
	}
	if string(data) != "Hello world" {
		t.Fatalf("unexpected render: %s", string(data))
	}
}

// Test render from cache missing gives helpful error message.
func TestTemplatesRender_FromCacheMissing(t *testing.T) {
	dir := t.TempDir()
	templateFile = "" // use cache path
	templateCache = filepath.Join(dir, "cache")
	templateName = "demo"
	templateVersion = "1.0.0"
	// Ensure cache dir exists but not the template
	if err := os.MkdirAll(templateCache, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	cmd := templatesRenderCmd
	cmd.SetArgs([]string{})
	err := cmd.RunE(cmd, []string{})
	if err == nil {
		t.Fatalf("expected error when cache template missing")
	}
}

// Additional end-to-end command coverage without interactive input.

func TestTemplatesListAndFetchAndRender(t *testing.T) {
	dir := t.TempDir()
	repoConfigPath = filepath.Join(dir, "repos.yaml")

	// Create template package
	tplContent := `
files:
  - path: README.md
    content: "Hello {{.name}}"
`
	tplPath := filepath.Join(dir, "pkg.yaml")
	if err := os.WriteFile(tplPath, []byte(tplContent), 0o644); err != nil {
		t.Fatalf("write tpl: %v", err)
	}

	// Index file
	index := "apiVersion: promptkit.altairalabs.ai/v1\n" +
		"kind: TemplateIndex\n" +
		"spec:\n" +
		"  entries:\n" +
		"    - name: demo\n" +
		"      version: \"1.0.0\"\n" +
		"      description: sample\n" +
		"      source: \"" + tplPath + "\"\n"
	indexPath := filepath.Join(dir, "index.yaml")
	if err := os.WriteFile(indexPath, []byte(index), 0o644); err != nil {
		t.Fatalf("write index: %v", err)
	}

	// Repo config with named entry
	repoConfigContent := `
repos:
  local: "` + indexPath + `"
`
	if err := os.WriteFile(repoConfigPath, []byte(repoConfigContent), 0o644); err != nil {
		t.Fatalf("write repo config: %v", err)
	}

	templateCache = filepath.Join(dir, "cache")
	templateIndex = indexPath

	// List
	buf := &bytes.Buffer{}
	templatesListCmd.SetOut(buf)
	if err := templatesListCmd.RunE(templatesListCmd, nil); err != nil {
		t.Fatalf("list run: %v", err)
	}
	out := buf.String()
	if !strings.Contains(out, "TEMPLATE") || !strings.Contains(out, "VERSION") || !strings.Contains(out, "demo") {
		t.Fatalf("expected demo in list output, got: %q", out)
	}

	// Fetch
	templatesFetchCmd.Flags().Set("template", "local/demo")
	templatesFetchCmd.Flags().Set("version", "1.0.0")
	if err := templatesFetchCmd.RunE(templatesFetchCmd, nil); err != nil {
		t.Fatalf("fetch run: %v", err)
	}

	// Render from cache
	templatesRenderCmd.Flags().Set("template", "local/demo")
	templatesRenderCmd.Flags().Set("version", "1.0.0")
	templatesRenderCmd.Flags().Set("out", filepath.Join(dir, "out"))
	valsFile := filepath.Join(dir, "values.yaml")
	if err := os.WriteFile(valsFile, []byte("name: world\n"), 0o644); err != nil {
		t.Fatalf("write values: %v", err)
	}

	templatesRenderCmd.Flags().Set("values", valsFile)
	if err := templatesRenderCmd.RunE(templatesRenderCmd, nil); err != nil {
		t.Fatalf("render run: %v", err)
	}
	data, err := os.ReadFile(filepath.Join(dir, "out", "README.md"))
	if err != nil {
		t.Fatalf("read rendered: %v", err)
	}
	if string(data) != "Hello world" {
		t.Fatalf("unexpected render: %s", string(data))
	}

	templatesUpdateCmd.Flags().Set("index", indexPath)
	templatesUpdateCmd.Flags().Set("cache-dir", templateCache)
	// Update should process single entry without error
	if err := templatesUpdateCmd.RunE(templatesUpdateCmd, nil); err != nil {
		t.Fatalf("update run: %v", err)
	}
}

func TestTemplatesRepoCommands(t *testing.T) {
	dir := t.TempDir()
	repoConfigPath = filepath.Join(dir, "repos.yaml")

	// Add repo
	repoName = "local"
	repoURL = "https://example.com/index.yaml"
	buf := &bytes.Buffer{}
	templatesRepoAddCmd.SetOut(buf)
	if err := templatesRepoAddCmd.RunE(templatesRepoAddCmd, nil); err != nil {
		t.Fatalf("repo add: %v", err)
	}

	// List repos
	buf.Reset()
	templatesRepoListCmd.SetOut(buf)
	if err := templatesRepoListCmd.RunE(templatesRepoListCmd, nil); err != nil {
		t.Fatalf("repo list: %v", err)
	}
	output := buf.String()
	if !strings.Contains(output, "REPO") || !strings.Contains(output, "local") || !strings.Contains(output, "example.com") {
		t.Fatalf("unexpected list output: %s", output)
	}

	// Remove repo
	repoName = "local"
	buf.Reset()
	templatesRepoRemoveCmd.SetOut(buf)
	if err := templatesRepoRemoveCmd.RunE(templatesRepoRemoveCmd, nil); err != nil {
		t.Fatalf("repo remove: %v", err)
	}
}

// Cover update over multiple entries and list prefix formatting variations.
func TestTemplatesUpdateMultipleAndListFormatting(t *testing.T) {
	dir := t.TempDir()
	repoConfigPath = filepath.Join(dir, "repos.yaml")
	templateCache = filepath.Join(dir, "cache")

	// Create two template packages
	tplA := "files:\n  - path: A.md\n    content: A\n"
	tplB := "files:\n  - path: B.md\n    content: B\n"
	tplAPath := filepath.Join(dir, "a.yaml")
	tplBPath := filepath.Join(dir, "b.yaml")
	if err := os.WriteFile(tplAPath, []byte(tplA), 0o600); err != nil {
		t.Fatalf("write a: %v", err)
	}
	if err := os.WriteFile(tplBPath, []byte(tplB), 0o600); err != nil {
		t.Fatalf("write b: %v", err)
	}

	// Index with two entries
	index := "apiVersion: promptkit.altairalabs.ai/v1\n" +
		"kind: TemplateIndex\n" +
		"spec:\n" +
		"  entries:\n" +
		"    - name: alpha\n" +
		"      version: \"1.0.0\"\n" +
		"      description: alpha\n" +
		"      source: \"" + tplAPath + "\"\n" +
		"    - name: beta\n" +
		"      version: \"2.0.0\"\n" +
		"      description: beta\n" +
		"      source: \"" + tplBPath + "\"\n"
	indexPath := filepath.Join(dir, "index.yaml")
	if err := os.WriteFile(indexPath, []byte(index), 0o600); err != nil {
		t.Fatalf("write index: %v", err)
	}

	// Map repo name to index
	cfg := &templates.RepoConfig{}
	cfg.Add("local", indexPath)
	if err := cfg.Save(repoConfigPath); err != nil {
		t.Fatalf("save cfg: %v", err)
	}

	// Update should fetch both entries
	templatesUpdateCmd.Flags().Set("index", "local")
	templatesUpdateCmd.Flags().Set("cache-dir", templateCache)
	if err := templatesUpdateCmd.RunE(templatesUpdateCmd, nil); err != nil {
		t.Fatalf("update run: %v", err)
	}
	// Verify both cached
	if _, err := os.Stat(filepath.Join(templateCache, "alpha", "1.0.0", "template.yaml")); err != nil {
		t.Fatalf("alpha missing: %v", err)
	}
	if _, err := os.Stat(filepath.Join(templateCache, "beta", "2.0.0", "template.yaml")); err != nil {
		t.Fatalf("beta missing: %v", err)
	}

	// List with and without repo prefix
	buf := &bytes.Buffer{}
	templatesListCmd.SetOut(buf)
	templateIndex = "local"
	if err := templatesListCmd.RunE(templatesListCmd, nil); err != nil {
		t.Fatalf("list run: %v", err)
	}
	out := buf.String()
	if !strings.Contains(out, "local/alpha") || !strings.Contains(out, "local/beta") {
		t.Fatalf("expected repo-prefixed names in list: %q", out)
	}
	buf.Reset()
	// Passthrough path (no repo name)
	templateIndex = indexPath
	if err := templatesListCmd.RunE(templatesListCmd, nil); err != nil {
		t.Fatalf("list run: %v", err)
	}
	out2 := buf.String()
	if strings.Contains(out2, "local/") {
		t.Fatalf("did not expect repo prefix when using raw path: %q", out2)
	}
}

// Cover render merging of --values and multiple --set overrides.
func TestTemplatesRenderMergeValuesAndSet(t *testing.T) {
	dir := t.TempDir()
	tpl := "files:\n  - path: README.md\n    content: X={{.x}} Y={{.y}} Z={{.z}}\n"
	tplPath := filepath.Join(dir, "tpl.yaml")
	if err := os.WriteFile(tplPath, []byte(tpl), 0o600); err != nil {
		t.Fatalf("write tpl: %v", err)
	}
	valuesPath := filepath.Join(dir, "values.yaml")
	if err := os.WriteFile(valuesPath, []byte("x: 1\ny: 2\n"), 0o600); err != nil {
		t.Fatalf("write values: %v", err)
	}

	templateFile = tplPath
	outputDir = filepath.Join(dir, "out")
	valuesFile = valuesPath
	templateValues = map[string]string{"y": "22", "z": "3"}
	promptMissing = false
	if err := templatesRenderCmd.RunE(templatesRenderCmd, nil); err != nil {
		t.Fatalf("render run: %v", err)
	}
	data, err := os.ReadFile(filepath.Join(outputDir, "README.md"))
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	got := string(data)
	if got != "X=1 Y=22 Z=3" {
		t.Fatalf("unexpected merge render: %s", got)
	}
}

func TestSplitTemplateRef(t *testing.T) {
	repo, name := splitTemplateRef("community/basic-chatbot")
	if repo != "community" || name != "basic-chatbot" {
		t.Fatalf("expected repo/community split, got %s/%s", repo, name)
	}
	repo, name = splitTemplateRef("basic-chatbot")
	if repo != "" || name != "basic-chatbot" {
		t.Fatalf("expected bare name, got %s/%s", repo, name)
	}
}

func TestResolveIndexPathWithRepoName(t *testing.T) {
	dir := t.TempDir()
	repoConfigPath = filepath.Join(dir, "repos.yaml")
	templateIndex = "local"
	cfg := `
repos:
  local: file:///tmp/index.yaml
`
	if err := os.WriteFile(repoConfigPath, []byte(cfg), 0o644); err != nil {
		t.Fatalf("write repo config: %v", err)
	}

	path, repo, err := resolveIndexPath()
	if err != nil {
		t.Fatalf("resolve index: %v", err)
	}
	if repo != "local" || path != "file:///tmp/index.yaml" {
		t.Fatalf("unexpected resolution: %s %s", repo, path)
	}
}

func TestTemplatesRenderMissingCacheError(t *testing.T) {
	dir := t.TempDir()
	templateCache = filepath.Join(dir, "cache")
	templateFile = ""
	templateName = "missing"
	templateVersion = "1.0.0"

	err := templatesRenderCmd.RunE(templatesRenderCmd, nil)
	if err == nil {
		t.Fatalf("expected error for missing cache")
	}
	if !strings.Contains(err.Error(), "templates fetch") {
		t.Fatalf("expected fetch hint in error, got: %v", err)
	}
}
