package file

import (
	"context"
	"testing"

	"github.com/AltairaLabs/PromptKit/runtime/statestore"
	"github.com/AltairaLabs/PromptKit/runtime/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestStore_AsMessageLog_DropIn asserts that *Store satisfies the
// statestore.MessageLog interface and that LogAppend → reload via Load
// returns the messages — the exact pattern the ProviderStage write-through
// relies on.
func TestStore_AsMessageLog_DropIn(t *testing.T) {
	root := t.TempDir()
	s1, err := NewStore(Options{Root: root})
	require.NoError(t, err)

	var ml statestore.MessageLog = s1 // compile-time interface check
	ctx := context.Background()
	_, err = ml.LogAppend(ctx, "conv-1", 0, []types.Message{
		makeMsg("user", "hello"),
		makeMsg("assistant", "hi"),
	})
	require.NoError(t, err)
	require.NoError(t, s1.Close())

	// Simulate process restart with a fresh store against the same root.
	s2, err := NewStore(Options{Root: root})
	require.NoError(t, err)
	defer func() { _ = s2.Close() }()

	loaded, err := s2.Load(ctx, "conv-1")
	require.NoError(t, err)
	require.Len(t, loaded.Messages, 2)
	assert.Equal(t, "hello", loaded.Messages[0].Content)
	assert.Equal(t, "hi", loaded.Messages[1].Content)
}

// TestStore_SatisfiesStoreInterfaces is a compile-time assertion that *Store
// implements every interface the SDK type-asserts. If this stops compiling,
// the file store has fallen out of step with the statestore contract.
func TestStore_SatisfiesStoreInterfaces(t *testing.T) {
	s, err := NewStore(Options{Root: t.TempDir()})
	require.NoError(t, err)
	defer func() { _ = s.Close() }()

	var _ statestore.Store = s
	var _ statestore.BulkWriter = s
	var _ statestore.MessageLog = s
	var _ statestore.MessageReader = s
	var _ statestore.MessageAppender = s
	var _ statestore.MetadataAccessor = s
	var _ statestore.SummaryAccessor = s
	var _ statestore.ListAccessor = s
}
