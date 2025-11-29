package templates

import (
	"os"
	"path/filepath"
	"testing"
)

func TestFetchTemplate_Errors(t *testing.T) {
	t.Parallel()
	// nil entry
	if _, err := FetchTemplate(nil, t.TempDir()); err == nil {
		t.Fatalf("expected error for nil entry")
	}
	// missing source
	_, err := FetchTemplate(&IndexEntry{Name: "x", Version: "1.0.0"}, t.TempDir())
	if err == nil {
		t.Fatalf("expected error for missing source")
	}
}

func TestRenderDryRun_ErrorPaths(t *testing.T) {
	t.Parallel()
	// nil package
	if err := RenderDryRun(nil, map[string]string{}, t.TempDir()); err == nil {
		t.Fatalf("expected error for nil package")
	}
	// empty file path
	pkg := &TemplatePackage{Files: []TemplateFile{{Path: "", Content: ""}}}
	if err := RenderDryRun(pkg, map[string]string{}, t.TempDir()); err == nil {
		t.Fatalf("expected error for empty file path")
	}
	// template with invalid syntax
	invalidTmpl := &TemplatePackage{Files: []TemplateFile{{Path: "bad.txt", Content: "{{range .NoClose"}}}
	if err := RenderDryRun(invalidTmpl, map[string]string{}, t.TempDir()); err == nil {
		t.Fatalf("expected parse error for invalid template")
	}
}

func TestLoadTemplatePackage_ParseError(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	bad := filepath.Join(dir, "bad.yaml")
	if err := os.WriteFile(bad, []byte("not: [valid"), 0o600); err != nil {
		t.Fatalf("write bad file: %v", err)
	}
	if _, err := LoadTemplatePackage(bad); err == nil {
		t.Fatalf("expected parse error")
	}
}

func TestIndex_FindLatest(t *testing.T) {
	t.Parallel()
	idx := &Index{
		APIVersion: "v1",
		Kind:       "TemplateIndex",
		Spec: IndexSpec{
			Entries: []IndexEntry{
				{Name: "foo", Version: "1.0.0"},
				{Name: "foo", Version: "1.2.0"},
				{Name: "foo", Version: "1.1.0"},
				{Name: "bar", Version: "2.0.0"},
			},
		},
	}
	entry, err := idx.FindEntry("foo", "latest")
	if err != nil {
		t.Fatalf("FindEntry latest: %v", err)
	}
	if entry.Version != "1.2.0" {
		t.Fatalf("expected version 1.2.0, got %s", entry.Version)
	}
	// Test not found
	if _, err := idx.FindEntry("missing", "latest"); err == nil {
		t.Fatalf("expected error for missing template")
	}
}

func TestCompareSemver_Ordering(t *testing.T) {
	t.Parallel()
	cases := []struct {
		a, b     string
		expected int
	}{
		{"1.0.0", "1.0.0", 0},
		{"1.0.1", "1.0.0", 1},
		{"1.0.0", "1.0.1", -1},
		{"1.1.0", "1.0.9", 1},
		{"2.0.0", "1.9.9", 1},
	}
	for _, c := range cases {
		result := compareSemver(c.a, c.b)
		if result != c.expected {
			t.Errorf("compareSemver(%s, %s) = %d, want %d", c.a, c.b, result, c.expected)
		}
	}
}

func TestDefaultCacheDir_Fallback(t *testing.T) {
	t.Parallel()
	// Reset cache to force initialization
	oldCache := defaultCacheDir
	defaultCacheDir = ""
	defer func() { defaultCacheDir = oldCache }()
	dir := DefaultCacheDir()
	if dir == "" {
		t.Fatalf("DefaultCacheDir returned empty")
	}
}
