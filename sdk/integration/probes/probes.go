package probes

import (
	"maps"
	"sync"
	"time"

	"github.com/AltairaLabs/PromptKit/runtime/events"
)

// Probes captures observed operation counts for a single Send window.
//
// Probes is wired into the SDK via Run: store calls are counted via a wrapping
// statestore.Store; events are counted via a SubscribeAll listener on the bus.
// Renderer / tool / provider counts are derived from existing event types
// rather than wrapping each interface — the lifecycle events on the bus are
// already authoritative for "this operation happened once."
//
// Counters are reset by ResetCounters; Run calls it after sdk.Open completes
// so that init-time Loads/Saves do not appear in Send-scoped snapshots.
type Probes struct {
	store *probedStore
	bus   *events.EventBus

	mu     sync.Mutex
	events map[events.EventType]int
}

// Snapshot is an immutable point-in-time view of probe counters.
//
// Operation names follow the convention "<dependency>.<method>" for direct
// wraps (e.g. "store.Load") and "events.<event_type>" for events (e.g.
// "events.prompt.template.rendered").
type Snapshot struct {
	counts map[Op]int
}

// Count returns the count for op, or 0 if op was never seen.
func (s Snapshot) Count(op Op) int {
	return s.counts[op]
}

// All returns a copy of the underlying counts map. Mostly useful for
// debugging — assertions should use Count.
func (s Snapshot) All() map[Op]int {
	out := make(map[Op]int, len(s.counts))
	maps.Copy(out, s.counts)
	return out
}

// Snapshot captures the current counter state.
func (p *Probes) Snapshot() Snapshot {
	loads, saves, forks := p.store.snapshot()

	p.mu.Lock()
	defer p.mu.Unlock()

	const storeOpCount = 3 // store.Load, store.Save, store.Fork

	counts := make(map[Op]int, len(p.events)+storeOpCount)
	counts["store.Load"] = loads
	counts["store.Save"] = saves
	counts["store.Fork"] = forks
	for et, n := range p.events {
		counts[Op("events."+string(et))] = n
	}
	return Snapshot{counts: counts}
}

// ResetCounters zeroes every counter. Run calls this after sdk.Open so that
// initialisation traffic does not leak into per-Send observations.
func (p *Probes) ResetCounters() {
	p.store.reset()

	p.mu.Lock()
	defer p.mu.Unlock()
	p.events = make(map[events.EventType]int, len(p.events))
}

// Bus returns the event bus the probes are listening on. Tests can use it
// for additional subscriptions, but should not close it — Run wires cleanup.
func (p *Probes) Bus() *events.EventBus {
	return p.bus
}

// WaitForCount blocks until the count for op reaches at least atLeast or
// the timeout expires. Returns true if the threshold was met, false on
// timeout.
//
// Counts grow monotonically inside a single observation window (we do not
// emit anti-events), so a successful WaitForCount guarantees the snapshot
// holds at least the requested count. Callers asserting an *exact* count
// should pair WaitForCount with a brief settle period and an Equal check
// to catch overshoot.
func (p *Probes) WaitForCount(op Op, atLeast int, timeout time.Duration) bool {
	const pollInterval = 5 * time.Millisecond
	deadline := time.Now().Add(timeout)
	for {
		if p.Snapshot().Count(op) >= atLeast {
			return true
		}
		if time.Now().After(deadline) {
			return false
		}
		time.Sleep(pollInterval)
	}
}
