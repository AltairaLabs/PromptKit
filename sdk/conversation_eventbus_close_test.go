package sdk

import (
	"context"
	"os"
	"path/filepath"
	"sync"
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

// pipeline.completed (and the last stage.completed) are emitted after the
// turn's output closes, so Send has already returned when they are published.
// Close must wait for them before closing the bus, or a store loses the
// completion of a turn closed straight after it finished.
func TestClose_KeepsEventsEmittedAfterTheTurnReturns(t *testing.T) {
	packPath := filepath.Join(t.TempDir(), "bus-close.pack.json")
	require.NoError(t, os.WriteFile(packPath, []byte(inertProbePackJSON), 0o600))
	store := newCountingEventStore()

	// The loss is a race (a few percent of turns), so take enough samples to
	// catch it every run, spread over workers to keep the test fast. Workers
	// use assert, not require: FailNow must not run off the test goroutine.
	const workers, turnsEach = 8, 25
	var wg sync.WaitGroup
	for range workers {
		wg.Go(func() {
			for range turnsEach {
				conv, err := Open(packPath, "chat",
					WithProvider(mock.NewProvider("mock-bus", "mock-model", false)),
					WithEventStore(store))
				if !assert.NoError(t, err) {
					return
				}
				_, err = conv.Send(context.Background(), "hi")
				assert.NoError(t, err)
				assert.NoError(t, conv.Close())
			}
		})
	}
	wg.Wait()
	assert.Equal(t, workers*turnsEach, store.count(events.EventPipelineCompleted),
		"every turn's pipeline.completed must reach the store before Close closes the bus")
}

// A fork shares its parent's config and so its bus. Closing either one must
// leave the bus open for the other; it closes when the last of them closes.
func TestClose_SharedBusClosesWithTheLastOfParentAndFork(t *testing.T) {
	for _, forkFirst := range []bool{true, false} {
		name := "parent first"
		if forkFirst {
			name = "fork first"
		}
		t.Run(name, func(t *testing.T) {
			parent := openForBusClose(t, WithEventStore(&inertStubEventStore{}))
			fork, err := parent.Fork()
			require.NoError(t, err)
			bus := parent.EventBus()
			require.Same(t, bus, fork.EventBus(), "a fork publishes to its parent's bus")

			first, last := parent, fork
			if forkFirst {
				first, last = fork, parent
			}
			require.NoError(t, first.Close())
			assert.True(t, bus.Publish(busCloseProbeEvent()),
				"closing one of parent and fork must not close the bus the other still uses")

			require.NoError(t, last.Close())
			assert.False(t, bus.Publish(busCloseProbeEvent()),
				"the bus must close when the last conversation using it closes")
		})
	}
}
