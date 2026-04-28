package stages

import (
	"testing"

	"github.com/AltairaLabs/PromptKit/pkg/config"
	"github.com/AltairaLabs/PromptKit/runtime/pipeline/stage"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPersonaAssemblyStage_PopulatesTurnState(t *testing.T) {
	persona := &config.UserPersonaPack{
		ID:           "test-persona",
		Description:  "A persona used by the assembly stage tests",
		SystemPrompt: "You are a curious customer.",
	}

	turnState := stage.NewTurnState()
	s := NewPersonaAssemblyStageWithTurnState(persona, "us", map[string]string{"region": "us"}, turnState)

	results := runStage(t, s, []stage.StreamElement{
		newTestMessageElement("user", "Hello"),
	})

	require.Len(t, results, 1)
	assert.Equal(t, "You are a curious customer.", turnState.SystemPrompt)
	require.NotNil(t, turnState.Variables)
	assert.Equal(t, "us", turnState.Variables["region"])
}

func TestPersonaAssemblyStage_PreservesExistingTurnStateVariables(t *testing.T) {
	persona := &config.UserPersonaPack{
		ID:           "test-persona",
		SystemPrompt: "You are helpful.",
	}

	turnState := stage.NewTurnState()
	turnState.Variables = map[string]string{"region": "eu"}
	s := NewPersonaAssemblyStageWithTurnState(persona, "us", map[string]string{"region": "us"}, turnState)

	_ = runStage(t, s, []stage.StreamElement{newTestMessageElement("user", "Hi")})

	// Existing TurnState variable wins; persona's base var does not overwrite.
	assert.Equal(t, "eu", turnState.Variables["region"])
}

func TestPersonaAssemblyStage_BuildSystemPromptError(t *testing.T) {
	// SystemTemplate references a variable that is not provided, and there are
	// no fragments / fallback prompt — BuildSystemPrompt should error.
	persona := &config.UserPersonaPack{
		ID:             "broken",
		SystemTemplate: "Hello {{missing}}",
		RequiredVars:   []string{"missing"},
	}

	turnState := stage.NewTurnState()
	s := NewPersonaAssemblyStageWithTurnState(persona, "", nil, turnState)

	input := make(chan stage.StreamElement, 1)
	input <- newTestMessageElement("user", "Hi")
	close(input)
	output := make(chan stage.StreamElement, 1)

	err := s.Process(t.Context(), input, output)
	require.Error(t, err)
	assert.Empty(t, turnState.SystemPrompt)
}
