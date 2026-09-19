package stage

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/v2/pipeline"
	"github.com/AltairaLabs/PromptKit/runtime/v2/statestore"
	"github.com/AltairaLabs/PromptKit/runtime/v2/types"
)

// These cover #1945: WithContextWindow swaps StateStoreLoadStage for
// ContextAssemblyStage, and the assembly stage never derived the turn, so
// every guardrail and eval event on such a conversation reported turn 0. The
// turn is the position in the persisted transcript, whichever stage loads it.

// storeWithoutReader hides the MessageReader fast path so the fallback
// (full Load) branch can be exercised on the memory store.
type storeWithoutReader struct{ statestore.Store }

// runAssemblyStage drives ContextAssemblyStage over one incoming user message
// with a two-message hot window and returns the turn index it published. The
// small window is the point: counting what the stage loaded would undercount.
func runAssemblyStage(t *testing.T, store statestore.Store, convID string, incoming *types.Message) int {
	t.Helper()

	turnState := NewTurnState()
	s := NewContextAssemblyStageWithTurnState(&ContextAssemblyConfig{
		StateStoreConfig: &pipeline.StateStoreConfig{Store: store, ConversationID: convID},
		RecentMessages:   2,
		HasContextWindow: true,
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

func threeTurnStore(t *testing.T, convID string) *statestore.MemoryStore {
	t.Helper()
	store := statestore.NewMemoryStore()
	require.NoError(t, store.AppendMessages(context.Background(), convID, []types.Message{
		{Role: "user", Content: "one"}, {Role: "assistant", Content: "1"},
		{Role: "user", Content: "two"}, {Role: "assistant", Content: "2"},
		{Role: "user", Content: "three"}, {Role: "assistant", Content: "3"},
	}))
	return store
}

// The MessageReader fast path loads only the hot window; the turn must still
// come from the whole transcript.
func TestContextAssemblyStage_DerivesTurnIndexFromWholeTranscript(t *testing.T) {
	store := threeTurnStore(t, "c1")
	got := runAssemblyStage(t, store, "c1", &types.Message{Role: "user", Content: "four"})
	assert.Equal(t, 4, got, "three persisted turns plus this one is turn 4, not the window's count")
}

func TestContextAssemblyStage_FallbackPathDerivesTurnIndex(t *testing.T) {
	store := storeWithoutReader{threeTurnStore(t, "c1")}
	got := runAssemblyStage(t, store, "c1", &types.Message{Role: "user", Content: "four"})
	assert.Equal(t, 4, got)
}

func TestContextAssemblyStage_FirstTurnIsOne(t *testing.T) {
	got := runAssemblyStage(t, statestore.NewMemoryStore(), "fresh", &types.Message{Role: "user", Content: "hi"})
	assert.Equal(t, 1, got)
}

// A resumed turn whose user message is already persisted must not count twice
// — the same shape StateStoreLoadStage already handles.
func TestContextAssemblyStage_ResumedTurnDoesNotDoubleCount(t *testing.T) {
	store := statestore.NewMemoryStore()
	require.NoError(t, store.AppendMessages(context.Background(), "c2", []types.Message{
		{Role: "user", Content: "one"}, {Role: "assistant", Content: "1"},
		{Role: "user", Content: "two"},
	}))
	got := runAssemblyStage(t, store, "c2", nil)
	assert.Equal(t, 2, got, "the persisted user message is this turn, not a prior one")
}

// Both load stages must agree: the turn is a property of the transcript, not
// of which stage happened to read it.
func TestLoadStagesAgreeOnTurnIndex(t *testing.T) {
	store := threeTurnStore(t, "c1")
	incoming := &types.Message{Role: "user", Content: "four"}
	assert.Equal(t,
		runLoadStage(t, store, "c1", incoming),
		runAssemblyStage(t, store, "c1", incoming))
}
