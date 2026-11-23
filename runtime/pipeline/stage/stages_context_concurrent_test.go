package stage

import (
	"context"
	"errors"
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

// A message another writer appends after the hot window is read does not make
// retrieval repeat the window's first message.
func TestContextAssemblyStage_RetrievalDoesNotRepeatTheWindowWhenAnotherWriterAppends(t *testing.T) {
	stored := withIDs(
		types.Message{Role: "user", Content: "Tell me about dogs"},
		types.Message{Role: "assistant", Content: "Dogs are great pets."},
		types.Message{Role: "user", Content: "What about cats?"},
		types.Message{Role: "assistant", Content: "Cats are independent."},
	)
	store := &appendAfterReadStore{MemoryStore: statestore.NewMemoryStore()}
	require.NoError(t, store.AppendMessages(context.Background(), "conv-cw", stored))
	store.other = withIDs(types.Message{Role: "user", Content: "other writer"})
	idx := &mockMessageIndex{results: []statestore.IndexResult{
		{TurnIndex: 2, Message: stored[2], Score: 0.9},
		{TurnIndex: 1, Message: stored[1], Score: 0.8},
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

// appendBeforeReadStore is a MemoryStore shared with another writer, whose
// message lands just before the store answers a LoadRecentMessages, so the hot
// window is one message later than a count read before it says.
type appendBeforeReadStore struct {
	*statestore.MemoryStore
	other []types.Message
}

func (s *appendBeforeReadStore) LoadRecentMessages(ctx context.Context, id string, n int) ([]types.Message, error) {
	if len(s.other) > 0 {
		other := s.other
		s.other = nil
		if err := s.MemoryStore.AppendMessages(ctx, id, other); err != nil {
			return nil, err
		}
	}
	return s.MemoryStore.LoadRecentMessages(ctx, id, n)
}

// withIDs gives each message a stable id, as the save stage stores them.
func withIDs(msgs ...types.Message) []types.Message {
	for i := range msgs {
		msgs[i] = types.EnsureMessageID(msgs[i])
	}
	return msgs
}

// A message another writer appends just before the hot window is read moves
// the window one message later. The message that left the window is still
// retrieved, and the one in it is not repeated.
func TestContextAssemblyStage_RetrievalKeepsTheMessageAnotherWriterPushedOutOfTheWindow(t *testing.T) {
	stored := withIDs(
		types.Message{Role: "user", Content: "Tell me about dogs"},
		types.Message{Role: "assistant", Content: "Dogs are great pets."},
		types.Message{Role: "user", Content: "What about cats?"},
		types.Message{Role: "assistant", Content: "Cats are independent."},
	)
	store := &appendBeforeReadStore{MemoryStore: statestore.NewMemoryStore()}
	require.NoError(t, store.AppendMessages(context.Background(), "conv-cw", stored))
	store.other = withIDs(types.Message{Role: "user", Content: "other writer"})
	idx := &mockMessageIndex{results: []statestore.IndexResult{
		{TurnIndex: 3, Message: stored[3], Score: 0.9},
		{TurnIndex: 2, Message: stored[2], Score: 0.8},
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
		"What about cats?",
		"Cats are independent.",
		"other writer",
		"Current question",
	}, got)
}

// countlessStore is a MemoryStore that cannot count messages.
type countlessStore struct {
	*statestore.MemoryStore
}

func (s *countlessStore) MessageCount(context.Context, string) (int, error) {
	return 0, errors.New("count unavailable")
}

// Retrieval over a window whose messages carry ids matches them by id and
// needs no message count, so a store that cannot count still gets retrieval.
func TestContextAssemblyStage_RetrievalByIDNeedsNoMessageCount(t *testing.T) {
	stored := withIDs(
		types.Message{Role: "user", Content: "Tell me about dogs"},
		types.Message{Role: "assistant", Content: "Dogs are great pets."},
		types.Message{Role: "user", Content: "What about cats?"},
	)
	store := &countlessStore{MemoryStore: statestore.NewMemoryStore()}
	require.NoError(t, store.AppendMessages(context.Background(), "conv-cw", stored))
	idx := &mockMessageIndex{results: []statestore.IndexResult{
		{TurnIndex: 2, Message: stored[2], Score: 0.9},
		{TurnIndex: 0, Message: stored[0], Score: 0.8},
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
		"Tell me about dogs",
		"Dogs are great pets.",
		"What about cats?",
		"Current question",
	}, got)
}
