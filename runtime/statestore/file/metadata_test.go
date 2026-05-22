package file

import (
	"context"
	"testing"

	"github.com/AltairaLabs/PromptKit/runtime/statestore"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestStore_MergeMetadata_FreshConv(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	require.NoError(t, s.MergeMetadata(ctx, "conv-1", map[string]any{"a": 1, "b": "x"}))

	got, err := s.LoadMetadata(ctx, "conv-1")
	require.NoError(t, err)
	assert.Equal(t, float64(1), got["a"])
	assert.Equal(t, "x", got["b"])
}

func TestStore_MergeMetadata_OverwritesAndAdds(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	require.NoError(t, s.MergeMetadata(ctx, "conv-1", map[string]any{"a": 1, "b": "x"}))
	require.NoError(t, s.MergeMetadata(ctx, "conv-1", map[string]any{"b": "y", "c": true}))

	got, err := s.LoadMetadata(ctx, "conv-1")
	require.NoError(t, err)
	assert.Equal(t, float64(1), got["a"])
	assert.Equal(t, "y", got["b"])
	assert.Equal(t, true, got["c"])
}

func TestStore_LoadMetadata_Missing(t *testing.T) {
	s := newTestStore(t)
	_, err := s.LoadMetadata(context.Background(), "missing")
	assert.ErrorIs(t, err, statestore.ErrNotFound)
}

func TestStore_LoadMetadata_InvalidID(t *testing.T) {
	s := newTestStore(t)
	_, err := s.LoadMetadata(context.Background(), "")
	assert.ErrorIs(t, err, statestore.ErrInvalidID)
}

func TestStore_MergeMetadata_InvalidID(t *testing.T) {
	s := newTestStore(t)
	err := s.MergeMetadata(context.Background(), "", map[string]any{"a": 1})
	assert.ErrorIs(t, err, statestore.ErrInvalidID)
}

func TestStore_MergeMetadata_AfterClose(t *testing.T) {
	s := newTestStore(t)
	require.NoError(t, s.Close())
	err := s.MergeMetadata(context.Background(), "conv-1", map[string]any{"a": 1})
	assert.ErrorIs(t, err, ErrStoreClosed)

	_, err = s.LoadMetadata(context.Background(), "conv-1")
	assert.ErrorIs(t, err, ErrStoreClosed)
}

func TestStore_LoadMetadata_ReturnsDeepCopy(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	require.NoError(t, s.MergeMetadata(ctx, "conv-1", map[string]any{"a": 1}))

	got, err := s.LoadMetadata(ctx, "conv-1")
	require.NoError(t, err)
	got["a"] = 999

	got2, err := s.LoadMetadata(ctx, "conv-1")
	require.NoError(t, err)
	assert.Equal(t, float64(1), got2["a"], "caller mutations must not leak into the store")
}
