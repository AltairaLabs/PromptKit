package sdk

import (
	"sync/atomic"

	"github.com/AltairaLabs/PromptKit/runtime/v2/events"
)

// sharedEventBus reference-counts an event bus the SDK created, so the bus is
// closed when the last conversation using it closes. A conversation and its
// forks share one config, and so one bus; closing it with the first of them
// would cut the others off.
//
// Closing it at all is what stops the leak in #2146: once anything subscribed,
// a bus's worker goroutines blocked on its event channel until it was closed,
// and nothing closed it.
type sharedEventBus struct {
	bus  events.Bus
	refs atomic.Int32
}

// newSharedEventBus wraps bus with one reference, held by the conversation
// it was created for.
func newSharedEventBus(bus events.Bus) *sharedEventBus {
	s := &sharedEventBus{bus: bus}
	s.refs.Store(1)
	return s
}

// acquire takes another reference. It fails once every holder has released
// the bus, since the bus is closed by then.
func (s *sharedEventBus) acquire() bool {
	for {
		n := s.refs.Load()
		if n <= 0 {
			return false
		}
		if s.refs.CompareAndSwap(n, n+1) {
			return true
		}
	}
}

// release drops a reference and closes the bus when it was the last one.
// Closing drains pending events through the subscribers before returning.
func (s *sharedEventBus) release() {
	if s.refs.Add(-1) == 0 {
		s.bus.Close()
	}
}
