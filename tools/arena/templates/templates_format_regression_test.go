package templates

import (
	"os"
	"path/filepath"
	"testing"
)

// Regression: plain template package format (no apiVersion/kind) must parse via TemplatePackage
func TestLoadTemplatePackage_PlainFilesFormat(t *testing.T) {
	dir := t.TempDir()
	tpl := "files:\n  - path: README.md\n    content: Hello {{.name}}\n"
	p := filepath.Join(dir, "template.yaml")
	if err := os.WriteFile(p, []byte(tpl), 0o600); err != nil {
		t.Fatalf("write template: %v", err)
	}
	pkg, err := LoadTemplatePackage(p)
	if err != nil {
		t.Fatalf("load plain package: %v", err)
	}
	if pkg == nil || len(pkg.Files) != 1 || pkg.Files[0].Path != "README.md" {
		t.Fatalf("unexpected plain pkg: %#v", pkg)
	}
}

// Regression: CR format with apiVersion/kind should parse via CR path and resolve source
func TestLoadTemplatePackage_CRFormatWithAPIVersionKind(t *testing.T) {
	dir := t.TempDir()
	// content file referenced by spec.files[0].source
	srcFile := filepath.Join(dir, "content.txt")
	if err := os.WriteFile(srcFile, []byte("Hello {{.name}}"), 0o600); err != nil {
		t.Fatalf("write source: %v", err)
	}

	tpl := "apiVersion: promptkit.altairalabs.ai/v1\n" +
		"kind: Template\n" +
		"spec:\n" +
		"  files:\n" +
		"    - path: README.md\n" +
		"      source: content.txt\n"

	p := filepath.Join(dir, "template.yaml")
	if err := os.WriteFile(p, []byte(tpl), 0o600); err != nil {
		t.Fatalf("write template: %v", err)
	}
	pkg, err := LoadTemplatePackage(p)
	if err != nil {
		t.Fatalf("load CR package: %v", err)
	}
	if pkg == nil || len(pkg.Files) != 1 || pkg.Files[0].Path != "README.md" {
		t.Fatalf("unexpected CR pkg: %#v", pkg)
	}
	if pkg.Files[0].Content != "Hello {{.name}}" {
		t.Fatalf("expected resolved content, got: %q", pkg.Files[0].Content)
	}
}
