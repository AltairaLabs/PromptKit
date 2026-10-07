package sdk

import (
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/v2/events"
	"github.com/AltairaLabs/PromptKit/sdk/v2/hooks"
)

// Handlers registered through the hooks helpers stop with the conversation,
// even on a bus supplied with WithEventBus that outlives it (#2151).

// countingHook registers a hooks.On handler on source and counts its calls.
func countingHook(source hooks.EventSource) *atomic.Int32 {
	var n atomic.Int32
	hooks.On(source, events.EventPipelineStarted, func(*events.Event) { n.Add(1) })
	return &n
}

// publishAndSettle publishes a probe, then waits until a sentinel published
// after it has been dispatched, plus a moment for listeners on their own
// goroutines to have run the probe too.
func publishAndSettle(t *testing.T, bus events.Bus) {
	t.Helper()
	require.True(t, bus.Publish(busCloseProbeEvent()), "a supplied bus must stay open")
	seen := make(chan struct{})
	var once atomic.Bool
	unsubscribe := bus.Subscribe(events.EventPipelineCompleted, func(*events.Event) {
		if once.CompareAndSwap(false, true) {
			close(seen)
		}
	})
	defer unsubscribe()
	require.True(t, bus.Publish(&events.Event{Type: events.EventPipelineCompleted}))
	select {
	case <-seen:
	case <-time.After(2 * time.Second):
		t.Fatal("sentinel event never arrived")
	}
	time.Sleep(50 * time.Millisecond)
}

func waitForCount(t *testing.T, n *atomic.Int32, want int32) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for n.Load() < want && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	require.Equal(t, want, n.Load())
}

func TestHooks_StopOnClose_SuppliedBus(t *testing.T) {
	bus := events.NewEventBus()
	t.Cleanup(bus.Close)
	conv := openForBusClose(t, WithEventBus(bus))
	calls := countingHook(conv)

	publishAndSettle(t, bus)
	waitForCount(t, calls, 1)

	require.NoError(t, conv.Close())
	publishAndSettle(t, bus)

	assert.Equal(t, int32(1), calls.Load(), "a closed conversation's hook must no longer be on the supplied bus")
}

func TestHooks_ForkKeepsSharedBusHooksAlive(t *testing.T) {
	bus := events.NewEventBus()
	t.Cleanup(bus.Close)
	conv := openForBusClose(t, WithEventBus(bus))
	calls := countingHook(conv)
	fork, err := conv.Fork()
	require.NoError(t, err)

	require.NoError(t, conv.Close())
	publishAndSettle(t, bus)
	waitForCount(t, calls, 1)

	require.NoError(t, fork.Close())
	publishAndSettle(t, bus)
	assert.Equal(t, int32(1), calls.Load(), "the hook must stop once the last conversation sharing the bus closes")
}

func TestOwnSubscription_ClosedConversationUnsubscribesAtOnce(t *testing.T) {
	conv := openForBusClose(t)
	require.NoError(t, conv.Close())

	var ran atomic.Bool
	conv.OwnSubscription(func() { ran.Store(true) })
	assert.True(t, ran.Load(), "a subscription handed to a closed conversation must be removed immediately")
}
