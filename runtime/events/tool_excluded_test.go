package events

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ToolExcludedCtx publishes tool.excluded with its payload, and a recorded
// tool.excluded event decodes back to the same payload.
func TestToolExcluded_PublishedAndRecorded(t *testing.T) {
	bus := NewEventBus()
	t.Cleanup(bus.Close)
	got := make(chan *Event, 1)
	bus.Subscribe(EventToolExcluded, func(e *Event) { got <- e })

	NewEmitter(bus, "run", "session-1", "conv").ToolExcludedCtx(context.Background(),
		"send_message", "rejected twice in one tool loop", 2, 3)

	var e *Event
	select {
	case e = <-got:
	case <-time.After(2 * time.Second):
		t.Fatal("no tool.excluded event")
	}
	want := ToolExcludedData{ToolName: "send_message", Reason: "rejected twice in one tool loop", Rejections: 2, Round: 3}
	data, ok := e.Data.(*ToolExcludedData)
	require.True(t, ok, "got %T", e.Data)
	assert.Equal(t, want, *data)
	assert.Equal(t, "session-1", e.SessionID)

	store, err := NewFileEventStore(t.TempDir())
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })
	require.NoError(t, store.Append(context.Background(), e))
	back, err := store.Query(context.Background(), &EventFilter{SessionID: "session-1"})
	require.NoError(t, err)
	require.Len(t, back, 1)
	decoded, ok := back[0].Data.(*ToolExcludedData)
	require.True(t, ok, "recorded tool.excluded decodes to %T", back[0].Data)
	assert.Equal(t, want, *decoded)
}
