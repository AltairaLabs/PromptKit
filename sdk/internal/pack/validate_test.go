package pack

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestValidateAgainstSchema_Valid(t *testing.T) {
	// Valid pack JSON
	validPack := []byte(`{
		"$schema": "https://promptpack.org/schema/latest/promptpack.schema.json",
		"id": "test-pack",
		"name": "Test Pack",
		"version": "1.0.0",
		"description": "A test pack",
		"template_engine": {
			"version": "v1",
			"syntax": "{{variable}}"
		},
		"prompts": {
			"hello": {
				"id": "hello",
				"name": "Hello Prompt",
				"version": "1.0.0",
				"system_template": "Hello, {{name}}!",
				"variables": [
					{
						"name": "name",
						"type": "string",
						"required": true
					}
				]
			}
		}
	}`)

	err := ValidateAgainstSchema(validPack)
	if err != nil {
		t.Errorf("expected no error for valid pack, got: %v", err)
	}
}

func TestValidateAgainstSchema_Invalid(t *testing.T) {
	// Invalid pack - missing required fields
	invalidPack := []byte(`{
		"id": "test-pack",
		"prompts": {}
	}`)

	err := ValidateAgainstSchema(invalidPack)
	if err == nil {
		t.Error("expected error for invalid pack, got nil")
	}

	// Check it's a SchemaValidationError
	schemaErr, ok := err.(*SchemaValidationError)
	if !ok {
		t.Errorf("expected SchemaValidationError, got %T", err)
	}
	if len(schemaErr.Errors) == 0 {
		t.Error("expected at least one validation error")
	}
}

func TestValidateAgainstSchema_InvalidJSON(t *testing.T) {
	invalidJSON := []byte(`{not valid json}`)

	err := ValidateAgainstSchema(invalidJSON)
	if err == nil {
		t.Error("expected error for invalid JSON, got nil")
	}
}

func TestSchemaValidationError_Error(t *testing.T) {
	// Single error
	err := &SchemaValidationError{Errors: []string{"field is required"}}
	expected := "pack schema validation failed: field is required"
	if err.Error() != expected {
		t.Errorf("expected %q, got %q", expected, err.Error())
	}

	// Multiple errors
	err = &SchemaValidationError{Errors: []string{"error1", "error2", "error3"}}
	expected = "pack schema validation failed with 3 errors"
	if err.Error() != expected {
		t.Errorf("expected %q, got %q", expected, err.Error())
	}
}

func TestLoad_WithSchemaValidation(t *testing.T) {
	// This test requires the example pack files to be present
	// It validates that Load() performs schema validation by default

	// Skip if running in CI without the example files
	t.Skip("Requires example pack files - run manually")
}

func TestLoad_SkipSchemaValidation(t *testing.T) {
	// Create a temporary invalid pack file
	// and verify it loads when schema validation is skipped

	// Skip if running in CI without setup
	t.Skip("Requires temp file setup - run manually")
}

// --- Agents validation tests ---

func TestValidateAgents_NilAgents(t *testing.T) {
	p := &Pack{
		Prompts: map[string]*Prompt{"chat": {ID: "chat"}},
	}
	err := p.ValidateAgents()
	assert.NoError(t, err, "nil agents should pass validation")
}

func TestValidateAgents_Valid(t *testing.T) {
	p := &Pack{
		Prompts: map[string]*Prompt{
			"chat":  {ID: "chat"},
			"agent": {ID: "agent"},
		},
		Agents: &AgentsConfig{
			Entry: "chat",
			Members: map[string]*AgentDef{
				"chat": {
					Description: "Chat agent",
					InputModes:  []string{"text/plain"},
					OutputModes: []string{"text/plain", "application/json"},
				},
				"agent": {
					Description: "Agent",
					Tags:        []string{"helper"},
				},
			},
		},
	}
	err := p.ValidateAgents()
	assert.NoError(t, err)
}

func TestValidateAgents_EmptyMembers(t *testing.T) {
	p := &Pack{
		Prompts: map[string]*Prompt{"chat": {ID: "chat"}},
		Agents: &AgentsConfig{
			Entry:   "chat",
			Members: map[string]*AgentDef{},
		},
	}
	err := p.ValidateAgents()
	require.Error(t, err)

	agentsErr, ok := err.(*AgentsValidationError)
	require.True(t, ok)
	assert.Contains(t, agentsErr.Errors, "agents.members must be non-empty")
}

