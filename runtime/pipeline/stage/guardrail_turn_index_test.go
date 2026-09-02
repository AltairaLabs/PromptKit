package stage

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	_ "github.com/AltairaLabs/PromptKit/runtime/evals/handlers"
	"github.com/AltairaLabs/PromptKit/runtime/events"
	"github.com/AltairaLabs/PromptKit/runtime/hooks"
	"github.com/AltairaLabs/PromptKit/runtime/hooks/guardrails"
	"github.com/AltairaLabs/PromptKit/runtime/pipeline"
	"github.com/AltairaLabs/PromptKit/runtime/providers/mock"
	"github.com/AltairaLabs/PromptKit/runtime/statestore"
	"github.com/AltairaLabs/PromptKit/runtime/types"
)

// runLoadStage drives StateStoreLoadStage over one incoming user message and
// returns the turn index it published.
func runLoadStage(t *testing.T, store statestore.Store, convID string, incoming *types.Message) int {
	t.Helper()

	turnState := NewTurnState()
	s := NewStateStoreLoadStageWithTurnState(&pipeline.StateStoreConfig{
		Store:          store,
		ConversationID: convID,
	}, turnState)

	input := make(chan StreamElement, 1)
	if incoming != nil {
		input <- NewMessageElement(incoming)
	}
	close(input)

	output := make(chan StreamElement, 64)
	require.NoError(t, s.Process(context.Background(), input, output))
	for range output { //nolint:revive // drain
	}
	return turnState.TurnIndex()
}

// The turn index comes from the persisted transcript, so a conversation resumed
// with prior history continues from where it left off.
//
// This is what a counter cannot do. An in-process counter starts at zero every
// time the program does, so a resumed conversation would report turn 1 for what
// the transcript shows as turn 4 — and the number exists precisely to line these
// events up against that transcript.
func TestStateStoreLoadStage_DerivesTurnIndexFromTranscript(t *testing.T) {
	ctx := context.Background()
	store := statestore.NewMemoryStore()
	// Three completed turns already on the record.
	require.NoError(t, store.AppendMessages(ctx, "c1", []types.Message{
		{Role: "user", Content: "one"}, {Role: "assistant", Content: "1"},
		{Role: "user", Content: "two"}, {Role: "assistant", Content: "2"},
		{Role: "user", Content: "three"}, {Role: "assistant", Content: "3"},
	}))

	got := runLoadStage(t, store, "c1", &types.Message{Role: "user", Content: "four"})
	assert.Equal(t, 4, got, "three persisted turns plus this one is turn 4")
}

// A conversation with nothing persisted is on its first turn.
func TestStateStoreLoadStage_FirstTurnIsOne(t *testing.T) {
	store := statestore.NewMemoryStore()
	got := runLoadStage(t, store, "fresh", &types.Message{Role: "user", Content: "hi"})
	assert.Equal(t, 1, got)
}

// A turn that resumes without a new user message — the suspended-client-tool
// shape, where the user message was already persisted — must not count twice.
func TestStateStoreLoadStage_ResumedTurnDoesNotDoubleCount(t *testing.T) {
	ctx := context.Background()
	store := statestore.NewMemoryStore()
	// The turn's user message is already on the record; the resume carries no
	// new user input.
	require.NoError(t, store.AppendMessages(ctx, "c2", []types.Message{
		{Role: "user", Content: "one"}, {Role: "assistant", Content: "1"},
		{Role: "user", Content: "two"},
	}))

	got := runLoadStage(t, store, "c2", nil)
	assert.Equal(t, 2, got, "the persisted user message is this turn, not a prior one")
}

// The ProviderStage reports the turn TurnState carries, rather than counting
// executions itself.
//
// A local counter would be right only while one stage instance outlives the
// conversation; a caller that rebuilds the pipeline per turn would get 1
// forever. Both turns here are stamped 7, which no counter would produce.
func TestProviderStage_ReportsTurnStateTurnIndex(t *testing.T) {
	guard, err := guardrails.NewGuardrailHook("length", map[string]any{
		"max_characters": 5,
	})
	require.NoError(t, err)

	bus := events.NewEventBus()
	getEvents := collectValidationFailures(t, bus)
	emitter := events.NewEmitter(bus, "run1", "sess1", "conv1")

	turnState := NewTurnState()
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
		assert.Equal(t, 7, d.TurnIndex, "the stage must report the turn state's number")
	}
}

// With no turn state the stage reports 0 — visibly "unknown" rather than a
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
