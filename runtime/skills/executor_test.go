package skills

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// writeSkillWithTools creates a SKILL.md with optional allowed-tools in the frontmatter.
func writeSkillWithTools(
	t *testing.T, dir, name, description, instructions string, tools []string,
) string {
	t.Helper()
	skillDir := filepath.Join(dir, name)
	if err := os.MkdirAll(skillDir, 0o755); err != nil {
		t.Fatal(err)
	}
	var sb strings.Builder
	sb.WriteString("---\n")
	sb.WriteString(fmt.Sprintf("name: %s\n", name))
	sb.WriteString(fmt.Sprintf("description: %s\n", description))
	if len(tools) > 0 {
		sb.WriteString("allowed-tools:\n")
		for _, tool := range tools {
			sb.WriteString(fmt.Sprintf("  - %s\n", tool))
		}
	}
	sb.WriteString("---\n\n")
	sb.WriteString(instructions)
	err := os.WriteFile(
		filepath.Join(skillDir, "SKILL.md"), []byte(sb.String()), 0o644,
	)
	if err != nil {
		t.Fatal(err)
	}
	return skillDir
}

// newTestExecutor creates an Executor with a registry containing the given skills.
func newTestExecutor(
	t *testing.T, dir string, packTools []string, maxActive int,
) *Executor {
	t.Helper()
	reg := NewRegistry()
	if err := reg.Discover([]SkillSource{{Dir: dir}}); err != nil {
		t.Fatalf("Discover failed: %v", err)
	}
	return NewExecutor(ExecutorConfig{
		Registry:  reg,
		PackTools: packTools,
		MaxActive: maxActive,
	})
}

func TestActivateReturnsInstructionsAndTools(t *testing.T) {
	dir := t.TempDir()
	writeSkillWithTools(t, dir, "code-review", "Code review skill",
		"Review the code carefully.", []string{"read-file", "write-file"})

	exec := newTestExecutor(t, dir, []string{"read-file", "write-file", "run-tests"}, 0)

	instructions, addedTools, err := exec.Activate("code-review")
	if err != nil {
		t.Fatalf("Activate failed: %v", err)
	}
	if instructions != "Review the code carefully." {
		t.Errorf("unexpected instructions: %q", instructions)
	}
	sort.Strings(addedTools)
	if len(addedTools) != 2 {
		t.Fatalf("expected 2 added tools, got %d: %v", len(addedTools), addedTools)
	}
	if addedTools[0] != "read-file" || addedTools[1] != "write-file" {
		t.Errorf("unexpected tools: %v", addedTools)
	}
}

func TestActivateCapsToolsByPack(t *testing.T) {
	dir := t.TempDir()
	writeSkillWithTools(t, dir, "deploy", "Deploy skill",
		"Deploy instructions.", []string{"tool-a", "tool-b", "tool-c"})

	// Pack only has tool-a and tool-c.
	exec := newTestExecutor(t, dir, []string{"tool-a", "tool-c"}, 0)

	_, addedTools, err := exec.Activate("deploy")
	if err != nil {
		t.Fatalf("Activate failed: %v", err)
	}
	sort.Strings(addedTools)
	if len(addedTools) != 2 {
		t.Fatalf("expected 2 tools, got %d: %v", len(addedTools), addedTools)
	}
	if addedTools[0] != "tool-a" || addedTools[1] != "tool-c" {
		t.Errorf("expected [tool-a, tool-c], got %v", addedTools)
	}
}

func TestActivateUnknownSkill(t *testing.T) {
	dir := t.TempDir()
	exec := newTestExecutor(t, dir, nil, 0)

	_, _, err := exec.Activate("nonexistent")
	if err == nil {
		t.Fatal("expected error for unknown skill")
	}
	if !strings.Contains(err.Error(), "nonexistent") {
		t.Errorf("error should mention skill name: %v", err)
	}
}

func TestActivateAlreadyActiveIsIdempotent(t *testing.T) {
	dir := t.TempDir()
	writeSkillWithTools(t, dir, "lint", "Lint skill",
		"Lint instructions.", []string{"lint-tool"})

	exec := newTestExecutor(t, dir, []string{"lint-tool"}, 0)

	instr1, tools1, err := exec.Activate("lint")
	if err != nil {
		t.Fatalf("first Activate failed: %v", err)
	}

	instr2, tools2, err := exec.Activate("lint")
	if err != nil {
		t.Fatalf("second Activate failed: %v", err)
	}

	if instr1 != instr2 {
		t.Errorf("instructions differ: %q vs %q", instr1, instr2)
	}
	if len(tools1) != len(tools2) {
		t.Errorf("tools differ: %v vs %v", tools1, tools2)
	}
}