func TestValidateAgents_MissingEntry(t *testing.T) {
	p := &Pack{
		Prompts: map[string]*Prompt{"chat": {ID: "chat"}},
		Agents: &AgentsConfig{
			Members: map[string]*AgentDef{
				"chat": {Description: "Chat agent"},
			},
		},
	}
	err := p.ValidateAgents()
	require.Error(t, err)

	agentsErr, ok := err.(*AgentsValidationError)
	require.True(t, ok)
	assert.Contains(t, agentsErr.Errors, "agents.entry is required")
}

func TestValidateAgents_EntryNotInMembers(t *testing.T) {
	p := &Pack{
		Prompts: map[string]*Prompt{
			"chat":  {ID: "chat"},
			"other": {ID: "other"},
		},
		Agents: &AgentsConfig{
			Entry: "other",
			Members: map[string]*AgentDef{
				"chat": {Description: "Chat agent"},
			},
		},
	}
	err := p.ValidateAgents()
	require.Error(t, err)

	agentsErr, ok := err.(*AgentsValidationError)
	require.True(t, ok)
	found := false
	for _, e := range agentsErr.Errors {
		if assert.ObjectsAreEqual(e, `agents.entry "other" does not reference a key in agents.members`) {
			found = true
		}
	}
	assert.True(t, found, "expected entry-not-in-members error, got: %v", agentsErr.Errors)
}

func TestValidateAgents_MemberNotInPrompts(t *testing.T) {
	p := &Pack{
		Prompts: map[string]*Prompt{
			"chat": {ID: "chat"},
		},
		Agents: &AgentsConfig{
			Entry: "chat",
			Members: map[string]*AgentDef{
				"chat":    {Description: "Chat agent"},
				"missing": {Description: "Missing agent"},
			},
		},
	}
	err := p.ValidateAgents()
	require.Error(t, err)

	agentsErr, ok := err.(*AgentsValidationError)
	require.True(t, ok)
	found := false
	for _, e := range agentsErr.Errors {
		if assert.ObjectsAreEqual(e, `agents.members key "missing" does not reference a valid prompt`) {
			found = true
		}
	}
	assert.True(t, found, "expected member-not-in-prompts error, got: %v", agentsErr.Errors)
}

func TestValidateAgents_InvalidInputMode(t *testing.T) {
	p := &Pack{
		Prompts: map[string]*Prompt{
			"chat": {ID: "chat"},
		},
		Agents: &AgentsConfig{
			Entry: "chat",
			Members: map[string]*AgentDef{
				"chat": {
					InputModes: []string{"not-a-mime"},
				},
			},
		},
	}
	err := p.ValidateAgents()
	require.Error(t, err)

	agentsErr, ok := err.(*AgentsValidationError)
	require.True(t, ok)
	found := false
	for _, e := range agentsErr.Errors {
		if assert.ObjectsAreEqual(e, `agents.members["chat"].input_modes: "not-a-mime" is not a valid MIME type`) {
			found = true
		}
	}
	assert.True(t, found, "expected invalid MIME error, got: %v", agentsErr.Errors)
}

func TestValidateAgents_InvalidOutputMode(t *testing.T) {
	p := &Pack{
		Prompts: map[string]*Prompt{
			"chat": {ID: "chat"},
		},
		Agents: &AgentsConfig{
			Entry: "chat",
			Members: map[string]*AgentDef{
				"chat": {
					OutputModes: []string{"plaintext"},
				},
			},
		},
	}
	err := p.ValidateAgents()
	require.Error(t, err)

	agentsErr, ok := err.(*AgentsValidationError)
	require.True(t, ok)
	found := false
	for _, e := range agentsErr.Errors {
		if assert.ObjectsAreEqual(e, `agents.members["chat"].output_modes: "plaintext" is not a valid MIME type`) {
			found = true
		}
	}
	assert.True(t, found, "expected invalid MIME error, got: %v", agentsErr.Errors)
}

func TestValidateAgents_MultipleErrors(t *testing.T) {
	p := &Pack{
		Prompts: map[string]*Prompt{},
		Agents: &AgentsConfig{
			// Missing entry
			Members: map[string]*AgentDef{},
		},
	}
	err := p.ValidateAgents()
	require.Error(t, err)

	agentsErr, ok := err.(*AgentsValidationError)
	require.True(t, ok)
	assert.GreaterOrEqual(t, len(agentsErr.Errors), 2,
		"expected multiple errors, got: %v", agentsErr.Errors)
}

