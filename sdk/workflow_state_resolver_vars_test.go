package sdk

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/v2/persistence/memory"
	"github.com/AltairaLabs/PromptKit/runtime/v2/pipeline/stage"
	"github.com/AltairaLabs/PromptKit/runtime/v2/prompt"
	"github.com/AltairaLabs/PromptKit/runtime/v2/workflow"
)

// The handoff loads the destination prompt with the turn's variables, so a
// fragment a turn variable chooses resolves, as PromptAssemblyStage loads it.
func TestRenderState_DynamicFragmentFromTurnVariables(t *testing.T) {
	repo := memory.NewPromptRepository()
	repo.RegisterFragment("persona_us", &prompt.Fragment{Content: "Use US spelling."})
	repo.RegisterPrompt("specialist", &prompt.Config{Spec: prompt.Spec{
		TaskType:       "specialist",
		SystemTemplate: "Specialist. {{persona_{{region}}}}",
		Fragments:      []prompt.FragmentRef{{Name: "persona_{{region}}", Required: true}},
	}})
	spec := &workflow.Spec{Version: 1, Entry: "specialist",
		States: map[string]*workflow.State{"specialist": {PromptTask: "specialist"}}}
	r := newWorkflowStateResolver(workflow.NewStateMachine(spec), spec, nil, prompt.NewRegistryWithRepository(repo))
	ts := stage.NewTurnState()
	ts.Variables = map[string]string{"region": "us"}
	r.turnState = func() *stage.TurnState { return ts }

	got, _, err := r.renderState(spec.States["specialist"], "", "")
	require.NoError(t, err)
	assert.Equal(t, "Specialist. Use US spelling.", got)
}
