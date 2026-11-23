package events

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ToolOfferRebuiltCtx publishes tool.offer.rebuilt with its payload, and a
// recorded tool.offer.rebuilt event decodes back to the same payload.
func TestToolOfferRebuilt_PublishedAndRecorded(t *testing.T) {
	bus := NewEventBus()
	t.Cleanup(bus.Close)
	got := make(chan *Event, 1)
	bus.Subscribe(EventToolOfferRebuilt, func(e *Event) { got <- e })

	want := ToolOfferRebuiltData{
		Reasons:         []string{ToolOfferReasonSkillGrant},
		Added:           []string{"refund"},
		Removed:         []string{"lookup_order"},
		Round:           2,
		CachingProvider: true,
	}
	sent := want
	NewEmitter(bus, "run", "session-1", "conv").ToolOfferRebuiltCtx(context.Background(), &sent)

	var e *Event
	select {
	case e = <-got:
	case <-time.After(2 * time.Second):
		t.Fatal("no tool.offer.rebuilt event")
	}
	assert.Equal(t, EventType("tool.offer.rebuilt"), e.Type)
	data, ok := e.Data.(*ToolOfferRebuiltData)
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
	decoded, ok := back[0].Data.(*ToolOfferRebuiltData)
	require.True(t, ok, "recorded tool.offer.rebuilt decodes to %T", back[0].Data)
	assert.Equal(t, want, *decoded)
}

// A provider call's cache writes survive the event store on both
// provider.call.completed and provider.call.usage.
func TestProviderCallEvents_CacheWriteTokensRecorded(t *testing.T) {
	store, err := NewFileEventStore(t.TempDir())
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })
	ctx := context.Background()
	require.NoError(t, store.Append(ctx, &Event{
		Type: EventProviderCallCompleted, SessionID: "s", Timestamp: time.Now(),
		Data: &ProviderCallCompletedData{InputTokens: 3, CachedTokens: 0, CacheWriteTokens: 9656, CallID: "c1"},
	}))
	require.NoError(t, store.Append(ctx, &Event{
		Type: EventProviderCallUsage, SessionID: "s", Timestamp: time.Now(),
		Data: &ProviderCallUsageData{CacheWriteTokens: 41, CallID: "c1"},
	}))
	back, err := store.Query(ctx, &EventFilter{SessionID: "s"})
	require.NoError(t, err)
	require.Len(t, back, 2)
	completed, ok := back[0].Data.(*ProviderCallCompletedData)
	require.True(t, ok, "got %T", back[0].Data)
	assert.Equal(t, 9656, completed.CacheWriteTokens)
	usage, ok := back[1].Data.(*ProviderCallUsageData)
	require.True(t, ok, "got %T", back[1].Data)
	assert.Equal(t, 41, usage.CacheWriteTokens)
}
