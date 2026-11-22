// Package stagetest holds test helpers that the tests of runtime/pipeline/stage
// and of its stage/internal packages share. It imports only stage/internal/core
// (and runtime test doubles), never stage itself: an internal package's tests
// that reached stage would recompile all of it for every mutant (#2876).
package stagetest

import (
	"sync"
	"time"

	"github.com/AltairaLabs/PromptKit/runtime/v2/pipeline/stage/internal/core"
)

// FakeIdleTimer is a virtual clock with one AfterFunc timer on it, for
// core.WithIdleTimer. The timer fires only when Advance moves the clock to its
// deadline, so a test decides exactly when the idle window ends relative to
// the resets it makes. It is safe for the pipeline's goroutines to reset it
// while the test advances it.
type FakeIdleTimer struct {
	mu            sync.Mutex
	now, deadline time.Duration
	armed         bool
	fire          func()
}

// AfterFunc has time.AfterFunc's contract on the virtual clock: fn runs once
// Advance reaches d from now.
func (f *FakeIdleTimer) AfterFunc(d time.Duration, fn func()) core.IdleTimer {
	f.mu.Lock()
	f.fire = fn
	f.mu.Unlock()
	f.Reset(d)
	return f
}

// Stop disarms the timer, reporting whether it was armed.
func (f *FakeIdleTimer) Stop() bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	was := f.armed
	f.armed = false
	return was
}

// Reset arms the timer to fire d from now, reporting whether it was armed.
func (f *FakeIdleTimer) Reset(d time.Duration) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	was := f.armed
	f.deadline, f.armed = f.now+d, true
	return was
}

// Advance moves the clock on by d, firing the timer if its deadline falls in
// that time.
func (f *FakeIdleTimer) Advance(d time.Duration) {
	f.mu.Lock()
	f.now += d
	fire := f.armed && f.now >= f.deadline
	if fire {
		f.armed = false
	}
	fn := f.fire
	f.mu.Unlock()
	if fire {
		fn()
	}
}
