package sdk

import (
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/v2/events"
	"github.com/AltairaLabs/PromptKit/runtime/v2/providers/mock"
)

// Conversation.Close must close the event bus the SDK created for it. Before
// it did, a bus with any subscriber (an event store, OTel, metrics) kept its
// worker goroutines blocked on the event channel forever: ten leaked per
// conversation, found by the round1 load test with --event-store (#2146).

func openForBusClose(t *testing.T, opts ...Option) *Conversation {
	t.Helper()
	packPath := filepath.Join(t.TempDir(), "bus-close.pack.json")
	require.NoError(t, os.WriteFile(packPath, []byte(inertProbePackJSON), 0o600))

	opts = append([]Option{WithProvider(mock.NewProvider("mock-bus", "mock-model", false))}, opts...)
	conv, err := Open(packPath, "chat", opts...)
	require.NoError(t, err)
	return conv
}

func busCloseProbeEvent() *events.Event {
	return &events.Event{Type: events.EventPipelineStarted, Data: &events.PipelineStartedData{}}
}

func TestClose_ClosesSDKCreatedEventBus(t *testing.T) {
	conv := openForBusClose(t, WithEventStore(&inertStubEventStore{}))
	bus := conv.EventBus()
	require.NotNil(t, bus)
	require.True(t, bus.Publish(busCloseProbeEvent()), "bus must accept events while the conversation is open")

	require.NoError(t, conv.Close())

	assert.False(t, bus.Publish(busCloseProbeEvent()),
		"Close must close the bus it created, or its workers leak")
}

func TestClose_LeavesSuppliedEventBusOpen(t *testing.T) {
	bus := events.NewEventBus()
	t.Cleanup(bus.Close)

	conv := openForBusClose(t, WithEventBus(bus), WithEventStore(&inertStubEventStore{}))
	require.NoError(t, conv.Close())

	assert.True(t, bus.Publish(busCloseProbeEvent()),
		"a bus passed via WithEventBus may be shared and is the caller's to close")
}

func TestClose_DeliversPendingEventsBeforeReturning(t *testing.T) {
	conv := openForBusClose(t)
	var delivered atomic.Int64
	conv.EventBus().SubscribeAll(func(*events.Event) { delivered.Add(1) })

	const published = 50
	for range published {
		require.True(t, conv.EventBus().Publish(busCloseProbeEvent()))
	}
	require.NoError(t, conv.Close())

	assert.GreaterOrEqual(t, delivered.Load(), int64(published),
		"closing the bus drains it, so events published before Close reach subscribers by the time it returns")
}
