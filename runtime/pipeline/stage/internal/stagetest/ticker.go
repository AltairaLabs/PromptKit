package stagetest

import "time"

// FakeTicker is a heartbeat ticker for core.KeepIdleAliveWith (or a stage's
// own heartbeat hook) that ticks only when the test sends on Ticks. Stopped
// closes when the heartbeat stops its ticker, which it does as it exits.
type FakeTicker struct {
	// Interval is the period the heartbeat asked for.
	Interval time.Duration
	Ticks    chan time.Time
	Stopped  chan struct{}
}

// NewFakeTicker is a FakeTicker that has not been started.
func NewFakeTicker() *FakeTicker {
	return &FakeTicker{Ticks: make(chan time.Time), Stopped: make(chan struct{})}
}

// Start has the signature of a heartbeat's ticker: it records d and returns
// the test-driven tick channel and a stop func that closes Stopped.
func (f *FakeTicker) Start(d time.Duration) (<-chan time.Time, func()) {
	f.Interval = d
	return f.Ticks, func() { close(f.Stopped) }
}
