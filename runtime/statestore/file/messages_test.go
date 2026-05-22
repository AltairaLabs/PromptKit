package file

import (
	"context"
	"fmt"
	"testing"

	"github.com/AltairaLabs/PromptKit/runtime/statestore"
	"github.com/AltairaLabs/PromptKit/runtime/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestStore_MessageLog_Append(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	msgs := []types.Message{
		makeMsg("user", "hello"),
		makeMsg("assistant", "hi"),
		makeMsg("user", "how are you"),
	}
	total, err := s.LogAppend(ctx, "conv-1", 0, msgs)
	require.NoError(t, err)
	assert.Equal(t, 3, total)

	n, err := s.LogLen(ctx, "conv-1")
	require.NoError(t, err)
	assert.Equal(t, 3, n)

	loaded, err := s.LogLoad(ctx, "conv-1", 0)
	require.NoError(t, err)
	require.Len(t, loaded, 3)
	assert.Equal(t, "hello", loaded[0].Content)
	assert.Equal(t, "how are you", loaded[2].Content)
}

func TestStore_MessageLog_AppendIdempotent(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	first := []types.Message{
		makeMsg("user", "a"),
		makeMsg("assistant", "b"),
		makeMsg("user", "c"),
	}
	total, err := s.LogAppend(ctx, "conv-1", 0, first)
	require.NoError(t, err)
	require.Equal(t, 3, total)

	retry := []types.Message{
		makeMsg("user", "a"),
		makeMsg("assistant", "b"),
		makeMsg("user", "c"),
		makeMsg("assistant", "d"),
		makeMsg("user", "e"),
	}
	total, err = s.LogAppend(ctx, "conv-1", 0, retry)
	require.NoError(t, err)
	assert.Equal(t, 5, total)

	loaded, err := s.LogLoad(ctx, "conv-1", 0)
	require.NoError(t, err)
	require.Len(t, loaded, 5)
	assert.Equal(t, "d", loaded[3].Content)
}

func TestStore_MessageLog_AppendDelta(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	_, err := s.LogAppend(ctx, "conv-1", 0, []types.Message{
		makeMsg("user", "a"), makeMsg("assistant", "b"), makeMsg("user", "c"),
	})
	require.NoError(t, err)

	total, err := s.LogAppend(ctx, "conv-1", 3, []types.Message{
		makeMsg("assistant", "d"), makeMsg("user", "e"),
	})
	require.NoError(t, err)
	assert.Equal(t, 5, total)
}

func TestStore_MessageLog_AppendClampsHighStartSeq(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	_, err := s.LogAppend(ctx, "conv-1", 0, []types.Message{makeMsg("user", "a")})
	require.NoError(t, err)

	total, err := s.LogAppend(ctx, "conv-1", 99, []types.Message{makeMsg("user", "b")})
	require.NoError(t, err)
	assert.Equal(t, 2, total)
}

func TestStore_MessageLog_LoadRecent(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	msgs := make([]types.Message, 10)
	for i := range msgs {
		msgs[i] = makeMsg("user", fmt.Sprintf("msg-%d", i))
	}
	_, err := s.LogAppend(ctx, "conv-1", 0, msgs)
	require.NoError(t, err)

	recent, err := s.LogLoad(ctx, "conv-1", 3)
	require.NoError(t, err)
	require.Len(t, recent, 3)
	assert.Equal(t, "msg-7", recent[0].Content)
	assert.Equal(t, "msg-9", recent[2].Content)
}

func TestStore_MessageLog_EmptyConversation(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	loaded, err := s.LogLoad(ctx, "missing", 0)
	require.NoError(t, err)
	assert.Empty(t, loaded)

	n, err := s.LogLen(ctx, "missing")
	require.NoError(t, err)
	assert.Equal(t, 0, n)
}

func TestStore_MessageLog_AppendInvalidID(t *testing.T) {
	s := newTestStore(t)
	_, err := s.LogAppend(context.Background(), "", 0, []types.Message{makeMsg("user", "x")})
	assert.ErrorIs(t, err, statestore.ErrInvalidID)
}

func TestStore_MessageLog_AppendAfterClose(t *testing.T) {
	s := newTestStore(t)
	require.NoError(t, s.Close())
	_, err := s.LogAppend(context.Background(), "conv-1", 0,
		[]types.Message{makeMsg("user", "x")})
	assert.ErrorIs(t, err, ErrStoreClosed)
}

func TestStore_MessageLog_LoadInvalidID(t *testing.T) {
	s := newTestStore(t)
	_, err := s.LogLoad(context.Background(), "", 0)
	assert.ErrorIs(t, err, statestore.ErrInvalidID)
	_, err = s.LogLen(context.Background(), "")
	assert.ErrorIs(t, err, statestore.ErrInvalidID)
}

func TestStore_LogAppend_AllSkipped(t *testing.T) {
	// startSeq matches current length and payload already fits — full skip.
	s := newTestStore(t)
	ctx := context.Background()

	_, err := s.LogAppend(ctx, "conv-1", 0, []types.Message{makeMsg("user", "a")})
	require.NoError(t, err)
	total, err := s.LogAppend(ctx, "conv-1", 0, []types.Message{makeMsg("user", "a")})
	require.NoError(t, err)
	assert.Equal(t, 1, total)
}

func TestStore_MessageReader(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	_, err := s.LogAppend(ctx, "conv-1", 0, []types.Message{
		makeMsg("user", "a"), makeMsg("assistant", "b"), makeMsg("user", "c"),
	})
	require.NoError(t, err)

	n, err := s.MessageCount(ctx, "conv-1")
	require.NoError(t, err)
	assert.Equal(t, 3, n)

	recent, err := s.LoadRecentMessages(ctx, "conv-1", 2)
	require.NoError(t, err)
	require.Len(t, recent, 2)
	assert.Equal(t, "b", recent[0].Content)
}

func TestStore_MessageReader_NotFound(t *testing.T) {
	s := newTestStore(t)
	_, err := s.MessageCount(context.Background(), "missing")
	require.ErrorIs(t, err, statestore.ErrNotFound)

	_, err = s.LoadRecentMessages(context.Background(), "missing", 5)
	require.ErrorIs(t, err, statestore.ErrNotFound)
}

func TestStore_MessageReader_InvalidID(t *testing.T) {
	s := newTestStore(t)
	_, err := s.MessageCount(context.Background(), "")
	assert.ErrorIs(t, err, statestore.ErrInvalidID)
	_, err = s.LoadRecentMessages(context.Background(), "", 1)
	assert.ErrorIs(t, err, statestore.ErrInvalidID)
}

func TestStore_AppendMessages(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	require.NoError(t, s.AppendMessages(ctx, "conv-1", []types.Message{
		makeMsg("user", "x"), makeMsg("assistant", "y"),
	}))
	loaded, err := s.LogLoad(ctx, "conv-1", 0)
	require.NoError(t, err)
	require.Len(t, loaded, 2)
}

func TestStore_AppendMessages_InvalidID(t *testing.T) {
	s := newTestStore(t)
	err := s.AppendMessages(context.Background(), "", []types.Message{makeMsg("user", "x")})
	assert.ErrorIs(t, err, statestore.ErrInvalidID)
}

func TestStore_AppendMessages_AfterClose(t *testing.T) {
	s := newTestStore(t)
	require.NoError(t, s.Close())
	err := s.AppendMessages(context.Background(), "conv-1", []types.Message{makeMsg("user", "x")})
	assert.ErrorIs(t, err, ErrStoreClosed)
}
