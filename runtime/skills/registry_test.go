package skills

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

func writeTestSkill(t *testing.T, dir, name, description, instructions string) string {
	t.Helper()
	skillDir := filepath.Join(dir, name)
	if err := os.MkdirAll(skillDir, 0o755); err != nil {
		t.Fatal(err)
	}
	content := fmt.Sprintf("---\nname: %s\ndescription: %s\n---\n\n%s", name, description, instructions)
	if err := os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return skillDir
}

func TestDiscoverDirectorySource(t *testing.T) {
	dir := t.TempDir()
	writeTestSkill(t, dir, "alpha", "Alpha skill", "Alpha instructions")
	writeTestSkill(t, dir, "beta", "Beta skill", "Beta instructions")

	reg := NewRegistry()
	err := reg.Discover([]SkillSource{{Dir: dir}})
	if err != nil {
		t.Fatalf("Discover failed: %v", err)
	}

	list := reg.List()
	if len(list) != 2 {
		t.Fatalf("expected 2 skills, got %d", len(list))
	}
	if list[0].Name != "alpha" {
		t.Errorf("expected first skill 'alpha', got %q", list[0].Name)
	}
	if list[1].Name != "beta" {
		t.Errorf("expected second skill 'beta', got %q", list[1].Name)
	}
}

func TestDiscoverInlineSource(t *testing.T) {
	reg := NewRegistry()
	err := reg.Discover([]SkillSource{
		{Name: "inline-skill", Description: "An inline skill", Instructions: "Do stuff"},
	})
	if err != nil {
		t.Fatalf("Discover failed: %v", err)
	}

	list := reg.List()
	if len(list) != 1 {
		t.Fatalf("expected 1 skill, got %d", len(list))
	}
	if list[0].Name != "inline-skill" {
		t.Errorf("expected 'inline-skill', got %q", list[0].Name)
	}
	if list[0].Description != "An inline skill" {
		t.Errorf("expected description 'An inline skill', got %q", list[0].Description)
	}
}

func TestDiscoverFirstWins(t *testing.T) {
	dir1 := t.TempDir()
	dir2 := t.TempDir()
	writeTestSkill(t, dir1, "dupe", "First description", "First instructions")
	writeTestSkill(t, dir2, "dupe", "Second description", "Second instructions")

	reg := NewRegistry()
	err := reg.Discover([]SkillSource{{Dir: dir1}, {Dir: dir2}})
	if err != nil {
		t.Fatalf("Discover failed: %v", err)
	}

	list := reg.List()
	if len(list) != 1 {
		t.Fatalf("expected 1 skill, got %d", len(list))
	}
	if list[0].Description != "First description" {
		t.Errorf("expected first-wins description, got %q", list[0].Description)
	}
}

func TestDiscoverInlineFirstWins(t *testing.T) {
	dir := t.TempDir()
	writeTestSkill(t, dir, "overlap", "Dir description", "Dir instructions")

	reg := NewRegistry()
	err := reg.Discover([]SkillSource{
		{Name: "overlap", Description: "Inline description", Instructions: "Inline"},
		{Dir: dir},
	})
	if err != nil {
		t.Fatalf("Discover failed: %v", err)
	}

	list := reg.List()
	if len(list) != 1 {
		t.Fatalf("expected 1 skill, got %d", len(list))
	}
	if list[0].Description != "Inline description" {
		t.Errorf("expected inline to win, got %q", list[0].Description)
	}
}

func TestListForDirFiltersByDirectory(t *testing.T) {
	root := t.TempDir()
	billing := filepath.Join(root, "billing")
	orders := filepath.Join(root, "orders")

	writeTestSkill(t, billing, "bill-skill", "Billing", "Billing instructions")
	writeTestSkill(t, orders, "order-skill", "Orders", "Order instructions")

	reg := NewRegistry()
	err := reg.Discover([]SkillSource{{Dir: root}})
	if err != nil {
		t.Fatalf("Discover failed: %v", err)
	}

	billingSkills := reg.ListForDir(billing)
	if len(billingSkills) != 1 {
		t.Fatalf("expected 1 billing skill, got %d", len(billingSkills))
	}
	if billingSkills[0].Name != "bill-skill" {
		t.Errorf("expected 'bill-skill', got %q", billingSkills[0].Name)
	}

	orderSkills := reg.ListForDir(orders)
	if len(orderSkills) != 1 {
		t.Fatalf("expected 1 order skill, got %d", len(orderSkills))
	}
	if orderSkills[0].Name != "order-skill" {
		t.Errorf("expected 'order-skill', got %q", orderSkills[0].Name)
	}
}

func TestListForDirEmptyReturnsAll(t *testing.T) {
	dir := t.TempDir()
	writeTestSkill(t, dir, "a", "A", "A instructions")
	writeTestSkill(t, dir, "b", "B", "B instructions")

	reg := NewRegistry()
	err := reg.Discover([]SkillSource{{Dir: dir}})
	if err != nil {
		t.Fatalf("Discover failed: %v", err)
	}

	all := reg.ListForDir("")
	if len(all) != 2 {
		t.Fatalf("expected 2 skills, got %d", len(all))
	}
}