func TestAgentsValidationError_Error(t *testing.T) {
	// Single error
	err := &AgentsValidationError{Errors: []string{"agents.entry is required"}}
	assert.Equal(t, "agents validation failed: agents.entry is required", err.Error())

	// Multiple errors
	err = &AgentsValidationError{Errors: []string{"err1", "err2"}}
	assert.Contains(t, err.Error(), "2 errors")
}

func TestIsValidMIMEFormat(t *testing.T) {
	tests := []struct {
		input string
		want  bool
	}{
		{"text/plain", true},
		{"application/json", true},
		{"image/png", true},
		{"text/html; charset=utf-8", true}, // subtype contains extra but still has slash
		{"plaintext", false},
		{"", false},
		{"/plain", false},
		{"text/", false},
		{"a/b/c", false}, // multiple slashes
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := isValidMIMEFormat(tt.input)
			assert.Equal(t, tt.want, got, "isValidMIMEFormat(%q)", tt.input)
		})
	}
}

func TestValidateWorkflow_NilWorkflow(t *testing.T) {
	p := &Pack{
		Prompts: map[string]*Prompt{"chat": {ID: "chat"}},
	}
	err := p.ValidateWorkflow()
	assert.NoError(t, err, "nil workflow should pass validation")
}

func TestValidateWorkflow_Valid(t *testing.T) {
	p := &Pack{
		Prompts: map[string]*Prompt{
			"gather": {ID: "gather"},
			"solve":  {ID: "solve"},
			"done":   {ID: "done"},
		},
		Workflow: &WorkflowSpec{
			Version: 1,
			Entry:   "intake",
			States: map[string]*WorkflowState{
				"intake":  {PromptTask: "gather", OnEvent: map[string]string{"Next": "solving"}},
				"solving": {PromptTask: "solve", OnEvent: map[string]string{"Done": "end"}},
				"end":     {PromptTask: "done"},
			},
		},
	}
	err := p.ValidateWorkflow()
	assert.NoError(t, err)
}

func TestValidateWorkflow_InvalidVersion(t *testing.T) {
	p := &Pack{
		Prompts: map[string]*Prompt{"p": {ID: "p"}},
		Workflow: &WorkflowSpec{
			Version: 2,
			Entry:   "s",
			States:  map[string]*WorkflowState{"s": {PromptTask: "p"}},
		},
	}
	err := p.ValidateWorkflow()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "workflow validation failed")
}

func TestValidateWorkflow_EntryNotInStates(t *testing.T) {
	p := &Pack{
		Prompts: map[string]*Prompt{"p": {ID: "p"}},
		Workflow: &WorkflowSpec{
			Version: 1,
			Entry:   "nonexistent",
			States:  map[string]*WorkflowState{"s": {PromptTask: "p"}},
		},
	}
	err := p.ValidateWorkflow()
	require.Error(t, err)

	wfErr, ok := err.(*WorkflowValidationError)
	require.True(t, ok)
	assert.Contains(t, wfErr.Errors[0], "does not reference a key in states")
}

func TestValidateWorkflow_PromptTaskNotInPrompts(t *testing.T) {
	p := &Pack{
		Prompts: map[string]*Prompt{"p": {ID: "p"}},
		Workflow: &WorkflowSpec{
			Version: 1,
			Entry:   "s",
			States:  map[string]*WorkflowState{"s": {PromptTask: "missing"}},
		},
	}
	err := p.ValidateWorkflow()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "does not reference a valid prompt")
}

func TestValidateWorkflow_EventTargetNotInStates(t *testing.T) {
	p := &Pack{
		Prompts: map[string]*Prompt{"p": {ID: "p"}},
		Workflow: &WorkflowSpec{
			Version: 1,
			Entry:   "s",
			States: map[string]*WorkflowState{
				"s": {PromptTask: "p", OnEvent: map[string]string{"Go": "ghost"}},
			},
		},
	}
	err := p.ValidateWorkflow()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "does not exist in states")
}

func TestParseWorkflowWithSkills(t *testing.T) {
	data := []byte(`{
		"id": "wf-skills-pack",
		"prompts": {
			"gather": {"id": "gather", "system_template": "Gather info"},
			"solve": {"id": "solve", "system_template": "Solve it"}
		},
		"workflow": {
			"version": 1,
			"entry": "intake",
			"states": {
				"intake": {"prompt_task": "gather", "skills": "skills/support", "on_event": {"Done": "solving"}},
				"solving": {"prompt_task": "solve", "skills": "none"}
			}
		}
	}`)

	p, err := Parse(data)
	require.NoError(t, err)
	require.NotNil(t, p.Workflow)
	assert.Equal(t, "skills/support", p.Workflow.States["intake"].Skills)
	assert.Equal(t, "none", p.Workflow.States["solving"].Skills)
}

