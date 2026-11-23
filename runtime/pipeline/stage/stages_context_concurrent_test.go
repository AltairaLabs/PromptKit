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

// appendAfterReadStore is a MemoryStore shared with another writer, whose
// message lands just after the store answers a LoadRecentMessages: after the
// hot window is read and before anything else is.
type appendAfterReadStore struct {
	*statestore.MemoryStore
	other []types.Message
}

func (s *appendAfterReadStore) LoadRecentMessages(ctx context.Context, id string, n int) ([]types.Message, error) {
	msgs, err := s.MemoryStore.LoadRecentMessages(ctx, id, n)
	if err != nil || len(s.other) == 0 {
		return msgs, err
	}
	other := s.other
	s.other = nil
	return msgs, s.MemoryStore.AppendMessages(ctx, id, other)
}

// A message another writer appends after the hot window is read does not move
// where the window is taken to start, so retrieval does not repeat the
// window's first message.
func TestContextAssemblyStage_RetrievalDoesNotRepeatTheWindowWhenAnotherWriterAppends(t *testing.T) {
	store := &appendAfterReadStore{MemoryStore: statestore.NewMemoryStore()}
	require.NoError(t, store.AppendMessages(context.Background(), "conv-cw", []types.Message{
		{Role: "user", Content: "Tell me about dogs"},
		{Role: "assistant", Content: "Dogs are great pets."},
		{Role: "user", Content: "What about cats?"},
		{Role: "assistant", Content: "Cats are independent."},
	}))
	store.other = []types.Message{{Role: "user", Content: "other writer"}}
	idx := &mockMessageIndex{results: []statestore.IndexResult{
		{TurnIndex: 2, Message: types.Message{Role: "user", Content: "What about cats?"}, Score: 0.9},
		{TurnIndex: 1, Message: types.Message{Role: "assistant", Content: "Dogs are great pets."}, Score: 0.8},
	}}
	s := NewContextAssemblyStage(&ContextAssemblyConfig{
		StateStoreConfig: &pipeline.StateStoreConfig{Store: store, ConversationID: "conv-cw"},
		RecentMessages:   2,
		MessageIndex:     idx,
		RetrievalTopK:    2,
	})

	results := runTestStage(t, s, []StreamElement{newTestMsgElement("user", "Current question")})

	var got []string
	for _, r := range results {
		got = append(got, r.Message.Content)
	}
	assert.Equal(t, []string{
		"Dogs are great pets.",
		"What about cats?",
		"Cats are independent.",
		"Current question",
	}, got)
}
