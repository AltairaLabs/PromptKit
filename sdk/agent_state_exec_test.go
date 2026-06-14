package sdk

import (
	"testing"

	"github.com/AltairaLabs/PromptKit/runtime/providers/mock"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// stateBackedAgentPackJSON: workflow entry is "start", but the state-backed
// agent member "triage" points at the "investigate" state — so a correct
// implementation opens that member's pipeline AT "investigate", not the entry.
const stateBackedAgentPackJSON = `{
	"schema_version": "2025.1",
	"id": "state-backed-agents",
	"version": "1.0.0",
	"template_engine": {"version": "v1", "syntax": "handlebars", "features": []},
	"prompts": {
		"analyst":     {"id": "analyst", "name": "Analyst", "version": "1.0.0", "system_template": "You analyze."},
		"triage":      {"id": "triage", "name": "Triage", "version": "1.0.0", "system_template": "You triage."},
		"investigate": {"id": "investigate", "name": "Investigate", "version": "1.0.0", "system_template": "You investigate."}
	},
	"workflow": {
		"version": 1,
		"entry": "start",
		"states": {
			"start": {"prompt_task": "triage", "on_event": {"Go": "investigate"}},
			"investigate": {"prompt_task": "investigate", "terminal": true}
		}
	},
	"agents": {
		"entry": "analyst",
		"members": {
			"analyst": {},
			"triage": {"state": "investigate"}
		}
	}
}`

// RFC 0011: a state-backed agent member is a workflow pipeline opened at the
// agent's state — the same workflow machinery, just entered at that state.
func TestOpenMultiAgent_StateBackedMemberStartsAtState(t *testing.T) {
	repo := mock.NewInMemoryMockRepository("ok")
	mockProv := mock.NewProviderWithRepository("m", "m", false, repo)
	packPath := createTestPackFile(t, stateBackedAgentPackJSON)

	sess, err := OpenMultiAgent(packPath, WithSkipSchemaValidation(), WithProvider(mockProv))
	require.NoError(t, err)
	defer sess.Close()

	member := sess.Members()["triage"]
	wc, ok := member.(*WorkflowConversation)
	require.True(t, ok, "state-backed member should be a workflow-backed pipeline")
	assert.Equal(t, "investigate", wc.machine.CurrentState(),
		"member pipeline must start at the agent's state, not the workflow entry")
}
