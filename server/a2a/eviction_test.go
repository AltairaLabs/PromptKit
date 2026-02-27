package a2aserver

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/a2a"
	"github.com/AltairaLabs/PromptKit/runtime/types"
)

// --- eviction tests ---

func TestServer_EvictOnce_EvictsTerminalTasks(t *testing.T) {
	store := NewInMemoryTaskStore()

	// Create a completed task with an old timestamp.
	_, err := store.Create("old-task", "ctx-1")
	require.NoError(t, err)
	require.NoError(t, store.SetState("old-task", a2a.TaskStateWorking, nil))
	require.NoError(t, store.SetState("old-task", a2a.TaskStateCompleted, nil))

	// Backdate the task's timestamp to 2 hours ago.
	task, _ := store.Get("old-task")
	old := time.Now().Add(-2 * time.Hour)
	task.Status.Timestamp = &old

	// Create a recent completed task that should NOT be evicted.
	_, err = store.Create("new-task", "ctx-1")
	require.NoError(t, err)
	require.NoError(t, store.SetState("new-task", a2a.TaskStateWorking, nil))
	require.NoError(t, store.SetState("new-task", a2a.TaskStateCompleted, nil))

	// Create a working task that should NOT be evicted.
	_, err = store.Create("working-task", "ctx-1")
	require.NoError(t, err)
	require.NoError(t, store.SetState("working-task", a2a.TaskStateWorking, nil))

	conv := &mockConv{
		sendFunc: func(_ context.Context, _ any) (SendResult, error) {
			return &mockSendResult{
				parts: []types.ContentPart{types.NewTextPart("ok")},
				text:  "ok",
			}, nil
		},
	}
	opener := func(_ string) (Conversation, error) { return conv, nil }

	srv := NewServer(opener,
		WithTaskStore(store),
		WithTaskTTL(1*time.Hour),
		WithConversationTTL(0), // disable conv eviction for this test
	)
	defer func() { _ = srv.Shutdown(context.Background()) }()

	// Add a broadcaster for the old task to verify it gets cleaned up.
	b := srv.getBroadcaster("old-task")
	assert.NotNil(t, b)

	// Run eviction.
	srv.evictOnce()

	// The old task should be evicted.
	_, err = store.Get("old-task")
	assert.ErrorIs(t, err, ErrTaskNotFound)

	// The new and working tasks should still exist.
	_, err = store.Get("new-task")
	assert.NoError(t, err)

	_, err = store.Get("working-task")
	assert.NoError(t, err)

	// The broadcaster for old-task should have been removed.
	srv.subsMu.Lock()
	_, hasBroadcaster := srv.subs["old-task"]
	srv.subsMu.Unlock()
	assert.False(t, hasBroadcaster, "broadcaster for evicted task should be removed")
}

func TestServer_EvictOnce_EvictsIdleConversations(t *testing.T) {
	opener := func(_ string) (Conversation, error) {
		return &mockConv{
			sendFunc: func(_ context.Context, _ any) (SendResult, error) {
				return &mockSendResult{
					parts: []types.ContentPart{types.NewTextPart("ok")},
					text:  "ok",
				}, nil
			},
		}, nil
	}

	srv := NewServer(opener,
		WithTaskTTL(0), // disable task eviction
		WithConversationTTL(1*time.Hour),
	)
	defer func() { _ = srv.Shutdown(context.Background()) }()

	// Manually add conversations with different last-use timestamps.
	oldConv := &mockConv{
		sendFunc: func(_ context.Context, _ any) (SendResult, error) {
			return &mockSendResult{
				parts: []types.ContentPart{types.NewTextPart("ok")},
				text:  "ok",
			}, nil
		},
	}
	newConv := &mockConv{
		sendFunc: func(_ context.Context, _ any) (SendResult, error) {
			return &mockSendResult{
				parts: []types.ContentPart{types.NewTextPart("ok")},
				text:  "ok",
			}, nil
		},
	}
	srv.convsMu.Lock()
	srv.convs["old-ctx"] = oldConv
	srv.convLastUse["old-ctx"] = time.Now().Add(-2 * time.Hour)
	srv.convs["new-ctx"] = newConv
	srv.convLastUse["new-ctx"] = time.Now()
	srv.convsMu.Unlock()

	// Run eviction.
	srv.evictOnce()

	// The old conversation should be evicted and closed.
	srv.convsMu.RLock()
	_, hasOld := srv.convs["old-ctx"]
	_, hasNew := srv.convs["new-ctx"]
	srv.convsMu.RUnlock()

	assert.False(t, hasOld, "idle conversation should be evicted")
	assert.True(t, hasNew, "recent conversation should be kept")
	assert.True(t, oldConv.closed.Load(), "evicted conversation should be closed")
	assert.False(t, newConv.closed.Load(), "active conversation should not be closed")
}

