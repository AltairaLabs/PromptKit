package skills

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/AltairaLabs/PromptKit/runtime/tools"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBuildSkillActivateDescriptor(t *testing.T) {
	desc := BuildSkillActivateDescriptor()
	assert.Equal(t, SkillActivateTool, desc.Name)
	assert.Equal(t, SkillNamespace, desc.Namespace)
	assert.Equal(t, SkillExecutorName, desc.Mode)
	assert.NotEmpty(t, desc.InputSchema)
	assert.NotEmpty(t, desc.OutputSchema)
	assert.NotEmpty(t, desc.Description)
}

func TestBuildSkillActivateDescriptorWithIndex(t *testing.T) {
	index := "Available skills:\n- billing: Handle billing\n- orders: Handle orders"
	desc := BuildSkillActivateDescriptorWithIndex(index)
	assert.Equal(t, SkillActivateTool, desc.Name)
	assert.Equal(t, SkillNamespace, desc.Namespace)
	assert.Equal(t, SkillExecutorName, desc.Mode)
	assert.Contains(t, desc.Description, "Available skills:")
	assert.Contains(t, desc.Description, "billing: Handle billing")
	assert.Contains(t, desc.Description, "Activate a skill")
}

func TestBuildSkillDeactivateDescriptor(t *testing.T) {
	desc := BuildSkillDeactivateDescriptor()
	assert.Equal(t, SkillDeactivateTool, desc.Name)
	assert.Equal(t, SkillNamespace, desc.Namespace)
	assert.Equal(t, SkillExecutorName, desc.Mode)
	assert.NotEmpty(t, desc.InputSchema)
	assert.NotEmpty(t, desc.OutputSchema)
}

func TestBuildSkillReadResourceDescriptor(t *testing.T) {
	desc := BuildSkillReadResourceDescriptor()
	assert.Equal(t, SkillReadResourceTool, desc.Name)
	assert.Equal(t, SkillNamespace, desc.Namespace)
	assert.Equal(t, SkillExecutorName, desc.Mode)
	assert.NotEmpty(t, desc.InputSchema)
	assert.NotEmpty(t, desc.OutputSchema)
}

func TestToolExecutor_Name(t *testing.T) {
	exec := NewToolExecutor(nil)
	assert.Equal(t, SkillExecutorName, exec.Name())
}

func newTestToolExecutor(t *testing.T, sources []SkillSource) (*Executor, *tools.Registry) {
	t.Helper()
	reg := NewRegistry()
	require.NoError(t, reg.Discover(sources))
	executor := NewExecutor(ExecutorConfig{Registry: reg})

	toolReg := tools.NewRegistry()
	_ = toolReg.Register(BuildSkillActivateDescriptor())
	_ = toolReg.Register(BuildSkillDeactivateDescriptor())
	_ = toolReg.Register(BuildSkillReadResourceDescriptor())
	toolReg.RegisterExecutor(NewToolExecutor(executor))

	return executor, toolReg
}

func TestToolExecutor_Activate(t *testing.T) {
	sources := []SkillSource{{
		Name:         "test-skill",
		Description:  "A test skill",
		Instructions: "Test instructions.",
	}}
	_, toolReg := newTestToolExecutor(t, sources)

	result, err := toolReg.Execute(SkillActivateTool, []byte(`{"name":"test-skill"}`))
	require.NoError(t, err)
	assert.NotEmpty(t, result.Result)
	assert.Empty(t, result.Error)

	var parsed map[string]any
	require.NoError(t, json.Unmarshal(result.Result, &parsed))
	assert.Equal(t, "Test instructions.", parsed["instructions"])
}

func TestToolExecutor_Deactivate(t *testing.T) {
	sources := []SkillSource{{
		Name:         "test-skill",
		Description:  "A test skill",
		Instructions: "Test instructions.",
	}}
	_, toolReg := newTestToolExecutor(t, sources)

	// Activate first
	_, err := toolReg.Execute(SkillActivateTool, []byte(`{"name":"test-skill"}`))
	require.NoError(t, err)

	// Then deactivate
	result, err := toolReg.Execute(SkillDeactivateTool, []byte(`{"name":"test-skill"}`))
	require.NoError(t, err)
	assert.NotEmpty(t, result.Result)
	assert.Empty(t, result.Error)
}

func TestToolExecutor_ReadResource(t *testing.T) {
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

	sources := []SkillSource{{Dir: dir}}
	_, toolReg := newTestToolExecutor(t, sources)

	result, err := toolReg.Execute(
		SkillReadResourceTool,
		[]byte(`{"skill_name":"res-skill","path":"data.txt"}`),
	)
	require.NoError(t, err)
	assert.Contains(t, string(result.Result), "hello world")
	assert.Empty(t, result.Error)
}

func TestToolExecutor_ActivateUnknownSkill(t *testing.T) {
	sources := []SkillSource{{
		Name:         "test-skill",
		Description:  "A test skill",
		Instructions: "Test instructions.",
	}}
	_, toolReg := newTestToolExecutor(t, sources)

	result, err := toolReg.Execute(SkillActivateTool, []byte(`{"name":"nonexistent"}`))
	require.NoError(t, err)
	assert.NotEmpty(t, result.Error)
}

func TestToolExecutor_DeactivateInactive(t *testing.T) {
	sources := []SkillSource{{
		Name:         "test-skill",
		Description:  "A test skill",
		Instructions: "Test instructions.",
	}}
	_, toolReg := newTestToolExecutor(t, sources)

	result, err := toolReg.Execute(SkillDeactivateTool, []byte(`{"name":"test-skill"}`))
	require.NoError(t, err)
	assert.NotEmpty(t, result.Error)
}

func TestToolExecutor_InvalidArgs(t *testing.T) {
	sources := []SkillSource{{
		Name:         "test-skill",
		Description:  "A test skill",
		Instructions: "Test instructions.",
	}}
	_, toolReg := newTestToolExecutor(t, sources)

	// Registry validates JSON before passing to executor, so error may come from either layer
	result, err := toolReg.Execute(SkillActivateTool, []byte(`{invalid`))
	if err != nil {
		assert.Contains(t, err.Error(), "invalid")
	} else {
		assert.NotEmpty(t, result.Error)
	}
}

func TestToolExecutor_UnknownTool(t *testing.T) {
	exec := NewToolExecutor(nil)
	desc := &tools.ToolDescriptor{Name: "skill__unknown", Mode: SkillExecutorName}
	_, err := exec.Execute(desc, nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unknown skill tool")
}
