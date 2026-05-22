package file

import (
	"context"
	"os"
	"sort"
	"testing"
	"time"

	"github.com/AltairaLabs/PromptKit/runtime/statestore"
	"github.com/AltairaLabs/PromptKit/runtime/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestStore_Fork(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	require.NoError(t, s.Save(ctx, &statestore.ConversationState{
		ID: "src", UserID: "alice",
		Messages: []types.Message{makeMsg("user", "a")},
	}))

	require.NoError(t, s.Fork(ctx, "src", "dst"))

	loaded, err := s.Load(ctx, "dst")
	require.NoError(t, err)
	assert.Equal(t, "dst", loaded.ID)
	require.Len(t, loaded.Messages, 1)
	assert.Equal(t, "a", loaded.Messages[0].Content)

	src, err := s.Load(ctx, "src")
	require.NoError(t, err)
	assert.Equal(t, "src", src.ID)
}

func TestStore_Fork_SourceMissing(t *testing.T) {
	s := newTestStore(t)
	err := s.Fork(context.Background(), "missing", "dst")
	assert.ErrorIs(t, err, statestore.ErrNotFound)
}

func TestStore_Fork_InvalidID(t *testing.T) {
	s := newTestStore(t)
	assert.ErrorIs(t, s.Fork(context.Background(), "", "dst"), statestore.ErrInvalidID)
	assert.ErrorIs(t, s.Fork(context.Background(), "src", ""), statestore.ErrInvalidID)
}

func TestStore_Fork_AfterClose(t *testing.T) {
	s := newTestStore(t)
	require.NoError(t, s.Close())
	err := s.Fork(context.Background(), "src", "dst")
	assert.ErrorIs(t, err, ErrStoreClosed)
}

func TestStore_Delete(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	require.NoError(t, s.Save(ctx, &statestore.ConversationState{ID: "conv-1"}))

	require.NoError(t, s.Delete(ctx, "conv-1"))

	_, err := s.Load(ctx, "conv-1")
	assert.ErrorIs(t, err, statestore.ErrNotFound)
}

func TestStore_Delete_Missing(t *testing.T) {
	s := newTestStore(t)
	require.NoError(t, s.Delete(context.Background(), "missing"))
}

func TestStore_Delete_InvalidID(t *testing.T) {
	s := newTestStore(t)
	err := s.Delete(context.Background(), "")
	assert.ErrorIs(t, err, statestore.ErrInvalidID)
}

func TestStore_Delete_AfterClose(t *testing.T) {
	s := newTestStore(t)
	require.NoError(t, s.Close())
	err := s.Delete(context.Background(), "conv-1")
	assert.ErrorIs(t, err, ErrStoreClosed)
}

func TestStore_List_All(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	for _, id := range []string{"a", "b", "c"} {
		require.NoError(t, s.Save(ctx, &statestore.ConversationState{ID: id}))
	}

	got, err := s.List(ctx, statestore.ListOptions{})
	require.NoError(t, err)
	sort.Strings(got)
	assert.Equal(t, []string{"a", "b", "c"}, got)
}

func TestStore_List_FilterByUser(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	require.NoError(t, s.Save(ctx, &statestore.ConversationState{ID: "a", UserID: "alice"}))
	require.NoError(t, s.Save(ctx, &statestore.ConversationState{ID: "b", UserID: "bob"}))
	require.NoError(t, s.Save(ctx, &statestore.ConversationState{ID: "c", UserID: "alice"}))

	got, err := s.List(ctx, statestore.ListOptions{UserID: "alice"})
	require.NoError(t, err)
	sort.Strings(got)
	assert.Equal(t, []string{"a", "c"}, got)
}

func TestStore_List_PaginationOffsetBeyondLen(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	require.NoError(t, s.Save(ctx, &statestore.ConversationState{ID: "a"}))

	got, err := s.List(ctx, statestore.ListOptions{Offset: 5})
	require.NoError(t, err)
	assert.Empty(t, got)
}

func TestStore_List_RespectsLimit(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	for _, id := range []string{"a", "b", "c"} {
		require.NoError(t, s.Save(ctx, &statestore.ConversationState{ID: id}))
	}
	got, err := s.List(ctx, statestore.ListOptions{Limit: 2})
	require.NoError(t, err)
	assert.Len(t, got, 2)
}

func TestStore_List_AfterClose(t *testing.T) {
	s := newTestStore(t)
	require.NoError(t, s.Close())
	_, err := s.List(context.Background(), statestore.ListOptions{})
	assert.ErrorIs(t, err, ErrStoreClosed)
}

func TestStore_TTLSweep_RemovesStaleConvs(t *testing.T) {
	root := t.TempDir()

	s1, err := NewStore(Options{Root: root})
	require.NoError(t, err)
	ctx := context.Background()
	require.NoError(t, s1.Save(ctx, &statestore.ConversationState{ID: "fresh"}))
	require.NoError(t, s1.Save(ctx, &statestore.ConversationState{ID: "stale"}))
	stalePath := s1.stateFile("stale")
	require.NoError(t, s1.Close())

	old := time.Now().Add(-48 * time.Hour)
	require.NoError(t, os.Chtimes(stalePath, old, old))

	s2, err := NewStore(Options{Root: root, TTL: 24 * time.Hour})
	require.NoError(t, err)
	defer func() { _ = s2.Close() }()

	_, err = s2.Load(ctx, "fresh")
	require.NoError(t, err)
	_, err = s2.Load(ctx, "stale")
	assert.ErrorIs(t, err, statestore.ErrNotFound)
}

func TestStore_TTLSweep_NoTTL_KeepsEverything(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	require.NoError(t, s.Save(ctx, &statestore.ConversationState{ID: "x"}))
	require.NoError(t, s.sweepStale(time.Now()))
	_, err := s.Load(ctx, "x")
	require.NoError(t, err)
}
