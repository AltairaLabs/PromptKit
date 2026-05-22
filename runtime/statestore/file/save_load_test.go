package file

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/AltairaLabs/PromptKit/runtime/statestore"
	"github.com/AltairaLabs/PromptKit/runtime/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func makeMsg(role, content string) types.Message {
	return types.Message{Role: role, Content: content}
}

func newTestStore(t *testing.T) *Store {
	t.Helper()
	s, err := NewStore(Options{Root: t.TempDir()})
	require.NoError(t, err)
	t.Cleanup(func() { _ = s.Close() })
	return s
}

func TestStore_SaveAndLoad(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	state := &statestore.ConversationState{
		ID:           "conv-1",
		UserID:       "alice",
		SystemPrompt: "you are helpful",
		TokenCount:   42,
		Messages: []types.Message{
			makeMsg("user", "hi"),
			makeMsg("assistant", "hello"),
		},
		Metadata: map[string]any{"k": "v"},
	}
	require.NoError(t, s.Save(ctx, state))

	got, err := s.Load(ctx, "conv-1")
	require.NoError(t, err)
	require.NotNil(t, got)
	assert.Equal(t, "conv-1", got.ID)
	assert.Equal(t, "alice", got.UserID)
	assert.Equal(t, "you are helpful", got.SystemPrompt)
	assert.Equal(t, 42, got.TokenCount)
	require.Len(t, got.Messages, 2)
	assert.Equal(t, "hello", got.Messages[1].Content)
	assert.Equal(t, "v", got.Metadata["k"])
}

func TestStore_Load_NotFound(t *testing.T) {
	s := newTestStore(t)
	_, err := s.Load(context.Background(), "missing")
	assert.ErrorIs(t, err, statestore.ErrNotFound)
}

func TestStore_Load_InvalidID(t *testing.T) {
	s := newTestStore(t)
	_, err := s.Load(context.Background(), "")
	assert.ErrorIs(t, err, statestore.ErrInvalidID)
}

func TestStore_Save_InvalidID(t *testing.T) {
	s := newTestStore(t)
	err := s.Save(context.Background(), &statestore.ConversationState{ID: ""})
	assert.ErrorIs(t, err, statestore.ErrInvalidID)
}

func TestStore_Save_NilState(t *testing.T) {
	s := newTestStore(t)
	err := s.Save(context.Background(), nil)
	assert.ErrorIs(t, err, statestore.ErrInvalidState)
}

func TestStore_Save_AfterClose(t *testing.T) {
	s := newTestStore(t)
	require.NoError(t, s.Close())
	err := s.Save(context.Background(), &statestore.ConversationState{ID: "x"})
	assert.ErrorIs(t, err, ErrStoreClosed)

	_, err = s.Load(context.Background(), "x")
	assert.ErrorIs(t, err, ErrStoreClosed)
}

func TestStore_Save_AtomicReplaces(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	require.NoError(t, s.Save(ctx, &statestore.ConversationState{
		ID:       "conv-1",
		Messages: []types.Message{makeMsg("user", "v1")},
	}))
	require.NoError(t, s.Save(ctx, &statestore.ConversationState{
		ID:       "conv-1",
		Messages: []types.Message{makeMsg("user", "v1"), makeMsg("assistant", "v2")},
	}))

	got, err := s.Load(ctx, "conv-1")
	require.NoError(t, err)
	require.Len(t, got.Messages, 2)
	assert.Equal(t, "v2", got.Messages[1].Content)
}

func TestStore_Save_TruncatesMessages(t *testing.T) {
	// On-disk longer than the in-memory state → full rewrite.
	s := newTestStore(t)
	ctx := context.Background()

	require.NoError(t, s.Save(ctx, &statestore.ConversationState{
		ID: "conv-1",
		Messages: []types.Message{
			makeMsg("user", "a"), makeMsg("assistant", "b"), makeMsg("user", "c"),
		},
	}))
	require.NoError(t, s.Save(ctx, &statestore.ConversationState{
		ID:       "conv-1",
		Messages: []types.Message{makeMsg("user", "only")},
	}))

	got, err := s.Load(ctx, "conv-1")
	require.NoError(t, err)
	require.Len(t, got.Messages, 1)
	assert.Equal(t, "only", got.Messages[0].Content)
}

func TestStore_Save_TruncatesToEmpty(t *testing.T) {
	// On-disk has messages, in-memory state has zero → rewrite to empty file.
	s := newTestStore(t)
	ctx := context.Background()

	require.NoError(t, s.Save(ctx, &statestore.ConversationState{
		ID:       "conv-1",
		Messages: []types.Message{makeMsg("user", "a"), makeMsg("assistant", "b")},
	}))
	require.NoError(t, s.Save(ctx, &statestore.ConversationState{
		ID:       "conv-1",
		Messages: nil,
	}))

	got, err := s.Load(ctx, "conv-1")
	require.NoError(t, err)
	assert.Empty(t, got.Messages)
}

func TestStore_Save_RoundTripsSummaries(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	require.NoError(t, s.Save(ctx, &statestore.ConversationState{
		ID: "conv-1",
		Summaries: []statestore.Summary{
			{StartTurn: 0, EndTurn: 5, Content: "first", TokenCount: 10},
			{StartTurn: 5, EndTurn: 10, Content: "second", TokenCount: 12},
		},
	}))

	got, err := s.Load(ctx, "conv-1")
	require.NoError(t, err)
	require.Len(t, got.Summaries, 2)
	assert.Equal(t, "first", got.Summaries[0].Content)
	assert.Equal(t, "second", got.Summaries[1].Content)
}

func TestStore_Save_TruncatesSummaries(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	require.NoError(t, s.Save(ctx, &statestore.ConversationState{
		ID: "conv-1",
		Summaries: []statestore.Summary{
			{Content: "a"}, {Content: "b"}, {Content: "c"},
		},
	}))
	require.NoError(t, s.Save(ctx, &statestore.ConversationState{
		ID:        "conv-1",
		Summaries: []statestore.Summary{{Content: "only"}},
	}))
	require.NoError(t, s.Save(ctx, &statestore.ConversationState{
		ID: "conv-1",
	}))

	got, err := s.Load(ctx, "conv-1")
	require.NoError(t, err)
	assert.Empty(t, got.Summaries)
}

func TestStore_FSyncOff_Roundtrips(t *testing.T) {
	dir := t.TempDir()
	s, err := NewStore(Options{Root: dir, FSync: FSyncOff})
	require.NoError(t, err)
	defer func() { _ = s.Close() }()
	ctx := context.Background()
	require.NoError(t, s.Save(ctx, &statestore.ConversationState{
		ID: "conv-1", Messages: []types.Message{makeMsg("user", "a")},
	}))
	got, err := s.Load(ctx, "conv-1")
	require.NoError(t, err)
	require.Len(t, got.Messages, 1)
}

func TestStore_Save_NoTempLeftovers(t *testing.T) {
	s := newTestStore(t)
	require.NoError(t, s.Save(context.Background(), &statestore.ConversationState{
		ID: "conv-1", Messages: []types.Message{makeMsg("user", "a")},
	}))
	entries, err := os.ReadDir(filepath.Join(s.root, "conv-conv-1"))
	require.NoError(t, err)
	for _, e := range entries {
		assert.False(t, strings.HasPrefix(e.Name(), ".tmp"), "no .tmp leftovers: %s", e.Name())
	}
}
