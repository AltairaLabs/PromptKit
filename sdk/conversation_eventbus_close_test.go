package sdk

import (
	"context"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/v2/events"
	"github.com/AltairaLabs/PromptKit/runtime/v2/providers/mock"
)

// Conversation.Close must close the event bus the SDK created for it. Before
// it did, a bus with any subscriber (an event store, OTel, metrics) kept its
// worker goroutines blocked on the event channel forever: ten leaked per
// conversation, found by the round1 load test with --event-store (#2146).

// writeBusClosePack writes a minimal one-prompt pack and returns its path.
func writeBusClosePack(t *testing.T) string {
	t.Helper()
	packPath := filepath.Join(t.TempDir(), "bus-close.pack.json")
	require.NoError(t, os.WriteFile(packPath, []byte(inertProbePackJSON), 0o600))
	return packPath
}

func openForBusClose(t *testing.T, opts ...Option) *Conversation {
	t.Helper()
	packPath := writeBusClosePack(t)

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
	packPath := writeBusClosePack(t)
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

// captureEventBuses records every bus Open creates for the rest of the test.
// Not for parallel tests: it swaps a package variable.
func captureEventBuses(t *testing.T) *[]events.Bus {
	t.Helper()
	var buses []events.Bus
	orig := newEventBus
	newEventBus = func() events.Bus {
		b := orig()
		buses = append(buses, b)
		return b
	}
	t.Cleanup(func() { newEventBus = orig })
	return &buses
}

// An Open that fails after creating the conversation's bus returns no
// conversation, so Close never runs: the failed Open itself must close the bus,
// or every failed attempt leaks its workers (the #2146 leak, by another door).
// An MCP server named without an endpoint resolver fails Open after the bus
// exists, on every Open variant.
func TestOpen_FailureAfterBusCreationClosesTheBus(t *testing.T) {
	packPath := writeBusClosePack(t)
	unresolvableMCP := WithMCPServer(NewMCPServerByName("codegen"))
	provider := WithProvider(mock.NewProvider("mock-bus", "mock-model", false))

	opens := map[string]func() (*Conversation, error){
		"Open": func() (*Conversation, error) {
			return Open(packPath, "chat", provider, WithEventStore(&inertStubEventStore{}), unresolvableMCP)
		},
		"OpenDuplex": func() (*Conversation, error) {
			return OpenDuplex(packPath, "chat", provider, WithEventStore(&inertStubEventStore{}), unresolvableMCP)
		},
		"PackTemplate.Open": func() (*Conversation, error) {
			tmpl, err := LoadTemplate(packPath)
			if err != nil {
				return nil, err
			}
			return tmpl.Open("chat", provider, WithEventStore(&inertStubEventStore{}), unresolvableMCP)
		},
	}
	for name, open := range opens {
		t.Run(name, func(t *testing.T) {
			buses := captureEventBuses(t)

			conv, err := open()
			require.Error(t, err)
			require.Nil(t, conv)
			require.Len(t, *buses, 1, "the failure must come after the bus was created, or this proves nothing")

			assert.False(t, (*buses)[0].Publish(busCloseProbeEvent()),
				"a failed Open must close the bus it created")
		})
	}
}

// Forking a closed conversation must fail like Send does: its providers are
// closed and its bus released, so a fork would publish into a closed bus.
func TestFork_ClosedConversationIsRejected(t *testing.T) {
	conv := openForBusClose(t, WithEventStore(&inertStubEventStore{}))
	require.NoError(t, conv.Close())

	fork, err := conv.Fork()
	assert.ErrorIs(t, err, ErrConversationClosed)
	assert.Nil(t, fork)
}

// Fork racing Close either loses (ErrConversationClosed) or wins and holds its
// own reference, so the shared bus stays open until the fork closes too. Under
// -race this also covers the busRef hand-off between Close and Fork.
func TestClose_ConcurrentForkKeepsItsBusOpen(t *testing.T) {
	for i := range 100 {
		parent := openForBusClose(t, WithEventStore(&inertStubEventStore{}))
		bus := parent.EventBus()

		closed := make(chan error, 1)
		go func() { closed <- parent.Close() }()
		if i%2 == 1 {
			// Let Close get ahead on half the runs, so both orders occur.
			time.Sleep(time.Duration(i%7) * 100 * time.Microsecond)
		}
		fork, err := parent.Fork()
		require.NoError(t, <-closed)

		if err != nil {
			require.ErrorIs(t, err, ErrConversationClosed)
			continue
		}
		require.True(t, bus.Publish(busCloseProbeEvent()),
			"a fork that won the race holds a reference, so the bus must stay open")
		require.NoError(t, fork.Close())
		require.False(t, bus.Publish(busCloseProbeEvent()),
			"the bus must close with the last of parent and fork")
	}
}

// On a bus the caller supplied, Close leaves the bus open but removes the
// listeners Open added for this conversation. Otherwise each conversation
// left its store (and OTel and metrics) listeners on the caller's bus, each
// with its own goroutine and queue, for as long as that bus lived.
func TestClose_UnsubscribesFromSuppliedEventBus(t *testing.T) {
	bus := events.NewEventBus()
	t.Cleanup(bus.Close)
	store := newCountingEventStore()

	conv := openForBusClose(t, WithEventBus(bus), WithEventStore(store))
	require.NoError(t, conv.Close())
	before := store.count(events.EventPipelineStarted)

	probe := busCloseProbeEvent()
	probe.SessionID = "after-close"
	require.True(t, bus.Publish(probe), "a supplied bus must stay open")

	// A sentinel published after the probe arrives once the bus has dispatched
	// the probe. Listeners run on their own goroutines, so allow a moment for
	// a still-subscribed store to have run it too.
	seen := make(chan struct{})
	unsubscribe := bus.Subscribe(events.EventPipelineCompleted, func(*events.Event) { close(seen) })
	defer unsubscribe()
	require.True(t, bus.Publish(&events.Event{Type: events.EventPipelineCompleted}))
	select {
	case <-seen:
	case <-time.After(2 * time.Second):
		t.Fatal("sentinel event never arrived")
	}
	time.Sleep(50 * time.Millisecond)

	assert.Equal(t, before, store.count(events.EventPipelineStarted),
		"the closed conversation's store listener must no longer be on the supplied bus")
}
