package sdk

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/v2/events"
)

func TestSharedEventBus_ClosesOnLastRelease(t *testing.T) {
	bus := events.NewEventBus()
	ref := newSharedEventBus(bus)
	require.True(t, ref.acquire())

	ref.release()
	assert.True(t, bus.Publish(busCloseProbeEvent()), "one reference is still held")

	ref.release()
	assert.False(t, bus.Publish(busCloseProbeEvent()), "the last release closes the bus")
}

func TestSharedEventBus_AcquireFailsOnceReleased(t *testing.T) {
	ref := newSharedEventBus(events.NewEventBus())
	ref.release()

	assert.False(t, ref.acquire(),
		"a closed bus must not be handed out, or its new holder publishes into nothing")
}
