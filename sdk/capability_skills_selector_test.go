package sdk

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/v2/packspec"
	"github.com/AltairaLabs/PromptKit/runtime/v2/selection"
	"github.com/AltairaLabs/PromptKit/runtime/v2/skills"
	"github.com/AltairaLabs/PromptKit/runtime/v2/tools"
	"github.com/AltairaLabs/PromptKit/sdk/v2/internal/pack"
)

func writeTaggedSkillFile(t *testing.T, dir, name, tags string) {
	t.Helper()
	skillDir := filepath.Join(dir, name)
	require.NoError(t, os.MkdirAll(skillDir, 0o755))
	body := "---\nname: " + name + "\ndescription: desc-" + name +
		"\nmetadata:\n  tags: " + tags + "\n---\nInstructions."
	require.NoError(t, os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte(body), 0o644))
}

func initTaggedSkills(t *testing.T, cap *SkillsCapability, ctx CapabilityContext) {
	t.Helper()
	dir := t.TempDir()
	writeTaggedSkillFile(t, dir, "alpha", "coding")
	writeTaggedSkillFile(t, dir, "beta", "writing")
	cap.sources = []skills.SkillSource{{Dir: dir}}
	ctx.Pack = &pack.Pack{Pack: packspec.Pack{ID: "t", Prompts: map[string]*pack.Prompt{"chat": {ID: "chat"}}}}
	ctx.PromptName = "chat"
	require.NoError(t, cap.Init(ctx))
}

// A SkillSelector given through WithSkillSelector, or through
// WithSkillSelectorOption (which wireSkillsConfig copies onto the
// capability), narrows the conversation's skill index each turn.
func TestSkillsCapability_SkillSelectorNarrowsTheTurnsSkills(t *testing.T) {
	tag := skills.NewTagSelector([]string{"coding"})
	viaOption := NewSkillsCapability(nil)
	wireSkillsConfig([]Capability{viaOption}, &config{skillSelector: tag})

	for name, cap := range map[string]*SkillsCapability{
		"WithSkillSelector":       NewSkillsCapability(nil, WithSkillSelector(tag)),
		"WithSkillSelectorOption": viaOption,
	} {
		t.Run(name, func(t *testing.T) {
			initTaggedSkills(t, cap, CapabilityContext{})

			assert.Equal(t, []string{"alpha"}, cap.selectSkills(context.Background(), "refactor this", cap.NewActiveSet()))

			registry := tools.NewRegistry()
			cap.RegisterTools(registry)
			cap.RefreshSkillIndex(context.Background(), "refactor this", registry)
			desc := registry.Get(skills.SkillActivateTool).Description
			assert.Contains(t, desc, "alpha")
			assert.NotContains(t, desc, "beta")
		})
	}
}

// A selector bound in RuntimeConfig takes precedence over a SkillSelector.
func TestSkillsCapability_RuntimeConfigSelectorWinsOverSkillSelector(t *testing.T) {
	cap := NewSkillsCapability(nil, WithSkillSelector(skills.NewTagSelector([]string{"coding"})))
	initTaggedSkills(t, cap, CapabilityContext{
		Selectors:          map[string]selection.Selector{"s": &capTestSelector{name: "s", selected: []string{"beta"}}},
		SkillsSelectorName: "s",
	})

	assert.Equal(t, []string{"beta"}, cap.selectSkills(context.Background(), "q", cap.NewActiveSet()))
}
