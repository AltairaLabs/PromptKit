package events

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewFileEventStore(t *testing.T) {
	dir := t.TempDir()

	store, err := NewFileEventStore(dir)
	require.NoError(t, err)
	require.NotNil(t, store)
	defer store.Close()

	assert.Equal(t, dir, store.dir)
}

func TestNewFileEventStore_CreatesDirectory(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "nested", "events")

	store, err := NewFileEventStore(dir)
	require.NoError(t, err)
	defer store.Close()

	info, err := os.Stat(dir)
	require.NoError(t, err)
	assert.True(t, info.IsDir())
}

func TestFileEventStore_Append(t *testing.T) {
	store, err := NewFileEventStore(t.TempDir())
	require.NoError(t, err)
	defer store.Close()

	event := &Event{
		Type:      EventMessageCreated,
		Timestamp: time.Now(),
		SessionID: "session-123",
		Data: &MessageCreatedData{
			Role:    "user",
			Content: "Hello, world!",
		},
	}

	err = store.Append(context.Background(), event)
	require.NoError(t, err)

	// Verify file was created
	path := store.sessionPath("session-123")
	_, err = os.Stat(path)
	require.NoError(t, err)
}

func TestFileEventStore_Append_RequiresSessionID(t *testing.T) {
	store, err := NewFileEventStore(t.TempDir())
	require.NoError(t, err)
	defer store.Close()

	event := &Event{
		Type:      EventMessageCreated,
		Timestamp: time.Now(),
		// No SessionID
	}

	err = store.Append(context.Background(), event)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "session ID")
}

func TestFileEventStore_Query(t *testing.T) {
	store, err := NewFileEventStore(t.TempDir())
	require.NoError(t, err)
	defer store.Close()

	sessionID := "session-query-test"
	now := time.Now()

	// Append multiple events
	events := []*Event{
		{Type: EventMessageCreated, Timestamp: now, SessionID: sessionID, ConversationID: "conv-1"},
		{Type: EventToolCallStarted, Timestamp: now.Add(time.Second), SessionID: sessionID, ConversationID: "conv-1"},
		{Type: EventToolCallCompleted, Timestamp: now.Add(2 * time.Second), SessionID: sessionID, ConversationID: "conv-1"},
		{Type: EventMessageCreated, Timestamp: now.Add(3 * time.Second), SessionID: sessionID, ConversationID: "conv-2"},
	}

	for _, e := range events {
		require.NoError(t, store.Append(context.Background(), e))
	}
	require.NoError(t, store.Sync())

	t.Run("all events for session", func(t *testing.T) {
		result, err := store.Query(context.Background(), &EventFilter{SessionID: sessionID})
		require.NoError(t, err)
		assert.Len(t, result, 4)
	})

	t.Run("filter by conversation", func(t *testing.T) {
		result, err := store.Query(context.Background(), &EventFilter{
			SessionID:      sessionID,
			ConversationID: "conv-1",
		})
		require.NoError(t, err)
		assert.Len(t, result, 3)
	})

	t.Run("filter by type", func(t *testing.T) {
		result, err := store.Query(context.Background(), &EventFilter{
			SessionID: sessionID,
			Types:     []EventType{EventMessageCreated},
		})
		require.NoError(t, err)
		assert.Len(t, result, 2)
	})

	t.Run("limit results", func(t *testing.T) {
		result, err := store.Query(context.Background(), &EventFilter{
			SessionID: sessionID,
			Limit:     2,
		})
		require.NoError(t, err)
		assert.Len(t, result, 2)
	})

	t.Run("non-existent session", func(t *testing.T) {
		result, err := store.Query(context.Background(), &EventFilter{SessionID: "no-such-session"})
		require.NoError(t, err)
		assert.Empty(t, result)
	})

	t.Run("requires session ID", func(t *testing.T) {
		_, err := store.Query(context.Background(), &EventFilter{})
		require.Error(t, err)
	})
}

func TestFileEventStore_Stream(t *testing.T) {
	store, err := NewFileEventStore(t.TempDir())
	require.NoError(t, err)
	defer store.Close()

	sessionID := "session-stream-test"

	// Append events
	for i := 0; i < 5; i++ {
		require.NoError(t, store.Append(context.Background(), &Event{
			Type:      EventMessageCreated,
			Timestamp: time.Now(),
			SessionID: sessionID,
		}))
	}

	// Close the file to ensure data is flushed
	require.NoError(t, store.Close())

	// Reopen for reading
	store, err = NewFileEventStore(store.dir)
	require.NoError(t, err)
	defer store.Close()

	ch, err := store.Stream(context.Background(), sessionID)
	require.NoError(t, err)

	var count int
	for range ch {
		count++
	}
	assert.Equal(t, 5, count)
}

func TestFileEventStore_Stream_NonExistentSession(t *testing.T) {
	store, err := NewFileEventStore(t.TempDir())
	require.NoError(t, err)
	defer store.Close()

	ch, err := store.Stream(context.Background(), "no-such-session")
	require.NoError(t, err)

	var count int
	for range ch {
		count++
	}
	assert.Equal(t, 0, count)
}

func TestFileEventStore_Stream_ContextCancellation(t *testing.T) {
	store, err := NewFileEventStore(t.TempDir())
	require.NoError(t, err)
	defer store.Close()

	sessionID := "session-cancel-test"

	// Append many events
	for i := 0; i < 100; i++ {
		require.NoError(t, store.Append(context.Background(), &Event{
			Type:      EventMessageCreated,
			Timestamp: time.Now(),
			SessionID: sessionID,
		}))
	}

	require.NoError(t, store.Close())
	store, err = NewFileEventStore(store.dir)
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(context.Background())
	ch, err := store.Stream(ctx, sessionID)
	require.NoError(t, err)

	// Read a few then cancel
	<-ch
	<-ch
	cancel()

	// Channel should close eventually
	for range ch {
		// drain
	}
}

func TestEventBus_WithStore(t *testing.T) {
	dir := t.TempDir()
	store, err := NewFileEventStore(dir)
	require.NoError(t, err)
	defer store.Close()

	bus := NewEventBus().WithStore(store)
	assert.Equal(t, store, bus.Store())

	sessionID := "session-bus-test"

	// Publish an event
	event := &Event{
		Type:      EventMessageCreated,
		Timestamp: time.Now(),
		SessionID: sessionID,
		Data:      &MessageCreatedData{Role: "user", Content: "test"},
	}
	bus.Publish(event)

	// Sync to disk
	require.NoError(t, store.Sync())

	// Query the store
	events, err := store.Query(context.Background(), &EventFilter{SessionID: sessionID})
	require.NoError(t, err)
	require.Len(t, events, 1)
	assert.Equal(t, EventMessageCreated, events[0].Type)
}

func TestEventBus_WithStore_SkipsEventsWithoutSessionID(t *testing.T) {
	store, err := NewFileEventStore(t.TempDir())
	require.NoError(t, err)
	defer store.Close()

	bus := NewEventBus().WithStore(store)

	// Publish event without session ID
	event := &Event{
		Type:      EventPipelineStarted,
		Timestamp: time.Now(),
		// No SessionID
	}
	bus.Publish(event)

	time.Sleep(50 * time.Millisecond)

	// No files should be created
	entries, _ := os.ReadDir(store.dir)
	assert.Empty(t, entries)
}
