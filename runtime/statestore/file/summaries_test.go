package file

import (
	"context"
	"testing"

	"github.com/AltairaLabs/PromptKit/runtime/statestore"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestStore_SaveSummary_AndLoad(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	require.NoError(t, s.SaveSummary(ctx, "conv-1", statestore.Summary{
		StartTurn: 0, EndTurn: 5, Content: "first", TokenCount: 10,
	}))
	require.NoError(t, s.SaveSummary(ctx, "conv-1", statestore.Summary{
		StartTurn: 5, EndTurn: 10, Content: "second", TokenCount: 12,
	}))

	got, err := s.LoadSummaries(ctx, "conv-1")
	require.NoError(t, err)
	require.Len(t, got, 2)
	assert.Equal(t, "first", got[0].Content)
	assert.Equal(t, "second", got[1].Content)
}

func TestStore_LoadSummaries_Empty(t *testing.T) {
	s := newTestStore(t)
	got, err := s.LoadSummaries(context.Background(), "missing")
	require.NoError(t, err)
	assert.Empty(t, got)
}

func TestStore_SaveSummary_InvalidID(t *testing.T) {
	s := newTestStore(t)
	err := s.SaveSummary(context.Background(), "", statestore.Summary{})
	assert.ErrorIs(t, err, statestore.ErrInvalidID)
}

func TestStore_LoadSummaries_InvalidID(t *testing.T) {
	s := newTestStore(t)
	_, err := s.LoadSummaries(context.Background(), "")
	assert.ErrorIs(t, err, statestore.ErrInvalidID)
}

func TestStore_SaveSummary_AfterClose(t *testing.T) {
	s := newTestStore(t)
	require.NoError(t, s.Close())
	err := s.SaveSummary(context.Background(), "conv-1", statestore.Summary{})
	assert.ErrorIs(t, err, ErrStoreClosed)
}
