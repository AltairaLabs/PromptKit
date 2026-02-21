package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/AltairaLabs/PromptKit/runtime/prompt"
	"github.com/AltairaLabs/PromptKit/runtime/workflow"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func writeSkillMD(t *testing.T, dir, name, desc string) {
	t.Helper()
	require.NoError(t, os.MkdirAll(dir, 0755))
	content := "---\nname: " + name + "\ndescription: " + desc + "\n---\nInstructions here."
	require.NoError(t, os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte(content), 0644))
}

func writeSkillMDWithTools(t *testing.T, dir, name, desc string, tools []string) {
	t.Helper()
	require.NoError(t, os.MkdirAll(dir, 0755))
	content := "---\nname: " + name + "\ndescription: " + desc + "\nallowed-tools:\n"
	for _, tool := range tools {
		content += "  - " + tool + "\n"
	}
	content += "---\nInstructions here."
	require.NoError(t, os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte(content), 0644))
}

func TestValidateSkillErrors_ValidDirectory(t *testing.T) {
	tmpDir := t.TempDir()
	skillDir := filepath.Join(tmpDir, "skills", "my-skill")
	writeSkillMD(t, skillDir, "my-skill", "A test skill")

	pack := &prompt.Pack{
		Skills: []prompt.SkillSourceConfig{
			{Dir: "skills"},
		},
		Prompts: map[string]*prompt.PackPrompt{"p": {ID: "p"}},
	}

	errs := ValidateSkillErrors(pack, tmpDir)
	assert.Empty(t, errs)
}

func TestValidateSkillErrors_MissingDirectory(t *testing.T) {
	tmpDir := t.TempDir()

	pack := &prompt.Pack{
		Skills: []prompt.SkillSourceConfig{
			{Dir: "nonexistent-dir"},
		},
		Prompts: map[string]*prompt.PackPrompt{"p": {ID: "p"}},
	}

	errs := ValidateSkillErrors(pack, tmpDir)
	require.Len(t, errs, 1)
	assert.Contains(t, errs[0], "does not exist")
}

func TestValidateSkillErrors_MissingSkillMD(t *testing.T) {
	tmpDir := t.TempDir()
	// Create an empty directory (no SKILL.md)
	require.NoError(t, os.MkdirAll(filepath.Join(tmpDir, "skills", "empty-skill"), 0755))

	pack := &prompt.Pack{
		Skills: []prompt.SkillSourceConfig{
			{Dir: "skills"},
		},
		Prompts: map[string]*prompt.PackPrompt{"p": {ID: "p"}},
	}

	// No error — a directory with no SKILL.md is simply not discovered as a skill
	errs := ValidateSkillErrors(pack, tmpDir)
	assert.Empty(t, errs)
}

func TestValidateSkillErrors_DuplicateNames(t *testing.T) {
	tmpDir := t.TempDir()
	writeSkillMD(t, filepath.Join(tmpDir, "skills-a", "sk"), "dupe-skill", "Skill A")
	writeSkillMD(t, filepath.Join(tmpDir, "skills-b", "sk"), "dupe-skill", "Skill B")

	pack := &prompt.Pack{
		Skills: []prompt.SkillSourceConfig{
			{Dir: "skills-a"},
			{Dir: "skills-b"},
		},
		Prompts: map[string]*prompt.PackPrompt{"p": {ID: "p"}},
	}

	errs := ValidateSkillErrors(pack, tmpDir)
	require.Len(t, errs, 1)
	assert.Contains(t, errs[0], "duplicate skill name")
}

func TestValidateSkillErrors_InlineComplete(t *testing.T) {
	pack := &prompt.Pack{
		Skills: []prompt.SkillSourceConfig{
			{Name: "inline-skill", Description: "An inline skill", Instructions: "Do the thing."},
		},
		Prompts: map[string]*prompt.PackPrompt{"p": {ID: "p"}},
	}

	errs := ValidateSkillErrors(pack, "/tmp")
	assert.Empty(t, errs)
}

