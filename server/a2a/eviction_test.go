package a2aserver

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/v2/a2a"
	"github.com/AltairaLabs/PromptKit/runtime/v2/types"
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
	old := time.Now().Add(-2 * time.Hour)
	setTaskTimestamp(store, "old-task", old)

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

	// A stale cancel func for the old task must be cleaned up too.
	srv.registerCancel("old-task", func() {})

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

	srv.cancelsMu.Lock()
	_, hasCancel := srv.cancels["old-task"]
	srv.cancelsMu.Unlock()
	assert.False(t, hasCancel, "cancel func for evicted task should be removed")
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

// evictionFixture is a server whose store holds a completed task finished
// age ago and whose conversation table holds one conversation idle for age.
type evictionFixture struct {
	srv   *Server
	store *InMemoryTaskStore
	conv  *mockConv
}

func newEvictionFixture(t *testing.T, age time.Duration, opts ...Option) *evictionFixture {
	t.Helper()
	store := NewInMemoryTaskStore()
	_, err := store.Create("done", "ctx-1")
	require.NoError(t, err)
	require.NoError(t, store.SetState("done", a2a.TaskStateWorking, nil))
	require.NoError(t, store.SetState("done", a2a.TaskStateCompleted, nil))
	setTaskTimestamp(store, "done", time.Now().Add(-age))

	opener := func(_ string) (Conversation, error) { return nil, nil }
	srv := NewServer(opener, append([]Option{WithTaskStore(store)}, opts...)...)
	t.Cleanup(func() { _ = srv.Shutdown(context.Background()) })

	conv := &mockConv{}
	srv.convsMu.Lock()
	srv.convs["idle"] = conv
	srv.convLastUse["idle"] = time.Now().Add(-age)
	srv.convsMu.Unlock()
	return &evictionFixture{srv: srv, store: store, conv: conv}
}

func (f *evictionFixture) taskKept() bool {
	_, err := f.store.Get("done")
	return err == nil
}

func (f *evictionFixture) convKept() bool {
	f.srv.convsMu.RLock()
	defer f.srv.convsMu.RUnlock()
	_, ok := f.srv.convs["idle"]
	return ok
}

// WithTaskTTL sets how long a finished task is kept: one older than the TTL
// is evicted, one younger is kept.
func TestWithTaskTTL(t *testing.T) {
	older := newEvictionFixture(t, time.Hour, WithTaskTTL(30*time.Minute), WithConversationTTL(0))
	older.srv.evictOnce()
	assert.False(t, older.taskKept(), "a task finished before the TTL must be evicted")

	younger := newEvictionFixture(t, 10*time.Minute, WithTaskTTL(30*time.Minute), WithConversationTTL(0))
	younger.srv.evictOnce()
	assert.True(t, younger.taskKept(), "a task finished within the TTL must be kept")
}

// WithConversationTTL sets how long an idle conversation is kept: one idle
// longer is closed and dropped, one idle less is kept open.
func TestWithConversationTTL(t *testing.T) {
	older := newEvictionFixture(t, time.Hour, WithTaskTTL(0), WithConversationTTL(45*time.Minute))
	older.srv.evictOnce()
	assert.False(t, older.convKept())
	assert.True(t, older.conv.closed.Load(), "an evicted conversation must be closed")

	younger := newEvictionFixture(t, 30*time.Minute, WithTaskTTL(0), WithConversationTTL(45*time.Minute))
	younger.srv.evictOnce()
	assert.True(t, younger.convKept())
	assert.False(t, younger.conv.closed.Load())
}

// A TTL of zero disables that eviction: nothing is evicted however old.
func TestServer_DisabledEviction(t *testing.T) {
	f := newEvictionFixture(t, 1000*time.Hour, WithTaskTTL(0), WithConversationTTL(0))
	f.srv.evictOnce()
	assert.True(t, f.taskKept(), "task eviction is off at TTL 0")
	assert.True(t, f.convKept(), "conversation eviction is off at TTL 0")
	assert.False(t, f.conv.closed.Load())
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
	_, err := srv.getOrCreateConversation(&rpcCall{}, "ctx-1")
	require.NoError(t, err)

	srv.convsMu.RLock()
	firstUse := srv.convLastUse["ctx-1"]
	srv.convsMu.RUnlock()
	assert.False(t, firstUse.IsZero())

	// Small sleep to ensure timestamp difference.
	time.Sleep(5 * time.Millisecond)

	// Second call should update last-use.
	_, err = srv.getOrCreateConversation(&rpcCall{}, "ctx-1")
	require.NoError(t, err)

	srv.convsMu.RLock()
	secondUse := srv.convLastUse["ctx-1"]
	srv.convsMu.RUnlock()
	assert.True(t, secondUse.After(firstUse), "last-use should be updated on reuse")
}