func TestLoadReturnsFullSkill(t *testing.T) {
	dir := t.TempDir()
	writeTestSkill(t, dir, "loadme", "Loadable", "These are the full instructions")

	reg := NewRegistry()
	err := reg.Discover([]SkillSource{{Dir: dir}})
	if err != nil {
		t.Fatalf("Discover failed: %v", err)
	}

	skill, err := reg.Load("loadme")
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}
	if skill.Instructions != "These are the full instructions" {
		t.Errorf("unexpected instructions: %q", skill.Instructions)
	}
	if skill.Path == "" {
		t.Error("expected non-empty path")
	}
}

func TestLoadInlineSkill(t *testing.T) {
	reg := NewRegistry()
	err := reg.Discover([]SkillSource{
		{Name: "inline", Description: "Inline", Instructions: "Inline body"},
	})
	if err != nil {
		t.Fatalf("Discover failed: %v", err)
	}

	skill, err := reg.Load("inline")
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}
	if skill.Instructions != "Inline body" {
		t.Errorf("unexpected instructions: %q", skill.Instructions)
	}
}

func TestLoadUnknownSkill(t *testing.T) {
	reg := NewRegistry()
	_, err := reg.Load("nonexistent")
	if err == nil {
		t.Fatal("expected error for unknown skill")
	}
}

func TestReadResource(t *testing.T) {
	dir := t.TempDir()
	skillDir := writeTestSkill(t, dir, "with-refs", "Refs", "Instructions")

	refsDir := filepath.Join(skillDir, "references")
	if err := os.MkdirAll(refsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(refsDir, "data.txt"), []byte("hello world"), 0o644); err != nil {
		t.Fatal(err)
	}

	reg := NewRegistry()
	err := reg.Discover([]SkillSource{{Dir: dir}})
	if err != nil {
		t.Fatalf("Discover failed: %v", err)
	}

	data, err := reg.ReadResource("with-refs", "references/data.txt")
	if err != nil {
		t.Fatalf("ReadResource failed: %v", err)
	}
	if string(data) != "hello world" {
		t.Errorf("unexpected content: %q", string(data))
	}
}

func TestReadResourcePathTraversal(t *testing.T) {
	dir := t.TempDir()
	writeTestSkill(t, dir, "traversal-test", "Test", "Instructions")

	// Write a file outside the skill directory.
	if err := os.WriteFile(filepath.Join(dir, "secret.txt"), []byte("secret"), 0o644); err != nil {
		t.Fatal(err)
	}

	reg := NewRegistry()
	err := reg.Discover([]SkillSource{{Dir: dir}})
	if err != nil {
		t.Fatalf("Discover failed: %v", err)
	}

	_, err = reg.ReadResource("traversal-test", "../secret.txt")
	if err == nil {
		t.Fatal("expected error for path traversal")
	}
	if !errors.Is(err, ErrPathTraversal) {
		t.Errorf("expected ErrPathTraversal, got: %v", err)
	}
}

func TestReadResourceMissingFile(t *testing.T) {
	dir := t.TempDir()
	writeTestSkill(t, dir, "missing-res", "Test", "Instructions")

	reg := NewRegistry()
	err := reg.Discover([]SkillSource{{Dir: dir}})
	if err != nil {
		t.Fatalf("Discover failed: %v", err)
	}

	_, err = reg.ReadResource("missing-res", "no-such-file.txt")
	if err == nil {
		t.Fatal("expected error for missing file")
	}
	if !errors.Is(err, os.ErrNotExist) {
		t.Errorf("expected os.ErrNotExist, got: %v", err)
	}
}

func TestReadResourceInlineSkillError(t *testing.T) {
	reg := NewRegistry()
	err := reg.Discover([]SkillSource{
		{Name: "inline-only", Description: "Inline", Instructions: "Body"},
	})
	if err != nil {
		t.Fatalf("Discover failed: %v", err)
	}

	_, err = reg.ReadResource("inline-only", "file.txt")
	if err == nil {
		t.Fatal("expected error for inline skill resource read")
	}
}

func TestReadResourceUnknownSkill(t *testing.T) {
	reg := NewRegistry()
	_, err := reg.ReadResource("nonexistent", "file.txt")
	if err == nil {
		t.Fatal("expected error for unknown skill")
	}
}

func TestHas(t *testing.T) {
	dir := t.TempDir()
	writeTestSkill(t, dir, "exists", "Exists", "Instructions")

	reg := NewRegistry()
	err := reg.Discover([]SkillSource{{Dir: dir}})
	if err != nil {
		t.Fatalf("Discover failed: %v", err)
	}

	if !reg.Has("exists") {
		t.Error("expected Has('exists') to be true")
	}
	if reg.Has("nope") {
		t.Error("expected Has('nope') to be false")
	}
}

