package persist

// Copies of stage's test helpers that the moved tests use, as the other
// internal packages keep their own (#2876).

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/v2/pipeline/stage/internal/core"
	"github.com/AltairaLabs/PromptKit/runtime/v2/statestore"
	"github.com/AltairaLabs/PromptKit/runtime/v2/types"
)

// newTestMsgElement builds a message element with the given role and content.
func newTestMsgElement(role, content string) core.StreamElement {
	msg := &types.Message{
		Role:    role,
		Content: content,
	}
	return core.NewMessageElement(msg)
}

// runTestStage runs s over inputs and returns everything it emitted.
func runTestStage(t *testing.T, s core.Stage, inputs []core.StreamElement) []core.StreamElement {
	t.Helper()

	input := make(chan core.StreamElement, len(inputs))
	for _, elem := range inputs {
		input <- elem
	}
	close(input)

	output := make(chan core.StreamElement, 100)
	ctx := context.Background()

	err := s.Process(ctx, input, output)
	require.NoError(t, err)

	var results []core.StreamElement
	for elem := range output {
		results = append(results, elem)
	}
	return results
}

// minimalStore implements only statestore.Store (Load/Save/Fork), not MessageReader.
type minimalStore struct {
	states map[string]*statestore.ConversationState
}

func newMinimalStore() *minimalStore {
	return &minimalStore{
		states: make(map[string]*statestore.ConversationState),
	}
}

func (s *minimalStore) Load(_ context.Context, id string) (*statestore.ConversationState, error) {
	state, exists := s.states[id]
	if !exists {
		return nil, statestore.ErrNotFound
	}
	return state, nil
}

func (s *minimalStore) Save(_ context.Context, state *statestore.ConversationState) error {
	s.states[state.ID] = state
	return nil
}

func (s *minimalStore) Fork(_ context.Context, sourceID, newID string) error {
	state, exists := s.states[sourceID]
	if !exists {
		return statestore.ErrNotFound
	}
	copied := *state
	copied.ID = newID
	s.states[newID] = &copied
	return nil
}
