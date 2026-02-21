package sdk

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/AltairaLabs/PromptKit/runtime/skills"
	"github.com/AltairaLabs/PromptKit/runtime/tools"
	"github.com/AltairaLabs/PromptKit/sdk/internal/pack"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSkillsCapability_Name(t *testing.T) {
	cap := NewSkillsCapability(nil)
	assert.Equal(t, "skills", cap.Name())
}

func TestSkillsCapability_Close(t *testing.T) {
	cap := NewSkillsCapability(nil)
	assert.NoError(t, cap.Close())
}

func TestSkillsCapability_Init_DiscoversSkills(t *testing.T) {
	// Create a temp directory with a skill
	dir := t.TempDir()
	skillDir := filepath.Join(dir, "my-skill")
	require.NoError(t, os.MkdirAll(skillDir, 0o755))

	skillContent := `---
name: test-skill
description: A test skill
---
These are the instructions.`
	require.NoError(t, os.WriteFile(
		filepath.Join(skillDir, "SKILL.md"), []byte(skillContent), 0o644,
	))

	sources := []skills.SkillSource{{Dir: dir}}
	cap := NewSkillsCapability(sources)

	p := &pack.Pack{
		ID:      "test",
		Prompts: map[string]*pack.Prompt{"chat": {ID: "chat"}},
	}
	err := cap.Init(CapabilityContext{Pack: p, PromptName: "chat"})
	require.NoError(t, err)
	require.NotNil(t, cap.Executor())
}

func TestSkillsCapability_RegisterTools(t *testing.T) {
	// Create a temp directory with a skill
	dir := t.TempDir()
	skillDir := filepath.Join(dir, "my-skill")
	require.NoError(t, os.MkdirAll(skillDir, 0o755))

	skillContent := `---
name: test-skill
description: A test skill
---
Instructions here.`
	require.NoError(t, os.WriteFile(
		filepath.Join(skillDir, "SKILL.md"), []byte(skillContent), 0o644,
	))

	sources := []skills.SkillSource{{Dir: dir}}
	cap := NewSkillsCapability(sources)

	p := &pack.Pack{
		ID:      "test",
		Prompts: map[string]*pack.Prompt{"chat": {ID: "chat"}},
	}
	require.NoError(t, cap.Init(CapabilityContext{Pack: p, PromptName: "chat"}))

	registry := tools.NewRegistry()
	cap.RegisterTools(registry)

	// Verify all 3 skill tools are registered
	activateTool := registry.Get(skills.SkillActivateTool)
	require.NotNil(t, activateTool, "skill__activate should be registered")
	assert.Equal(t, skills.SkillNamespace, activateTool.Namespace)
	assert.Contains(t, activateTool.Description, "Available skills:")
	assert.Contains(t, activateTool.Description, "test-skill")

	deactivateTool := registry.Get(skills.SkillDeactivateTool)
	require.NotNil(t, deactivateTool, "skill__deactivate should be registered")
	assert.Equal(t, skills.SkillNamespace, deactivateTool.Namespace)

	readResourceTool := registry.Get(skills.SkillReadResourceTool)
	require.NotNil(t, readResourceTool, "skill__read_resource should be registered")
	assert.Equal(t, skills.SkillNamespace, readResourceTool.Namespace)
}

func TestSkillsCapability_RegisterTools_NilExecutor(t *testing.T) {
	cap := NewSkillsCapability(nil)
	registry := tools.NewRegistry()

	// Should not panic even without Init
	cap.RegisterTools(registry)

	assert.Nil(t, registry.Get(skills.SkillActivateTool))
}

func TestSkillsCapability_WithSkillSelector(t *testing.T) {
	selector := skills.NewTagSelector([]string{"coding"})
	cap := NewSkillsCapability(nil, WithSkillSelector(selector))
	assert.Equal(t, selector, cap.selector)
}

func TestSkillsCapability_WithMaxActiveSkills(t *testing.T) {
	cap := NewSkillsCapability(nil, WithMaxActiveSkills(10))
	assert.Equal(t, 10, cap.maxActive)
}

