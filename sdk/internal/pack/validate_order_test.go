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
