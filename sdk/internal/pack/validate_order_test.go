package pack

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/v2/packspec"
)

// Agents findings come in member-name order on every run.
func TestValidateAgents_FindingsInMemberOrder(t *testing.T) {
	p := &Pack{Pack: packspec.Pack{
		Prompts: map[string]*Prompt{},
		Agents: &AgentsConfig{Entry: "a", Members: map[string]*AgentDef{
			"d": {InputModes: []string{"bad"}}, "b": {InputModes: []string{"bad"}},
			"c": {State: "s"}, "a": {State: "s"},
		}},
	}}
	err := validateAgentsSection(p)
	require.Error(t, err)
	var first []string
	for i := 0; i < 30; i++ {
		agentsErr, ok := validateAgentsSection(p).(*AgentsValidationError)
		require.True(t, ok)
		if i == 0 {
			first = agentsErr.Errors
			continue
		}
		require.Equal(t, first, agentsErr.Errors, "run %d", i)
	}
	assert.Equal(t, []string{
		`agents.members key "a" does not reference a valid prompt`,
		`agents.members key "b" does not reference a valid prompt`,
		`agents.members key "c" does not reference a valid prompt`,
		`agents.members key "d" does not reference a valid prompt`,
		`agents.members["b"].input_modes: "bad" is not a valid MIME type`,
		`agents.members["d"].input_modes: "bad" is not a valid MIME type`,
		`agents.members["a"] sets state "s" but requires a top-level workflow`,
		`agents.members["c"] sets state "s" but requires a top-level workflow`,
	}, first)
}

// Workflow findings come in state, then event, name order on every run.
func TestValidateWorkflow_FindingsInStateAndEventOrder(t *testing.T) {
	p := &Pack{Pack: packspec.Pack{
		Prompts: map[string]*Prompt{"ok": {ID: "ok"}},
		Workflow: &WorkflowSpec{Version: 1, Entry: "b", States: map[string]*WorkflowState{
			"c": {PromptTask: "ok", OnEvent: map[string]string{"Z": "gone", "A": "gone"}},
			"b": {PromptTask: "missing"},
			"a": {PromptTask: "ok", OnEvent: map[string]string{"Next": "gone"}},
		}},
	}}
	var first []string
	for i := 0; i < 30; i++ {
		wfErr, ok := validateWorkflowSection(p).(*WorkflowValidationError)
		require.True(t, ok)
		if i == 0 {
			first = wfErr.Errors
			continue
		}
		require.Equal(t, first, wfErr.Errors, "run %d", i)
	}
	assert.Equal(t, []string{
		`workflow.states["a"].on_event["Next"] target "gone" does not exist in states`,
		`workflow.states["b"].prompt_task "missing" does not reference a valid prompt`,
		`workflow.states["c"].on_event["A"] target "gone" does not exist in states`,
		`workflow.states["c"].on_event["Z"] target "gone" does not exist in states`,
	}, first)
}
