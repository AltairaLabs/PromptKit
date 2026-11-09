package file

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/AltairaLabs/PromptKit/runtime/v2/statestore"
	"github.com/AltairaLabs/PromptKit/runtime/v2/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewStore_RequiresRoot(t *testing.T) {
	_, err := NewStore(Options{})
	require.Error(t, err)
	assert.Contains(t, strings.ToLower(err.Error()), "root")
}

func TestNewStore_CreatesRoot(t *testing.T) {
	dir := t.TempDir()
	root := filepath.Join(dir, "store")

	s, err := NewStore(Options{Root: root})
	require.NoError(t, err)
	defer func() { _ = s.Close() }()

	info, err := os.Stat(root)
	require.NoError(t, err)
	assert.True(t, info.IsDir())
}

// TestFileStore_ClosedStoreRejectsEveryCall pins the documented Close
// contract: Close is idempotent, and every method called after it returns
// ErrStoreClosed rather than touching disk.
func TestFileStore_ClosedStoreRejectsEveryCall(t *testing.T) {
	s, err := NewStore(Options{Root: t.TempDir()})
	require.NoError(t, err)
	ctx := context.Background()
	require.NoError(t, s.Save(ctx, &statestore.ConversationState{ID: "conv"}))
	before, err := s.Load(ctx, "conv")
	require.NoError(t, err)
	assert.Equal(t, "conv", before.ID, "the store serves reads until Close")

	require.NoError(t, s.Close())
	require.NoError(t, s.Close(), "Close is idempotent")

	msgs := []types.Message{{Role: "user", Content: "hi"}}
	calls := map[string]func() error{
		"Load":               func() error { _, err := s.Load(ctx, "conv"); return err },
		"Save":               func() error { return s.Save(ctx, &statestore.ConversationState{ID: "conv"}) },
		"Fork":               func() error { return s.Fork(ctx, "conv", "copy") },
		"Delete":             func() error { return s.Delete(ctx, "conv") },
		"List":               func() error { _, err := s.List(ctx, statestore.ListOptions{}); return err },
		"AppendMessages":     func() error { return s.AppendMessages(ctx, "conv", msgs) },
		"LoadRecentMessages": func() error { _, err := s.LoadRecentMessages(ctx, "conv", 1); return err },
		"MessageCount":       func() error { _, err := s.MessageCount(ctx, "conv"); return err },
		"LogAppend":          func() error { _, err := s.LogAppend(ctx, "conv", 0, msgs); return err },
		"LogLoad":            func() error { _, err := s.LogLoad(ctx, "conv", 0); return err },
		"LogLen":             func() error { _, err := s.LogLen(ctx, "conv"); return err },
		"LoadMetadata":       func() error { _, err := s.LoadMetadata(ctx, "conv"); return err },
		"MergeMetadata":      func() error { return s.MergeMetadata(ctx, "conv", map[string]any{"k": "v"}) },
		"LoadSummaries":      func() error { _, err := s.LoadSummaries(ctx, "conv"); return err },
		"SaveSummary":        func() error { return s.SaveSummary(ctx, "conv", statestore.Summary{Content: "s"}) },
		"AppendList":         func() error { return s.AppendList(ctx, "conv", "l", [][]byte{[]byte(`1`)}) },
		"LoadList":           func() error { _, err := s.LoadList(ctx, "conv", "l"); return err },
		"ListLen":            func() error { _, err := s.ListLen(ctx, "conv", "l"); return err },
	}
	for name, call := range calls {
		t.Run(name, func(t *testing.T) {
			assert.ErrorIs(t, call(), ErrStoreClosed)
		})
	}
}