func TestActivateMaxActiveLimit(t *testing.T) {
	dir := t.TempDir()
	writeSkillWithTools(t, dir, "skill-one", "First", "First.", nil)
	writeSkillWithTools(t, dir, "skill-two", "Second", "Second.", nil)

	exec := newTestExecutor(t, dir, nil, 1)

	_, _, err := exec.Activate("skill-one")
	if err != nil {
		t.Fatalf("first Activate failed: %v", err)
	}

	_, _, err = exec.Activate("skill-two")
	if err == nil {
		t.Fatal("expected error when exceeding max active limit")
	}
	if !strings.Contains(err.Error(), "max active limit") {
		t.Errorf("error should mention max active limit: %v", err)
	}
}

func TestDeactivateReturnsRemovedTools(t *testing.T) {
	dir := t.TempDir()
	writeSkillWithTools(t, dir, "refactor", "Refactor skill",
		"Refactor instructions.", []string{"read-file", "write-file"})

	exec := newTestExecutor(t, dir, []string{"read-file", "write-file"}, 0)

	_, _, err := exec.Activate("refactor")
	if err != nil {
		t.Fatalf("Activate failed: %v", err)
	}

	removed, err := exec.Deactivate("refactor")
	if err != nil {
		t.Fatalf("Deactivate failed: %v", err)
	}
	sort.Strings(removed)
	if len(removed) != 2 {
		t.Fatalf("expected 2 removed tools, got %d: %v", len(removed), removed)
	}
	if removed[0] != "read-file" || removed[1] != "write-file" {
		t.Errorf("unexpected removed tools: %v", removed)
	}

	// Verify skill is no longer active.
	active := exec.ActiveSkills()
	if len(active) != 0 {
		t.Errorf("expected no active skills, got %v", active)
	}
}

func TestDeactivateSharedToolsNotRemoved(t *testing.T) {
	dir := t.TempDir()
	writeSkillWithTools(t, dir, "skill-a", "Skill A",
		"A instructions.", []string{"shared-tool", "tool-a"})
	writeSkillWithTools(t, dir, "skill-b", "Skill B",
		"B instructions.", []string{"shared-tool", "tool-b"})

	exec := newTestExecutor(t, dir,
		[]string{"shared-tool", "tool-a", "tool-b"}, 0)

	if _, _, err := exec.Activate("skill-a"); err != nil {
		t.Fatalf("Activate skill-a failed: %v", err)
	}
	if _, _, err := exec.Activate("skill-b"); err != nil {
		t.Fatalf("Activate skill-b failed: %v", err)
	}

	removed, err := exec.Deactivate("skill-a")
	if err != nil {
		t.Fatalf("Deactivate failed: %v", err)
	}

	// shared-tool should NOT be removed since skill-b still needs it.
	for _, t2 := range removed {
		if t2 == "shared-tool" {
			t.Error("shared-tool should not be removed while skill-b is active")
		}
	}

	// tool-a should be removed.
	found := false
	for _, t2 := range removed {
		if t2 == "tool-a" {
			found = true
		}
	}
	if !found {
		t.Error("tool-a should be in removed tools")
	}
}

func TestDeactivateUnknownSkill(t *testing.T) {
	dir := t.TempDir()
	exec := newTestExecutor(t, dir, nil, 0)

	_, err := exec.Deactivate("nonexistent")
	if err == nil {
		t.Fatal("expected error for unknown skill")
	}
}

func TestDeactivateInactiveSkill(t *testing.T) {
	dir := t.TempDir()
	writeSkillWithTools(t, dir, "inactive", "Inactive skill", "Instructions.", nil)

	exec := newTestExecutor(t, dir, nil, 0)

	_, err := exec.Deactivate("inactive")
	if err == nil {
		t.Fatal("expected error for inactive skill")
	}
	if !strings.Contains(err.Error(), "not active") {
		t.Errorf("error should mention 'not active': %v", err)
	}
}

