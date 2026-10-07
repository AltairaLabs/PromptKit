package sdk

import (
	"sync"
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
//
// Subscriptions a caller hands over with own (the hooks helpers, #2151) are
// removed at the same point, before letGo.
type sharedEventBus struct {
	letGo func()
	refs  atomic.Int32

	mu       sync.Mutex
	released bool
	owned    []func()
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

// own ties a subscription to the bus's lifetime: unsubscribe runs when the
// last holder releases it. On a bus already let go it runs at once.
func (s *sharedEventBus) own(unsubscribe func()) {
	s.mu.Lock()
	if s.released {
		s.mu.Unlock()
		unsubscribe()
		return
	}
	s.owned = append(s.owned, unsubscribe)
	s.mu.Unlock()
}

// release drops a reference and lets the bus go when it was the last one.
// Closing a bus drains pending events through its subscribers first, and so
// does unsubscribing.
func (s *sharedEventBus) release() {
	if s.refs.Add(-1) != 0 {
		return
	}
	s.mu.Lock()
	s.released = true
	owned := s.owned
	s.owned = nil
	s.mu.Unlock()
	for _, unsubscribe := range owned {
		unsubscribe()
	}
	s.letGo()
}
