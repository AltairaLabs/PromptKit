package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/AltairaLabs/PromptKit/tools/arena/templates"
)

// Covers: templatesUpdateCmd error when a fetch fails, templatesRenderCmd missing version error,
// loadValuesFile parse error and mergeValues behavior via render.
// Note: These tests modify global flags and cannot run in parallel.
func TestTemplates_Update_FetchError(t *testing.T) {
	tmp := t.TempDir()
	// Create an index with a single entry pointing to a non-existent source.
	idxPath := filepath.Join(tmp, "index.yaml")
	idxContent := "apiVersion: promptkit.altairalabs.ai/v1\nkind: TemplateIndex\nspec:\n  entries:\n    - name: bad\n      version: 1.0.0\n      source: " + filepath.Join(tmp, "missing.yaml") + "\n"
	if err := os.WriteFile(idxPath, []byte(idxContent), 0o600); err != nil {
		t.Fatalf("write index: %v", err)
	}
	// Point flags
	templateIndex = idxPath
	templateCache = t.TempDir()
	// Run update expecting error due to missing source.
	if err := templatesUpdateCmd.RunE(templatesUpdateCmd, nil); err == nil {
		t.Fatalf("expected error from update when fetch fails")
	}
}

func TestTemplates_Render_MissingVersionFromCache(t *testing.T) {
	templateName = "repo/name"
	templateVersion = ""
	templateCache = t.TempDir()
	// Expect error about required --version when rendering from cache
	if err := templatesRenderCmd.RunE(templatesRenderCmd, nil); err == nil {
		t.Fatalf("expected error requiring version when rendering from cache")
	}
}

func TestLoadValuesFile_ParseError(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	bad := filepath.Join(dir, "values.yaml")
	if err := os.WriteFile(bad, []byte("x: [broken"), 0o600); err != nil {
		t.Fatalf("write bad values: %v", err)
	}
	if _, err := loadValuesFile(bad); err == nil {
		t.Fatalf("expected parse error from values file")
	}
}

func TestMergeValues_OverrideWins(t *testing.T) {
	t.Parallel()
	base := map[string]string{"a": "1", "b": "2"}
	override := map[string]string{"b": "3", "c": "4"}
	out := mergeValues(base, override)
	if out["a"] != "1" || out["b"] != "3" || out["c"] != "4" {
		t.Fatalf("mergeValues unexpected: %#v", out)
	}
}

func TestLoadValuesFile_Empty(t *testing.T) {
	t.Parallel()
	vals, err := loadValuesFile("")
	if err != nil {
		t.Fatalf("loadValuesFile empty path: %v", err)
	}
	if len(vals) != 0 {
		t.Fatalf("expected empty map, got %d entries", len(vals))
	}
}

func TestSplitTemplateRef_WithAndWithoutRepo(t *testing.T) {
	t.Parallel()
	repo, name := templates.SplitTemplateRef("myrepo/mytemplate")
	if repo != "myrepo" || name != "mytemplate" {
		t.Fatalf("splitTemplateRef with repo: got (%s, %s)", repo, name)
	}
	repo2, name2 := templates.SplitTemplateRef("justname")
	if repo2 != "" || name2 != "justname" {
		t.Fatalf("splitTemplateRef without repo: got (%s, %s)", repo2, name2)
	}
	// Edge: empty parts
	repo3, name3 := templates.SplitTemplateRef("/name")
	if repo3 != "" || name3 != "/name" {
		t.Fatalf("splitTemplateRef with leading slash: got (%s, %s)", repo3, name3)
	}
}

func TestExtractPlaceholders_MultipleKeys(t *testing.T) {
	t.Parallel()
	// Use templates package types
	pkg := &templates.TemplatePackage{
		Files: []templates.TemplateFile{
			{Path: "a.txt", Content: "{{.Var1}} and {{.Var2}}"},
			{Path: "b.txt", Content: "{{.Var1}} again"},
		},
	}
	keys := extractPlaceholders(pkg)
	if len(keys) != 2 {
		t.Fatalf("expected 2 keys, got %d: %v", len(keys), keys)
	}
}

func TestResolveIndexPath_RepoMapping(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "repos.yaml")
	repoConfigPath = cfgPath
	templateIndex = "custom"
	// resolveIndexPath will load config; if missing, it returns defaults
	path, repoName, err := resolveIndexPath()
	if err != nil {
		t.Fatalf("resolveIndexPath: %v", err)
	}
	// Without a config file, templateIndex is returned as-is
	if path != "custom" {
		t.Fatalf("expected custom, got %s", path)
	}
	if repoName != "" {
		t.Fatalf("expected empty repo name for unknown repo, got %s", repoName)
	}
}

func TestTemplates_List_OutputFormatting(t *testing.T) {
	tmp := t.TempDir()
	idxPath := filepath.Join(tmp, "index.yaml")
	idxContent := `apiVersion: promptkit.altairalabs.ai/v1
kind: TemplateIndex
spec:
  entries:
    - name: tmpl1
      version: 1.0.0
      description: "First template"
    - name: tmpl2
      version: 2.0.0
      description: "Second template"
`
	if err := os.WriteFile(idxPath, []byte(idxContent), 0o600); err != nil {
		t.Fatalf("write index: %v", err)
	}
	templateIndex = idxPath
	var buf bytes.Buffer
	templatesListCmd.SetOut(&buf)
	if err := templatesListCmd.RunE(templatesListCmd, nil); err != nil {
		t.Fatalf("list cmd: %v", err)
	}
	output := buf.String()
	if !strings.Contains(output, "tmpl1") || !strings.Contains(output, "tmpl2") {
		t.Fatalf("expected tmpl1 and tmpl2 in output, got: %s", output)
	}
}
