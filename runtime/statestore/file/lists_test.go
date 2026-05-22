package file

import (
	"context"
	"testing"

	"github.com/AltairaLabs/PromptKit/runtime/statestore"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestStore_ListAccessor_AppendAndLoad(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	require.NoError(t, s.AppendList(ctx, "conv-1", "workflow.history",
		[][]byte{[]byte(`{"step":1}`), []byte(`{"step":2}`)}))
	require.NoError(t, s.AppendList(ctx, "conv-1", "workflow.history",
		[][]byte{[]byte(`{"step":3}`)}))

	items, err := s.LoadList(ctx, "conv-1", "workflow.history")
	require.NoError(t, err)
	require.Len(t, items, 3)
	assert.Equal(t, `{"step":1}`, string(items[0]))
	assert.Equal(t, `{"step":3}`, string(items[2]))

	n, err := s.ListLen(ctx, "conv-1", "workflow.history")
	require.NoError(t, err)
	assert.Equal(t, 3, n)
}

func TestStore_ListAccessor_LoadEmpty(t *testing.T) {
	s := newTestStore(t)
	got, err := s.LoadList(context.Background(), "missing", "x")
	require.NoError(t, err)
	assert.Nil(t, got)

	n, err := s.ListLen(context.Background(), "missing", "x")
	require.NoError(t, err)
	assert.Equal(t, 0, n)
}

func TestStore_ListAccessor_PerListIsolated(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	require.NoError(t, s.AppendList(ctx, "conv-1", "a", [][]byte{[]byte("1")}))
	require.NoError(t, s.AppendList(ctx, "conv-1", "b", [][]byte{[]byte("9")}))

	a, err := s.LoadList(ctx, "conv-1", "a")
	require.NoError(t, err)
	require.Len(t, a, 1)
	assert.Equal(t, "1", string(a[0]))

	b, err := s.LoadList(ctx, "conv-1", "b")
	require.NoError(t, err)
	require.Len(t, b, 1)
	assert.Equal(t, "9", string(b[0]))
}

func TestStore_ListAccessor_InvalidID(t *testing.T) {
	s := newTestStore(t)
	err := s.AppendList(context.Background(), "", "x", [][]byte{[]byte("1")})
	assert.ErrorIs(t, err, statestore.ErrInvalidID)

	_, err = s.LoadList(context.Background(), "", "x")
	assert.ErrorIs(t, err, statestore.ErrInvalidID)

	_, err = s.ListLen(context.Background(), "", "x")
	assert.ErrorIs(t, err, statestore.ErrInvalidID)
}

func TestStore_ListAccessor_EmptyListName(t *testing.T) {
	s := newTestStore(t)
	err := s.AppendList(context.Background(), "conv-1", "", [][]byte{[]byte("1")})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "list name")
}

func TestStore_AppendList_AfterClose(t *testing.T) {
	s := newTestStore(t)
	require.NoError(t, s.Close())
	err := s.AppendList(context.Background(), "conv-1", "x", [][]byte{[]byte("1")})
	assert.ErrorIs(t, err, ErrStoreClosed)
}