func TestSkillsCapability_InferCapabilities_DetectsSkills(t *testing.T) {
	p := &pack.Pack{
		ID:      "test",
		Prompts: map[string]*pack.Prompt{"chat": {ID: "chat"}},
		Skills: []pack.SkillSourceConfig{
			{Name: "inline-skill", Description: "An inline skill"},
		},
	}

	caps := inferCapabilities(p)

	var found bool
	for _, cap := range caps {
		if cap.Name() == "skills" {
			found = true
			break
		}
	}
	assert.True(t, found, "inferCapabilities should detect skills from pack")
}

func TestSkillsCapability_InferCapabilities_NoSkills(t *testing.T) {
	p := &pack.Pack{
		ID:      "test",
		Prompts: map[string]*pack.Prompt{"chat": {ID: "chat"}},
	}

	caps := inferCapabilities(p)

	for _, cap := range caps {
		assert.NotEqual(t, "skills", cap.Name(),
			"inferCapabilities should not create SkillsCapability without skills")
	}
}

func TestSkillsCapability_Init_InlineSkills(t *testing.T) {
	sources := []skills.SkillSource{
		{
			Name:         "inline-test",
			Description:  "An inline skill",
			Instructions: "Do the thing.",
		},
	}
	cap := NewSkillsCapability(sources)

	p := &pack.Pack{
		ID:      "test",
		Prompts: map[string]*pack.Prompt{"chat": {ID: "chat"}},
	}
	err := cap.Init(CapabilityContext{Pack: p, PromptName: "chat"})
	require.NoError(t, err)
	require.NotNil(t, cap.Executor())
}

func TestSkillsCapability_Init_PreloadSkills(t *testing.T) {
	// Create a temp directory with a preload skill
	dir := t.TempDir()
	skillDir := filepath.Join(dir, "preloaded")
	require.NoError(t, os.MkdirAll(skillDir, 0o755))

	skillContent := `---
name: preloaded-skill
description: A preloaded skill
---
Preloaded instructions.`
	require.NoError(t, os.WriteFile(
		filepath.Join(skillDir, "SKILL.md"), []byte(skillContent), 0o644,
	))

	sources := []skills.SkillSource{{Dir: dir, Preload: true}}
	cap := NewSkillsCapability(sources)

	p := &pack.Pack{
		ID:      "test",
		Prompts: map[string]*pack.Prompt{"chat": {ID: "chat"}},
	}
	require.NoError(t, cap.Init(CapabilityContext{Pack: p, PromptName: "chat"}))

	// The preloaded skill should be active
	activeSkills := cap.Executor().ActiveSkills()
	assert.Contains(t, activeSkills, "preloaded-skill")
}

func TestSkillsCapability_SkillExecutor_Activate(t *testing.T) {
	sources := []skills.SkillSource{
		{
			Name:         "test-skill",
			Description:  "A test skill",
			Instructions: "Test instructions.",
		},
	}
	cap := NewSkillsCapability(sources)

	p := &pack.Pack{
		ID:      "test",
		Prompts: map[string]*pack.Prompt{"chat": {ID: "chat"}},
	}
	require.NoError(t, cap.Init(CapabilityContext{Pack: p, PromptName: "chat"}))

	registry := tools.NewRegistry()
	cap.RegisterTools(registry)

	// Execute the activate tool
	result, err := registry.Execute(skills.SkillActivateTool, []byte(`{"name":"test-skill"}`))
	require.NoError(t, err)
	assert.NotEmpty(t, result.Result)
	assert.Empty(t, result.Error)
}

func TestSkillsCapability_SkillExecutor_Deactivate(t *testing.T) {
	sources := []skills.SkillSource{
		{
			Name:         "test-skill",
			Description:  "A test skill",
			Instructions: "Test instructions.",
		},
	}
	cap := NewSkillsCapability(sources)

	p := &pack.Pack{
		ID:      "test",
		Prompts: map[string]*pack.Prompt{"chat": {ID: "chat"}},
	}
	require.NoError(t, cap.Init(CapabilityContext{Pack: p, PromptName: "chat"}))

	registry := tools.NewRegistry()
	cap.RegisterTools(registry)

	// First activate, then deactivate
	_, err := registry.Execute(skills.SkillActivateTool, []byte(`{"name":"test-skill"}`))
	require.NoError(t, err)

	result, err := registry.Execute(skills.SkillDeactivateTool, []byte(`{"name":"test-skill"}`))
	require.NoError(t, err)
	assert.NotEmpty(t, result.Result)
	assert.Empty(t, result.Error)
}

