package skills

import (
	"bytes"
	"log/slog"
	"strings"
	"testing"

	"github.com/AltairaLabs/PromptKit/runtime/v2/logger"
)

// These cover #1954: two sources declaring the same skill name resolved
// first-wins at Debug, so a deployment-specific override source silently lost
// to the base directory it was meant to override. The rule stays first-wins;
// the collision is now reported at Warn naming which copy is live and which
// was dropped.

func captureSkillWarnLogs(t *testing.T) *bytes.Buffer {
	t.Helper()
	var buf bytes.Buffer
	logger.SetLogger(slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelWarn})))
	t.Cleanup(func() { logger.SetLogger(nil) })
	return &buf
}

func TestDiscoverDuplicateDirectorySkillWarnsNamingBothPaths(t *testing.T) {
	logs := captureSkillWarnLogs(t)
	base := t.TempDir()
	override := t.TempDir()
	writeTestSkill(t, base, "refund-policy", "Base", "Base instructions")
	writeTestSkill(t, override, "refund-policy", "Tenant", "Tenant instructions")

	reg := NewRegistry()
	if err := reg.Discover([]SkillSource{{Dir: base}, {Dir: override}}); err != nil {
		t.Fatalf("Discover failed: %v", err)
	}

	out := logs.String()
	for _, want := range []string{"duplicate skill", "refund-policy", base, override} {
		if !strings.Contains(out, want) {
			t.Errorf("warning must contain %q; got:\n%s", want, out)
		}
	}
	if list := reg.List(); len(list) != 1 || list[0].Description != "Base" {
		t.Fatalf("first source must still win; got %+v", list)
	}
}

func TestDiscoverDuplicateInlineSkillWarns(t *testing.T) {
	logs := captureSkillWarnLogs(t)
	dir := t.TempDir()
	writeTestSkill(t, dir, "overlap", "Dir", "Dir instructions")

	reg := NewRegistry()
	err := reg.Discover([]SkillSource{
		{Name: "overlap", Description: "Inline", Instructions: "Inline"},
		{Dir: dir},
	})
	if err != nil {
		t.Fatalf("Discover failed: %v", err)
	}

	out := logs.String()
	if !strings.Contains(out, "duplicate skill") || !strings.Contains(out, "overlap") {
		t.Errorf("collision must be reported at Warn naming the skill; got:\n%s", out)
	}
}

// TestDiscoverPreloadUpgradeFromDuplicateStillWarns pins the asymmetry the
// issue called out: the loser's preload flag is merged, but its content is
// still dropped, and that is still a collision the operator should see.
func TestDiscoverPreloadUpgradeFromDuplicateStillWarns(t *testing.T) {
	logs := captureSkillWarnLogs(t)
	base := t.TempDir()
	override := t.TempDir()
	writeTestSkill(t, base, "brand-voice", "Base", "Base instructions")
	writeTestSkill(t, override, "brand-voice", "Tenant", "Tenant instructions")

	reg := NewRegistry()
	if err := reg.Discover([]SkillSource{{Dir: base}, {Dir: override, Preload: true}}); err != nil {
		t.Fatalf("Discover failed: %v", err)
	}

	if !strings.Contains(logs.String(), "duplicate skill") {
		t.Errorf("a duplicate that only upgrades preload is still a dropped override; got:\n%s", logs.String())
	}
	if pre := reg.PreloadedSkills(); len(pre) != 1 {
		t.Fatalf("preload flag must still be merged from the duplicate; got %d preloaded", len(pre))
	}
}

func TestDiscoverDistinctSkillsDoNotWarn(t *testing.T) {
	logs := captureSkillWarnLogs(t)
	dir := t.TempDir()
	writeTestSkill(t, dir, "one", "One", "One")
	writeTestSkill(t, dir, "two", "Two", "Two")

	reg := NewRegistry()
	if err := reg.Discover([]SkillSource{{Dir: dir}}); err != nil {
		t.Fatalf("Discover failed: %v", err)
	}
	if logs.Len() != 0 {
		t.Errorf("distinct skills must not warn; got:\n%s", logs.String())
	}
}