func TestPreloadedSkills(t *testing.T) {
	dir := t.TempDir()
	writeTestSkill(t, dir, "preloaded", "Preloaded", "Preloaded instructions")
	writeTestSkill(t, dir, "not-preloaded", "Not preloaded", "Other instructions")

	reg := NewRegistry()
	// First source with preload, second without.
	err := reg.Discover([]SkillSource{
		{Dir: filepath.Join(dir, "preloaded"), Preload: true},
		{Dir: filepath.Join(dir, "not-preloaded")},
	})
	if err != nil {
		t.Fatalf("Discover failed: %v", err)
	}

	preloaded := reg.PreloadedSkills()
	if len(preloaded) != 1 {
		t.Fatalf("expected 1 preloaded skill, got %d", len(preloaded))
	}
	if preloaded[0].Name != "preloaded" {
		t.Errorf("expected 'preloaded', got %q", preloaded[0].Name)
	}
	if preloaded[0].Instructions != "Preloaded instructions" {
		t.Errorf("unexpected instructions: %q", preloaded[0].Instructions)
	}
}

func TestPreloadedInlineSkills(t *testing.T) {
	reg := NewRegistry()
	err := reg.Discover([]SkillSource{
		{Name: "pre-inline", Description: "Pre", Instructions: "Inline pre", Preload: true},
		{Name: "no-pre", Description: "No pre", Instructions: "Not preloaded"},
	})
	if err != nil {
		t.Fatalf("Discover failed: %v", err)
	}

	preloaded := reg.PreloadedSkills()
	if len(preloaded) != 1 {
		t.Fatalf("expected 1 preloaded skill, got %d", len(preloaded))
	}
	if preloaded[0].Name != "pre-inline" {
		t.Errorf("expected 'pre-inline', got %q", preloaded[0].Name)
	}
}

func TestNewRegistryIsEmpty(t *testing.T) {
	reg := NewRegistry()
	if len(reg.List()) != 0 {
		t.Error("expected empty registry")
	}
	if reg.Has("anything") {
		t.Error("expected Has to return false on empty registry")
	}
}

func TestDiscoverEmptySources(t *testing.T) {
	reg := NewRegistry()
	err := reg.Discover(nil)
	if err != nil {
		t.Fatalf("Discover with nil sources failed: %v", err)
	}
	if len(reg.List()) != 0 {
		t.Error("expected empty registry after nil sources")
	}
}

func TestDiscoverNonexistentDirectory(t *testing.T) {
	reg := NewRegistry()
	err := reg.Discover([]SkillSource{{Dir: "/nonexistent/path/12345"}})
	if err == nil {
		t.Fatal("expected error for nonexistent directory")
	}
}

func TestDiscoverResolveAtRef(t *testing.T) {
	// Set up a project directory with .promptkit/skills/testorg/testskill/ containing SKILL.md.
	projectDir := t.TempDir()
	promptkitSkillDir := filepath.Join(projectDir, ".promptkit", "skills", "testorg", "testskill")
	if err := os.MkdirAll(promptkitSkillDir, 0o755); err != nil {
		t.Fatal(err)
	}
	content := "---\nname: resolved-skill\ndescription: A resolved skill\n---\n\nDo something"
	if err := os.WriteFile(filepath.Join(promptkitSkillDir, "SKILL.md"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	// Change to project dir so DefaultSkillsProjectDir() returns the right path.
	origWd, _ := os.Getwd()
	if err := os.Chdir(projectDir); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.Chdir(origWd) }()

	reg := NewRegistry()
	err := reg.Discover([]SkillSource{{Dir: "@testorg/testskill"}})
	if err != nil {
		t.Fatalf("Discover() error: %v", err)
	}

	if !reg.Has("resolved-skill") {
		t.Error("expected skill 'resolved-skill' to be registered after @-ref resolution")
	}
}

func TestDiscoverResolveAtRefNotInstalled(t *testing.T) {
	reg := NewRegistry()
	// @nonexistent/skill won't be found anywhere.
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	err := reg.Discover([]SkillSource{{Dir: "@nonexistent/skill"}})
	if err == nil {
		t.Fatal("expected error for uninstalled @-ref")
	}
}

func TestDiscoverResolveAtRefInvalidFormat(t *testing.T) {
	reg := NewRegistry()
	err := reg.Discover([]SkillSource{{Dir: "@invalid"}})
	if err == nil {
		t.Fatal("expected error for invalid @-ref format")
	}
}

func TestResolveRefIntegration(t *testing.T) {
	// Set up a project-level skill directory.
	projectDir := t.TempDir()
	promptkitSkills := filepath.Join(projectDir, ".promptkit", "skills", "org", "skill")
	if err := os.MkdirAll(promptkitSkills, 0o755); err != nil {
		t.Fatal(err)
	}

	origWd, _ := os.Getwd()
	if err := os.Chdir(projectDir); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.Chdir(origWd) }()

	reg := NewRegistry()
	ref := SkillRef{Org: "org", Name: "skill"}
	path, err := reg.ResolveRef(ref)
	if err != nil {
		t.Fatalf("ResolveRef() error: %v", err)
	}

	// Use EvalSymlinks for macOS /var -> /private/var resolution.
	wantPath, _ := filepath.EvalSymlinks(promptkitSkills)
	gotPath, _ := filepath.EvalSymlinks(path)
	if gotPath != wantPath {
		t.Errorf("ResolveRef() = %q, want %q", gotPath, wantPath)
	}
}
