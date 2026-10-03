package sdk

import (
	"sync/atomic"
)

// sharedEventBus reference-counts a conversation's hold on its event bus, so
// the bus is let go when the last conversation using it closes. A
// conversation and its forks share one config, and so one bus; letting go
// with the first of them would cut the others off.
//
// Letting go means closing the bus when the SDK created it — what stops the
// leak in #2146, where nothing closed it and its goroutines waited forever —
// or, for a bus the caller supplied via WithEventBus, unsubscribing the
// listeners the SDK added for this conversation, which would otherwise hold
// their goroutines on the caller's bus for as long as it lives.
type sharedEventBus struct {
	letGo func()
	refs  atomic.Int32
}

// newSharedEventBus returns a reference count of one, held by the
// conversation it was created for; letGo runs when the last is released.
func newSharedEventBus(letGo func()) *sharedEventBus {
	s := &sharedEventBus{letGo: letGo}
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

// release drops a reference and lets the bus go when it was the last one.
// Closing a bus drains pending events through its subscribers first.
func (s *sharedEventBus) release() {
	if s.refs.Add(-1) == 0 {
		s.letGo()
	}
}