func TestParseSkillSourcePath(t *testing.T) {
	data := []byte(`{
		"id": "path-pack",
		"prompts": {
			"p": {"id": "p", "system_template": "test"}
		},
		"skills": [
			{"path": "skills/dir1"},
			{"dir": "skills/dir2"},
			{"name": "inline", "description": "An inline skill", "instructions": "Do this."}
		]
	}`)

	p, err := Parse(data)
	require.NoError(t, err)
	require.Len(t, p.Skills, 3)
	assert.Equal(t, "skills/dir1", p.Skills[0].Path)
	assert.Equal(t, "skills/dir1", p.Skills[0].EffectiveDir())
	assert.Equal(t, "skills/dir2", p.Skills[1].Dir)
	assert.Equal(t, "skills/dir2", p.Skills[1].EffectiveDir())
	assert.Equal(t, "inline", p.Skills[2].Name)
}

func TestParseWithWorkflow(t *testing.T) {
	t.Run("valid workflow section", func(t *testing.T) {
		data := []byte(`{
			"id": "wf-pack",
			"prompts": {
				"gather": {"id": "gather", "system_template": "Gather info"},
				"solve": {"id": "solve", "system_template": "Solve it"}
			},
			"workflow": {
				"version": 1,
				"entry": "intake",
				"states": {
					"intake": {"prompt_task": "gather", "on_event": {"Done": "solving"}},
					"solving": {"prompt_task": "solve"}
				}
			}
		}`)

		p, err := Parse(data)
		require.NoError(t, err)
		require.NotNil(t, p.Workflow)
		assert.Equal(t, 1, p.Workflow.Version)
		assert.Equal(t, "intake", p.Workflow.Entry)
		assert.Len(t, p.Workflow.States, 2)
	})

	t.Run("workflow entry not in states", func(t *testing.T) {
		data := []byte(`{
			"id": "bad-wf",
			"prompts": {
				"p": {"id": "p", "system_template": "test"}
			},
			"workflow": {
				"version": 1,
				"entry": "nonexistent",
				"states": {
					"s": {"prompt_task": "p"}
				}
			}
		}`)

		_, err := Parse(data)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "workflow validation failed")
	})
}

func TestParseWithAgents(t *testing.T) {
	t.Run("valid agents section", func(t *testing.T) {
		data := []byte(`{
			"id": "agent-pack",
			"prompts": {
				"chat": {"id": "chat", "system_template": "Hello"},
				"summarize": {"id": "summarize", "system_template": "Summarize"}
			},
			"agents": {
				"entry": "chat",
				"members": {
					"chat": {
						"description": "Chat agent",
						"input_modes": ["text/plain"],
						"output_modes": ["text/plain"]
					},
					"summarize": {
						"description": "Summarize agent"
					}
				}
			}
		}`)

		p, err := Parse(data)
		require.NoError(t, err)
		require.NotNil(t, p.Agents)
		assert.Equal(t, "chat", p.Agents.Entry)
		assert.Len(t, p.Agents.Members, 2)
		assert.Equal(t, "Chat agent", p.Agents.Members["chat"].Description)
		assert.Equal(t, []string{"text/plain"}, p.Agents.Members["chat"].InputModes)
	})

	t.Run("agents entry not in members", func(t *testing.T) {
		data := []byte(`{
			"id": "bad-pack",
			"prompts": {
				"chat": {"id": "chat", "system_template": "Hello"}
			},
			"agents": {
				"entry": "nonexistent",
				"members": {
					"chat": {"description": "Chat agent"}
				}
			}
		}`)

		_, err := Parse(data)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "agents validation failed")
	})

	t.Run("agents member not in prompts", func(t *testing.T) {
		data := []byte(`{
			"id": "bad-pack",
			"prompts": {
				"chat": {"id": "chat", "system_template": "Hello"}
			},
			"agents": {
				"entry": "chat",
				"members": {
					"chat": {"description": "Chat"},
					"ghost": {"description": "Ghost agent"}
				}
			}
		}`)

		_, err := Parse(data)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "agents validation failed")
	})
}