func TestValidateSkillErrors_InlineIncomplete(t *testing.T) {
	pack := &prompt.Pack{
		Skills: []prompt.SkillSourceConfig{
			{Name: "incomplete-skill"},
		},
		Prompts: map[string]*prompt.PackPrompt{"p": {ID: "p"}},
	}

	errs := ValidateSkillErrors(pack, "/tmp")
	require.Len(t, errs, 2)
	assert.Contains(t, errs[0], "missing required field: description")
	assert.Contains(t, errs[1], "missing required field: instructions")
}

func TestValidateSkillErrors_PathAlias(t *testing.T) {
	tmpDir := t.TempDir()
	skillDir := filepath.Join(tmpDir, "my-skills", "sk")
	writeSkillMD(t, skillDir, "path-skill", "Uses path alias")

	pack := &prompt.Pack{
		Skills: []prompt.SkillSourceConfig{
			{Path: "my-skills"}, // using path instead of dir
		},
		Prompts: map[string]*prompt.PackPrompt{"p": {ID: "p"}},
	}

	errs := ValidateSkillErrors(pack, tmpDir)
	assert.Empty(t, errs)
}

func TestValidateSkillErrors_DirTakesPrecedenceOverPath(t *testing.T) {
	tmpDir := t.TempDir()
	writeSkillMD(t, filepath.Join(tmpDir, "dir-skills", "sk"), "dir-skill", "From dir")

	pack := &prompt.Pack{
		Skills: []prompt.SkillSourceConfig{
			{Dir: "dir-skills", Path: "nonexistent"}, // dir wins
		},
		Prompts: map[string]*prompt.PackPrompt{"p": {ID: "p"}},
	}

	errs := ValidateSkillErrors(pack, tmpDir)
	assert.Empty(t, errs)
}

func TestValidateSkillErrors_WorkflowStateSkills_Valid(t *testing.T) {
	tmpDir := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(tmpDir, "state-skills"), 0755))

	pack := &prompt.Pack{
		Workflow: &workflow.Spec{
			Version: 1,
			Entry:   "start",
			States: map[string]*workflow.State{
				"start": {
					PromptTask: "p",
					Skills:     "state-skills",
				},
			},
		},
		Prompts: map[string]*prompt.PackPrompt{"p": {ID: "p"}},
	}

	errs := ValidateSkillErrors(pack, tmpDir)
	assert.Empty(t, errs)
}

func TestValidateSkillErrors_WorkflowStateSkills_None(t *testing.T) {
	pack := &prompt.Pack{
		Workflow: &workflow.Spec{
			Version: 1,
			Entry:   "start",
			States: map[string]*workflow.State{
				"start": {
					PromptTask: "p",
					Skills:     "none",
				},
			},
		},
		Prompts: map[string]*prompt.PackPrompt{"p": {ID: "p"}},
	}

	errs := ValidateSkillErrors(pack, "/tmp")
	assert.Empty(t, errs)
}

func TestValidateSkillErrors_WorkflowStateSkills_Missing(t *testing.T) {
	tmpDir := t.TempDir()

	pack := &prompt.Pack{
		Workflow: &workflow.Spec{
			Version: 1,
			Entry:   "start",
			States: map[string]*workflow.State{
				"start": {
					PromptTask: "p",
					Skills:     "nonexistent-skills-dir",
				},
			},
		},
		Prompts: map[string]*prompt.PackPrompt{"p": {ID: "p"}},
	}

	errs := ValidateSkillErrors(pack, tmpDir)
	require.Len(t, errs, 1)
	assert.Contains(t, errs[0], "does not exist")
}