func TestReadResourceDelegatesToRegistry(t *testing.T) {
	dir := t.TempDir()
	skillDir := writeSkillWithTools(t, dir, "with-res", "Res skill", "Instructions.", nil)

	resDir := filepath.Join(skillDir, "refs")
	if err := os.MkdirAll(resDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(
		filepath.Join(resDir, "data.json"), []byte(`{"key":"val"}`), 0o644,
	); err != nil {
		t.Fatal(err)
	}

	exec := newTestExecutor(t, dir, nil, 0)

	data, err := exec.ReadResource("with-res", "refs/data.json")
	if err != nil {
		t.Fatalf("ReadResource failed: %v", err)
	}
	if string(data) != `{"key":"val"}` {
		t.Errorf("unexpected content: %q", string(data))
	}
}

func TestSkillIndexFormatsCorrectly(t *testing.T) {
	dir := t.TempDir()
	writeSkillWithTools(t, dir, "alpha", "Alpha description", "Alpha.", nil)
	writeSkillWithTools(t, dir, "beta", "Beta description", "Beta.", nil)

	exec := newTestExecutor(t, dir, nil, 0)
	index := exec.SkillIndex("")

	if !strings.HasPrefix(index, "Available skills:") {
		t.Errorf("index should start with 'Available skills:', got: %q", index)
	}
	if !strings.Contains(index, "- alpha: Alpha description") {
		t.Errorf("index should contain alpha entry, got: %q", index)
	}
	if !strings.Contains(index, "- beta: Beta description") {
		t.Errorf("index should contain beta entry, got: %q", index)
	}
}

func TestSkillIndexWithDirectoryFilter(t *testing.T) {
	root := t.TempDir()
	billing := filepath.Join(root, "billing")
	orders := filepath.Join(root, "orders")

	writeSkillWithTools(t, billing, "bill-skill", "Billing", "Billing.", nil)
	writeSkillWithTools(t, orders, "order-skill", "Orders", "Orders.", nil)

	reg := NewRegistry()
	if err := reg.Discover([]SkillSource{{Dir: root}}); err != nil {
		t.Fatalf("Discover failed: %v", err)
	}
	exec := NewExecutor(ExecutorConfig{Registry: reg})

	index := exec.SkillIndex(billing)
	if !strings.Contains(index, "bill-skill") {
		t.Errorf("expected bill-skill in filtered index, got: %q", index)
	}
	if strings.Contains(index, "order-skill") {
		t.Errorf("order-skill should not appear in billing-filtered index, got: %q", index)
	}
}

func TestSkillIndexEmpty(t *testing.T) {
	dir := t.TempDir()
	exec := newTestExecutor(t, dir, nil, 0)

	index := exec.SkillIndex("")
	if index != "No skills available." {
		t.Errorf("expected 'No skills available.', got: %q", index)
	}
}

func TestActiveSkillsReturnsSortedNames(t *testing.T) {
	dir := t.TempDir()
	writeSkillWithTools(t, dir, "zeta", "Zeta", "Zeta.", nil)
	writeSkillWithTools(t, dir, "alpha", "Alpha", "Alpha.", nil)

	exec := newTestExecutor(t, dir, nil, 0)

	if _, _, err := exec.Activate("zeta"); err != nil {
		t.Fatalf("Activate zeta failed: %v", err)
	}
	if _, _, err := exec.Activate("alpha"); err != nil {
		t.Fatalf("Activate alpha failed: %v", err)
	}

	active := exec.ActiveSkills()
	if len(active) != 2 {
		t.Fatalf("expected 2 active skills, got %d", len(active))
	}
	if active[0] != "alpha" || active[1] != "zeta" {
		t.Errorf("expected [alpha, zeta], got %v", active)
	}
}

func TestActiveToolsDeduplicates(t *testing.T) {
	dir := t.TempDir()
	writeSkillWithTools(t, dir, "skill-x", "X",
		"X.", []string{"shared", "only-x"})
	writeSkillWithTools(t, dir, "skill-y", "Y",
		"Y.", []string{"shared", "only-y"})

	exec := newTestExecutor(t, dir,
		[]string{"shared", "only-x", "only-y"}, 0)

	if _, _, err := exec.Activate("skill-x"); err != nil {
		t.Fatalf("Activate skill-x failed: %v", err)
	}
	if _, _, err := exec.Activate("skill-y"); err != nil {
		t.Fatalf("Activate skill-y failed: %v", err)
	}

	tools := exec.ActiveTools()
	expected := []string{"only-x", "only-y", "shared"}
	if len(tools) != len(expected) {
		t.Fatalf("expected %d tools, got %d: %v", len(expected), len(tools), tools)
	}
	for i, e := range expected {
		if tools[i] != e {
			t.Errorf("tool[%d]: expected %q, got %q", i, e, tools[i])
		}
	}
}

func TestNewExecutorDefaultsToModelDrivenSelector(t *testing.T) {
	reg := NewRegistry()
	exec := NewExecutor(ExecutorConfig{Registry: reg})

	if exec.selector == nil {
		t.Fatal("expected non-nil selector")
	}
	if _, ok := exec.selector.(*ModelDrivenSelector); !ok {
		t.Errorf(
			"expected *ModelDrivenSelector, got %T", exec.selector,
		)
	}
}
