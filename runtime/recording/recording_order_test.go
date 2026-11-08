package recording

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/v2/events"
)

// Events with the same timestamp keep the order they were appended in, so a
// session whose events share timestamps does not come back shuffled.
func TestExport_EqualTimestampsKeepAppendOrder(t *testing.T) {
	store, err := events.NewFileEventStore(t.TempDir())
	require.NoError(t, err)
	defer store.Close()

	const n = 64
	base := time.Now()
	for i := 0; i < n; i++ {
		// Two instants, interleaved: later events in each instant must stay
		// after earlier ones.
		ts := base.Add(time.Duration((n-i)%2) * time.Millisecond)
		require.NoError(t, store.Append(context.Background(), &events.Event{
			Type: events.EventMessageCreated, Timestamp: ts, SessionID: "s",
			Data: &events.MessageCreatedData{Role: "user", Content: "m"},
		}))
	}
	require.NoError(t, store.Sync())

	rec, err := Export(context.Background(), store, "s")
	require.NoError(t, err)
	require.Len(t, rec.Events, n)
	for i := 1; i < n; i++ {
		prev, cur := rec.Events[i-1], rec.Events[i]
		if prev.Timestamp.Equal(cur.Timestamp) {
			assert.Less(t, prev.Sequence, cur.Sequence, "events %d and %d at one instant are out of append order", i-1, i)
		} else {
			assert.True(t, prev.Timestamp.Before(cur.Timestamp))
		}
	}
}