func TestSkillsCapability_SkillExecutor_ReadResource(t *testing.T) {
	dir := t.TempDir()
	skillDir := filepath.Join(dir, "res-skill")
	require.NoError(t, os.MkdirAll(skillDir, 0o755))

	skillContent := `---
name: res-skill
description: A skill with resources
---
Instructions.`
	require.NoError(t, os.WriteFile(
		filepath.Join(skillDir, "SKILL.md"), []byte(skillContent), 0o644,
	))
	require.NoError(t, os.WriteFile(
		filepath.Join(skillDir, "data.txt"), []byte("hello world"), 0o644,
	))

	sources := []skills.SkillSource{{Dir: dir}}
	cap := NewSkillsCapability(sources)

	p := &pack.Pack{
		ID:      "test",
		Prompts: map[string]*pack.Prompt{"chat": {ID: "chat"}},
	}
	require.NoError(t, cap.Init(CapabilityContext{Pack: p, PromptName: "chat"}))

	registry := tools.NewRegistry()
	cap.RegisterTools(registry)

	result, err := registry.Execute(
		skills.SkillReadResourceTool,
		[]byte(`{"skill_name":"res-skill","path":"data.txt"}`),
	)
	require.NoError(t, err)
	assert.Contains(t, string(result.Result), "hello world")
	assert.Empty(t, result.Error)
}

func TestConvertSkillSources(t *testing.T) {
	configs := []pack.SkillSourceConfig{
		{Dir: "/skills", Preload: true},
		{Name: "inline", Description: "desc", Instructions: "inst"},
	}

	sources := convertSkillSources(configs)

	require.Len(t, sources, 2)
	assert.Equal(t, "/skills", sources[0].Dir)
	assert.True(t, sources[0].Preload)
	assert.Equal(t, "inline", sources[1].Name)
	assert.Equal(t, "desc", sources[1].Description)
	assert.Equal(t, "inst", sources[1].Instructions)
}

func TestEnsureSkillsCapability_AddsWhenNeeded(t *testing.T) {
	cfg := &config{
		skillsDirs: []string{"/some/dir"},
	}
	caps := ensureSkillsCapability(nil, cfg)
	require.Len(t, caps, 1)
	assert.Equal(t, "skills", caps[0].Name())
}

func TestEnsureSkillsCapability_SkipsWhenPresent(t *testing.T) {
	cfg := &config{
		skillsDirs: []string{"/some/dir"},
	}
	existing := []Capability{NewSkillsCapability(nil)}
	caps := ensureSkillsCapability(existing, cfg)
	assert.Len(t, caps, 1, "should not add duplicate SkillsCapability")
}

func TestEnsureSkillsCapability_SkipsWhenNoDirs(t *testing.T) {
	cfg := &config{}
	caps := ensureSkillsCapability(nil, cfg)
	assert.Empty(t, caps)
}

func TestWireSkillsConfig(t *testing.T) {
	selector := skills.NewModelDrivenSelector()
	cfg := &config{
		skillSelector:   selector,
		maxActiveSkills: 8,
	}

	cap := NewSkillsCapability(nil)
	caps := []Capability{cap}

	wireSkillsConfig(caps, cfg)

	assert.Equal(t, selector, cap.selector)
	assert.Equal(t, 8, cap.maxActive)
}

func TestWireSkillsConfig_DoesNotOverride(t *testing.T) {
	originalSelector := skills.NewTagSelector([]string{"test"})
	cfgSelector := skills.NewModelDrivenSelector()

	cfg := &config{
		skillSelector:   cfgSelector,
		maxActiveSkills: 8,
	}

	cap := NewSkillsCapability(nil, WithSkillSelector(originalSelector), WithMaxActiveSkills(3))
	caps := []Capability{cap}

	wireSkillsConfig(caps, cfg)

	// Should keep original values since they were already set
	assert.Equal(t, originalSelector, cap.selector)
	assert.Equal(t, 3, cap.maxActive)
}