func TestValidateSkills_AllowedToolsCrossRef(t *testing.T) {
	tmpDir := t.TempDir()
	skillDir := filepath.Join(tmpDir, "skills", "my-skill")
	writeSkillMDWithTools(t, skillDir, "my-skill", "Skill with tools", []string{"existing_tool", "missing_tool"})

	pack := &prompt.Pack{
		Skills: []prompt.SkillSourceConfig{
			{Dir: "skills"},
		},
		Tools: map[string]*prompt.PackTool{
			"existing_tool": {Name: "existing_tool", Description: "Exists"},
		},
		Prompts: map[string]*prompt.PackPrompt{"p": {ID: "p"}},
	}

	warnings := ValidateSkills(pack, tmpDir)
	require.Len(t, warnings, 1)
	assert.Contains(t, warnings[0], "missing_tool")
	assert.Contains(t, warnings[0], "not defined in pack tools")
}

func TestValidateSkills_AllToolsExist(t *testing.T) {
	tmpDir := t.TempDir()
	skillDir := filepath.Join(tmpDir, "skills", "my-skill")
	writeSkillMDWithTools(t, skillDir, "my-skill", "Skill with tools", []string{"tool_a"})

	pack := &prompt.Pack{
		Skills: []prompt.SkillSourceConfig{
			{Dir: "skills"},
		},
		Tools: map[string]*prompt.PackTool{
			"tool_a": {Name: "tool_a", Description: "Tool A"},
		},
		Prompts: map[string]*prompt.PackPrompt{"p": {ID: "p"}},
	}

	warnings := ValidateSkills(pack, tmpDir)
	assert.Empty(t, warnings)
}

func TestValidateSkillErrors_SkillMDMissingName(t *testing.T) {
	tmpDir := t.TempDir()
	skillDir := filepath.Join(tmpDir, "skills", "bad-skill")
	require.NoError(t, os.MkdirAll(skillDir, 0755))
	content := "---\ndescription: no name field\n---\nInstructions."
	require.NoError(t, os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte(content), 0644))

	pack := &prompt.Pack{
		Skills: []prompt.SkillSourceConfig{
			{Dir: "skills"},
		},
		Prompts: map[string]*prompt.PackPrompt{"p": {ID: "p"}},
	}

	errs := ValidateSkillErrors(pack, tmpDir)
	require.Len(t, errs, 1)
	assert.Contains(t, errs[0], "name is required")
}

func TestValidateSkillErrors_SkillMDMissingDescription(t *testing.T) {
	tmpDir := t.TempDir()
	skillDir := filepath.Join(tmpDir, "skills", "bad-skill")
	require.NoError(t, os.MkdirAll(skillDir, 0755))
	content := "---\nname: no-desc\n---\nInstructions."
	require.NoError(t, os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte(content), 0644))

	pack := &prompt.Pack{
		Skills: []prompt.SkillSourceConfig{
			{Dir: "skills"},
		},
		Prompts: map[string]*prompt.PackPrompt{"p": {ID: "p"}},
	}

	errs := ValidateSkillErrors(pack, tmpDir)
	require.Len(t, errs, 1)
	assert.Contains(t, errs[0], "description is required")
}

func TestValidateSkillErrors_DuplicateAcrossInlineAndDir(t *testing.T) {
	tmpDir := t.TempDir()
	writeSkillMD(t, filepath.Join(tmpDir, "skills", "sk"), "shared-name", "Dir skill")

	pack := &prompt.Pack{
		Skills: []prompt.SkillSourceConfig{
			{Dir: "skills"},
			{Name: "shared-name", Description: "Inline skill", Instructions: "Do stuff"},
		},
		Prompts: map[string]*prompt.PackPrompt{"p": {ID: "p"}},
	}

	errs := ValidateSkillErrors(pack, tmpDir)
	require.Len(t, errs, 1)
	assert.Contains(t, errs[0], "duplicate skill name")
}

func TestValidateSkillErrors_NoSkills(t *testing.T) {
	pack := &prompt.Pack{
		Prompts: map[string]*prompt.PackPrompt{"p": {ID: "p"}},
	}

	errs := ValidateSkillErrors(pack, "/tmp")
	assert.Empty(t, errs)
}