func TestServer_EvictOnce_EvictsClosedBroadcasters(t *testing.T) {
	opener := func(_ string) (Conversation, error) {
		return &mockConv{
			sendFunc: func(_ context.Context, _ any) (SendResult, error) {
				return &mockSendResult{
					parts: []types.ContentPart{types.NewTextPart("ok")},
					text:  "ok",
				}, nil
			},
		}, nil
	}

	srv := NewServer(opener,
		WithTaskTTL(0),
		WithConversationTTL(0),
	)
	defer func() { _ = srv.Shutdown(context.Background()) }()

	// Add an open and a closed broadcaster.
	openB := srv.getBroadcaster("open-task")
	closedB := srv.getBroadcaster("closed-task")
	closedB.close()

	// Manually run eviction (TTLs are 0, so only broadcaster cleanup runs).
	srv.evictOnce()

	srv.subsMu.Lock()
	_, hasOpen := srv.subs["open-task"]
	_, hasClosed := srv.subs["closed-task"]
	srv.subsMu.Unlock()

	assert.True(t, hasOpen, "open broadcaster should be kept")
	assert.False(t, hasClosed, "closed broadcaster should be evicted")
	_ = openB // keep reference
}

func TestServer_ShutdownStopsEviction(t *testing.T) {
	opener := func(_ string) (Conversation, error) {
		return &mockConv{
			sendFunc: func(_ context.Context, _ any) (SendResult, error) {
				return &mockSendResult{
					parts: []types.ContentPart{types.NewTextPart("ok")},
					text:  "ok",
				}, nil
			},
		}, nil
	}

	srv := NewServer(opener,
		WithTaskTTL(1*time.Hour),
		WithConversationTTL(1*time.Hour),
	)

	// Shutdown should close the stop channel.
	err := srv.Shutdown(context.Background())
	assert.NoError(t, err)

	// Verify the stop channel is closed (non-blocking receive should succeed).
	select {
	case <-srv.stopCh:
		// expected
	default:
		t.Fatal("stopCh should be closed after Shutdown")
	}
}

func TestWithTaskTTL(t *testing.T) {
	opener := func(_ string) (Conversation, error) { return nil, nil }

	srv := NewServer(opener, WithTaskTTL(30*time.Minute))
	defer func() { _ = srv.Shutdown(context.Background()) }()
	assert.Equal(t, 30*time.Minute, srv.taskTTL)
}

func TestWithConversationTTL(t *testing.T) {
	opener := func(_ string) (Conversation, error) { return nil, nil }

	srv := NewServer(opener, WithConversationTTL(45*time.Minute))
	defer func() { _ = srv.Shutdown(context.Background()) }()
	assert.Equal(t, 45*time.Minute, srv.convTTL)
}

func TestServer_DisabledEviction(t *testing.T) {
	opener := func(_ string) (Conversation, error) { return nil, nil }

	// Both TTLs set to 0 should not start the eviction goroutine.
	srv := NewServer(opener,
		WithTaskTTL(0),
		WithConversationTTL(0),
	)
	defer func() { _ = srv.Shutdown(context.Background()) }()

	// evictOnce should be safe to call even with TTLs disabled.
	srv.evictOnce()
}

func TestServer_ConversationLastUseUpdated(t *testing.T) {
	conv := &mockConv{
		sendFunc: func(_ context.Context, _ any) (SendResult, error) {
			return &mockSendResult{
				parts: []types.ContentPart{types.NewTextPart("ok")},
				text:  "ok",
			}, nil
		},
	}
	opener := func(_ string) (Conversation, error) { return conv, nil }

	srv := NewServer(opener,
		WithTaskTTL(0),
		WithConversationTTL(1*time.Hour),
	)
	defer func() { _ = srv.Shutdown(context.Background()) }()

	// First call creates the conversation and sets last-use.
	_, err := srv.getOrCreateConversation("ctx-1")
	require.NoError(t, err)

	srv.convsMu.RLock()
	firstUse := srv.convLastUse["ctx-1"]
	srv.convsMu.RUnlock()
	assert.False(t, firstUse.IsZero())

	// Small sleep to ensure timestamp difference.
	time.Sleep(5 * time.Millisecond)

	// Second call should update last-use.
	_, err = srv.getOrCreateConversation("ctx-1")
	require.NoError(t, err)

	srv.convsMu.RLock()
	secondUse := srv.convLastUse["ctx-1"]
	srv.convsMu.RUnlock()
	assert.True(t, secondUse.After(firstUse), "last-use should be updated on reuse")
}
