package provider

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	_ "github.com/AltairaLabs/PromptKit/runtime/v2/evals/handlers"
	"github.com/AltairaLabs/PromptKit/runtime/v2/events"
	"github.com/AltairaLabs/PromptKit/runtime/v2/hooks"
	"github.com/AltairaLabs/PromptKit/runtime/v2/hooks/guardrails"
	"github.com/AltairaLabs/PromptKit/runtime/v2/pipeline/stage/internal/core"
	"github.com/AltairaLabs/PromptKit/runtime/v2/providers/mock"
)

// The ProviderStage reports the turn TurnState carries, rather than counting
// executions itself, as the 0-based index hosts see (#2797): TurnState's 7th
// turn is turn index 6.
//
// A local counter would be right only while one stage instance outlives the
// conversation; a caller that rebuilds the pipeline per turn would get the
// first turn forever. Both turns here are stamped 7, which no counter would
// produce.
func TestProviderStage_ReportsTurnStateTurnIndex(t *testing.T) {
	guard, err := guardrails.NewGuardrailHook("length", map[string]any{
		"max_characters": 5,
	})
	require.NoError(t, err)

	bus := events.NewEventBus()
	getEvents := collectValidationFailures(t, bus)
	emitter := events.NewEmitter(bus, "run1", "sess1", "conv1")

	turnState := core.NewTurnState()
	stage := NewProviderStageWithTurnState(
		mock.NewProvider("p", "m", false), nil, nil,
		&ProviderConfig{MaxTokens: 100},
		emitter,
		hooks.NewRegistry(hooks.WithProviderHook(guard)),
		turnState,
	)

	for range 2 {
		turnState.SetTurnIndex(7)
		_, runErr := runProviderStage(t, stage, "hello")
		require.NoError(t, runErr)
	}

	got := getEvents()
	require.Len(t, got, 2)
	for _, d := range got {
		assert.Equal(t, 6, d.TurnIndex, "the stage must report the turn state's turn, 0-based")
	}
}

// With no turn state the stage reports 0 — never a negative index, and never a
// locally invented count that would look like data.
func TestProviderStage_NoTurnStateReportsUnknown(t *testing.T) {
	guard, err := guardrails.NewGuardrailHook("length", map[string]any{
		"max_characters": 5,
	})
	require.NoError(t, err)

	bus := events.NewEventBus()
	getEvents := collectValidationFailures(t, bus)
	emitter := events.NewEmitter(bus, "run1", "sess1", "conv1")

	stage := NewProviderStageWithHooks(
		mock.NewProvider("p", "m", false), nil, nil,
		&ProviderConfig{MaxTokens: 100},
		emitter,
		hooks.NewRegistry(hooks.WithProviderHook(guard)),
	)

	_, runErr := runProviderStage(t, stage, "hello")
	require.NoError(t, runErr)

	got := getEvents()
	require.Len(t, got, 1)
	assert.Equal(t, 0, got[0].TurnIndex, "no turn state means no turn, not turn 1")
}
