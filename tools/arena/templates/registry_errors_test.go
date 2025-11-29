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
