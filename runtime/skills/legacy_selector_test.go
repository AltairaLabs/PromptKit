package skills

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/AltairaLabs/PromptKit/runtime/v2/selection"
)

// writeTaggedSkill writes a skill whose metadata carries the given tags.
func writeTaggedSkill(t *testing.T, dir, name, tags string) {
	t.Helper()
	skillDir := filepath.Join(dir, name)
	if err := os.MkdirAll(skillDir, 0o755); err != nil {
		t.Fatal(err)
	}
	body := "---\nname: " + name + "\ndescription: desc-" + name +
		"\nmetadata:\n  tags: " + tags + "\n---\n\nbody-" + name
	if err := os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func newTaggedExecutor(t *testing.T, cfg ExecutorConfig) *Executor {
	t.Helper()
	dir := t.TempDir()
	writeTaggedSkill(t, dir, "alpha", "coding, review")
	writeTaggedSkill(t, dir, "beta", "writing")
	reg := NewRegistry()
	if err := reg.Discover([]SkillSource{{Dir: dir}}); err != nil {
		t.Fatal(err)
	}
	cfg.Registry = reg
	return NewExecutor(cfg)
}

// ExecutorConfig.Selector takes a SkillSelector, which is adapted onto the
// selection.Selector path, so a TagSelector narrows the skill index as an
// external selector does.
func TestExecutorConfig_SkillSelectorNarrowsTheIndex(t *testing.T) {
	e := newTaggedExecutor(t, ExecutorConfig{Selector: NewTagSelector([]string{"coding"})})

	got := e.SelectSkillsIn(context.Background(), "help me refactor", NewActiveSet())
	if len(got) != 1 || got[0] != "alpha" {
		t.Fatalf("SelectSkillsIn = %v, want [alpha]", got)
	}
	index := e.SkillIndexFiltered(context.Background(), "help me refactor", "")
	if !strings.Contains(index, "alpha") || strings.Contains(index, "beta") {
		t.Errorf("index should list alpha only:\n%s", index)
	}
}

// A selection.Selector, when both are configured, is the one that runs.
func TestExecutorConfig_NewSelectorWinsOverSkillSelector(t *testing.T) {
	sel := &fakeSelector{name: "s", selected: []string{"beta"}}
	e := newTaggedExecutor(t, ExecutorConfig{
		Selector:    NewTagSelector([]string{"coding"}),
		NewSelector: sel,
	})

	got := e.SelectSkillsIn(context.Background(), "q", NewActiveSet())
	if len(got) != 1 || got[0] != "beta" {
		t.Fatalf("SelectSkillsIn = %v, want [beta]", got)
	}
}

// The adapter hands the legacy selector each skill's metadata, and its
// query, and returns its choice.
func TestAdaptSkillSelector(t *testing.T) {
	if AdaptSkillSelector(nil) != nil {
		t.Fatal("a nil SkillSelector adapts to nil")
	}
	rec := &recordingSkillSelector{}
	sel := AdaptSkillSelector(rec)
	if sel.Name() == "" {
		t.Error("adapted selector needs a name")
	}
	if err := sel.Init(selection.SelectorContext{}); err != nil {
		t.Fatal(err)
	}
	e := newTaggedExecutor(t, ExecutorConfig{Selector: rec})
	_ = e.SelectSkillsIn(context.Background(), "the query", NewActiveSet())
	if rec.query != "the query" {
		t.Errorf("query = %q", rec.query)
	}
	if len(rec.saw) != 2 || rec.saw[0].Metadata["tags"] != "coding, review" {
		t.Errorf("skills seen = %+v", rec.saw)
	}
}

type recordingSkillSelector struct {
	query string
	saw   []SkillMetadata
}

func (r *recordingSkillSelector) Select(_ context.Context, q string, available []SkillMetadata) ([]string, error) {
	r.query = q
	r.saw = available
	return []string{available[0].Name}, nil
}
